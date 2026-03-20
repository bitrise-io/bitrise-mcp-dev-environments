package tool

import (
	"context"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListImages lists available machine images.
var ListImages = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_images",
		mcp.WithDescription("List available machine images for devenv templates. Each image has an ID, name, and cluster."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath("/v1/images"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list images", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// ListMachineTypes lists available machine types.
var ListMachineTypes = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_machine_types",
		mcp.WithDescription("List available machine types for devenv templates. Each machine type has an ID, name, and cluster."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath("/v1/machine-types"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list machine types", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
