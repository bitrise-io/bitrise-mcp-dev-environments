package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// Execute runs a bash command on a session's machine.
var Execute = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_execute",
		mcp.WithDescription(`Execute a bash command on a running devenv session's machine.

The command is passed to bash -c on the remote machine. Output (stdout + stderr combined) is returned.

IMPORTANT:
- The session must be in "running" status
- Long-running commands may time out (2 minute limit)
- For background processes, redirect output: "nohup ./server &>/dev/null &"
- For large outputs, pipe through head: "find / -name '*.log' | head -100"
- Commands run as the default VM user (usually "vagrant")
- IMPORTANT: Do NOT use osascript commands on macOS sessions as they trigger security permission popups that block execution. Use alternative approaches instead (e.g., swift with CoreGraphics for screen info).`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier of the running session"),
			mcp.Required(),
		),
		mcp.WithString("command",
			mcp.Description("The bash command to execute"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		command, err := request.RequireString("command")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := devenv.CallAPILongTimeout(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/execute", sessionID)),
			Body:   map[string]any{"bash_c_command": command},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("execute command", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
