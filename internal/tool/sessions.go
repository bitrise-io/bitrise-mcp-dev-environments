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
		mcp.WithDescription(`List devenv sessions. By default (scope="mine") returns the currently authenticated user's own sessions; set scope="workspace" to list sessions owned by the workspace itself instead.

Returns a lightweight view of each session: ID, name, description, status, agent_session_status, labels, owner_type ("user" or "workspace"), owner_id (user UUID or workspace slug), template_id, template_deleted flag, SSH/VNC connection details, AI config, and a template_snapshot containing the template_name, stack_id, and machine_type.

agent_session_status reflects the current state of the AI agent running in the session (working, waiting_for_input, idle, or unspecified). It is reset whenever the session is stopped or started.

Sessions created without a template have an empty template_id and a template_snapshot with stack_id and machine_type but no template_name.

Use label_selectors to filter sessions server-side by their labels. Each selector is a "key=value" exact-match equality; multiple selectors are ANDed, so a session must match all of them. For example, label_selectors=["team=mobile", "branch=main"] returns only sessions carrying both labels, instead of listing everything and filtering client-side.

To get the full template snapshot (session inputs, feature flags, workspace links, working directory, script flags), use bitrise_devenv_get on a specific session.
To check if a session's template has been updated, look at the template_outdated field on bitrise_devenv_get and use bitrise_devenv_compare_template for details.`),
		mcp.WithArray("label_selectors",
			mcp.Description(`Optional label filters of the form "key=value" (exact-match equality on one label). Multiple selectors are ANDed: only sessions matching every selector are returned. At most 8 selectors; duplicate keys are rejected (they can never match under AND); bare keys without "=" are invalid. System-owned "bitrise.io/"-prefixed keys may be used in selectors.`),
			mcp.WithStringItems(),
		),
		mcp.WithString("scope",
			mcp.Description(`Ownership scope of the listing. "mine" (default) returns the calling user's own sessions. "workspace" returns sessions owned by the workspace itself — e.g. device-preview sessions started from workspace preview links — which are visible to every workspace member.`),
			mcp.Enum("mine", "workspace"),
			mcp.DefaultString("mine"),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var repeatedParams map[string][]string
		if selectors := request.GetStringSlice("label_selectors", nil); len(selectors) > 0 {
			repeatedParams = map[string][]string{"label_selectors": selectors}
		}
		params := map[string]string{}
		if request.GetString("scope", "") == "workspace" {
			params["scope"] = "SESSION_LIST_SCOPE_WORKSPACE"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method:         http.MethodGet,
			Path:           devenv.WsPath(ctx, "/sessions"),
			Params:         params,
			RepeatedParams: repeatedParams,
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
- stack_id: stack ID
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
- agent_session_status_updated_at: timestamp when agent_session_status was last changed
- labels: key/value metadata attached to the session (set at creation or via bitrise_devenv_update; filterable in bitrise_devenv_list via label_selectors)

For sessions created without a template, template_id is empty and the snapshot is minimal: only stack_id and machine_type are populated, has_warmup_script/has_startup_script are false, and there is no template_name, session_inputs, feature_flags, or workspace_links. template_outdated is always false for such sessions.

By default, secret session input values are redacted from the snapshot; set include_secrets=true to receive plaintext values.`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier (UUID) of the session"),
			mcp.Required(),
		),
		mcp.WithBoolean("include_secrets",
			mcp.Description("When true, secret session input values are returned in plaintext. Defaults to false (secret values are redacted)."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params := map[string]string{}
		if request.GetBool("include_secrets", false) {
			params["include_secrets"] = "true"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s", sessionID)),
			Params: params,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateSession creates a new session, either from a template or directly from
// a stack + machine type (template-less).
var CreateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create",
		mcp.WithDescription(`Create a new devenv session. There are two ways to create one:

A) From a template (template_id set):
1. List templates with bitrise_devenv_list_templates to find available templates and their session inputs
2. Optionally list saved inputs with bitrise_devenv_list_saved_inputs to find saved credentials
3. Provide values for session inputs (either direct values or references to saved inputs), or set map_saved_to_session_inputs=true to auto-fill session inputs from the user's saved inputs by key match
The session inherits the template's stack, machine type, scripts, feature flags, and workspace links. You may optionally pass stack_id and/or machine_type to override the template's values for this session only.

B) Without a template (template_id omitted):
Supply stack_id and machine_type directly to get a base environment with no warmup/startup scripts and no template configuration (no session inputs, feature flags, or workspace links). Use bitrise_devenv_list_stacks and bitrise_devenv_list_machine_types to discover valid values. This is the quickest way to spin up an environment for a repo when no template is needed.

The session will start provisioning immediately after creation.`),
		mcp.WithString("name",
			mcp.Description("Human-readable name for the session"),
			mcp.Required(),
		),
		mcp.WithString("description",
			mcp.Description("Description of the session"),
		),
		mcp.WithString("template_id",
			mcp.Description("ID of the template to use. Optional: omit to create a session without a template, in which case stack_id and machine_type are required and no warmup/startup scripts run."),
		),
		mcp.WithString("stack_id",
			mcp.Description("Stack ID (e.g. 'osx-xcode-16.0.x-edge'). Required when template_id is omitted. When a template is given, optionally overrides the template's stack for this session. Use bitrise_devenv_list_stacks to find valid IDs."),
		),
		mcp.WithString("machine_type",
			mcp.Description("Machine type name (e.g. 'g2.mac.m2pro.4c'). Required when template_id is omitted. When a template is given, optionally overrides the template's machine type for this session. Use bitrise_devenv_list_machine_types to find valid names."),
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
			mcp.Description("Target cluster name. Required when the chosen stack + machine type are available in multiple clusters — whether they come from the template or, for a template-less session, from the stack_id and machine_type supplied directly. The candidates are the stack's cluster_names (from bitrise_devenv_list_stacks) that also match the machine type's cluster_name (from bitrise_devenv_list_machine_types). Omit when only one cluster matches."),
		),
		mcp.WithString("ai_prompt",
			mcp.Description("Optional AI prompt to pass to Claude Code when the session starts"),
		),
		mcp.WithNumber("auto_terminate_minutes",
			mcp.Description("Minutes before auto-termination. Default: 7200 (5 days). Set to 0 to disable."),
		),
		mcp.WithObject("labels",
			mcp.Description(`Optional key/value string labels to attach to the session, e.g. {"team": "mobile", "branch": "main"}. At most 32 labels; keys are 1-63 characters of [a-zA-Z0-9._/-] starting and ending alphanumeric; values are 1-255 bytes of [a-zA-Z0-9._/:+-] with no positional rules (timestamps with offsets, branch names, paths, and semver all fit; spaces, '@', '=', newlines, and non-ASCII are rejected). The "bitrise.io/" key prefix is reserved for system-owned labels and rejected. Labels are returned on session reads and filterable in bitrise_devenv_list via label_selectors.`),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID := request.GetString("template_id", "")
		stackID := request.GetString("stack_id", "")
		machineType := request.GetString("machine_type", "")

		// Without a template the session is built directly from a stack and
		// machine type, so both must be supplied.
		if templateID == "" && (stackID == "" || machineType == "") {
			return mcp.NewToolResultError("either template_id, or both stack_id and machine_type (to create a session without a template), must be provided"), nil
		}

		body := map[string]any{
			"name": request.GetString("name", ""),
		}
		if templateID != "" {
			body["template_id"] = templateID
		}
		if stackID != "" {
			body["stack_id"] = stackID
		}
		if machineType != "" {
			body["machine_type"] = machineType
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
		if labels, ok := request.GetArguments()["labels"]; ok {
			body["labels"] = labels
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(ctx, "/sessions"),
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

Restorable statuses: SESSION_STATUS_TERMINATED (user terminated), SESSION_STATUS_DRAINED (node was reclaimed under the session), SESSION_STATUS_FAILED. All three are terminal-and-restorable — restoring recreates the VM.

A session in SESSION_STATUS_UNKNOWN (the backend can't currently determine the machine state, e.g. its node lost network connectivity) cannot be restored, terminated or deleted until the state settles — retry shortly.`),
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
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s/restore", sessionID)),
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
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s/terminate", sessionID)),
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
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s", sessionID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete session", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateSession updates a session's name, description, labels, or
// auto-terminate settings.
var UpdateSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update",
		mcp.WithDescription("Update a session's name, description, labels, or auto-terminate settings. Only provided fields are updated."),
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
		mcp.WithObject("labels",
			mcp.Description(`Labels to add or update on the session. Merged into the existing labels: listed keys are overwritten, unlisted keys are left untouched. Same constraints as in bitrise_devenv_create (at most 32 labels total; keys 1-63 chars of [a-zA-Z0-9._/-] starting and ending alphanumeric; values 1-255 bytes of [a-zA-Z0-9._/:+-]; "bitrise.io/" key prefix reserved). To delete keys use remove_labels; if a key appears in both, the removal wins.`),
			mcp.AdditionalProperties(map[string]any{"type": "string"}),
		),
		mcp.WithArray("remove_labels",
			mcp.Description("Label keys to remove from the session. Unknown keys are ignored."),
			mcp.WithStringItems(),
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
		if labels, ok := request.GetArguments()["labels"]; ok {
			body["labels"] = labels
		}
		if removeLabels := request.GetStringSlice("remove_labels", nil); len(removeLabels) > 0 {
			body["remove_labels"] = removeLabels
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s", sessionID)),
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
- template_name, stack_id, machine_type, working_directory
- startup_script, warmup_script (full text)
- feature_flags (name, description, enabled)
- session_inputs (key, description, required, default_value)
- template_variables (key, is_secret — values never exposed)
- changed_variable_keys: list of variable keys whose values differ (computed server-side)

Use this when template_outdated is true on a session to see exactly what changed.
If the current template was deleted, the current field will be null.
Sessions created without a template have nothing to compare against, so the current field is null for them.`),
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
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s/template-diff", sessionID)),
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
		mcp.WithDescription(`Delete all terminated devenv sessions in the given ownership scope. By default (scope="mine") deletes the current user's terminated sessions; set scope="workspace" to delete terminated workspace-owned sessions instead. Returns the number of deleted sessions.`),
		mcp.WithString("scope",
			mcp.Description(`Ownership scope of the cleanup. "mine" (default) deletes the calling user's own terminated sessions. "workspace" deletes terminated sessions owned by the workspace itself — e.g. device-preview sessions started from workspace preview links.`),
			mcp.Enum("mine", "workspace"),
			mcp.DefaultString("mine"),
		),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}
		if request.GetString("scope", "") == "workspace" {
			body["scope"] = "SESSION_LIST_SCOPE_WORKSPACE"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(ctx, "/sessions:delete-terminated"),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete terminated sessions", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
