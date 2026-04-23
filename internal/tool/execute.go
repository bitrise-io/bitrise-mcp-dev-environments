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

// getSessionResponse mirrors the minimal shape of GetSessionResponse from the
// codespaces backend: the session object is wrapped under a top-level
// "session" key (see proto/codespaces/v1/codespaces.proto). Only the fields
// needed to open an SSH connection are deserialized.
type getSessionResponse struct {
	Session sessionSSHFields `json:"session"`
}

// Field names are camelCase because grpc-gateway's default protojson marshaler
// emits proto3 JSON spec names (lowerCamelCase), not the snake_case proto
// field names. The backend doesn't set UseProtoNames: true.
type sessionSSHFields struct {
	Status            string `json:"status"`
	SSHAddress        string `json:"sshAddress"`
	SSHPassword       string `json:"sshPassword"`
	SSHConnectionOpen bool   `json:"sshConnectionOpen"`
}

// Execute runs a bash command on a session's machine over a direct SSH
// connection from the MCP server to the session VM.
var Execute = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_execute",
		mcp.WithDescription(`Execute a bash command on a running devenv session's machine.

The command runs over a direct SSH connection from the MCP server to the session VM,
inside a forced-interactive login bash shell (bash -i -l -c). This ensures the full
shell environment is available — PATH, brew-installed binaries, git-lfs, language
version managers (nvm, pyenv, rbenv, asdf), and anything the template's warmup
script writes to ~/.bashrc, ~/.bash_profile, ~/.profile, or /etc/profile are all
loaded.

Output is returned as a JSON object with three fields:
- exit_code: the command's exit status (0 = success)
- stdout:    captured stdout as a string
- stderr:    captured stderr as a string

If the MCP server's host has a local SSH agent (SSH_AUTH_SOCK set), the agent is
forwarded into the remote session. This means remote commands that authenticate
over SSH — e.g. "git push", "git clone git@github.com:...", "ssh some-other-host"
— can use the caller's local SSH keys without any per-session credential setup.

NOTE: Because the remote shell is forced-interactive without a TTY, bash emits two
harmless startup diagnostic lines to stderr on every invocation:
  "bash: cannot set terminal process group (-1): Inappropriate ioctl for device"
  "bash: no job control in this shell"
These are not errors from the user's command — ignore them. The exit_code field is
the source of truth for success/failure, not the presence of stderr output.

IMPORTANT:
- The session must be in "running" status with SSH remote access available.
  SSH remote access is provisioned automatically; if credentials aren't populated
  yet, the session is likely still starting up — wait briefly and retry.
- Long-running commands may time out (2 minute limit).
- For background processes, redirect output: "nohup ./server &>/dev/null &"
- For large outputs, pipe through head: "find / -name '*.log' | head -100"
- Commands run as the session user (vagrant on macOS, ubuntu on Linux).`),
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
		var resp getSessionResponse
		if err := json.Unmarshal([]byte(sessionJSON), &resp); err != nil {
			return mcp.NewToolResultErrorFromErr("parse session response", err), nil
		}
		s := resp.Session
		if s.Status != "SESSION_STATUS_RUNNING" {
			return mcp.NewToolResultError(fmt.Sprintf(
				"session is not running (status: %q); start the session before running commands",
				s.Status,
			)), nil
		}
		if !s.SSHConnectionOpen || s.SSHAddress == "" || s.SSHPassword == "" {
			return mcp.NewToolResultError(
				"session SSH is not ready yet (credentials not populated); remote access opens automatically during provisioning — wait a few seconds for the session to finish starting up, then retry",
			), nil
		}

		target, err := parseSSHAddress(s.SSHAddress)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("parse session ssh address", err), nil
		}
		target.Password = s.SSHPassword

		execCtx, cancel := context.WithTimeout(ctx, executeTimeout)
		defer cancel()

		client, err := dialSSH(execCtx, target)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("ssh dial", err), nil
		}
		defer client.Close()

		res, err := client.run(execCtx, command)
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
