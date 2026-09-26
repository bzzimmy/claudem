// claudem: a local Anthropic API proxy that reuses Claude Code's OAuth
// subscription credentials.
//
//	ANTHROPIC_BASE_URL=http://127.0.0.1:8787 ANTHROPIC_API_KEY=x <your tool>
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/bzzimmy/claudem/internal/creds"
	"github.com/bzzimmy/claudem/internal/logging"
	"github.com/bzzimmy/claudem/internal/proxy"
	"github.com/bzzimmy/claudem/internal/rewrite"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "address to listen on")
	upstream := flag.String("upstream", "https://api.anthropic.com", "Anthropic API base URL")
	fullBetas := flag.Bool("full-betas", false, "send Claude Code's full anthropic-beta list instead of the minimal one")
	token := flag.String("token", os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"), "static OAuth token (skips keychain/file and refresh)")
	rewrites := flag.String("rewrites", "", "JSON file with extra system-prompt rewrite rules for fingerprinted harnesses")
	verbose := flag.Bool("v", false, "log every request")
	flag.Parse()

	slog.SetDefault(slog.New(logging.New(os.Stderr)))

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

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", *listen)
	if err != nil {
		fatal("listen", err)
	}
	slog.Info("claudem listening", "addr", "http://"+ln.Addr().String(), "upstream", *upstream)
	reportCredentials(mgr)
	slog.Info("rewrite rules loaded", "harnesses", rw.Names())

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 30 * time.Second}
	if err := srv.Serve(ln); err != nil {
		fatal("server", err)
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

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
