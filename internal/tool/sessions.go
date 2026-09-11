package tool

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
- device (on sessions created with a device_spec): the virtual device and its readiness. device.state is VM-asserted — PREVIEW_DEVICE_STATE_BOOTING while the VM runs but the device is not proven, PREVIEW_DEVICE_STATE_READY once the device is booted and streaming (start working), PREVIEW_DEVICE_STATE_FAILED with device_notes when this boot gave up. Always read device.state together with the session status: PREVIEW_DEVICE_STATE_UNSPECIFIED with status pending/starting means the VM is not up yet (wait); UNSPECIFIED with a terminal status (terminated, terminating, draining, drained, failed) means the device is gone with the VM — restore the session or create a new one, do not keep polling. device.install_status / install_reason track the optional app install. A human watches and drives the device from the session's page in the RDE web UI ("Open device view" in the Device row), which requires being logged in with access to the session. template_snapshot.service_ports lists the device ports to tunnel: device-web-view is the browser view on both platforms, forwarded to local 3200 (VM side: serve-sim 3200 on iOS, ws-scrcpy 8000 on Android); Android adds adb (VM 5555 → local 15555). Full know-how: the resource bitrise-devenv://guides/device-sessions, or — when your client cannot read MCP resources — the bitrise_devenv_device_guide tool, which returns the same text.

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
		mcp.WithDescription(`Create a new devenv session. There are three ways to create one:

A) From a template (template_id set):
1. List templates with bitrise_devenv_list_templates to find available templates and their session inputs
2. Optionally list saved inputs with bitrise_devenv_list_saved_inputs to find saved credentials
3. Provide values for session inputs (either direct values or references to saved inputs), or set map_saved_to_session_inputs=true to auto-fill session inputs from the user's saved inputs by key match
The session inherits the template's stack, machine type, scripts, feature flags, and workspace links. You may optionally pass stack_id and/or machine_type to override the template's values for this session only.

B) Without a template (template_id omitted):
Supply stack_id and machine_type directly to get a base environment with no warmup/startup scripts and no template configuration (no session inputs, feature flags, or workspace links). Use bitrise_devenv_list_stacks and bitrise_devenv_list_machine_types to discover valid values. This is the quickest way to spin up an environment for a repo when no template is needed.

C) With a virtual device (device_spec set, or a template that declares one):
Boots an iOS simulator (platform "ios", macOS stack) or Android emulator (platform "android", a dockerless Android Linux stack such as ubuntu-resolute-26.04-bitrise-2026-android — the Docker-based linux-docker-* stacks keep the Android SDK inside a container and are rejected) alongside the session and streams it — the same device a Bitrise device preview link would give a PR reviewer, but on YOUR session, ready for adb / xcrun simctl / serve-sim. On a template-less session prefer omitting stack_id and machine_type (both or neither): the deployment's known-good per-platform defaults apply. cluster is never needed with a device — the backend picks one. With a template, the template's stack and machine type are used and must fit the platform. Optionally pass artifact to pre-install an app build. Delete the session when done.
Templates can declare a device themselves (device_spec on bitrise_devenv_get_template). Creating from such a template: omit device_spec to boot the template's device exactly as declared; pass device_spec to override it — with the same platform (or platform omitted) the two merge per field, so empty/zero request fields inherit the template's values and only what you set changes; with the other platform your device_spec replaces the template's wholesale; pass no_device=true to skip the device entirely (no_device and device_spec together are rejected; no_device is ignored when the template declares no device).
IMPORTANT: "running" is not "device ready". Poll bitrise_devenv_get until session.device.state is PREVIEW_DEVICE_STATE_READY (and install_status is PREVIEW_INSTALL_STATUS_OK if you passed an artifact) before touching the device, and READ THE GUIDE first — it covers connecting, the accessibility tree, input, screenshots, letting a human watch, and what never to do: if your client can read MCP resources, read bitrise-devenv://guides/device-sessions (then .../ios or .../android for the platform you boot); if it cannot, call bitrise_devenv_device_guide, which returns the same text. A human watches and drives the device from the session's page in the RDE web UI ("Open device view" in the Device row), which requires being logged in with access to the session.

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
			mcp.Description("Target cluster name. Not needed with device_spec (the backend picks one). Otherwise required when the chosen stack + machine type are available in multiple clusters — whether they come from the template or, for a template-less session, from the stack_id and machine_type supplied directly. The candidates are the stack's cluster_names (from bitrise_devenv_list_stacks) that also match the machine type's cluster_name (from bitrise_devenv_list_machine_types). Omit when only one cluster matches."),
		),
		mcp.WithString("ai_prompt",
			mcp.Description("Optional AI prompt to pass to Claude Code when the session starts"),
		),
		mcp.WithNumber("auto_terminate_minutes",
			mcp.Description("Minutes before auto-termination. Default: 7200 (5 days). Set to 0 to disable."),
		),
		mcp.WithObject("device_spec",
			deviceSpecSchema(`Optional virtual device to boot with the session (see C above). `+deviceSpecFieldsDoc+` If you also pass stack_id/machine_type they must fit the platform (OS family, >= 4 vCPU / 6-8 GB) or the request is rejected with the reason — prefer omitting them. With a template that declares its own device_spec, this is an override: same platform (or platform omitted) merges per field, the other platform replaces the template's device wholesale.`)...,
		),
		mcp.WithBoolean("no_device",
			mcp.Description("Create the session WITHOUT the device its template declares (see C above). Only meaningful with a template that has a device_spec; ignored otherwise. Cannot be combined with device_spec."),
		),
		mcp.WithObject("artifact",
			mcp.Description(`Optional app build to install on the device once it is READY (requires device_spec). url is an absolute http(s) URL the VM downloads directly (a signed URL is fine; it is never returned) — iOS: a zipped simulator .app, Android: an .apk. app_name / build_number / commit_sha are display metadata (shown in the viewer). Progress: session.device.install_status; a FAILED install (install_reason says why) leaves the device usable — install the app yourself.`),
			mcp.Properties(map[string]any{
				"url":          map[string]any{"type": "string", "description": "absolute http(s) download URL of the app build"},
				"app_name":     map[string]any{"type": "string"},
				"build_number": map[string]any{"type": "string"},
				"commit_sha":   map[string]any{"type": "string"},
			}),
			requiredProperties("url"),
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
		deviceSpec, hasDevice := request.GetArguments()["device_spec"]
		artifact, hasArtifact := request.GetArguments()["artifact"]
		noDevice := request.GetBool("no_device", false)

		// Without a template the session is built directly from a stack and
		// machine type, so both must be supplied — unless a device_spec is
		// set, in which case the backend fills whatever is missing from the
		// deployment's per-platform device defaults.
		if templateID == "" && !hasDevice && (stackID == "" || machineType == "") {
			return mcp.NewToolResultError("either template_id, device_spec, or both stack_id and machine_type (to create a session without a template), must be provided"), nil
		}
		if hasDevice && noDevice {
			return mcp.NewToolResultError("no_device cannot be combined with device_spec — either boot a device or skip it"), nil
		}
		// An artifact needs a device to land on: either a device_spec on the
		// request, or a template-declared device that no_device does not
		// suppress (whether the template really declares one is the
		// backend's call).
		if hasArtifact && !hasDevice && (templateID == "" || noDevice) {
			return mcp.NewToolResultError("artifact requires a device — pass device_spec, or create from a template that declares one without no_device"), nil
		}
		// The nested "required" lists above are advisory to the client; check
		// the two fields the backend cannot default before spending a round
		// trip on a request that is certain to be rejected.
		if hasDevice {
			if err := requireNonEmptyString(deviceSpec, "device_spec", "platform"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		if hasArtifact {
			if err := requireNonEmptyString(artifact, "artifact", "url"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
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
		if hasDevice {
			body["device_spec"] = deviceSpec
		}
		if noDevice {
			body["no_device"] = true
		}
		if hasArtifact {
			body["artifact"] = artifact
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

A session in SESSION_STATUS_UNKNOWN (the backend can't currently determine the machine state, e.g. its node lost network connectivity) cannot be restored, terminated or deleted until the state settles — retry shortly.

Only sessions that were terminated (not deleted) can be restored: bitrise_devenv_delete discards the VM and its disk for good.`),
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
		mcp.WithDescription(`Terminate a running devenv session but KEEP it for a later restore: the VM is stopped, its disk is preserved, and the session stays listed as SESSION_STATUS_TERMINATED until it is restored (bitrise_devenv_restore) or deleted. Resets agent_session_status.

Use this only when the user wants to come back to this exact session later (e.g. to keep uncommitted work or an expensive warm state). A terminated session keeps occupying disk until it is deleted, and forgotten terminated sessions are the main source of waste — so when the session is simply no longer needed, call bitrise_devenv_delete directly instead; it works on running sessions and does NOT require terminating first.

Asynchronous: returns while the session is still SESSION_STATUS_TERMINATING; poll bitrise_devenv_get if you need to observe it reach SESSION_STATUS_TERMINATED.`),
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

// DeleteSession permanently deletes a session in any state (RDE-54): a
// running VM is stopped and discarded by the backend, no terminate needed.
var DeleteSession = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete",
		mcp.WithDescription(`Permanently delete a devenv session in ANY state — running, starting, terminating, terminated or failed. This is the preferred way to get rid of a session you are done with: it does not have to be terminated first.

The session disappears from the list immediately and cannot be restored. If its VM is still running, the backend stops it and then discards it together with its disk in the background — any unsaved work on the VM is lost, so make sure anything worth keeping (commits, pushes, uploads) is already off the machine.

Prefer this over bitrise_devenv_terminate unless the user explicitly wants to restore the session later. Fails with a precondition error while the machine state is SESSION_STATUS_UNKNOWN — retry shortly.`),
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
		if strings.TrimSpace(res) == "" || strings.TrimSpace(res) == "{}" {
			// The API returns an empty body on success; say what happened so
			// the model doesn't have to guess from a blank result.
			return mcp.NewToolResultText(fmt.Sprintf("Session %s deleted. If its VM was still running it is being stopped and discarded in the background; the session cannot be restored.", sessionID)), nil
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
- device_spec (the virtual device the template boots with its sessions; absent when it declares none)
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
		mcp.WithDescription(`Delete all terminated devenv sessions in the given ownership scope. By default (scope="mine") deletes the current user's terminated sessions; set scope="workspace" to delete terminated workspace-owned sessions instead. Returns the number of deleted sessions. Running sessions are left alone — use bitrise_devenv_delete to delete a specific session regardless of its state.`),
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

// requiredProperties marks the listed keys of an object-typed tool parameter
// as required in its nested JSON schema. mcp.Required() only marks the
// parameter itself as required on the top-level schema; nothing in mcp-go
// sets "required" inside a nested object built with mcp.Properties.
func requiredProperties(names ...string) mcp.PropertyOption {
	return func(schema map[string]any) {
		schema["required"] = names
	}
}

// requireNonEmptyString checks that an object-typed argument carries a
// non-empty string under key, returning a client-facing error that names the
// parameter when it does not.
func requireNonEmptyString(arg any, param, key string) error {
	obj, ok := arg.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object with a %q field", param, key)
	}
	val, ok := obj[key].(string)
	if !ok || strings.TrimSpace(val) == "" {
		return fmt.Errorf("%s.%s is required and must be a non-empty string", param, key)
	}
	return nil
}

// deviceSpecFieldsDoc explains the fields of a device_spec object. It is
// shared by every tool that accepts one so the field semantics are described
// identically on sessions and templates.
const deviceSpecFieldsDoc = `platform is required: "ios" (simulator; macOS stack) or "android" (emulator; Linux stack). Everything else is optional: device_model (simctl device type like "iPhone 16" / emulator device profile like "pixel_7"; empty = platform default), os_version (iOS only: "18.2" or a simctl runtime id; empty = newest installed), system_image (Android only: sdkmanager package like "system-images;android-34;google_apis;x86_64"), ram_mb / cores (Android only: explicit emulator sizing, 0 = host-derived), cold_boot (Android only).`

// deviceSpecSchema returns the property options of a device_spec object
// parameter — the same shape wherever a virtual device is described
// (bitrise_devenv_create, bitrise_devenv_create_template,
// bitrise_devenv_update_template) — under the given tool-specific description.
func deviceSpecSchema(description string) []mcp.PropertyOption {
	return []mcp.PropertyOption{
		mcp.Description(description),
		mcp.Properties(map[string]any{
			"platform":     map[string]any{"type": "string", "enum": []string{"ios", "android"}, "description": `"ios" or "android"`},
			"device_model": map[string]any{"type": "string", "description": "simctl device type (iOS) or emulator device profile (Android); empty = platform default"},
			"os_version":   map[string]any{"type": "string", "description": "iOS only: an iOS version such as \"18.2\" or a simctl runtime id — anything else (and any value on Android) is rejected; empty = newest installed"},
			"system_image": map[string]any{"type": "string", "description": "Android only: sdkmanager system image package; empty = platform default"},
			"ram_mb":       map[string]any{"type": "integer", "description": "Android only: emulator RAM in MB; 0 = host-derived"},
			"cores":        map[string]any{"type": "integer", "description": "Android only: emulator CPU cores; 0 = host-derived"},
			"cold_boot":    map[string]any{"type": "boolean", "description": "Android only: full cold boot every start (no quickboot)"},
		}),
		requiredProperties("platform"),
	}
}
