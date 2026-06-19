# MCP Server - Patterns and Conventions

## Project Structure

```
bitrise-mcp-dev-environments/
├── main.go                    # Entry point (config, server modes, logger)
├── go.mod / go.sum            # Go module (separate from backend)
└── internal/
    ├── devenv/            # Shared utilities
    │   ├── call_api.go        # HTTP client (auth, timeouts)
    │   ├── context.go         # Context-based PAT storage
    │   └── tool.go            # Tool struct (Definition + Handler)
    └── tool/                  # MCP tool implementations
        ├── belt.go            # Tool registry
        ├── validate.go        # UUID validation helper
        ├── sessions.go        # Session CRUD + lifecycle
        ├── templates.go       # Template CRUD
        ├── saved_inputs.go     # Saved input/credential CRUD
        ├── images.go          # List images + machine types
        ├── execute.go         # Remote command execution
        ├── screenshot.go      # Session screenshot capture
        ├── gui.go             # Click, type, scroll, drag
        ├── upload.go          # File upload to session
        ├── download.go        # File download from session
        ├── open_remote_access.go # Remote access (SSH/VNC)
        └── me.go              # Current user info
```

## Key Dependencies

- `github.com/mark3labs/mcp-go` — MCP protocol implementation
- `github.com/jinzhu/configor` — Env-based configuration
- `go.uber.org/zap` — Structured logging (stderr only, stdout reserved for MCP stdio)
- `github.com/google/uuid` — UUID validation

## Server Modes

The transport is selected by the `ADDR` env var:

- **stdio** (`ADDR` unset): `BITRISE_TOKEN` required, injected into context via a tool-handler middleware. Used by Claude Code and other local MCP clients. This is the default and the only mode that exposes the local-only file-transfer tools.
- **HTTP** (`ADDR` set, e.g. `0.0.0.0:8000`): for a hosted, multi-tenant deployment. `BITRISE_TOKEN` must be empty. Each request authenticates via a bearer token resolved in `extractPAT` (`auth.go`): an external OAuth JWT is exchanged for a Bitrise PAT (RFC 8693) at `OIDC_TOKEN_ENDPOINT`; a raw PAT is passed through. When `EXTERNAL_OAUTH_ISSUER` is set, the server publishes RFC 9728 protected-resource metadata at `/.well-known/oauth-protected-resource` and `requireAuthMiddleware` (`middleware.go`) returns `401 + WWW-Authenticate` on credential-less POSTs so reactive OAuth clients start the flow.

### Workspace resolution

`WsPath(ctx, path)` reads the workspace from context. Resolution ladder (`belt.GateAndResolveWorkspace`): per-connection default (`BITRISE_WORKSPACE_ID` env in stdio / `x-bitrise-workspace-id` header in HTTP) → auto-detect the sole workspace via the main Bitrise API `GET /organizations` (`devenv.ListOrganizations` — codespaces-api has no list-workspaces endpoint). User-scoped tools (`me`, `list_workspaces`, saved-inputs) skip resolution; classification lives in `belt.go` (`userScoped`), default is workspace-scoped.

### Hosted tool filtering

`localOnly` tools in `belt.go` (`upload`/`download`) read/write the user's local filesystem, so they are hidden (via `server.WithToolFilter`) and rejected on the HTTP transport. `execute` works hosted but loses local SSH-agent forwarding.

## Configuration (env vars)

| Variable | Required | Description |
|---|---|---|
| `ADDR` | No | `host:port` for HTTP transport. Unset → stdio. |
| `BITRISE_TOKEN` | stdio only | PAT or dev token. Required in stdio mode; must be empty in HTTP mode. |
| `BITRISE_WORKSPACE_ID` | Recommended | Default workspace ID (slug). Optional when the user has exactly one workspace (auto-detected). |
| `BITRISE_API_BASE_URL` | No | Backend API base URL (default: `https://codespaces-api.services.bitrise.io`) |
| `BITRISE_MAIN_API_BASE_URL` | No | Main Bitrise API for workspace discovery (default: `https://api.bitrise.io/v0.1`) |
| `EXTERNAL_OAUTH_ISSUER` | No | External OAuth issuer URL. Enables OAuth (HTTP mode); requires the next two. |
| `OIDC_TOKEN_ENDPOINT` | No | RFC 8693 JWT→PAT token-exchange endpoint. |
| `SERVER_BASE_URL` | No | Public base URL of this server (used in metadata + `WWW-Authenticate`). |
| `LOG_LEVEL` | No | `debug`, `info` (default), `warn`, `error` |

## Running Locally

```bash
# stdio (local)
BITRISE_API_BASE_URL=http://localhost:8081 BITRISE_TOKEN=<token> BITRISE_WORKSPACE_ID=<workspace-id> go run .

# HTTP + OAuth (hosted-style)
ADDR=127.0.0.1:8000 \
  EXTERNAL_OAUTH_ISSUER=https://issuer.example.com \
  OIDC_TOKEN_ENDPOINT=<jwt-to-pat-exchange-url> \
  SERVER_BASE_URL=http://127.0.0.1:8000 go run .
```

## Tool Pattern

Every tool is a package-level `var` of type `devenv.Tool` (Definition + Handler):

```go
var ListSessions = devenv.Tool{
    Definition: mcp.NewTool("bitrise_devenv_list",
        mcp.WithDescription("..."),
        mcp.WithString("param_name", mcp.Description("..."), mcp.Required()),
    ),
    Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // 1. Extract & validate params (requireUUID for IDs)
        // 2. Call backend via devenv.CallAPI()
        // 3. Return mcp.NewToolResultText(res) or mcp.NewToolResultErrorFromErr(...)
        return mcp.NewToolResultText(res), nil
    },
}
```

### Conventions

- Tool names: `bitrise_devenv_<action>` (snake_case)
- All tools registered in `belt.go` — add new tools there
- UUID params validated with `requireUUID(request, "param_name")`
- Errors returned as `mcp.NewToolResultErrorFromErr("action name", err)` (not Go errors)
- Optional params: check with `request.GetString("name", "")` or `request.GetArguments()["name"]`
- File transfer tools use `devenv.CallAPILongTimeout()` (10 min timeout vs default 30s)

## API Client (`devenv/`)

- `CallAPI()` — standard 30s timeout, authenticated HTTP to backend
- `CallAPILongTimeout()` — 10 min timeout for file transfers
- Auth injected via context: `ContextWithPAT()` / `AuthFromCtx()`
- `BaseURL` set at startup from `BITRISE_API_BASE_URL`
- Returns raw JSON string responses (passed through to MCP client)

## macOS-Only Tools

The following GUI interaction tools only work on **macOS sessions** (Linux sessions do not have a graphical display):

- `screenshot` — capture session screen
- `click` — mouse click at coordinates
- `type` — keyboard text input
- `scroll` — scroll at cursor position
- `mouse_drag` — drag between two points

All other tools (execute, upload, download, open_remote_access, session/template/saved-input CRUD) work on both macOS and Linux sessions.

### Prefer `execute` over GUI tools when the action is scriptable

For macOS UI automation, **reach for `bitrise_devenv_execute` first**. It's one deterministic call vs. a screenshot + coordinate-estimation + click chain — faster, cheaper, and doesn't miss on coordinates. Use the GUI tools only when no scriptable path exists (e.g. inside a third-party app's custom canvas).

Common scripted entry points:

- **System Settings panes**: `open "x-apple.systempreferences:<pane-id>"` — e.g. `com.apple.Network-Settings.extension`, `com.apple.Displays-Settings.extension`, `com.apple.Wi-Fi-Settings.extension`.
- **Launch/focus an app**: `open -a "Safari"` or `osascript -e 'tell application "Safari" to activate'`.
- **Open URL / file**: `open "https://example.com"`, `open ~/Downloads`.
- **Menu / button / dialog**: `osascript` with System Events (`click menu item ... of menu bar 1`).
- **Keystrokes / shortcuts**: `osascript -e 'tell application "System Events" to keystroke "t" using {command down}'`.
- **State checks**: `defaults read`, or System Events queries for frontmost app / visible windows — often faster than screenshotting and reading pixels.

**osascript timeout safety net**: the common automation scopes (Automation, Accessibility, Screen Recording) are pre-approved on session images, so osascript normally runs without a prompt. An uncommon action could still trigger a TCC permission dialog — and with no human to click "Allow" the command will block until `execute`'s 2-minute cap. Wrap osascript calls in a short `timeout`, e.g. `timeout 15s osascript -e '...'`, so you fail fast and can fall back to the GUI tools.

When adding new docs or tool descriptions that touch the GUI, keep steering clients toward this path.

## Adding a New Tool

1. Create or extend a file in `internal/tool/` (group by domain)
2. Define a `var MyTool = devenv.Tool{...}` with Definition and Handler
3. Register it in `belt.go` under the appropriate section
4. The tool description is critical — it's what the MCP client shows to the LLM

## Logging

Logs go to **stderr** (stdout is reserved for MCP stdio protocol). Use zap SugaredLogger patterns consistent with the backend.
