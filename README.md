# Bitrise Dev Environments MCP Server

MCP Server for Bitrise Dev Environments, enabling AI assistants to create and manage remote development sessions from templates, execute commands, transfer files, and interact with macOS GUIs.

## Features

- **Template-Based Sessions**: Create sessions from templates that define machine images, startup scripts, template variables, and session inputs. Manage templates and saved input credentials.
- **Session Lifecycle**: Create, list, start, stop, update, and delete sessions. Bulk-delete archived sessions.
- **Command Execution**: Run shell commands on running sessions via `bash -c`.
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
| `bitrise_devenv_execute` | Run shell commands on a running session via `bash -c` |
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

- **Bash commands**: Commands run via `bash -c`, supporting pipes, redirects, and command chaining
- **Timeout**: Commands have a 2-minute execution limit
- **No osascript**: Do not use `osascript` on macOS sessions as it triggers permission popups
- **No file transfers via execute**: Use the dedicated upload/download tools instead

### File Transfer

- **Upload**: Local files/folders are compressed to tar.gz, uploaded via signed URL, then extracted on the session
- **Download**: Remote files/folders are archived, downloaded via signed URL, then extracted locally

### Screen Resolution

- **1920x1080**: macOS GUI operations use 1920x1080 screen resolution
- **Coordinate system**: Click and drag coordinates must be in the actual screen coordinate space (1920x1080), not in screenshot image pixel coordinates
