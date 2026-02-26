package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListUserInputs lists all saved user inputs.
var ListUserInputs = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_user_inputs",
		mcp.WithDescription("List all saved user inputs (credentials/values) for the current user. User inputs are mapped to session template requirements when creating sessions."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   "/v1/user-inputs",
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list user inputs", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// GetUserInput retrieves a single user input.
var GetUserInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_user_input",
		mcp.WithDescription("Get details of a specific user input."),
		mcp.WithString("user_input_id", mcp.Description("The unique identifier of the user input"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "user_input_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   fmt.Sprintf("/v1/user-inputs/%s", id),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get user input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateUserInput creates a new user input.
var CreateUserInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create_user_input",
		mcp.WithDescription("Create a new saved user input (credential/value). The key should match a template's required user input key for automatic mapping."),
		mcp.WithString("key", mcp.Description("Key/name of the input (used as env var name)"), mcp.Required()),
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
			Path:   "/v1/user-inputs",
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("create user input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateUserInput updates an existing user input.
var UpdateUserInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_user_input",
		mcp.WithDescription("Update an existing user input value."),
		mcp.WithString("user_input_id", mcp.Description("The unique identifier of the user input to update"), mcp.Required()),
		mcp.WithString("value", mcp.Description("Updated value"), mcp.Required()),
		mcp.WithBoolean("is_secret", mcp.Description("Updated secret flag")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "user_input_id")
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
			Path:   fmt.Sprintf("/v1/user-inputs/%s", id),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update user input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteUserInput deletes a user input.
var DeleteUserInput = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_user_input",
		mcp.WithDescription("Delete a saved user input. Sessions using this input are not affected."),
		mcp.WithString("user_input_id", mcp.Description("The unique identifier of the user input to delete"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, err := requireUUID(request, "user_input_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/v1/user-inputs/%s", id),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete user input", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
