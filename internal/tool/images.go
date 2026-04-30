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
		mcp.WithDescription(`List available machine images for devenv templates. Each image has an ID, name, and cluster. Use the name (not ID) when creating or updating templates.

Deprecated: 'osx-tahoe-26-edge' is being deprecated upstream. When creating a new template, prefer 'osx-26-edge' instead. Existing templates and sessions on this image continue to work but should be migrated.

Removed (contact Bitrise support to use): 'osx-tahoe-26', 'osx-sonoma-15', 'osx-sonoma-16', and 'osx-ventura-15' are no longer returned by this tool. If a user asks to create a template or session using one of these images, advise them to contact Bitrise support to have it enabled.`),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath("/images"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list images", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// ResolveClusters finds clusters that offer both a given image and machine type name.
var ResolveClusters = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_resolve_clusters",
		mcp.WithDescription("Find which clusters can run a given image + machine type combination. Use this before creating a session when you need to specify a cluster. If only one cluster is returned, you can omit the cluster parameter when creating a session."),
		mcp.WithString("image", mcp.Description("Image name (e.g. 'osx-xcode-edge')"), mcp.Required()),
		mcp.WithString("machine_type", mcp.Description("Machine type name (e.g. 'g2.mac.m2pro.4c')"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"image":        request.GetString("image", ""),
			"machine_type": request.GetString("machine_type", ""),
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath("/resolve-clusters"),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve clusters", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// ListMachineTypes lists available machine types.
var ListMachineTypes = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_machine_types",
		mcp.WithDescription("List available machine types for devenv templates. Each machine type has an ID, name, and cluster. Use the name (not ID) when creating or updating templates."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath("/machine-types"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list machine types", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
