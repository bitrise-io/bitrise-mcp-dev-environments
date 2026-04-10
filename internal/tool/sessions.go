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
		mcp.WithDescription(`List all devenv sessions for the currently authenticated user.

Returns a lightweight view of each session: ID, name, description, status, template_id, template_deleted flag, SSH/VNC connection details, AI config, and a template_snapshot containing the template_name, image, and machine_type.

To get the full template snapshot (session inputs, feature flags, workspace links, working directory, script flags), use bitrise_devenv_get on a specific session.
To check if a session's template has been updated, look at the template_outdated field on bitrise_devenv_get and use bitrise_devenv_compare_template for details.`),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath("/sessions"),
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
		mcp.WithDescription(`Get full details of a specific devenv session.

Returns status, SSH/VNC connection details, AI config, and the complete template_snapshot which contains:
- template_name: name of the template at creation time
- image: machine image name
- machine_type: machine type name
- session_inputs: input values (key, value, is_secret, expose_as_env_var) snapshotted at creation
- feature_flags: flag states (name, enabled) snapshotted at creation
- workspace_links: IDE folder links (label, folder_path) filtered by enabled flags
- working_directory: terminal working directory
- has_warmup_script / has_startup_script: whether scripts were configured

Also includes:
- template_deleted: true if the template was deleted after session creation (session still works from its snapshot)
- template_outdated: true if the template has been updated since this session was created (use bitrise_devenv_compare_template to see what changed)`),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s", sessionID)),
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
1. List templates with bitrise_devenv_list_templates to find available templates and their session inputs
2. Optionally list saved inputs with bitrise_devenv_list_saved_inputs to find saved credentials
3. Provide values for session inputs (either direct values or references to saved inputs)

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
		mcp.WithArray("session_inputs",
			mcp.Description("Values for the template's session inputs. Required inputs must have a value (direct or saved_input_id). Optional inputs use their default_value when omitted."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":            map[string]any{"type": "string", "description": "Key name matching a session input on the template"},
					"value":          map[string]any{"type": "string", "description": "Direct value (ignored if saved_input_id is set)"},
					"is_secret":      map[string]any{"type": "boolean", "description": "Whether the value is secret (ignored if saved_input_id is set)"},
					"saved_input_id": map[string]any{"type": "string", "description": "Optional: ID of a saved input to use instead of a direct value"},
				},
				"required": []string{"key"},
			}),
		),
		mcp.WithArray("enabled_feature_flag_names",
			mcp.Description("Names of feature flags to enable for this session"),
			mcp.WithStringItems(),
		),
		mcp.WithString("cluster",
			mcp.Description("Target cluster name. Required when the template's image + machine type are available in multiple clusters. Use bitrise_devenv_resolve_clusters to find available clusters. Omit when only one cluster matches."),
		),
		mcp.WithString("ai_prompt",
			mcp.Description("Optional AI prompt to pass to Claude Code when the session starts"),
		),
		mcp.WithNumber("auto_terminate_minutes",
			mcp.Description("Minutes before auto-termination. Default: 7200 (5 days). Set to 0 to disable."),
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
		if inputs, ok := request.GetArguments()["session_inputs"]; ok {
			body["session_inputs"] = inputs
		}
		if flags, ok := request.GetArguments()["enabled_feature_flag_names"]; ok {
			body["enabled_feature_flag_names"] = flags
		}
		if cluster := request.GetString("cluster", ""); cluster != "" {
			body["cluster"] = cluster
		}
		if aiPrompt := request.GetString("ai_prompt", ""); aiPrompt != "" {
			body["ai_prompt"] = aiPrompt
		}
		if minutes, ok, err := getOptionalInt(request, "auto_terminate_minutes"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if ok {
			body["auto_terminate_minutes"] = minutes
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath("/sessions"),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/start", sessionID)),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/stop", sessionID)),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s", sessionID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateSession updates a session's name, description, or auto-terminate settings.
var UpdateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update",
		mcp.WithDescription("Update a session's name, description, or auto-terminate settings. Only provided fields are updated."),
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
		mcp.WithNumber("auto_terminate_minutes",
			mcp.Description("Update auto-terminate duration in minutes. Resets the deadline to now + minutes. Set to 0 to disable."),
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
		if minutes, ok, err := getOptionalInt(request, "auto_terminate_minutes"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if ok {
			body["auto_terminate_minutes"] = minutes
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s", sessionID)),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CompareSessionTemplate compares a session's template snapshot with the current template.
var CompareSessionTemplate = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_compare_template",
		mcp.WithDescription(`Compare a session's template snapshot with the current template configuration.

Returns both the snapshot (template config at session creation time) and the current template config side-by-side, including:
- template_name, image, machine_type, working_directory
- startup_script, warmup_script (full text)
- feature_flags (name, description, enabled)
- session_inputs (key, description, required, default_value)
- template_variables (key, is_secret — values never exposed)
- changed_variable_keys: list of variable keys whose values differ (computed server-side)

Use this when template_outdated is true on a session to see exactly what changed.
If the current template was deleted, the current field will be null.`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to compare"),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/template-diff", sessionID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("compare session template", err), nil
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
			Path:   devenv.WsPath("/sessions:delete-archived"),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete archived sessions", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
