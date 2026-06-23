# Install Bitrise Dev Environments MCP Server in Windsurf

## Hosted Server (OAuth) — Recommended

The hosted server is the fastest way to get started. It uses OAuth: on your first tool use, Windsurf opens your browser to sign in to Bitrise. There is no token to copy and no Go toolchain to install.

### Prerequisites

- [Windsurf IDE](https://windsurf.com/) installed (latest version with MCP OAuth support)

### Configuration

Add the following to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "serverUrl": "https://mcp-rde.bitrise.io"
    }
  }
}
```

### Authentication

1. Click the hammer icon in Cascade, then **Configure** to open `~/.codeium/windsurf/mcp_config.json`.
2. Add the configuration above and save the file.
3. Click **Refresh** in the MCP toolbar.
4. On the first tool call Windsurf opens your browser to sign in to Bitrise.
5. Once you approve, you're done — the connection is authorized.

### Fallback: PAT-based authentication

For clients or older builds without MCP OAuth support, you can authenticate with a Personal Access Token instead. Use the same configuration plus an `Authorization` header:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "serverUrl": "https://mcp-rde.bitrise.io",
      "headers": {
        "Authorization": "Bearer YOUR_BITRISE_PAT"
      }
    }
  }
}
```

Create a PAT at [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security) and replace `YOUR_BITRISE_PAT` with it.

### Choosing a workspace

Session, template, and machine tools run in a single workspace. The workspace is resolved in this order:

1. A `workspace_id` argument passed on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

### Tool availability

The hosted server can manage and drive sessions: create/list/terminate sessions, run commands, GUI automation, screenshots, and remote-access details all work.

A few tools are **local-only** and are **not** available on the hosted server because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — these read and write your local filesystem.
- `bitrise_devenv_execute` works on the hosted server, but SSH-agent forwarding (using your local SSH keys on the remote session) only applies when running locally.

For those, use the Local setup below.

## Local Server (Go) — full toolset

The local build runs the MCP server on your own machine via Go and includes **every** tool, including file upload/download and local SSH-agent forwarding.

### Prerequisites

1. [Windsurf IDE](https://windsurf.com/) installed (latest version)
2. [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
3. [Go](https://go.dev/) (>=1.25) installed

### Setup

The Bitrise Dev Environments MCP server runs locally via Go.

### Configuration

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
        "BITRISE_TOKEN": "YOUR_BITRISE_PAT",
        "BITRISE_WORKSPACE_ID": "YOUR_WORKSPACE_ID"
      }
    }
  }
}
```

### Installation Steps

#### Manual Configuration

1. Click the hammer icon in Cascade
2. Click **Configure** to open `~/.codeium/windsurf/mcp_config.json`
3. Add the configuration from above
4. Replace `YOUR_BITRISE_PAT` with your actual token
5. Save the file
6. Click **Refresh** in the MCP toolbar

### Configuration Details

- **File path**: `~/.codeium/windsurf/mcp_config.json`
- **Scope**: Global configuration only (no per-project support)
- **Format**: Must be valid JSON (use a linter to verify)

## Verification

After installation:

1. Look for "1 available MCP server" in the MCP toolbar
2. Click the hammer icon to see available Bitrise Dev Environments tools
3. Test with: "List my dev environment sessions"
4. Check for green dot next to the server name

## Troubleshooting

### General Issues

- **OAuth**: re-authenticate via the client's MCP UI (e.g. `/mcp`), or remove and re-add the server
- **Authentication failures**: Verify PAT hasn't expired
- **Invalid JSON**: Validate with [jsonlint.com](https://jsonlint.com)
- **Tools not appearing**: Restart Windsurf completely
- **Go not found**: Ensure Go is installed and in your PATH
- **Check logs**: `~/.codeium/windsurf/logs/`

## Important Notes

- **Windsurf limitations**: No environment variable interpolation, global config only
