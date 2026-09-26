// claudem: a local Anthropic API proxy that reuses Claude Code's OAuth
// subscription credentials.
//
//	ANTHROPIC_BASE_URL=http://127.0.0.1:8787 ANTHROPIC_API_KEY=x <your tool>
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bzzimmy/claudem/internal/creds"
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

	extra, err := loadRewrites(*rewrites)
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}
	mgr := creds.NewManager(creds.DefaultStore(), *token)
	h, err := proxy.New(*upstream, mgr, rewrite.New(rewrite.Defaults, extra), *fullBetas, *verbose)
	if err != nil {
		slog.Error("startup", "err", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 30 * time.Second,
	}
	slog.Info("claudem listening", "addr", "http://"+*listen, "upstream", *upstream)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func loadRewrites(path string) ([]rewrite.Harness, error) {
	if path == "" {
		return nil, nil
	}
	return rewrite.LoadFile(path)
}
