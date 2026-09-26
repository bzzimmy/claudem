// claudem: a local Anthropic API proxy that reuses Claude Code's OAuth
// subscription credentials.
//
//	ANTHROPIC_BASE_URL=http://127.0.0.1:8787 ANTHROPIC_API_KEY=x <your tool>
//
// Run "claudem service install [flags]" to keep it running in the background.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bzzimmy/claudem/internal/creds"
	"github.com/bzzimmy/claudem/internal/logging"
	"github.com/bzzimmy/claudem/internal/proxy"
	"github.com/bzzimmy/claudem/internal/rewrite"
	"github.com/bzzimmy/claudem/internal/service"
)

// version is set at build time by GoReleaser (-X main.version=...). For
// `go install` builds it falls back to the module version from build info.
var version = "dev"

func main() {
	slog.SetDefault(slog.New(logging.New(os.Stderr)))
	if len(os.Args) > 1 && os.Args[1] == "service" {
		if err := runService(os.Args[2:]); err != nil {
			fatal("service", err)
		}
		return
	}

	showVersion := flag.Bool("version", false, "print version and exit")
	listen := flag.String("listen", "127.0.0.1:8787", "address to listen on")
	upstream := flag.String("upstream", "https://api.anthropic.com", "Anthropic API base URL")
	fullBetas := flag.Bool("full-betas", false, "send Claude Code's full anthropic-beta list instead of the minimal one")
	token := flag.String("token", os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"), "static OAuth token (skips keychain/file and refresh)")
	rewrites := flag.String("rewrites", "", "JSON file with extra system-prompt rewrite rules for fingerprinted harnesses")
	verbose := flag.Bool("v", false, "log every request")
	flag.Parse()

	if *showVersion {
		fmt.Println("claudem", resolveVersion())
		return
	}

	extra, err := loadRewrites(*rewrites)
	if err != nil {
		fatal("load rewrites", err)
	}
	rw := rewrite.New(rewrite.Defaults, extra)
	mgr := creds.NewManager(creds.DefaultStore(), *token)
	h, err := proxy.New(*upstream, mgr, rw, *fullBetas, *verbose)
	if err != nil {
		fatal("configure proxy", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *listen)
	if err != nil {
		fatal("listen", err)
	}
	slog.Info("claudem listening", "version", resolveVersion(), "addr", "http://"+ln.Addr().String(), "upstream", *upstream)
	reportCredentials(mgr)
	slog.Info("rewrite rules loaded", "harnesses", rw.Names())

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		fatal("server", err)
	case <-ctx.Done():
		stop()
		slog.Info("shutting down", "grace", shutdownGrace)
		sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			slog.Warn("shutdown incomplete, closing", "err", err)
			_ = srv.Close()
		}
	}
}

// shutdownGrace bounds how long in-flight (possibly streaming) requests may
// take to finish after SIGINT/SIGTERM before connections are closed.
const shutdownGrace = 30 * time.Second

const serviceUsage = `usage: claudem service <install|uninstall> [claudem flags...]

  install     write a per-user launchd agent (macOS) or systemd user unit (Linux)
              that runs claudem at login with the given flags, and start it
  uninstall   stop the service and remove the unit file

example: claudem service install -listen 127.0.0.1:8787 -v
`

func runService(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, serviceUsage)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		if resolved, rerr := filepath.EvalSymlinks(bin); rerr == nil {
			bin = resolved
		}
		res, err := service.Install(ctx, service.Spec{Binary: bin, Args: args[1:], Home: home})
		if err != nil {
			return err
		}
		slog.Info("service installed and started", "unit", res.Path, "binary", bin, "logs", res.Logs)
		return nil
	case "uninstall":
		res, err := service.Uninstall(ctx, home)
		if err != nil {
			return err
		}
		slog.Info("service removed", "unit", res.Path)
		return nil
	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, serviceUsage)
		return nil
	default:
		fmt.Fprint(os.Stderr, serviceUsage)
		return fmt.Errorf("unknown service command %q", args[0])
	}
}

// reportCredentials checks the credential store once at startup so a missing
// or broken Claude Code login is visible immediately rather than on first use.
func reportCredentials(mgr *creds.Manager) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	info, err := mgr.Info(ctx)
	if err != nil {
		slog.Warn("claude code credentials unavailable", "err", err,
			"hint", "run 'claude' and log in; the proxy retries on every request")
		return
	}
	slog.Info("claude code credentials found",
		"source", info["source"],
		"subscription", info["subscriptionType"],
		"tier", info["rateLimitTier"],
		"expires", info["expiresAt"])
}

func loadRewrites(path string) ([]rewrite.Harness, error) {
	if path == "" {
		return nil, nil
	}
	return rewrite.LoadFile(path)
}

func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
