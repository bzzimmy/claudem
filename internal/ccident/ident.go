// Package ccident holds the Claude Code identity constants the proxy mimics.
// Bump these when Claude Code changes (capture with ANTHROPIC_BASE_URL pointed
// at a logging proxy).
package ccident

import "runtime"

const (
	// SystemPrefix must be the exact, complete first system block on OAuth
	// requests; Sonnet/Opus return 429 "Error" otherwise.
	SystemPrefix = "You are Claude Code, Anthropic's official CLI for Claude."

	Version   = "2.1.283"
	UserAgent = "claude-cli/" + Version + " (external, cli)"

	APIVersion = "2023-06-01"

	OAuthClientID   = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	OAuthRefreshURL = "https://platform.claude.com/v1/oauth/token"
)

// BaseBetas are always injected. Only oauth-2025-04-20 was ever required and
// even that is currently optional; both are harmless.
var BaseBetas = []string{"claude-code-20250219", "oauth-2025-04-20"}

// FullBetas is the list Claude Code 2.1.283 sends on inference requests.
// Opt-in via --full-betas since some flags change semantics (e.g. context-1m).
var FullBetas = []string{
	"claude-code-20250219",
	"oauth-2025-04-20",
	"context-1m-2025-08-07",
	"interleaved-thinking-2025-05-14",
	"redact-thinking-2026-02-12",
	"thinking-token-count-2026-05-13",
	"context-management-2025-06-27",
	"prompt-caching-scope-2026-01-05",
	"mid-conversation-system-2026-04-07",
	"per-turn-control-2026-07-01",
	"mid-conversation-tool-changes-2026-07-01",
	"advisor-tool-2026-03-01",
	"effort-2025-11-24",
	"structured-outputs-2025-12-15",
}

// Headers returns the static identity headers Claude Code sends.
func Headers() map[string]string {
	return map[string]string{
		"User-Agent": UserAgent,
		"X-App":      "cli",
		"Accept":     "application/json",
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
		"X-Stainless-Lang":                          "js",
		"X-Stainless-Runtime":                       "node",
		"X-Stainless-Runtime-Version":               "v26.3.0",
		"X-Stainless-Package-Version":               "0.112.1",
		"X-Stainless-Os":                            stainlessOS(),
		"X-Stainless-Arch":                          stainlessArch(),
		"X-Stainless-Timeout":                       "600",
		"X-Stainless-Retry-Count":                   "0",
	}
}

func stainlessOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

func stainlessArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	default:
		return runtime.GOARCH
	}
}
