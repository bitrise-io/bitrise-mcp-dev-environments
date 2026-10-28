# Bitrise Dev Environments MCP Server

MCP Server for Bitrise Dev Environments, enabling AI assistants to create and manage remote development sessions from templates, execute commands, transfer files, and interact with macOS GUIs.

## Features

- **Template-Based or Template-less Sessions**: Create sessions from templates that define a stack, startup scripts, template variables, session inputs, and optionally a virtual device every session boots, or create them without a template by supplying a stack and machine type directly. Manage templates and saved input credentials.
- **Session Lifecycle**: Create, list, start, stop, update, and delete sessions. Bulk-delete terminated sessions.
- **Command Execution**: Run shell commands on running sessions over SSH in a forced-interactive login shell (`bash -i -l -c`), so the template's PATH, brew tools, git-lfs, and language version managers are all visible. Local SSH agent is forwarded so git-over-SSH uses the caller's keys.
- **File Transfer**: Upload local files/folders to sessions and download artifacts back.
- **GUI Automation** (macOS only): Interact with the session's graphical display via screenshots, mouse clicks, keyboard input, scrolling, and drag operations.
- **Remote Access**: Open SSH and VNC connections to running sessions.

## Quickstart (hosted, OAuth)

The fastest way to start — no install, no token. A hosted version runs over HTTP with OAuth; you sign in with your Bitrise account in the browser on first use. In Claude Code:

```bash
claude mcp add --transport http bitrise-dev-environments https://mcp-rde.bitrise.io
```

Run `/mcp` and authenticate. (Add `--scope user` to make it available across projects.) Any client that supports remote MCP servers with OAuth can point at the same URL — see the per-client guides under [Installation](#installation).

> **The hosted server covers most tools, but not all.** File transfer (`bitrise_devenv_upload` / `bitrise_devenv_download`) is **local-only** — it bridges your own machine's filesystem — and `bitrise_devenv_execute` works but without local SSH-agent forwarding. For the **full toolset**, use the local install (the "Local" setup in each guide below).

### Choosing a workspace

Session, template, and machine tools operate within a single workspace, resolved in this order:

1. a `workspace_id` argument on the tool call (e.g. ask the assistant to "use workspace `<id>`");
2. an `x-bitrise-workspace-id` header on the connection — useful for automation when the workspace is known up front: `claude mcp add --transport http --header "x-bitrise-workspace-id: <slug>" bitrise-dev-environments https://mcp-rde.bitrise.io`;
3. auto-detected when you belong to exactly one workspace.

Use `bitrise_devenv_list_workspaces` to discover your workspace IDs.

## Installation

Each guide covers the **hosted (OAuth)** setup first — recommended and fastest — and the **local (Go)** setup for the full toolset:

- **[VS Code](/docs/install-vscode.md)** - VS Code IDE
- **[GitHub Copilot in other IDEs](/docs/install-other-copilot-ides.md)** - JetBrains, Visual Studio, Eclipse, and Xcode with GitHub Copilot
- **[Claude Applications](/docs/install-claude.md)** - Claude Desktop and Claude Code CLI
- **[Cursor](/docs/install-cursor.md)** - Cursor IDE
- **[Windsurf](/docs/install-windsurf.md)** - Windsurf IDE
- **[Gemini CLI](/docs/install-gemini-cli.md)** - Gemini CLI

## Configuration (local install)

| Variable | Required | Description |
|---|---|---|
| `BITRISE_TOKEN` | Yes | Personal access token, Workspace API Token (`bitwat_…`, created with Remote Dev Environments access) or dev token. See [Workspace API Tokens](#workspace-api-tokens) for what changes with one. |
| `BITRISE_WORKSPACE_ID` | Recommended | Bitrise workspace ID (slug) for workspace-scoped API calls. If omitted and you belong to exactly one workspace, it is auto-detected; with multiple workspaces it is required. Required with a Workspace API Token (auto-detection needs a user). |
| `BITRISE_API_BASE_URL` | No | Backend API base URL (default: `https://codespaces-api.services.bitrise.io`) |
| `LOG_LEVEL` | No | `debug`, `info` (default), `warn`, `error` |

### Transports

The server runs over **stdio** (above) for local use. It can also run over **HTTP with OAuth** for a hosted deployment — set `ADDR` (e.g. `0.0.0.0:8000`) to switch transports. In HTTP mode `BITRISE_TOKEN` must not be set; clients authenticate per-request via OAuth (or an `Authorization` bearer header), and the workspace comes from the `x-bitrise-workspace-id` header (or auto-detection). OAuth is enabled by setting `EXTERNAL_OAUTH_ISSUER`, `OIDC_TOKEN_ENDPOINT`, and `SERVER_BASE_URL`. The file-transfer tools (`upload`/`download`) are local-only and are hidden on the hosted server.

## Workspace API Tokens

`BITRISE_TOKEN` (or the HTTP bearer header) may be a **Workspace API Token** instead of a personal access token — the right credential for CI and other unattended callers, since it belongs to the workspace rather than a person. With one:

- every session the token creates is **owned by the workspace** (as are the devices opened from any preview link, whichever token minted it) (`owner` is implied `workspace`; `owner="user"` is rejected), visible to and manageable by every workspace member, and listed with `scope="workspace"` — which is also the token's default and only scope for `bitrise_devenv_list` / `bitrise_devenv_delete_terminated`;
- the token fully manages those sessions (get, update, terminate, restore, delete, execute, screenshots, computer use, file transfer) and reads templates, stacks and machine types, but never sees a member's personal session;
- template session inputs must be given as plain values in `session_inputs` — saved inputs are personal, so `saved_input_id` / `map_saved_to_session_inputs` are rejected, as is `ai_prompt`;
- the user-scoped tools (`bitrise_devenv_me`, `bitrise_devenv_list_workspaces`, the saved-input tools) and template authoring are unavailable, and `BITRISE_WORKSPACE_ID` must be set.

## Available Tools

### User

| Tool | Description |
|------|-------------|
| `bitrise_devenv_me` | Get the currently authenticated Bitrise user information |

### Session Lifecycle

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list` | List sessions with their status, name, labels, owner, and template info; filterable server-side by `key=value` label selectors, and scopeable to your own sessions (default) or workspace-owned sessions |
| `bitrise_devenv_get` | Get details of a specific session including status, machine info, SSH/VNC credentials and, on a session that runs lifecycle hooks, `lifecycle` (the setup phase and every hook run) |
| `bitrise_devenv_create` | Create a new session, either from a template (with template ID, session inputs, and feature flags) or without one by supplying a stack and machine type directly; optionally boot a virtual device with it (`device_spec`: iOS simulator / Android emulator, optional `artifact` to pre-install) and attach key/value labels. A template that declares a device boots it by default — pass `device_spec` to override it or `no_device` to skip it. `owner="workspace"` creates a session owned by the workspace (shared with every member; session inputs as plain values only) — the only kind a Workspace API Token creates. `warm_pool_id` claims a booted session from a warm pool instead (the pool fixes template, flags, machine and device; a claim adds only the personal inputs — the template's On claim and On restore inputs, saved-input mapping and `ai_prompt` — and is yours unless `owner="workspace"`). A typed input value may be marked `is_secret` on a direct create and on a claim |
| `bitrise_devenv_update` | Update a session's name, description, or labels |
| `bitrise_devenv_restore` | Restore a terminated (or failed/drained) session |
| `bitrise_devenv_terminate` | Terminate a running session but keep it for a later restore (stops the VM, preserves its disk; the session stays listed as terminated) |
| `bitrise_devenv_run_hook` | On a session that runs lifecycle hooks, queue one hook (`on_claim`, `on_restore`, or the platform's `personalize` system setup step) to run again after a failure, keeping the machine. On create can't run again on the same machine: use `bitrise_devenv_rebuild` |
| `bitrise_devenv_rebuild` | Replace a session's machine and run its whole lifecycle again from On create with the same creation-time configuration (the old VM and its disk are discarded) |
| `bitrise_devenv_get_logs` | Read a session's setup log: one lifecycle hook's output (`on_create`, `on_claim`, `on_restore`), the platform's `personalize` system setup step or its `system` output, or a legacy session's `warmup` / `startup` script. Returns what the log holds so far (the last 64 KB), and works while On create is still running |
| `bitrise_devenv_delete` | Permanently delete a session in any state — running sessions are stopped and discarded, no terminate needed; preferred over terminate unless you plan to restore |
| `bitrise_devenv_delete_terminated` | Delete all terminated sessions in the chosen scope (your own by default, or workspace-owned) |
| `bitrise_devenv_list_session_notifications` | List notifications for a session (e.g., agent stopped, permission prompt). Supports pagination and polling via timestamp cursors. |

### Templates

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_templates` | List all available templates, including the `device_spec` each one declares (if any) |
| `bitrise_devenv_get_template` | Get template details including scripts or lifecycle `hooks` (`lifecycle_mode`), stack, template variables and session inputs (each with its scope, `hook`: `shared`, `on_create`, `on_claim` or `on_restore`; a variable may be `protected`), feature flags, and the declared `device_spec` |
| `bitrise_devenv_create_template` | Create a new template with stack, machine type, scripts — the legacy `startup_script` / `warmup_script` pair or lifecycle `hooks` (`on_create`, `on_claim`, `on_restore`) — variables and inputs (each in a scope: `hook`), and optionally a `device_spec` that every session created from it boots |
| `bitrise_devenv_update_template` | Update an existing template; `hooks` replaces its lifecycle hooks as a whole, `device_spec` replaces the declared device, `clear_device_spec` removes it |
| `bitrise_devenv_delete_template` | Delete a template |

### Warm Pools

A warm pool is a stored session configuration (template, session input values, feature flags, optional stack / machine type / cluster overrides) plus a `pool_size`, owned by the workspace: every member and Workspace API Token sees, claims from and manages it, and a session claimed from it is the claimant's. The backend keeps that many sessions booted and idle; `bitrise_devenv_create` with `warm_pool_id` is handed one of them instantly (`warm_state: "claimed"`) or, when none is available, gets a session built from the pool's configuration (`"cold"`). `pool_size: 0` drains a pool but keeps it usable as a preset — a scheduler can scale it up for business hours and down at night.

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_warm_pools` | List the warm pools you can see (the workspace's plus your own), optionally for one template; `all: true` lists every pool in the workspace read-only (needs billing-view permission) |
| `bitrise_devenv_get_warm_pool` | Get a warm pool with its live status — ready / warming counts, claimed and cold totals, errors — and its per-session inventory |
| `bitrise_devenv_create_warm_pool` | Create a warm pool from a template with session inputs (plain values; saved inputs are personal and never reach a pool), feature flags, optional stack / machine type / cluster overrides and the device its sessions boot (`device_spec` / `no_device`, as on `bitrise_devenv_create`) |
| `bitrise_devenv_update_warm_pool` | Scale a pool (`pool_size`; 0 drains it), rename it, or change its stored configuration (arrays replace all entries; a redacted secret sent back empty keeps the stored value; `""` clears a machine override, `device_spec: {}` the device override) |
| `bitrise_devenv_delete_warm_pool` | Delete a warm pool; its unclaimed warm sessions are terminated, claimed sessions are untouched |
| `bitrise_devenv_retry_warm_pool` | Resume a warm pool paused after three consecutive On create failures (`setup_failed`): delete the kept failed machine and warm again |

### Saved Inputs

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_saved_inputs` | List all saved inputs (credentials/values) |
| `bitrise_devenv_get_saved_input` | Get details of a specific saved input |
| `bitrise_devenv_create_saved_input` | Create a new saved input (key/value, optionally secret) |
| `bitrise_devenv_update_saved_input` | Update an existing saved input value |
| `bitrise_devenv_delete_saved_input` | Delete a saved input |

### Stacks & Machine Types

| Tool | Description |
|------|-------------|
| `bitrise_devenv_list_stacks` | List available stacks (with title, OS, and status) for templates and sessions |
| `bitrise_devenv_list_machine_types` | List available machine types for templates |

### Workspace Usage

| Tool | Description |
|------|-------------|
| `bitrise_devenv_get_workspace_usage` | Snapshot of active sessions and vCPU/memory totals, split by OS, workspace-wide and per user (requires billing-view permission) |

### Command & File Operations

| Tool | Description |
|------|-------------|
| `bitrise_devenv_execute` | Run shell commands on a running session over SSH (`bash -i -l -c`, full login shell, local SSH agent forwarded). Returns `{exit_code, stdout, stderr}` JSON. |
| `bitrise_devenv_upload` | Upload local files/folders to a session |
| `bitrise_devenv_download` | Download files/folders from a session |

### GUI Interaction (macOS only)

| Tool | Description |
|------|-------------|
| `bitrise_devenv_screenshot` | Capture the session's macOS display (1920x1080 resolution) — the desktop, not the way to look at a device session's simulator/emulator |
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

### Device Preview Links

| Tool | Description |
|------|-------------|
| `bitrise_devenv_create_preview_link` | Turn an app build into a shareable link that opens it on a live iOS simulator / Android emulator in someone else's browser — no Bitrise login needed. For handing a build to a reviewer; to drive a device yourself use `bitrise_devenv_create` with `device_spec`. The devices it opens are always workspace-owned (listed with `scope="workspace"`), whoever mints the link. `warm_pool_id` serves the link's opens from a warm pool whose configuration boots a device (omit `device_spec`, `stack_id` and `machine_type` then) |

### Guides

| Tool | Description |
|------|-------------|
| `bitrise_devenv_device_guide` | Returns a device-session guide (`device-sessions`, `ios` or `android`) as markdown — read `device-sessions` before creating or driving a session with a device; the same content is also served as the [resources](#resources) below |

## Resources

Besides tools, the server exposes read-only **resources** (markdown guides an
agent reads on demand — they cost no context until requested):

| URI | Description |
|-----|-------------|
| `bitrise-devenv://guides/device-sessions` | Device sessions: create a session that boots an iOS simulator / Android emulator (`bitrise_devenv_create` with `device_spec`), wait for `device.state` READY, connect, drive the device (accessibility tree first), let a human watch from the session page in the RDE web UI, do-nots and recovery |
| `bitrise-devenv://guides/device-sessions/ios` | iOS simulator specifics: `xcrun simctl`, serve-sim CLI and `/ax` accessibility endpoint |
| `bitrise-devenv://guides/device-sessions/android` | Android emulator specifics: adb, `uiautomator dump`, input, install |

The guides mirror the RDE backend's device-session documentation (the source of truth) and are synced from it with the backend's `docs/device-sessions/sync-mirrors.sh` — never edited here. The `bitrise_devenv_device_guide` tool returns the same text for clients that cannot read resources.

## Usage Notes

### Sessions & Templates

- **Device sessions**: Pass `device_spec` (`{"platform": "ios"|"android", …}`) to `bitrise_devenv_create` to boot a virtual device with the session — stack/machine type/cluster then default to the platform's known-good pair on a template-less session. A `running` session is **not** a ready device: poll `bitrise_devenv_get` until `device.state` is `PREVIEW_DEVICE_STATE_READY`, and touch nothing on the VM while it is `BOOTING`. Call `bitrise_devenv_device_guide` (or read the `bitrise-devenv://guides/device-sessions` resource) before creating or driving the device
- **Templates can declare a device**: Give a template a `device_spec` (`bitrise_devenv_create_template` / `bitrise_devenv_update_template`; the template's stack and machine type must fit the platform) and every session created from it boots that device with no `device_spec` on the create call. Per session you can still override it — a `device_spec` without a `platform` tweaks it per field (only the fields you set change), one with a `platform` replaces it whole — or skip it with `no_device: true`. `bitrise_devenv_update_template` replaces the declared device as a whole (`device_spec`) or removes it (`clear_device_spec: true`); existing sessions keep the device they were created with

- **Template-based or template-less**: Sessions can be created from a template that defines the stack, startup scripts, template variables, and session inputs, or without a template by supplying a stack and machine type directly (a base environment with no warmup/startup scripts)
- **Session inputs**: When creating a session, provide values for session inputs (either direct values or references to saved inputs for secrets)
- **Warm pools skip the boot**: When the same configuration is created repeatedly, store it once as a warm pool (`bitrise_devenv_create_warm_pool`) and claim from it with `bitrise_devenv_create` `warm_pool_id` — a booted session is handed over instantly when one is ready, and a cold one is built from the pool's configuration otherwise. Pass only the per-session fields (`name`, `description`, `labels`, `auto_terminate_minutes`, `artifact`) with a claim; configuration fields are rejected. Scale with `bitrise_devenv_update_warm_pool` `pool_size` (0 drains the pool but keeps the preset)
- **Done with a session? Delete it**: `bitrise_devenv_delete` works on running sessions too — the VM is stopped and discarded along with its disk. Only use `bitrise_devenv_terminate` when you intend to `bitrise_devenv_restore` the same session later; terminated sessions keep using disk until deleted
- **Lifecycle hooks**: A template can carry `hooks` instead of the legacy scripts — `on_create` runs once when the session is created (on a warm pool: when the pool warms the machine), `on_claim` on every claim, `on_restore` on every restore; a hook is done when its script exits (long-running work is started in the background so the script can exit; an On create that has not exited after 20 minutes is ended by RDE with exit code 124). A session created from such a template reports `lifecycle` on `bitrise_devenv_get`: `running` is **not** "set up" — while On create is pending or running `bitrise_devenv_execute` and every other interactive tool are refused ("Session is setting up: On create is still running. Access opens when it finishes."), so poll until `lifecycle.phase` is `LIFECYCLE_PHASE_READY` (a `*_FAILED` phase means the last hook failed; `lifecycle.runs` has the exit code; On create is re-run only by `bitrise_devenv_rebuild`). Every template variable and session input belongs to a scope (`hook`): `shared` (the default) reaches every hook, `on_create` / `on_claim` / `on_restore` that hook only — a hook gets the shared values plus its own, and On restore has its own inputs rather than the claim values. A direct create needs every required input of every scope, a warm pool is created with the shared and `on_create` inputs, a claim supplies the `on_claim` and `on_restore` ones; the same key may exist on several scopes, so one value (or saved input) fills all of them. A `protected` variable is secret and write-only (never returned, replaced but never viewed). `expose_as_env_var` ("Also in terminal") exists on every value: hooks always receive it, the flag additionally writes it to the session's shell profile when its scope's step runs (later steps override earlier ones; exposed shared and `on_create` values stay on a pool machine for whoever claims it). Every hook is optional: an empty hook runs nothing, so a template with no `on_create` is set up as soon as it boots. Converting a template to hooks (`bitrise_devenv_update_template` with `hooks`) is one-way. Saving hooks requires lifecycle hooks to be available on the workspace
- **Always check first**: Call `bitrise_devenv_list` before creating to reuse existing sessions

### Command Execution

- **Execution path**: Commands run over a direct SSH connection from the MCP server to the session VM, invoked as `bash -i -l -c <cmd>`. Both login (`-l`) and interactive (`-i`) modes are forced so `/etc/profile`, `~/.bash_profile`/`~/.profile`, and `~/.bashrc` are all sourced fully — PATH, brew-installed binaries, git-lfs, and language version managers (nvm, pyenv, rbenv, asdf) are available.
- **Structured output**: Results come back as a JSON object with `exit_code`, `stdout`, and `stderr` fields. `exit_code` is the source of truth for success/failure.
- **Bash startup diagnostics**: Because `-i` is used without a TTY, bash emits two harmless lines to stderr on every invocation (`cannot set terminal process group`, `no job control in this shell`). These are not errors from the user's command and can be ignored.
- **SSH agent forwarding**: If the MCP host has a running local SSH agent (`SSH_AUTH_SOCK` set), it is forwarded into the remote session. Remote commands like `git push git@github.com:...`, `git clone git@...`, and `ssh some-other-host` authenticate with the caller's local keys — no per-session credential setup required.
- **Timeout**: Commands have a 2-minute execution limit.
- **Bash features**: Pipes, redirects, command chaining, and subshells all work as expected.
- **No file transfers via execute**: Use the dedicated upload/download tools instead.
- **DNS-related dial failures**: Session SSH hostnames are dynamic tunnel addresses whose DNS answer depends on which resolver is asked — the local/automatic resolver can return an IP that's unreachable from the caller's network even though a public resolver (1.1.1.1 / 8.8.8.8) would return a reachable one. If the SSH dial fails with either an outright DNS lookup error or a dial timeout, the tool result includes a hint suggesting the caller switch this machine's DNS resolver to a public one — verified against a live session where switching from automatic to 1.1.1.1/8.8.8.8 DNS was the actual fix. Other dial failures (e.g. connection refused) are returned as-is.

### File Transfer

- **Upload**: Local files/folders are compressed to tar.gz, uploaded via signed URL, then extracted on the session
- **Download**: Remote files/folders are archived, downloaded via signed URL, then extracted locally

### Screen Resolution

- **1920x1080**: macOS GUI operations use 1920x1080 screen resolution
- **Coordinate system**: Click and drag coordinates must be in the actual screen coordinate space (1920x1080), not in screenshot image pixel coordinates
