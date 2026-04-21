package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

const executeTimeout = 2 * time.Minute

// sessionSSHFields is a partial view of the Session JSON returned by
// GET /sessions/{id}. Only the fields needed to open an SSH connection are
// deserialized.
type sessionSSHFields struct {
	Status            string `json:"status"`
	SSHAddress        string `json:"ssh_address"`
	SSHPassword       string `json:"ssh_password"`
	SSHConnectionOpen bool   `json:"ssh_connection_open"`
}

// Execute runs a bash command on a session's machine over a direct SSH
// connection from the MCP server to the session VM.
var Execute = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_execute",
		mcp.WithDescription(`Execute a bash command on a running devenv session's machine.

The command runs over a direct SSH connection from the MCP server to the session VM,
inside a forced-interactive login bash shell (bash -i -l -c). This ensures the full
shell environment is available — PATH, brew-installed binaries, git-lfs, language
version managers (nvm, pyenv, rbenv, asdf), and anything the template's warmup script
writes to ~/.bashrc, ~/.bash_profile, ~/.profile, or /etc/profile are all loaded.

Output is returned as a JSON object with three fields:
- exit_code: the command's exit status (0 = success)
- stdout:    captured stdout as a string
- stderr:    captured stderr as a string

The remote command runs in a forced-interactive login shell so that .bashrc sources
fully. If you see any stderr lines mentioning "cannot set terminal process group" or
"no job control in this shell", these are harmless bash startup diagnostics from the
interactive-without-TTY shell — ignore them and treat the command as having executed
normally. The exit_code field is the source of truth for success/failure, not the
presence of stderr output. (These two lines are normally filtered out before return,
but future bash versions may phrase them differently.)

IMPORTANT:
- The session must be in "running" status and have SSH remote access open.
  If SSH is not open, call bitrise_devenv_open_remote_access first, then retry.
- Long-running commands may time out (2 minute limit).
- For background processes, redirect output: "nohup ./server &>/dev/null &"
- For large outputs, pipe through head: "find / -name '*.log' | head -100"
- Commands run as the session user (vagrant on macOS, ubuntu on Linux).
- IMPORTANT: Do NOT use osascript commands on macOS sessions as they trigger security
  permission popups that block execution. Use alternative approaches instead (e.g.,
  swift with CoreGraphics for screen info).`),
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

		sessionJSON, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s", sessionID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("fetch session for execute", err), nil
		}
		var s sessionSSHFields
		if err := json.Unmarshal([]byte(sessionJSON), &s); err != nil {
			return mcp.NewToolResultErrorFromErr("parse session response", err), nil
		}
		if s.Status != "SESSION_STATUS_RUNNING" {
			return mcp.NewToolResultError(fmt.Sprintf(
				"session is not running (status: %q); start the session before running commands",
				s.Status,
			)), nil
		}
		if !s.SSHConnectionOpen || s.SSHAddress == "" || s.SSHPassword == "" {
			return mcp.NewToolResultError(
				"session does not have SSH remote access open; call bitrise_devenv_open_remote_access first, then retry bitrise_devenv_execute",
			), nil
		}

		target, err := devenv.ParseSSHAddress(s.SSHAddress)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("parse session ssh address", err), nil
		}
		target.Password = s.SSHPassword

		execCtx, cancel := context.WithTimeout(ctx, executeTimeout)
		defer cancel()

		client, err := devenv.Dial(execCtx, target)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("ssh dial", err), nil
		}
		defer client.Close()

		res, err := client.Run(execCtx, command)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("execute command", err), nil
		}

		payload, err := json.Marshal(map[string]any{
			"exit_code": res.ExitCode,
			"stdout":    string(res.Stdout),
			"stderr":    string(res.Stderr),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("marshal execute result", err), nil
		}
		return mcp.NewToolResultText(string(payload)), nil
	},
}
