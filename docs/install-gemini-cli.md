# Install Bitrise Dev Environments MCP Server in Google Gemini CLI

The Bitrise Dev Environments MCP server is available two ways:

- **Hosted server (OAuth) — recommended.** Connect to `https://mcp-rde.bitrise.io` over HTTP and sign in to Bitrise in your browser on first tool use. Nothing to install, no token to copy.
- **Local server (Go) — full toolset.** Run the server as a local Go binary. This build includes every tool, including file upload/download and local SSH-agent forwarding.

## Hosted Server (OAuth) — Recommended

The hosted server uses OAuth: the first time a tool runs, your browser opens to sign in to Bitrise. There is no token to copy or store.

There are two ways to connect Gemini CLI to the hosted server.

### Method 1 (Recommended): Gemini extension

Install the extension directly from the repository:

```bash
gemini extensions install https://github.com/bitrise-io/bitrise-mcp-dev-environments
```

The extension handles OAuth automatically on first use (browser sign-in to Bitrise).

### Method 2: Remote server in settings

MCP servers for Gemini CLI are configured in its settings JSON under an `mcpServers` key.

- **Global configuration**: `~/.gemini/settings.json` where `~` is your home directory
- **Project-specific**: `.gemini/settings.json` in your project directory

Add the hosted server using the `httpUrl` key:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "httpUrl": "https://mcp-rde.bitrise.io"
    }
  }
}
```

### Authentication

1. Add the server (install the extension, or add the config block above and restart Gemini CLI).
2. On the first tool use, your browser opens to sign in to Bitrise.
3. Once you approve, you're done — Gemini CLI is connected.

#### Fallback: PAT-based authentication

For clients or older builds without MCP OAuth support, authenticate with a Personal Access Token instead. Add an `Authorization: Bearer` header alongside the server URL:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "httpUrl": "https://mcp-rde.bitrise.io",
      "headers": {
        "Authorization": "Bearer YOUR_BITRISE_PAT"
      }
    }
  }
}
```

Create a PAT at [https://app.bitrise.io/me/account/security](https://app.bitrise.io/me/account/security).

### Choosing a workspace

Session, template, and machine tools run in a single workspace. The workspace is resolved in this order:

1. A `workspace_id` argument on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

### Tool availability

The hosted server can manage and drive sessions: create, list, and terminate sessions, run commands, automate GUIs, capture screenshots, and fetch remote-access details.

A few tools are **local-only** and are **not** available on the hosted server, because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — read and write your local filesystem.
- `bitrise_devenv_execute` works on the hosted server, but **SSH-agent forwarding** (using your local SSH keys on the remote session) only applies when running locally.

For those, use the Local setup below.

## Local Server (Go) — full toolset

This build runs the MCP server as a local Go binary and includes **every** tool, including file upload/download and local SSH-agent forwarding.

### Prerequisites

1. The latest version of Google Gemini CLI installed (see [official Gemini CLI documentation](https://github.com/google-gemini/gemini-cli))
2. [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
3. [Go](https://go.dev/) (>=1.25) installed

<details>
<summary><b>Storing Your PAT Securely</b></summary>
<br>

For security, avoid hardcoding your token. Create or update `~/.gemini/.env` (where `~` is your home or project directory) with your PAT:

```bash
# ~/.gemini/.env
BITRISE_PAT=your_token_here
BITRISE_WORKSPACE_ID=your_workspace_id_here
```

</details>

### Setup

MCP servers for Gemini CLI are configured in its settings JSON under an `mcpServers` key.

- **Global configuration**: `~/.gemini/settings.json` where `~` is your home directory
- **Project-specific**: `.gemini/settings.json` in your project directory

After securely storing your PAT, you can add the Bitrise Dev Environments MCP server configuration to your settings file. You may need to restart the Gemini CLI for changes to take effect.

#### Configuration

Add this to `~/.gemini/settings.json`:

```json
{
    "mcpServers": {
        "bitrise-dev-environments": {
            "command": "go",
            "args": [
                "run",
                "github.com/bitrise-io/bitrise-mcp-dev-environments@latest"
            ],
            "env": {
                "BITRISE_TOKEN": "$BITRISE_PAT",
                "BITRISE_WORKSPACE_ID": "$BITRISE_WORKSPACE_ID"
            }
        }
    }
}
```

## Verification

To verify that the Bitrise Dev Environments MCP server has been configured, start Gemini CLI in your terminal with `gemini`, then:

1. **Check MCP server status**:

    ```
    /mcp list
    ```

    ```
    ℹ Configured MCP servers:

    🟢 bitrise-dev-environments - Ready (30 tools)
        - bitrise_devenv_me
        - bitrise_devenv_list
        - bitrise_devenv_create
        - bitrise_devenv_execute
        - bitrise_devenv_upload
        - bitrise_devenv_download
        ...
    ```

2. **Test with a prompt**
    ```
    List my dev environment sessions
    ```

## Troubleshooting

### Authentication Issues

- **OAuth**: re-authenticate via the client's MCP UI (e.g. `/mcp`), or remove and re-add the server.
- **Token expired**: Generate a new Bitrise token

### Configuration Issues

- **Invalid JSON**: Validate your configuration:
    ```bash
    cat ~/.gemini/settings.json | jq .
    ```
- **MCP connection issues**: Check logs for connection errors:
    ```bash
    gemini --debug "test command"
    ```
- **Go not found**: Ensure Go is installed and in your PATH (Local setup only)
