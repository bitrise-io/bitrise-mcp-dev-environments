package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListTemplates lists all available templates.
var ListTemplates = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_templates",
		mcp.WithDescription("List all available devenv templates. Templates define the machine image, scripts, and required inputs for creating sessions."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   "/v1/templates",
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list templates", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// GetTemplate retrieves a template by ID.
var GetTemplate = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_template",
		mcp.WithDescription("Get details of a specific template including startup/warmup scripts, machine image, required inputs, shared inputs, and feature flags."),
		mcp.WithString("template_id",
			mcp.Description("The unique identifier (UUID) of the template"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   fmt.Sprintf("/v1/templates/%s", templateID),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get template", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateTemplate creates a new template.
var CreateTemplate = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create_template",
		mcp.WithDescription(`Create a new devenv template. Use bitrise_devenv_list_images and bitrise_devenv_list_machine_types to find valid image and machine_type values. IMPORTANT: The image and machine_type must be UUIDs (not names), and they must belong to the same cluster.`),
		mcp.WithString("name", mcp.Description("Template name"), mcp.Required()),
		mcp.WithString("description", mcp.Description("Template description")),
		mcp.WithString("startup_script", mcp.Description("Bash script that runs every time a session starts"), mcp.Required()),
		mcp.WithString("warmup_script", mcp.Description("Bash script that runs once during initial session creation")),
		mcp.WithString("image", mcp.Description("Machine image UUID (use bitrise_devenv_list_images to find the ID). Must be from the same cluster as machine_type."), mcp.Required()),
		mcp.WithString("machine_type", mcp.Description("Machine type UUID (use bitrise_devenv_list_machine_types to find the ID). Must be from the same cluster as image."), mcp.Required()),
		mcp.WithString("working_directory", mcp.Description("Working directory for terminal sessions (absolute path)")),
		mcp.WithObject("shared_inputs", mcp.Description(`JSON array: [{"key": "...", "value": "...", "is_secret": true}]`)),
		mcp.WithObject("required_user_inputs", mcp.Description(`JSON array: [{"key": "...", "description": "..."}]`)),
		mcp.WithObject("feature_flags", mcp.Description(`JSON array: [{"name": "..."}]`)),
		mcp.WithObject("workspace_links", mcp.Description(`JSON array: [{"label": "...", "folder_path": "...", "feature_flag_name": "..."}]`)),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"name":           request.GetString("name", ""),
			"startup_script": request.GetString("startup_script", ""),
			"image":          request.GetString("image", ""),
			"machine_type":   request.GetString("machine_type", ""),
		}
		for _, key := range []string{"description", "warmup_script", "working_directory"} {
			if v := request.GetString(key, ""); v != "" {
				body[key] = v
			}
		}
		for _, key := range []string{"shared_inputs", "required_user_inputs", "feature_flags", "workspace_links"} {
			if v, ok := request.GetArguments()[key]; ok {
				body[key] = v
			}
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   "/v1/templates",
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("create template", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateTemplate updates an existing template.
var UpdateTemplate = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_template",
		mcp.WithDescription("Update an existing devenv template. Only provided fields are updated. For array fields (shared_inputs, required_user_inputs, feature_flags, workspace_links), set the corresponding update_* flag to true."),
		mcp.WithString("template_id", mcp.Description("The unique identifier of the template to update"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Updated template name")),
		mcp.WithString("description", mcp.Description("Updated description")),
		mcp.WithString("startup_script", mcp.Description("Updated startup script")),
		mcp.WithString("warmup_script", mcp.Description("Updated warmup script")),
		mcp.WithString("image", mcp.Description("Updated machine image UUID (use bitrise_devenv_list_images to find the ID). Must be from the same cluster as machine_type.")),
		mcp.WithString("machine_type", mcp.Description("Updated machine type UUID (use bitrise_devenv_list_machine_types to find the ID). Must be from the same cluster as image.")),
		mcp.WithString("working_directory", mcp.Description("Updated working directory")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{}
		for _, key := range []string{"name", "description", "startup_script", "warmup_script", "image", "machine_type", "working_directory"} {
			if v := request.GetString(key, ""); v != "" {
				body[key] = v
			}
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   fmt.Sprintf("/v1/templates/%s", templateID),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update template", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteTemplate soft-deletes a template.
var DeleteTemplate = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_template",
		mcp.WithDescription("Delete a devenv template. Existing sessions based on this template are not affected."),
		mcp.WithString("template_id", mcp.Description("The unique identifier of the template to delete"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/v1/templates/%s", templateID),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete template", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
