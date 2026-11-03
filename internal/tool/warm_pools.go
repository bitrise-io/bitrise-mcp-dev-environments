package tool

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// warmPoolConceptDoc explains what a warm pool is. It is shared by the tool
// descriptions so an agent reads the same model wherever a pool is mentioned.
const warmPoolConceptDoc = `A pool (warm pool) is a stored session configuration — a template, its session input values, feature flags and optional stack / machine type / cluster overrides — plus warm_sessions and max_sessions, owned by the workspace. The RDE backend keeps warm_sessions sessions of that configuration booted and idle ("warm sessions", status.ready / status.warming). Creating a session with bitrise_devenv_create warm_pool_id hands out a warm session instantly (Session.warm_state "claimed") or, when none is available, builds one from the pool's configuration (warm_state "cold") — so a pool with warm_sessions 0 still works as a configuration preset. max_sessions caps how many of the pool's handed-out sessions (claimed or cold) may be in use at once; 0 means no limit. A full pool refuses claims that find no warm session, restores and starts with reason POOL_FULL until one of its sessions ends (status.in_use, status.limit_state). Every member and Workspace API Token sees, claims from and manages every pool. A session claimed from a pool is the claimant's (bitrise_devenv_create owner); it is the workspace's only while it waits in the pool.`

// warmPoolSessionInputsSchema is the session_inputs array a pool stores — the
// same shape bitrise_devenv_create takes, since a pool is a stored session
// create request.
func warmPoolSessionInputsSchema(description string) []mcp.PropertyOption {
	return []mcp.PropertyOption{
		mcp.Description(description),
		mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":       map[string]any{"type": "string", "description": "Key name matching a session input on the template"},
				"value":     map[string]any{"type": "string", "description": "The value every warm session is created with. Saved inputs are personal and cannot be referenced from a pool."},
				"is_secret": map[string]any{"type": "boolean", "description": "Mark the typed value secret (stored encrypted, redacted in responses) — the same flag bitrise_devenv_create takes on a direct create and on a claim"},
				"hook":      map[string]any{"type": "string", "enum": valueScopes, "description": "Optional: the scope this value is for (shared or on_create on a pool); omitted, the value applies to every scope that defines the key, and a scoped value wins over an unscoped one for its scope."},
			},
			"required": []string{"key"},
		}),
	}
}

// ListWarmPools lists the warm pools the caller may see.
var ListWarmPools = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_warm_pools",
		mcp.WithTitleAnnotation("List warm pools"),
		mcp.WithDescription(`List warm pools. `+warmPoolConceptDoc+`

WHEN TO USE THIS: before creating a session, to find a pool that already has your configuration booted (then pass its id as warm_pool_id to bitrise_devenv_create); before creating a pool, to avoid a duplicate; or to review what is kept warm — and paid for — in the workspace.

Every member sees every pool. all=true is the read-only cost view the Usage page uses (requires the workspace's view_billing_data permission). Optionally restrict to one template with template_id.

Each pool carries its configuration (secret input values redacted), warm_sessions, max_sessions (0 = no limit) and a status with ready / warming counts, lifetime claimed_total / cold_total (a high cold_total means warm_sessions is too low for the demand), in_use (the pool's handed-out sessions in use right now — what max_sessions caps), limit_state ("no_limit"; "under_max"; "full": claims that find no warm session, restores and starts are refused with reason POOL_FULL; "over_max": the max was lowered below the number in use, nothing is terminated, claims are refused until enough sessions end), refused_7d (claims, restores and starts refused in the last 7 days — a growing number means the max is too low for the demand), last_error, config_error (the stored configuration no longer builds — fix it with bitrise_devenv_update_warm_pool) and paused_until. On a template with lifecycle hooks a pool also carries setup_failed while it is paused after three consecutive On create failures: {session_id, failed_at, exit_code, message} — the pool stopped warming, claims are refused (FAILED_PRECONDITION) and session_id is the last failed machine, kept for inspection with its logs and terminal; fix the template's On create, then bitrise_devenv_retry_warm_pool (any configuration change does the same). The list view omits the per-session inventory; use bitrise_devenv_get_warm_pool for status.sessions.`),
		mcp.WithString("template_id",
			mcp.Description("Optional: only list pools created from this template (UUID)."),
		),
		mcp.WithBoolean("all",
			mcp.Description("When true, use the read-only cost view (requires the view_billing_data permission). Defaults to false."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := optionalUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params := map[string]string{}
		if templateID != "" {
			params["template_id"] = templateID
		}
		if request.GetBool("all", false) {
			params["scope"] = "WARM_POOL_LIST_SCOPE_ALL"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/warm-pools"),
			Params: params,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list warm pools", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// GetWarmPool retrieves a warm pool with its live status and inventory.
var GetWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_warm_pool",
		mcp.WithTitleAnnotation("Get warm pool"),
		mcp.WithDescription(`Get one warm pool with its full live status. `+warmPoolConceptDoc+`

WHEN TO USE THIS: to check whether a pool has a session ready before claiming from it (status.ready > 0 means bitrise_devenv_create with warm_pool_id returns instantly; status.limit_state "full" or "over_max" means a claim that finds no warm session is refused with reason POOL_FULL — the claim itself stays the authoritative check), to diagnose a pool that is not filling (status.last_error, status.config_error, status.paused_until), to see how close it is to its max (status.in_use of max_sessions, status.refused_7d), or to see its inventory — status.sessions lists every warming / ready session with session_id, state, created_at and ready_at, oldest first (this call only; the list view omits it).

Secret session input values are redacted in the response.`),
		mcp.WithString("warm_pool_id",
			mcp.Description("The unique identifier (UUID) of the warm pool"),
			mcp.Required(),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		warmPoolID, err := requireUUID(request, "warm_pool_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/warm-pools/%s", warmPoolID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get warm pool", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// CreateWarmPool creates a warm pool: a stored session configuration the
// backend keeps warm_sessions sessions of booted.
var CreateWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create_warm_pool",
		mcp.WithTitleAnnotation("Create warm pool"),
		mcp.WithDescription(`Create a warm pool. `+warmPoolConceptDoc+`

WHEN TO USE THIS: the same session configuration is created over and over and the boot time (machine + On create + device) is in the way — for example a team's daily dev sessions, an agent fleet that spins up a session per task, or a device configuration that preview links should open instantly. Create the pool once; afterwards every bitrise_devenv_create with warm_pool_id is handed a booted session.

The configuration fields are those of bitrise_devenv_create, validated the same way (a pool can only store what a session request could send): template_id (required — a pool is always template-based), session_inputs (required inputs must be given; secret values are stored encrypted and redacted in responses), enabled_feature_flag_names, optional stack_id / machine_type / cluster overrides of the template's values, and the device every warm session boots: omit device_spec to boot the template's declared device as is, pass one to tweak it per field (no platform) or replace it whole (with a platform), or no_device=true to boot none. Per-session fields (name, description, labels, auto_terminate_minutes, artifact) are NOT part of the pool — they are given at claim time.

Ownership: every pool belongs to the workspace — shared with every member and Workspace API Tokens, and the kind a preview link (bitrise_devenv_create_preview_link warm_pool_id) serves from. Session inputs must be plain values: saved inputs are personal and never reach a pool. A session claimed from the pool is the claimant's by default (see bitrise_devenv_create owner).

Sizing: warm_sessions is how many warm sessions to keep booted — each one is a running machine you pay for while idle, so start small and watch status.cold_total (claims that found no warm session) to tune it. 0 creates an inert pool that still serves as a configuration preset for claims.

Limit: max_sessions is the most sessions from this pool that may be in use at once (claimed or created cold; a session keeps its origin pool for life). When the pool is full, claims that find no warm session, restores and starts are refused with reason POOL_FULL until a session ends. Warm sessions only fill the room the max leaves, so claiming a warm session at the max always succeeds, and warm_sessions cannot be more than max_sessions. 0 (the default) means no limit. A ceiling, not a reservation: the workspace's CPU quota still applies on top. Use it to keep one group — an agent fleet, preview-link viewers — from taking the CPUs the whole workspace shares.`),
		mcp.WithString("name", mcp.Description("Human-readable name of the pool"), mcp.Required()),
		mcp.WithString("template_id",
			mcp.Description("ID (UUID) of the template every warm session is created from. Use bitrise_devenv_list_templates to find one and read its session inputs."),
			mcp.Required(),
		),
		mcp.WithNumber("warm_sessions",
			mcp.Description("How many warm sessions to keep booted and idle. 0 creates a drained pool that only serves as a configuration preset. Each warm session is a running machine, so keep it to the concurrency you actually need. Cannot be more than max_sessions when one is set."),
			mcp.Required(),
		),
		mcp.WithNumber("max_sessions",
			mcp.Description("The most sessions from this pool that may be in use at once, warm ones included; a full pool refuses claims that find no warm session, restores and starts with reason POOL_FULL until a session ends. 0 or omitted: no limit."),
		),
		mcp.WithArray("session_inputs",
			warmPoolSessionInputsSchema("Values for the template's session inputs that every warm session is created with — set once, for everyone who claims from the pool. On a template with lifecycle hooks only the shared and on_create inputs belong here (session_inputs[].hook in bitrise_devenv_get_template); an on_claim or on_restore key is rejected, each claimant supplies those on bitrise_devenv_create. Required inputs must have a value; optional inputs fall back to their default_value. Exposed (\"Also in terminal\") shared and on_create values stay on the pool machine for whoever claims it.")...,
		),
		mcp.WithArray("enabled_feature_flag_names",
			mcp.Description("Names of the template's feature flags to enable on every warm session"),
			mcp.WithStringItems(),
		),
		mcp.WithString("stack_id",
			mcp.Description("Optional stack override; omit to use the template's stack. Use bitrise_devenv_list_stacks to find valid IDs."),
		),
		mcp.WithString("machine_type",
			mcp.Description("Optional machine type override; omit to use the template's machine type. Use bitrise_devenv_list_machine_types to find valid names."),
		),
		mcp.WithString("cluster",
			mcp.Description("Optional target cluster name; omit to resolve it from stack + machine type. Needed only when that pair is available in multiple clusters."),
		),
		mcp.WithObject("device_spec",
			deviceSpecSchema(`Optional virtual device every warm session boots (same shape as bitrise_devenv_create's). `+deviceSpecFieldsDoc+` Omit to boot the template's declared device as is; without a platform the fields you set tweak that device (a template without a device needs a platform); with a platform it is the complete device to boot. A claimed session's artifact still comes at claim time.`)...,
		),
		mcp.WithBoolean("no_device",
			mcp.Description("Boot the warm sessions WITHOUT the device the template declares. Only meaningful with a template that has a device_spec; ignored otherwise. Cannot be combined with device_spec."),
		),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		poolSize, ok, err := getOptionalInt(request, "warm_sessions")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !ok {
			return mcp.NewToolResultError("warm_sessions is required — how many sessions to keep booted and ready to claim (0 for a drained preset pool)"), nil
		}

		body := map[string]any{
			"name":          request.GetString("name", ""),
			"template_id":   templateID,
			"warm_sessions": poolSize,
		}
		if maxSessions, ok, err := getOptionalInt(request, "max_sessions"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if ok {
			body["max_sessions"] = maxSessions
		}
		for _, key := range []string{"session_inputs", "enabled_feature_flag_names"} {
			if v, ok := request.GetArguments()[key]; ok {
				body[key] = v
			}
		}
		for _, key := range []string{"stack_id", "machine_type", "cluster"} {
			if v := request.GetString(key, ""); v != "" {
				body[key] = v
			}
		}
		// The device knobs follow bitrise_devenv_create: a spec object, or
		// no_device, never both.
		deviceSpec, hasDevice := request.GetArguments()["device_spec"]
		noDevice := request.GetBool("no_device", false)
		if hasDevice {
			if _, ok := deviceSpec.(map[string]any); !ok {
				return mcp.NewToolResultError("device_spec must be an object (see the parameter description for its fields)"), nil
			}
			if noDevice {
				return mcp.NewToolResultError("no_device cannot be combined with device_spec — either boot a device or skip it"), nil
			}
			body["device_spec"] = deviceSpec
		}
		if noDevice {
			body["no_device"] = true
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(ctx, "/warm-pools"),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("create warm pool", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// UpdateWarmPool updates a warm pool's name, limits or configuration.
var UpdateWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_warm_pool",
		mcp.WithTitleAnnotation("Update warm pool"),
		mcp.WithDescription(`Update a warm pool. Only provided fields change.

WHEN TO USE THIS:
- Scale a pool: set warm_sessions. Raise it when status.cold_total keeps growing (claims are not finding warm sessions); set it to 0 to drain the pool while keeping it usable as a configuration preset — the natural move for a scheduler that scales up for business hours and down at night. Scaling touches no claimed session.
- Change the limit: set max_sessions. Raise it when status.refused_7d keeps growing (the pool is full more often than its users can bear); set it to 0 to remove the limit. Lowering it below status.in_use terminates nothing: the pool reads limit_state "over_max" and refuses claims until enough sessions end. warm_sessions cannot be more than max_sessions. Changing warm_sessions or max_sessions requires the workspace's view_billing_data permission (the usage page's gate; a Workspace API Token always may); everything else any member may change.
- Fix a pool whose status.config_error says the stored configuration no longer builds (an input was removed from the template, a saved input was deleted, a stack was retired): correct session_inputs / enabled_feature_flag_names / stack_id / machine_type / cluster / device_spec.
- Change the device the warm sessions boot: device_spec (a new device, or a per-field tweak of the template's when it has no platform), device_spec {} to drop the pool's device override and boot the template's device as declared, or no_device true/false to skip or restore the template's device.
- Rename it: name.

Array fields: passing session_inputs or enabled_feature_flag_names replaces ALL existing entries (pass an empty array to clear); omit to leave unchanged. Secrets survive a resend: the session_inputs that bitrise_devenv_get_warm_pool returns has every secret value redacted to "", and sending that list back as is keeps each stored secret — only an input whose key is left out is removed, and a new value or a saved_input_id replaces the stored one. So to change one input, read the pool, edit that entry and send the whole list; nothing needs retyping. Override fields: pass stack_id, machine_type or cluster to set the override, an empty string "" to clear it back to the template's value; omit to leave unchanged.

A configuration change (anything but name, warm_sessions and max_sessions) invalidates the current warm sessions: the backend replaces them with sessions of the new configuration; a limit change never does.`),
		mcp.WithString("warm_pool_id", mcp.Description("The unique identifier (UUID) of the warm pool to update"), mcp.Required()),
		mcp.WithString("name", mcp.Description("New name of the pool")),
		mcp.WithNumber("warm_sessions",
			mcp.Description("New number of warm sessions to keep booted. 0 drains the pool but keeps it as a preset. Cannot be more than max_sessions when one is set. Requires view_billing_data (a Workspace API Token always may)."),
		),
		mcp.WithNumber("max_sessions",
			mcp.Description("New max sessions: the most sessions from this pool that may be in use at once. 0 removes the limit. Lowering it below status.in_use terminates nothing — claims are refused until enough sessions end. Requires view_billing_data (a Workspace API Token always may)."),
		),
		mcp.WithArray("session_inputs",
			warmPoolSessionInputsSchema("Replace ALL session input values with this list. Omit to leave unchanged. Pass an empty array to clear all. A secret entry sent with an empty value keeps the stored secret (the redacted list from bitrise_devenv_get_warm_pool can be sent back as is); a new value or a saved_input_id replaces it, leaving the key out removes it.")...,
		),
		mcp.WithArray("enabled_feature_flag_names",
			mcp.Description("Replace ALL enabled feature flags with this list. Omit to leave unchanged. Pass an empty array to clear all."),
			mcp.WithStringItems(),
		),
		mcp.WithString("stack_id", mcp.Description(`New stack override; "" clears it (the template's stack applies). Omit to leave unchanged.`)),
		mcp.WithString("machine_type", mcp.Description(`New machine type override; "" clears it (the template's machine type applies). Omit to leave unchanged.`)),
		mcp.WithString("cluster", mcp.Description(`New cluster override; "" clears it (resolved from stack + machine type). Omit to leave unchanged.`)),
		mcp.WithObject("device_spec",
			deviceSpecSchema(`New device override for the warm sessions. `+deviceSpecFieldsDoc+` Without a platform the fields you set tweak the template's declared device; with a platform it is the complete device to boot. Pass an empty object {} to drop the pool's override (the template's device applies as declared). Omit to leave unchanged. Cannot be combined with no_device=true.`)...,
		),
		mcp.WithBoolean("no_device",
			mcp.Description("true: the warm sessions boot WITHOUT the device the template declares; false: they boot it again. Omit to leave unchanged. Cannot be true together with a non-empty device_spec."),
		),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		warmPoolID, err := requireUUID(request, "warm_pool_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{}
		if _, ok := request.GetArguments()["name"]; ok {
			body["name"] = request.GetString("name", "")
		}
		for _, key := range []string{"warm_sessions", "max_sessions"} {
			if count, ok, err := getOptionalInt(request, key); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			} else if ok {
				body[key] = count
			}
		}
		// Array fields: auto-set the corresponding update_* switch when the
		// array is provided. The backend replaces the list only with the switch
		// on, so an omitted array leaves the pool's configuration unchanged.
		arrayFields := map[string]string{
			"session_inputs":             "update_session_inputs",
			"enabled_feature_flag_names": "update_enabled_feature_flag_names",
		}
		for arrayKey, flagKey := range arrayFields {
			if v, ok := request.GetArguments()[arrayKey]; ok {
				body[arrayKey] = v
				body[flagKey] = true
			}
		}
		// Override fields are optional scalars on the wire: present means
		// "set to this", and an empty string clears the override, so presence
		// (not non-emptiness) decides whether the key is sent.
		for _, key := range []string{"stack_id", "machine_type", "cluster"} {
			if _, ok := request.GetArguments()[key]; ok {
				body[key] = request.GetString(key, "")
			}
		}
		// Device: a spec object switches update_device_spec on; an EMPTY object
		// sends the switch alone, which the backend reads as "clear the
		// override". no_device is a presence-based bool like the overrides.
		if v, ok := request.GetArguments()["device_spec"]; ok {
			spec, isObj := v.(map[string]any)
			if !isObj {
				return mcp.NewToolResultError("device_spec must be an object (see the parameter description for its fields); pass {} to drop the pool's device override"), nil
			}
			if len(spec) > 0 {
				body["device_spec"] = spec
			}
			body["update_device_spec"] = true
		}
		if _, ok := request.GetArguments()["no_device"]; ok {
			body["no_device"] = request.GetBool("no_device", false)
		}
		if _, hasSpec := body["device_spec"]; hasSpec && body["no_device"] == true {
			return mcp.NewToolResultError("no_device cannot be true together with a device_spec — either boot a device or skip it"), nil
		}
		if len(body) == 0 {
			return mcp.NewToolResultError("nothing to update — pass at least one of name, warm_sessions, max_sessions, session_inputs, enabled_feature_flag_names, stack_id, machine_type, cluster, device_spec or no_device"), nil
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/warm-pools/%s", warmPoolID)),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update warm pool", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// DeleteWarmPool deletes a warm pool and drains its warm sessions.
var DeleteWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_delete_warm_pool",
		mcp.WithTitleAnnotation("Delete warm pool"),
		mcp.WithDescription(`Delete a warm pool. Its warm (unclaimed) sessions are terminated and deleted by the backend; sessions already claimed from it are untouched and keep running.

WHEN TO USE THIS: the configuration is no longer needed at all. Deleting invalidates every preview link minted against the pool — later opens fail as an expired link would. To stop paying for idle machines while keeping the configuration (and its links) around as a preset, prefer bitrise_devenv_update_warm_pool with warm_sessions 0 instead.`),
		mcp.WithString("warm_pool_id", mcp.Description("The unique identifier (UUID) of the warm pool to delete"), mcp.Required()),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		warmPoolID, err := requireUUID(request, "warm_pool_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/warm-pools/%s", warmPoolID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete warm pool", err), nil
		}
		if strings.TrimSpace(res) == "" || strings.TrimSpace(res) == "{}" {
			// The API returns an empty body on success; say what happened so
			// the model doesn't have to guess from a blank result.
			return mcp.NewToolResultText(fmt.Sprintf("Warm pool %s deleted. Its unclaimed warm sessions are being terminated and deleted in the background; sessions already claimed from it are untouched.", warmPoolID)), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// RetryWarmPool leaves the setup-failed state: the debug VM is deleted and
// the pool warms again.
var RetryWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_retry_warm_pool",
		mcp.WithTitleAnnotation("Retry a paused warm pool"),
		mcp.WithDescription(`Resume a warm pool that paused after three consecutive On create failures (setup_failed present on bitrise_devenv_get_warm_pool): the kept failed machine is deleted, the failure streak is cleared and the pool starts warming again.

WHEN TO USE THIS: after fixing what made On create fail — the template's script (bitrise_devenv_update_template) or something outside it (a token, a package mirror). Inspect the kept machine first: setup_failed.session_id is a session you can read the logs of and open a terminal on. Any configuration change to the pool or its template resumes warming the same way, so Retry is for "the script was fine, the world was not". A pool that is not paused this way is left as it is.`),
		mcp.WithString("warm_pool_id", mcp.Description("The unique identifier (UUID) of the warm pool"), mcp.Required()),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		warmPoolID, err := requireUUID(request, "warm_pool_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/warm-pools/%s/retry", warmPoolID)),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("retry warm pool", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
