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

## Adding a New Tool

1. Create or extend a file in `internal/tool/` (group by domain)
2. Define a `var MyTool = devenv.Tool{...}` with Definition and Handler
3. Register it in `belt.go` under the appropriate section
4. The tool description is critical — it's what the MCP client shows to the LLM

## Logging

Logs go to **stderr** (stdout is reserved for MCP stdio protocol). Use zap SugaredLogger patterns consistent with the backend.
