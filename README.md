# Bitrise Dev Environments MCP Server

MCP Server for Bitrise Dev Environments, enabling AI assistants to create and manage remote development sessions from templates, execute commands, transfer files, and interact with macOS GUIs.

## Features

- **Template-Based Sessions**: Create sessions from templates that define machine images, startup scripts, template variables, and session inputs. Manage templates and saved input credentials.
- **Session Lifecycle**: Create, list, start, stop, update, and delete sessions. Bulk-delete archived sessions.
- **Command Execution**: Run shell commands on running sessions over SSH in a forced-interactive login shell (`bash -i -l -c`), so the template's PATH, brew tools, git-lfs, and language version managers are all visible. Local SSH agent is forwarded so git-over-SSH uses the caller's keys.
- **File Transfer**: Upload local files/folders to sessions and download artifacts back.
- **GUI Automation** (macOS only): Interact with the session's graphical display via screenshots, mouse clicks, keyboard input, scrolling, and drag operations.
- **Remote Access**: Open SSH and VNC connections to running sessions.

## Installation

- **[VS Code](/docs/install-vscode.md)** - Installation for VS Code IDE
- **[GitHub Copilot in other IDEs](/docs/install-other-copilot-ides.md)** - Installation for JetBrains, Visual Studio, Eclipse, and Xcode with GitHub Copilot
- **[Claude Applications](/docs/install-claude.md)** - Installation guide for Claude Desktop and Claude Code CLI
- **[Cursor](/docs/install-cursor.md)** - Installation guide for Cursor IDE
- **[Windsurf](/docs/install-windsurf.md)** - Installation guide for Windsurf IDE
- **[Gemini CLI](/docs/install-gemini-cli.md)** - Installation guide for Gemini CLI

## Configuration

| Variable | Required | Description |
|---|---|---|
| `BITRISE_TOKEN` | Yes | Personal access token or dev token |
| `BITRISE_WORKSPACE_ID` | Yes | Bitrise workspace ID (slug) for workspace-scoped API calls |
| `BITRISE_API_BASE_URL` | No | Backend API base URL (default: `https://codespaces-api.services.bitrise.io`) |
| `LOG_LEVEL` | No | `debug`, `info` (default), `warn`, `error` |

## Available Tools

### User

| Tool | Description |
|------|-------------|
| `bitrise_devenv_me` | Get the currently authenticated Bitrise user information |

### Session Lifecycle

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list` | List all sessions with their status, name, and template info |
| `bitrise_devenv_get` | Get details of a specific session including status, machine info, and SSH/VNC credentials |
| `bitrise_devenv_create` | Create a new session from a template (with name, template ID, session inputs, and feature flags) |
| `bitrise_devenv_update` | Update a session's name or description |
| `bitrise_devenv_start` | Start a stopped (archived) session |
| `bitrise_devenv_stop` | Stop a running session (archives it for later restart) |
| `bitrise_devenv_delete` | Permanently delete a session |
| `bitrise_devenv_delete_archived` | Delete all archived (stopped) sessions |
| `bitrise_devenv_list_session_notifications` | List notifications for a session (e.g., agent stopped, permission prompt). Supports pagination and polling via timestamp cursors. |

### Templates

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_templates` | List all available templates |
| `bitrise_devenv_get_template` | Get template details including scripts, image, template variables, session inputs, and feature flags |
| `bitrise_devenv_create_template` | Create a new template with image, machine type, scripts, and inputs |
| `bitrise_devenv_update_template` | Update an existing template |
| `bitrise_devenv_delete_template` | Delete a template |

### Saved Inputs

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_saved_inputs` | List all saved inputs (credentials/values) |
| `bitrise_devenv_get_saved_input` | Get details of a specific saved input |
| `bitrise_devenv_create_saved_input` | Create a new saved input (key/value, optionally secret) |
| `bitrise_devenv_update_saved_input` | Update an existing saved input value |
| `bitrise_devenv_delete_saved_input` | Delete a saved input |

### Images & Machine Types

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_images` | List available machine images for templates |
| `bitrise_devenv_list_machine_types` | List available machine types for templates |

### Command & File Operations

| Tool | Description |
|------|-------------|
| `bitrise_devenv_execute` | Run shell commands on a running session over SSH (`bash -i -l -c`, full login shell, local SSH agent forwarded). Returns `{exit_code, stdout, stderr}` JSON. |
| `bitrise_devenv_upload` | Upload local files/folders to a session |
| `bitrise_devenv_download` | Download files/folders from a session |

### GUI Interaction (macOS only)

| Tool | Description |
|------|-------------|
| `bitrise_devenv_screenshot` | Capture the session's macOS display (1920x1080 resolution) |
| `bitrise_devenv_click` | Click at coordinates on the display (left/right/middle, single/double) |
| `bitrise_devenv_mouse_drag` | Drag the mouse between two points |
| `bitrise_devenv_type` | Type text as keyboard input |
| `bitrise_devenv_scroll` | Scroll up/down at the current mouse position |

> **Prefer `bitrise_devenv_execute` over GUI tools when the action is scriptable.** Opening a System Settings pane, launching an app, navigating a menu, or checking frontmost window state is one deterministic `execute` call — `open "x-apple.systempreferences:<pane-id>"`, `open -a <app>`, `osascript ...`, or `defaults read/write` — versus a screenshot + coordinate-estimation + click chain. Fall back to the GUI tools only when no scriptable path exists (e.g. a third-party app's custom canvas).
>
> **osascript timeout safety net**: common automations (Automation / Accessibility / Screen Recording) are pre-approved on session images, so osascript normally runs without a prompt. But an uncommon action could still trigger a TCC permission dialog, and with no human to click "Allow" the command will hang until the 2-minute execute cap. Wrap osascript calls in a short `timeout`, e.g. `timeout 15s osascript -e '...'`, so you fail fast and can fall back to GUI tools.

### Remote Access

| Tool | Description |
|------|-------------|
| `bitrise_devenv_open_remote_access` | Open SSH/VNC remote access tunnel and get connection details |

## Usage Notes

### Sessions & Templates

- **Template-based**: Sessions are always created from a template that defines the machine image, startup scripts, template variables, and session inputs
- **Session inputs**: When creating a session, provide values for session inputs (either direct values or references to saved inputs for secrets)
- **Stopped sessions**: Stopped (archived) sessions can be restarted later
- **Always check first**: Call `bitrise_devenv_list` before creating to reuse existing sessions

### Command Execution

- **Execution path**: Commands run over a direct SSH connection from the MCP server to the session VM, invoked as `bash -i -l -c <cmd>`. Both login (`-l`) and interactive (`-i`) modes are forced so `/etc/profile`, `~/.bash_profile`/`~/.profile`, and `~/.bashrc` are all sourced fully — PATH, brew-installed binaries, git-lfs, and language version managers (nvm, pyenv, rbenv, asdf) are available.
- **Structured output**: Results come back as a JSON object with `exit_code`, `stdout`, and `stderr` fields. `exit_code` is the source of truth for success/failure.
- **Bash startup diagnostics**: Because `-i` is used without a TTY, bash emits two harmless lines to stderr on every invocation (`cannot set terminal process group`, `no job control in this shell`). These are not errors from the user's command and can be ignored.
- **SSH agent forwarding**: If the MCP host has a running local SSH agent (`SSH_AUTH_SOCK` set), it is forwarded into the remote session. Remote commands like `git push git@github.com:...`, `git clone git@...`, and `ssh some-other-host` authenticate with the caller's local keys — no per-session credential setup required.
- **Timeout**: Commands have a 2-minute execution limit.
- **Bash features**: Pipes, redirects, command chaining, and subshells all work as expected.
- **No file transfers via execute**: Use the dedicated upload/download tools instead.

### File Transfer

- **Upload**: Local files/folders are compressed to tar.gz, uploaded via signed URL, then extracted on the session
- **Download**: Remote files/folders are archived, downloaded via signed URL, then extracted locally

### Screen Resolution

- **1920x1080**: macOS GUI operations use 1920x1080 screen resolution
- **Coordinate system**: Click and drag coordinates must be in the actual screen coordinate space (1920x1080), not in screenshot image pixel coordinates
