package tool

import (
	"context"
	"encoding/json"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListWorkspaces lists the workspaces (organizations) the user can access.
var ListWorkspaces = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_workspaces",
		mcp.WithDescription(`List the Bitrise workspaces (organizations) the authenticated user can access. Each workspace has a slug (ID) and a name.

Use this to discover workspace IDs. Session, template, image, and machine-type tools all operate within a single workspace, resolved in this order:
1. the BITRISE_WORKSPACE_ID env var (local stdio) or the x-bitrise-workspace-id request header (hosted server), if set;
2. otherwise, if the user has exactly one workspace, it is used automatically.

If the user has multiple workspaces and none is configured, those tools return an error listing the available workspaces — configure one of these IDs as the default.`),
		mcp.WithReadOnlyHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		orgs, err := devenv.ListOrganizations(ctx)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list workspaces", err), nil
		}
		payload, err := json.Marshal(orgs)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("marshal workspaces", err), nil
		}
		return mcp.NewToolResultText(string(payload)), nil
	},
}
