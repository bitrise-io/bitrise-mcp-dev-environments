# Install Bitrise Dev Environments MCP Server in VS Code

## Hosted Server (OAuth) — Recommended

The hosted server runs at `https://mcp-rde.bitrise.io` and uses OAuth: there's no token to copy. The first time a tool runs, VS Code opens your browser to sign in to Bitrise, and you're done.

Follow [VS Code | Add an MCP server](https://code.visualstudio.com/docs/copilot/customization/mcp-servers#_add-an-mcp-server) and add the following configuration to your settings:

```json
{
  "servers": {
    "bitrise-dev-environments": {
      "type": "http",
      "url": "https://mcp-rde.bitrise.io"
    }
  }
}
```

### Authentication

1. Add the server using the configuration above.
2. On first tool use, VS Code automatically handles OAuth and opens your browser to sign in to Bitrise.
3. Done — once you've signed in, the tools are ready to use.

### Fallback: PAT-based authentication

For clients or older builds without MCP OAuth support, you can authenticate with a Bitrise Personal Access Token instead. Add an `Authorization: Bearer` header using VS Code's inputs pattern, so the token is prompted for and stored securely rather than written into the config file:

```json
{
  "servers": {
    "bitrise-dev-environments": {
      "type": "http",
      "url": "https://mcp-rde.bitrise.io",
      "headers": {
        "Authorization": "Bearer ${input:bitrise-token}"
      }
    }
  },
  "inputs": [
    {
      "id": "bitrise-token",
      "type": "promptString",
      "description": "Bitrise Personal Access Token",
      "password": true
    }
  ]
}
```

Create a PAT at [Bitrise Account Settings → Security](https://app.bitrise.io/me/account/security).

### Choosing a workspace

Session, template, and machine tools run in the context of a single workspace. The workspace is resolved in this order:

1. A `workspace_id` argument passed on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

### Tool availability

The hosted server can manage and drive sessions: create, list, and terminate sessions, run commands, automate the GUI, capture screenshots, and fetch remote-access details.

A few tools are **local-only** and are **not** available on the hosted server, because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — these read and write your local filesystem.
- `bitrise_devenv_execute` works on the hosted server, but SSH-agent forwarding (using your local SSH keys on the remote session) only applies when running locally.

For those, use the Local setup below.

## Local Server (Go) — full toolset

This build runs the MCP server as a local Go binary over stdio and includes **every** tool, including file upload/download and local SSH-agent forwarding.

### Prerequisites

1. [VS Code](https://code.visualstudio.com/Download) installed
2. [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
3. [Go](https://go.dev/) (>=1.25) installed

### Setup

Follow [VS Code | Add an MCP server](https://code.visualstudio.com/docs/copilot/customization/mcp-servers#_add-an-mcp-server) and add the following configuration to your settings:

```json
{
  "servers": {
    "bitrise-dev-environments": {
      "type": "stdio",
      "command": "go",
      "args": [
        "run",
        "github.com/bitrise-io/bitrise-mcp-dev-environments@latest"
      ],
      "env": {
        "BITRISE_TOKEN": "${input:bitrise-token}",
        "BITRISE_WORKSPACE_ID": "${input:bitrise-workspace-id}"
      }
    }
  },
  "inputs": [
    {
      "id": "bitrise-token",
      "type": "promptString",
      "description": "Bitrise Personal Access Token",
      "password": true
    },
    {
      "id": "bitrise-workspace-id",
      "type": "promptString",
      "description": "Bitrise Workspace ID (slug)"
    }
  ]
}
```

Save the configuration. VS Code will automatically recognize the change and load the tools into Copilot Chat.

## Verification

1. Restart VS Code completely
2. Check for green dot in Settings → Tools & Integrations → MCP Tools
3. In chat/composer, check "Available Tools"
4. Test with: "List my dev environment sessions"

## Troubleshooting

- **MCP not loading**: Restart VS Code completely after configuration
- **Invalid JSON**: Validate that JSON format is correct
- **Tools not appearing**: Check server shows green dot in MCP settings
- **OAuth**: Re-authenticate via the client's MCP UI (e.g. `/mcp`), or remove and re-add the server
- **Go not found** (local setup): Ensure Go is installed and in your PATH
