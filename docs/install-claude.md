# Install Bitrise Dev Environments MCP Server in Claude Applications

## Claude Code CLI

### Hosted Server (OAuth) — Recommended

The hosted server runs at `https://mcp-rde.bitrise.io` and authenticates with OAuth: on first tool use, Claude Code opens your browser to sign in to Bitrise. There is no token to copy.

```bash
claude mcp add --transport http bitrise-dev-environments https://mcp-rde.bitrise.io
```

Add `--scope user` to make it available across projects.

**Authenticate:**

1. Add the server with the command above.
2. On first tool use, Claude Code opens your browser to sign in to Bitrise; approve the consent screen.
3. Done — you're connected.

<details>
<summary><b>Fallback: PAT-based authentication</b></summary>
<br>

For clients or older builds without MCP OAuth support, pass your [Personal Access Token](https://app.bitrise.io/me/account/security) as a header:

```bash
claude mcp add --transport http bitrise-dev-environments https://mcp-rde.bitrise.io --header "Authorization: Bearer YOUR_BITRISE_PAT"
```

To pin a workspace for automation, also pass `--header "x-bitrise-workspace-id: YOUR_WORKSPACE_ID"`.

</details>

#### Choosing a workspace

Session, template, and machine tools run in one workspace. The workspace is resolved in this order:

1. A `workspace_id` argument on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

#### Tool availability

The hosted server can manage and drive sessions: create/list/terminate, run commands, GUI automation, screenshots, and remote-access details. A few tools are **local-only** and are **not** available on the hosted server because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — read/write your local filesystem.
- `bitrise_devenv_execute` works on the hosted server, but SSH-agent forwarding (using your local SSH keys on the remote session) only applies when running locally.

For those, use the Local setup below.

### Verification

```bash
claude mcp list
claude mcp get bitrise-dev-environments
```

### Local Server (Go) — full toolset

This build runs the MCP server as a local Go binary over stdio and includes **every** tool — file upload/download and local SSH-agent forwarding included.

#### Prerequisites

- Claude Code CLI installed
- [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
- [Go](https://go.dev/) (>=1.25) installed
- Open Claude Code inside the directory for your project (recommended for best experience and clear scope of configuration)

<details>
<summary><b>Storing Your PAT Securely</b></summary>
<br>

For security, avoid hardcoding your token. One common approach:

1. Store your token in `.env` file
```
BITRISE_PAT=your_token_here
BITRISE_WORKSPACE_ID=your_workspace_id_here
```

2. Add to .gitignore
```bash
echo -e ".env\n.mcp.json" >> .gitignore
```

</details>

#### Setup

1. Run the following command in the Claude Code CLI:
```bash
claude mcp add bitrise-dev-environments -e BITRISE_TOKEN=YOUR_BITRISE_PAT -e BITRISE_WORKSPACE_ID=YOUR_WORKSPACE_ID -- go run github.com/bitrise-io/bitrise-mcp-dev-environments@latest
```

With an environment variable:
```bash
claude mcp add bitrise-dev-environments -e BITRISE_TOKEN=$(grep BITRISE_PAT .env | cut -d '=' -f2) -e BITRISE_WORKSPACE_ID=$(grep BITRISE_WORKSPACE_ID .env | cut -d '=' -f2) -- go run github.com/bitrise-io/bitrise-mcp-dev-environments@latest
```

2. Restart Claude Code
3. Run `claude mcp list` to see if the Bitrise Dev Environments server is configured

## Claude Desktop

### Hosted Server (OAuth) — Recommended

The hosted server runs at `https://mcp-rde.bitrise.io` and authenticates with OAuth: on first tool use, sign in to Bitrise in your browser. There is no token to copy. Claude Desktop connects through the `mcp-remote` adapter; recent versions also support MCP OAuth natively.

Add this codeblock to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "command": "npx",
      "args": ["mcp-remote", "https://mcp-rde.bitrise.io"]
    }
  }
}
```

**Authenticate:**

1. Add the server with the config above and restart Claude Desktop.
2. On first tool use, a browser opens to sign in to Bitrise; approve the consent screen.
3. Done — you're connected.

<details>
<summary><b>Fallback: PAT-based authentication</b></summary>
<br>

For clients or older builds without MCP OAuth support, pass your [Personal Access Token](https://app.bitrise.io/me/account/security) as a header by adding it to the `args` array:

```json
{
  "mcpServers": {
    "bitrise-dev-environments": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "https://mcp-rde.bitrise.io",
        "--header",
        "Authorization: Bearer YOUR_BITRISE_PAT"
      ]
    }
  }
}
```

To pin a workspace for automation, also add `"--header", "x-bitrise-workspace-id: YOUR_WORKSPACE_ID"` to the args array.

</details>

#### Choosing a workspace

Session, template, and machine tools run in one workspace. The workspace is resolved in this order:

1. A `workspace_id` argument on the tool call.
2. An `x-bitrise-workspace-id` header on the connection (good for automation).
3. Auto-detected when you belong to a single workspace.

Use the `bitrise_devenv_list_workspaces` tool to find workspace IDs.

#### Tool availability

The hosted server can manage and drive sessions: create/list/terminate, run commands, GUI automation, screenshots, and remote-access details. A few tools are **local-only** and are **not** available on the hosted server because they bridge your own machine:

- `bitrise_devenv_upload` and `bitrise_devenv_download` — read/write your local filesystem.
- `bitrise_devenv_execute` works on the hosted server, but SSH-agent forwarding (using your local SSH keys on the remote session) only applies when running locally.

For those, use the Local setup below.

### Local Server (Go) — full toolset

This build runs the MCP server as a local Go binary over stdio and includes **every** tool — file upload/download and local SSH-agent forwarding included.

#### Prerequisites

- Claude Desktop installed (latest version)
- [Create a Bitrise API Token](https://devcenter.bitrise.io/api/authentication):
   - Go to your [Bitrise Account Settings/Security](https://app.bitrise.io/me/account/security).
   - Navigate to the "Personal access tokens" section.
   - Copy the generated token.
- [Go](https://go.dev/) (>=1.25) installed

#### Configuration File Location

- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux**: `~/.config/Claude/claude_desktop_config.json`

#### Setup

Add this codeblock to your `claude_desktop_config.json`:

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
        "BITRISE_WORKSPACE_ID": "YOUR_WORKSPACE_ID",
        "PATH": "PATH to bin directory of go:PATH to directory of git",
        "GOPATH": "your GOPATH",
        "GOCACHE": "your GOCACHE"
      }
    }
  }
}
```

#### Manual Setup Steps

1. Open Claude Desktop
2. Go to Settings → Developer → Edit Config
3. Paste the code block above in your configuration file
4. If you're navigating to the configuration file outside of the app:
   - **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
   - **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
5. Open the file in a text editor
6. Paste the code block above
7. Replace `YOUR_BITRISE_PAT` with your actual token
8. Save the file
9. Restart Claude Desktop

## Troubleshooting

**Authentication Failed:**
- Check token hasn't expired
- OAuth: re-authenticate via the client's MCP UI (e.g. `/mcp`), or remove and re-add the server.

**Server Not Starting / Tools Not Showing:**
- Run `claude mcp list` to view currently configured MCP servers
- Validate JSON syntax
- If using an environment variable to store your PAT, make sure you're properly sourcing your PAT using the environment variable
- Restart Claude Code and check `/mcp` command
- Delete the server by running `claude mcp remove bitrise-dev-environments` and repeating the setup process
- Make sure you're running Claude Code within the project you're currently working on to ensure the MCP configuration is properly scoped to your project
- Check logs:
  - Claude Code: Use `/mcp` command
  - Claude Desktop: `ls ~/Library/Logs/Claude/` and `cat ~/Library/Logs/Claude/mcp-server-*.log` (macOS) or `%APPDATA%\Claude\logs\` (Windows)

## Important Notes

- Configuration scopes for Claude Code:
  - `-s user`: Available across all projects
  - `-s project`: Shared via `.mcp.json` file
  - Default: `local` (current project only)
