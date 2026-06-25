package tool

import (
	"context"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListStacks lists available development environment stacks.
var ListStacks = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_stacks",
		mcp.WithDescription(`List available stacks for devenv templates and sessions.

Each stack describes a provisionable development environment:
- id: stable stack identifier (e.g. 'osx-xcode-16.0.x-edge'). This is the value to store and to pass as stack_id when creating a session or template.
- title: human-friendly label (e.g. 'Xcode 16.0'). Show this to the user; fall back to id when title is empty.
- description / description_link: summary and a link to the stack's pre-installed tools / system report.
- os: 'macos' or 'linux'.
- os_version: numeric OS version (e.g. 26 for macOS, 24 for Ubuntu 24.04).
- status: 'edge', 'stable', or 'frozen'.
- xcode_version: Xcode version (e.g. '16.0'); empty for non-Xcode stacks. Informational.
- is_default: when true, this is the deployment's default stack — preselect it when the user has expressed no preference.
- cluster_names: the clusters where the stack can be provisioned. A machine type is compatible with the stack when its cluster_name is one of these (see bitrise_devenv_list_machine_types).`),
		mcp.WithReadOnlyHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/stacks"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list stacks", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// ListMachineTypes lists available machine types.
var ListMachineTypes = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_machine_types",
		mcp.WithDescription(`List available machine types for devenv templates and sessions.

Each machine type includes a name (use the name, not the ID, when creating or updating templates), a friendly title, cpu/ram specs, the os it runs, and the cluster_name it belongs to.

To pick a machine type compatible with a stack, choose one whose cluster_name is in that stack's cluster_names (from bitrise_devenv_list_stacks).`),
		mcp.WithReadOnlyHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/machine-types"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list machine types", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
