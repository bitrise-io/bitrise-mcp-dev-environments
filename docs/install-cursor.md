# Install Bitrise Dev Environments MCP Server in Cursor

## Hosted Server (OAuth) — Recommended

The hosted server at `https://mcp-rde.bitrise.io` is the fastest way to get started. It uses OAuth: the first time you use a tool, Cursor opens your browser to sign in to Bitrise — there is no token to copy or paste.

### Configuration

Add this to your global MCP configuration file at `~/.cursor/mcp.json` (or `.cursor/mcp.json` in your project root):

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "url": "https://mcp-rde.bitrise.io"
    }
  }
}
```

### Authentication

1. Add the server using the configuration above.
2. On first tool use Cursor opens your browser to sign in to Bitrise.
3. Done — Cursor remembers the authorization for future tool calls.

### Fallback: PAT-based authentication

For clients or older builds without MCP OAuth support, you can authenticate with a Personal Access Token instead. Use the same `url` and add a sibling `headers` object with an `Authorization: Bearer` header:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "url": "https://mcp-rde.bitrise.io",
      "headers": {
        "Authorization": "Bearer YOUR_BITRISE_PAT"
      }
    }
  }
}
```

Create a Personal Access Token at [https://app.bitrise.io/me/account/security](https://app.bitrise.io/me/account/security), then replace `YOUR_BITRISE_PAT` with it.

### Choosing a workspace

Session, template, and machine tools run in a single workspace. The workspace is resolved in this order:

1. A `workspace_id` argument passed on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

### Tool availability

The hosted server can manage and drive sessions: create/list/terminate, run commands, GUI automation, screenshots, and remote-access details. A few tools are **local-only** and are **not** available on the hosted server because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — these read and write your local filesystem.
- `bitrise_devenv_execute` works hosted, but **SSH-agent forwarding** (using your local SSH keys on the remote session) only applies when running locally.

For those capabilities, use the Local setup below.

## Local Server (Go) — full toolset

The Bitrise Dev Environments MCP server can also run locally via Go. This build includes **every** tool, including file upload/download and local SSH-agent forwarding.

### Prerequisites

1. [Cursor](https://cursor.com/download) IDE installed (latest version)
2. [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
3. [Go](https://go.dev/) (>=1.25) installed

### Install Steps

1. Go directly to your global MCP configuration file at `~/.cursor/mcp.json` and enter the code block below
2. In Tools & Integrations > MCP tools, click the pencil icon next to "bitrise-dev-environments"
3. Replace `YOUR_BITRISE_PAT` with your actual [Bitrise Personal Access Token](https://devcenter.bitrise.io/api/authentication)
4. Save the file
5. Restart Cursor

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

### Configuration Files

- **Global (all projects)**: `~/.cursor/mcp.json`
- **Project-specific**: `.cursor/mcp.json` in project root

## Verification

1. Restart Cursor completely
2. Check for green dot in Settings → Tools & Integrations → MCP Tools
3. In chat/composer, check "Available Tools"
4. Test with: "List my dev environment sessions"

## Troubleshooting

### General Issues

- **MCP not loading**: Restart Cursor completely after configuration
- **Invalid JSON**: Validate that JSON format is correct
- **Tools not appearing**: Check server shows green dot in MCP settings
- **Go not found** (local setup): Ensure Go is installed and in your PATH
- **OAuth**: Re-authenticate via Cursor's MCP UI, or remove and re-add the server
- **Check logs**: Look for MCP-related errors in Cursor logs

## Important Notes

- **Cursor specifics**: Supports both project and global configurations, uses `mcpServers` key with a `url` key for the hosted server
