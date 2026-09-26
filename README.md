# claudem

`claudem` is a local Anthropic API reverse proxy that automatically reuses your [Claude Code](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview) OAuth credentials. 

By masquerading as the official Claude CLI, `claudem` allows external AI coding tools and agents (like Pi, OpenCode, Cline, or Aider) to use Claude Code's authentication and count against your subscription's plan limits instead of API billing.

## Features

- **Seamless Authentication**: Automatically reads Claude Code's OAuth tokens from the macOS Keychain or your local `.claude` config directory.
- **Token Management**: Handles token refreshes in the background and writes them back, ensuring both the proxy and the official `claude` CLI stay logged in.
- **Identity Spoofing**: Injects the exact HTTP headers, User-Agents, and Beta flags expected from Claude Code.
- **System Prompt Injection**: Automatically prepends the mandatory `"You are Claude Code..."` system block required by Anthropic's OAuth route. Without it, Sonnet and Opus reject requests with a misleading 429 that is not a real rate limit.
- **Fingerprint Evasion**: Transparently rewrites third-party system prompts to bypass Anthropic's harness fingerprinting, preventing your requests from drawing from extra usage credits.

## Installation

Ensure you have [Go](https://go.dev/) 1.27+ installed, then clone and build the project:

```bash
git clone https://github.com/bzzimmy/claudem.git
cd claudem
go build -o claudem .
```

## Usage

1. Make sure you are logged into Claude Code (run `claude` once and sign in).
2. Start the `claudem` proxy:

```bash
./claudem
```

3. Point your AI coding tool to the local proxy by overriding the Anthropic base URL. For example:

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 ANTHROPIC_API_KEY=x your_tool
```
*(Note: The API key is ignored by the proxy since it uses OAuth, but most tools require a dummy value like `x` to start).*

For tools with their own provider configuration, override the base URL on the **built-in Anthropic provider** rather than adding a custom one, so you keep the tool's model catalog and metadata:

- **Pi** (`~/.pi/agent/models.json`): `"providers": {"anthropic": {"baseUrl": "http://127.0.0.1:8787", "apiKey": "x"}}`
- **OpenCode** (`opencode.json`): `"provider": {"anthropic": {"options": {"baseURL": "http://127.0.0.1:8787/v1", "apiKey": "x"}}}`
- **Cline / Roo / Kilo**: Anthropic provider → "Use custom base URL" → `http://127.0.0.1:8787`

The proxy also serves `GET /healthz` (credential status) and `GET /usage` (your 5-hour and 7-day utilization).

### Configuration

You can configure `claudem` via command-line flags:

```
-listen string     address to listen on (default "127.0.0.1:8787")
-upstream string   Anthropic API base URL (default "https://api.anthropic.com")
-full-betas        send Claude Code's full anthropic-beta list instead of the minimal one
-rewrites string   JSON file with extra system-prompt rewrite rules for fingerprinted harnesses
-token string      static OAuth token (skips keychain/file and refresh); defaults to $CLAUDE_CODE_OAUTH_TOKEN
-v                 log every request
```

## Fingerprint Evasion & Custom Rewrites

Anthropic attempts to identify third-party tools by fingerprinting their boilerplate system prompts (checking for exact known phrases). If detected, it may reject unmetered usage and return a 400 error indicating: `"Third-party apps now draw from your extra usage"`.

`claudem` includes built-in rewrite rules to slightly alter boilerplate for tools like `pi` and `opencode` (e.g., changing `"pi itself"` to `"the cli itself"`), breaking the fingerprint while preserving meaning.

If you are using a different tool that gets fingerprinted, `claudem` will warn you in the console. You can provide your own rewrite rules to bypass it via the `-rewrites` flag. Create a JSON file (e.g., `rewrites.json`):

```json
[
  {
    "name": "my-custom-tool",
    "rules": [
      {
        "from": "exact phrase that triggers the gate",
        "to": "slightly modified phrase"
      }
    ]
  }
]
```

Then run `claudem -rewrites rewrites.json`.

## License

See the [LICENSE](LICENSE) file for details.
