package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListSavedInputs lists all saved inputs for the current user.
var ListSavedInputs = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_saved_inputs",
		mcp.WithDescription("List all saved inputs (credentials/values) for the current user. Saved inputs can be referenced when creating sessions to provide values for template session inputs."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   "/v1/saved-inputs",
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list saved inputs", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// GetSavedInput retrieves a single saved input.
var GetSavedInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_saved_input",
		mcp.WithDescription("Get details of a specific saved input."),
		mcp.WithString("saved_input_id", mcp.Description("The unique identifier of the saved input"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "saved_input_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   fmt.Sprintf("/v1/saved-inputs/%s", id),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get saved input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateSavedInput creates a new saved input.
var CreateSavedInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create_saved_input",
		mcp.WithDescription("Create a new saved input (credential/value). The key should match a template's session input key for automatic pre-fill when creating sessions."),
		mcp.WithString("key", mcp.Description("Key/name of the input"), mcp.Required()),
		mcp.WithString("value", mcp.Description("Value of the input"), mcp.Required()),
		mcp.WithBoolean("is_secret", mcp.Description("Whether this is a secret value (will be encrypted at rest)")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"key":   request.GetString("key", ""),
			"value": request.GetString("value", ""),
		}
		if isSecret, ok := request.GetArguments()["is_secret"]; ok {
			body["is_secret"] = isSecret
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   "/v1/saved-inputs",
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("create saved input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateSavedInput updates an existing saved input.
var UpdateSavedInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_saved_input",
		mcp.WithDescription("Update an existing saved input value."),
		mcp.WithString("saved_input_id", mcp.Description("The unique identifier of the saved input to update"), mcp.Required()),
		mcp.WithString("value", mcp.Description("Updated value"), mcp.Required()),
		mcp.WithBoolean("is_secret", mcp.Description("Updated secret flag")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "saved_input_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{
			"value": request.GetString("value", ""),
		}
		if isSecret, ok := request.GetArguments()["is_secret"]; ok {
			body["is_secret"] = isSecret
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   fmt.Sprintf("/v1/saved-inputs/%s", id),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update saved input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteSavedInput deletes a saved input.
var DeleteSavedInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_saved_input",
		mcp.WithDescription("Delete a saved input. Sessions that used this input are not affected (values are snapshotted at creation time)."),
		mcp.WithString("saved_input_id", mcp.Description("The unique identifier of the saved input to delete"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "saved_input_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/v1/saved-inputs/%s", id),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete saved input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
