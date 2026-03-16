package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// ListSessions lists all sessions for the current user.
var ListSessions = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list",
		mcp.WithDescription("List all devenv sessions for the currently authenticated user. Returns session IDs, names, statuses, and template info."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   "/v1/sessions",
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list sessions", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// GetSession retrieves a single session by ID.
var GetSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get",
		mcp.WithDescription("Get details of a specific devenv session including status, machine info, SSH/VNC credentials, and feature flags."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   fmt.Sprintf("/v1/sessions/%s", sessionID),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateSession creates a new session from a template.
var CreateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create",
		mcp.WithDescription(`Create a new devenv session from a template.

Before creating a session:
1. List templates with bitrise_devenv_list_templates to find available templates
2. List user inputs with bitrise_devenv_list_user_inputs to find saved credentials
3. Map required template inputs to user inputs via input_mappings

The session will start provisioning immediately after creation.`),
		mcp.WithString("name",
			mcp.Description("Human-readable name for the session"),
			mcp.Required(),
		),
		mcp.WithString("description",
			mcp.Description("Description of the session"),
		),
		mcp.WithString("template_id",
			mcp.Description("ID of the template to use"),
			mcp.Required(),
		),
		mcp.WithObject("input_mappings",
			mcp.Description(`JSON array of input mappings: [{"required_user_input_id": "...", "user_input_id": "..."}]`),
		),
		mcp.WithObject("enabled_feature_flag_ids",
			mcp.Description("JSON array of feature flag IDs to enable"),
		),
		mcp.WithString("ai_prompt",
			mcp.Description("Optional AI prompt to pass to Claude Code when the session starts"),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"name":        request.GetString("name", ""),
			"template_id": request.GetString("template_id", ""),
		}
		if desc := request.GetString("description", ""); desc != "" {
			body["description"] = desc
		}
		if mappings, ok := request.GetArguments()["input_mappings"]; ok {
			body["input_mappings"] = mappings
		}
		if flags, ok := request.GetArguments()["enabled_feature_flag_ids"]; ok {
			body["enabled_feature_flag_ids"] = flags
		}
		if aiPrompt := request.GetString("ai_prompt", ""); aiPrompt != "" {
			body["ai_prompt"] = aiPrompt
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   "/v1/sessions",
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("create session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// StartSession starts a stopped/archived session.
var StartSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_start",
		mcp.WithDescription("Start a stopped (archived) devenv session. The session will begin provisioning and transition to running."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to start"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   fmt.Sprintf("/v1/sessions/%s/start", sessionID),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("start session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// StopSession stops a running session (archives it).
var StopSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_stop",
		mcp.WithDescription("Stop a running devenv session. The session will be archived and can be started again later."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to stop"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   fmt.Sprintf("/v1/sessions/%s/stop", sessionID),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("stop session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteSession permanently deletes a session.
var DeleteSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete",
		mcp.WithDescription("Permanently delete a devenv session. This cannot be undone."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to delete"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   fmt.Sprintf("/v1/sessions/%s", sessionID),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateSession updates a session's name or description.
var UpdateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update",
		mcp.WithDescription("Update a session's name or description. Only provided fields are updated."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to update"),
			mcp.Required(),
		),
		mcp.WithString("name",
			mcp.Description("Updated session name"),
		),
		mcp.WithString("description",
			mcp.Description("Updated session description"),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{}
		if _, ok := request.GetArguments()["name"]; ok {
			body["name"] = request.GetString("name", "")
		}
		if _, ok := request.GetArguments()["description"]; ok {
			body["description"] = request.GetString("description", "")
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   fmt.Sprintf("/v1/sessions/%s", sessionID),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteArchivedSessions deletes all archived sessions.
var DeleteArchivedSessions = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_archived",
		mcp.WithDescription("Delete all archived (stopped) devenv sessions for the current user. Returns the number of deleted sessions."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   "/v1/sessions:delete-archived",
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete archived sessions", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
