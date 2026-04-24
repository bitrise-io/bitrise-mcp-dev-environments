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

## Server Mode

The server runs in **stdio mode**: `BITRISE_TOKEN` required, injected into context via middleware. Used by Claude Code and other MCP clients.

## Configuration (env vars)

| Variable | Required | Description |
|---|---|---|
| `BITRISE_TOKEN` | Yes | PAT or dev token |
| `BITRISE_WORKSPACE_ID` | Yes | Bitrise workspace ID (slug) for workspace-scoped API calls |
| `BITRISE_API_BASE_URL` | No | Backend API base URL (default: `https://codespaces-api.services.bitrise.io`) |
| `LOG_LEVEL` | No | `debug`, `info` (default), `warn`, `error` |

## Running Locally

```bash
BITRISE_API_BASE_URL=http://localhost:8081 BITRISE_TOKEN=<token> BITRISE_WORKSPACE_ID=<workspace-id> go run .
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
