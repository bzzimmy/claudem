<p align="center">
  <img src="assets/logo.svg" alt="claudem logo" width="130" height="130" />
</p>

<h1 align="center">claudem</h1>

<p align="center">
  A local Anthropic API reverse proxy that automatically reuses your <a href="https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview">Claude Code</a> OAuth credentials.
</p>

By masquerading as the official Claude CLI, `claudem` allows external AI coding tools and agents (like Pi, OpenCode, Cline, or Aider) to use Claude Code's authentication and count against your subscription's plan limits instead of API billing.

## Features

- **Seamless Authentication**: Automatically reads Claude Code's OAuth tokens from the macOS Keychain or your local `.claude` config directory.
- **Token Management**: Handles token refreshes in the background and writes them back, ensuring both the proxy and the official `claude` CLI stay logged in.
- **Identity Spoofing**: Injects the exact HTTP headers, User-Agents, and Beta flags expected from Claude Code.
- **System Prompt Injection**: Automatically prepends the mandatory `"You are Claude Code..."` system block required by Anthropic's OAuth route. Without it, Sonnet and Opus reject requests with a misleading 429 that is not a real rate limit.
- **Fingerprint Evasion**: Transparently rewrites third-party system prompts to bypass Anthropic's harness fingerprinting, preventing your requests from drawing from extra usage credits.

## Installation

**Prebuilt binaries** for macOS, Linux, and Windows (amd64/arm64) are on the [releases page](https://github.com/bzzimmy/claudem/releases). Download the archive for your platform, extract it, and put `claudem` on your `PATH`.

**With Go** 1.27+ installed:

```bash
go install github.com/bzzimmy/claudem@latest
```

**From source:**

```bash
git clone https://github.com/bzzimmy/claudem.git
cd claudem
go build -o claudem .
```

Check your install with `claudem -version`.

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
- **Cline** (extension): Anthropic provider → "Use custom base URL" → `http://127.0.0.1:8787/v1`. **Cline CLI**: `cline auth -p anthropic -k x -m claude-sonnet-4-5`, then add `"baseUrl": "http://127.0.0.1:8787/v1"` to the `anthropic.settings` object in `~/.cline/data/settings/providers.json` (`cline auth --baseurl` rejects Anthropic)
- **Roo / Kilo**: Anthropic provider → "Use custom base URL" → `http://127.0.0.1:8787`
- **GitHub Copilot CLI** (BYOK): `COPILOT_PROVIDER_TYPE=anthropic COPILOT_PROVIDER_BASE_URL=http://127.0.0.1:8787 COPILOT_PROVIDER_API_KEY=x COPILOT_MODEL=claude-sonnet-4-5 copilot`
- **Hermes Agent** (`~/.hermes/config.yaml`): `"providers": {"claudem": {"api": "http://127.0.0.1:8787", "api_key": "x", "transport": "anthropic_messages"}}` then `hermes chat --provider claudem --model claude-sonnet-4-5`

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
-version           print version and exit
```

### Running as a background service

To run claudem continuously, use the built-in service manager. This creates a native user service that starts at login and restarts automatically on a crash. Windows is not supported.

```bash
claudem service install -listen 127.0.0.1:8787 -v
```

Any flags appended to the `install` command are passed directly to the claudem daemon. On macOS, this writes a per-user launchd LaunchAgent to `~/Library/LaunchAgents/com.bzzimmy.claudem.plist` and writes logs to `~/Library/Logs/claudem.log`. A per-user agent is required on macOS so the service can access the login session's Keychain. On Linux, it writes a systemd user unit to `~/.config/systemd/user/claudem.service` and logs can be read via `journalctl --user -u claudem`.

The service configuration uses the absolute path to the binary. If you move or rebuild the binary, you must run the install command again to update the path. Reinstalling overwrites the previous service definition.

```bash
claudem service uninstall
```

This command stops the background process and removes the service files.

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

## How claudem compares

claudem is a local reverse proxy for the native Anthropic Messages API with the smallest possible footprint. It is a single static Go binary with zero third-party dependencies that reads and writes Claude Code's credentials, passes the API through untouched, and injects identity headers and rewrite rules to masquerade as the official client. It deliberately excludes multi-provider support, multi-account load balancing, and protocol translation.

| Project | Approach | Best for |
|---|---|---|
| claudem | Single Go binary, native API passthrough, header/prompt masquerade | Smallest footprint, strict Anthropic API compatibility, user-extensible rewrite rules |
| CLIProxyAPI | Large Go proxy with multi-account routing and format translation | Users needing multiple providers, account load balancing, Docker, or OpenAI-compatible endpoints |
| Node/Python proxies | Header/prompt masquerade via npm or venv scripts | Existing Node or Python environments |
| CLI/Agent-SDK delegates | Shells out to `claude` CLI or Agent SDK, reconstructs streaming | Sanctioned API usage, avoiding direct API masquerade |
| OpenCode plugins | OAuth implementation inside the tool itself | Users only needing Claude access within a single tool |

Choose another tool if you need an OpenAI-compatible endpoint, access to multiple AI providers, Docker containerization, or a management API. If you prefer to use Anthropic's sanctioned approach, use a proxy that delegates to the Claude Agent SDK rather than interacting directly with the API.

## Disclaimer

This project is not affiliated with Anthropic. `claudem` relies on unsupported behavior to bypass fingerprinting, allowing third-party tools to use Claude subscription limits instead of API billing.

This may violate Anthropic's Terms of Service and could affect your account. The tool is provided without warranty, may break at any time, and you are solely responsible for its use. For supported integration, use the Claude Agent SDK or official `claude` CLI.

## License

See the [LICENSE](LICENSE) file for details.
