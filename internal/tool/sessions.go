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

Returns a lightweight view of each session: ID, name, description, status, agent_session_status, template_id, template_deleted flag, SSH/VNC connection details, AI config, and a template_snapshot containing the template_name, image, and machine_type.

agent_session_status reflects the current state of the AI agent running in the session (working, waiting_for_input, idle, or unspecified). It is reset whenever the session is stopped or started.

To get the full template snapshot (session inputs, feature flags, workspace links, working directory, script flags), use bitrise_devenv_get on a specific session.
To check if a session's template has been updated, look at the template_outdated field on bitrise_devenv_get and use bitrise_devenv_compare_template for details.`),
		mcp.WithReadOnlyHintAnnotation(true),
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
- template_outdated: true if the template has been updated since this session was created (use bitrise_devenv_compare_template to see what changed)
- agent_session_status: current state of the AI agent running in the session (working, waiting_for_input, idle, or unspecified). Reset on terminate/restore.
- agent_session_status_updated_at: timestamp when agent_session_status was last changed`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session"),
			mcp.Required(),
		),
		mcp.WithReadOnlyHintAnnotation(true),
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
3. Provide values for session inputs (either direct values or references to saved inputs), or set map_saved_to_session_inputs=true to auto-fill session inputs from the user's saved inputs by key match

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
		mcp.WithBoolean("map_saved_to_session_inputs",
			mcp.Description(`When true, the backend fills unreferenced template session inputs from the current user's saved inputs by matching keys, before required-input validation runs.

Use this as a shortcut instead of calling bitrise_devenv_list_saved_inputs and constructing a session_inputs entry for every saved credential that happens to match a template key.

Rules:
- Entries in session_inputs always win; auto-mapping only fills keys not already supplied.
- Required inputs that match neither session_inputs nor any saved input still fail with "missing required input: <key>" — the flag is not a bypass of required-input validation.
- The response includes an auto_mapped_inputs array listing {session_input_key, saved_input_id} for every key that was auto-filled, so you can report back exactly what the flag resolved.`),
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
		if mapSaved, ok := request.GetArguments()["map_saved_to_session_inputs"]; ok {
			body["map_saved_to_session_inputs"] = mapSaved
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

// RestoreSession restores a terminated (or restarts a failed) session.
var RestoreSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_restore",
		mcp.WithDescription(`Restore a devenv session that is not currently running. The session will begin provisioning and transition to running. Resets agent_session_status.

Restorable statuses: SESSION_STATUS_TERMINATED (user terminated), SESSION_STATUS_DRAINED (node was reclaimed under the session), SESSION_STATUS_FAILED. All three are terminal-and-restorable — restoring recreates the VM.`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to restore"),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/restore", sessionID)),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("restore session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// TerminateSession terminates a running session (stops the VM, keeping the
// session for later restore).
var TerminateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_terminate",
		mcp.WithDescription("Terminate a running devenv session. The VM is stopped and the session can be started again later. Resets agent_session_status."),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session to terminate"),
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
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/terminate", sessionID)),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("terminate session", err), nil
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
		mcp.WithDestructiveHintAnnotation(true),
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
		mcp.WithReadOnlyHintAnnotation(true),
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

// DeleteTerminatedSessions deletes all terminated sessions.
var DeleteTerminatedSessions = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_terminated",
		mcp.WithDescription("Delete all terminated devenv sessions for the current user. Returns the number of deleted sessions."),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath("/sessions:delete-terminated"),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete terminated sessions", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
