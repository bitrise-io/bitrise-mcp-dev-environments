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
const warmPoolConceptDoc = `A warm pool is a stored session configuration — a template, its session input values, feature flags and optional stack / machine type / cluster overrides — plus an owner and a desired_count. The RDE backend keeps desired_count sessions of that configuration booted and idle ("warm sessions", status.ready / status.warming). Creating a session with bitrise_devenv_create warm_pool_id hands out a warm session instantly (Session.warm_state "claimed") or, when none is available, builds one from the pool's configuration (warm_state "cold") — so a pool with desired_count 0 still works as a configuration preset. Owner "user" makes the pool private to its creator; "workspace" shares it with every member and Workspace API Tokens.`

// warmPoolSessionInputsSchema is the session_inputs array a pool stores — the
// same shape bitrise_devenv_create takes, since a pool is a stored session
// create request.
func warmPoolSessionInputsSchema(description string) []mcp.PropertyOption {
	return []mcp.PropertyOption{
		mcp.Description(description),
		mcp.Items(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":            map[string]any{"type": "string", "description": "Key name matching a session input on the template"},
				"value":          map[string]any{"type": "string", "description": "Direct value (ignored if saved_input_id is set)"},
				"is_secret":      map[string]any{"type": "boolean", "description": "Whether the value is secret (ignored if saved_input_id is set)"},
				"saved_input_id": map[string]any{"type": "string", "description": "Optional: ID of a saved input to use instead of a direct value. User-owned pools only — a workspace pool must carry plain values."},
			},
			"required": []string{"key"},
		}),
	}
}

// ListWarmPools lists the warm pools the caller may see.
var ListWarmPools = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_warm_pools",
		mcp.WithDescription(`List warm pools. `+warmPoolConceptDoc+`

WHEN TO USE THIS: before creating a session, to find a pool that already has your configuration booted (then pass its id as warm_pool_id to bitrise_devenv_create); before creating a pool, to avoid a duplicate; or to review what is kept warm — and paid for — in the workspace.

By default returns the workspace's pools plus your own user pools, never another user's. Set all=true to list every pool in the workspace read-only (cost visibility; requires the workspace's view_billing_data permission). Optionally restrict to one template with template_id.

Each pool carries its configuration (secret input values redacted), desired_count and a status with ready / warming counts, lifetime claimed_total / cold_total (a high cold_total means desired_count is too low for the demand), last_error, config_error (the stored configuration no longer builds — fix it with bitrise_devenv_update_warm_pool) and paused_until. The list view omits the per-session inventory; use bitrise_devenv_get_warm_pool for status.sessions.`),
		mcp.WithString("template_id",
			mcp.Description("Optional: only list pools created from this template (UUID)."),
		),
		mcp.WithBoolean("all",
			mcp.Description("When true, list every pool in the workspace regardless of owner (read-only cost view; requires view_billing_data permission). Defaults to false: the workspace's pools plus your own."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
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
		mcp.WithDescription(`Get one warm pool with its full live status. `+warmPoolConceptDoc+`

WHEN TO USE THIS: to check whether a pool has a session ready before claiming from it (status.ready > 0 means bitrise_devenv_create with warm_pool_id returns instantly), to diagnose a pool that is not filling (status.last_error, status.config_error, status.paused_until), or to see its inventory — status.sessions lists every warming / ready session with session_id, state, created_at and ready_at, oldest first (this call only; the list view omits it).

Secret session input values are redacted in the response.`),
		mcp.WithString("warm_pool_id",
			mcp.Description("The unique identifier (UUID) of the warm pool"),
			mcp.Required(),
		),
		mcp.WithReadOnlyHintAnnotation(true),
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
// backend keeps desired_count sessions of booted.
var CreateWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_create_warm_pool",
		mcp.WithDescription(`Create a warm pool. `+warmPoolConceptDoc+`

WHEN TO USE THIS: the same session configuration is created over and over and the boot time (machine + warmup script + device) is in the way — for example a team's daily dev sessions, an agent fleet that spins up a session per task, or a device configuration that preview links should open instantly. Create the pool once; afterwards every bitrise_devenv_create with warm_pool_id is handed a booted session.

The configuration fields are those of bitrise_devenv_create, validated the same way (a pool can only store what a session request could send): template_id (required — a pool is always template-based), session_inputs (required inputs must be given; secret values are stored encrypted and redacted in responses), enabled_feature_flag_names, optional stack_id / machine_type / cluster overrides of the template's values, and the device every warm session boots: omit device_spec to boot the template's declared device as is, pass one to tweak it per field (no platform) or replace it whole (with a platform), or no_device=true to boot none. Per-session fields (name, description, labels, auto_terminate_minutes, artifact) are NOT part of the pool — they are given at claim time.

Ownership: owner_type "user" (default for a personal token) keeps the pool and its sessions private to you and allows saved_input_id references in session_inputs. "workspace" shares the pool with every member and Workspace API Tokens, its sessions are workspace-owned, session inputs must be plain values, and it is the only kind a preview link (bitrise_devenv_create_preview_link warm_pool_id) may serve from. With a Workspace API Token omit owner_type or set "workspace".

Sizing: desired_count is how many warm sessions to keep booted — each one is a running machine you pay for while idle, so start small and watch status.cold_total (claims that found no warm session) to tune it. 0 creates an inert pool that still serves as a configuration preset for claims.`),
		mcp.WithString("name", mcp.Description("Human-readable name of the pool"), mcp.Required()),
		mcp.WithString("template_id",
			mcp.Description("ID (UUID) of the template every warm session is created from. Use bitrise_devenv_list_templates to find one and read its session inputs."),
			mcp.Required(),
		),
		mcp.WithNumber("desired_count",
			mcp.Description("How many warm sessions to keep booted and idle. 0 creates a drained pool that only serves as a configuration preset. Each warm session is a running machine, so keep it to the concurrency you actually need."),
			mcp.Required(),
		),
		mcp.WithString("owner_type",
			mcp.Description(`Who owns the pool and the sessions claimed from it. "user" (default with a personal token): private to you; saved_input_id references allowed. "workspace": shared with every member and Workspace API Tokens, plain-value inputs only, required for pools that serve preview links. Omit or "workspace" with a Workspace API Token.`),
			mcp.Enum("user", "workspace"),
		),
		mcp.WithArray("session_inputs",
			warmPoolSessionInputsSchema("Values for the template's session inputs that every warm session is created with. Required inputs must have a value (direct, or saved_input_id on a user pool); optional inputs fall back to their default_value.")...,
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
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		desiredCount, ok, err := getOptionalInt(request, "desired_count")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !ok {
			return mcp.NewToolResultError("desired_count is required — how many warm sessions to keep booted (0 for a drained preset pool)"), nil
		}

		body := map[string]any{
			"name":          request.GetString("name", ""),
			"template_id":   templateID,
			"desired_count": desiredCount,
		}
		if owner := request.GetString("owner_type", ""); owner != "" {
			body["owner_type"] = owner
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

// UpdateWarmPool updates a warm pool's name, desired count or configuration.
var UpdateWarmPool = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_warm_pool",
		mcp.WithDescription(`Update a warm pool. Only provided fields change.

WHEN TO USE THIS:
- Scale a pool: set desired_count. Raise it when status.cold_total keeps growing (claims are not finding warm sessions); set it to 0 to drain the pool while keeping it usable as a configuration preset — the natural move for a scheduler that scales up for business hours and down at night. Scaling touches no claimed session.
- Fix a pool whose status.config_error says the stored configuration no longer builds (an input was removed from the template, a saved input was deleted, a stack was retired): correct session_inputs / enabled_feature_flag_names / stack_id / machine_type / cluster / device_spec.
- Change the device the warm sessions boot: device_spec (a new device, or a per-field tweak of the template's when it has no platform), device_spec {} to drop the pool's device override and boot the template's device as declared, or no_device true/false to skip or restore the template's device.
- Rename it: name.

Array fields: passing session_inputs or enabled_feature_flag_names replaces ALL existing entries (pass an empty array to clear); omit to leave unchanged. Secrets survive a resend: the session_inputs that bitrise_devenv_get_warm_pool returns has every secret value redacted to "", and sending that list back as is keeps each stored secret — only an input whose key is left out is removed, and a new value or a saved_input_id replaces the stored one. So to change one input, read the pool, edit that entry and send the whole list; nothing needs retyping. Override fields: pass stack_id, machine_type or cluster to set the override, an empty string "" to clear it back to the template's value; omit to leave unchanged.

A configuration change (anything but name and desired_count) invalidates the current warm sessions: the backend replaces them with sessions of the new configuration.`),
		mcp.WithString("warm_pool_id", mcp.Description("The unique identifier (UUID) of the warm pool to update"), mcp.Required()),
		mcp.WithString("name", mcp.Description("New name of the pool")),
		mcp.WithNumber("desired_count",
			mcp.Description("New number of warm sessions to keep booted. 0 drains the pool but keeps it as a preset."),
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
		if count, ok, err := getOptionalInt(request, "desired_count"); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		} else if ok {
			body["desired_count"] = count
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
			return mcp.NewToolResultError("nothing to update — pass at least one of name, desired_count, session_inputs, enabled_feature_flag_names, stack_id, machine_type, cluster, device_spec or no_device"), nil
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
		mcp.WithDescription(`Delete a warm pool. Its warm (unclaimed) sessions are terminated and deleted by the backend; sessions already claimed from it are untouched and keep running.

WHEN TO USE THIS: the configuration is no longer needed at all. To stop paying for idle machines while keeping the configuration around as a preset, prefer bitrise_devenv_update_warm_pool with desired_count 0 instead. Preview links minted against the pool keep working after deletion, degraded to the ordinary cold boot path.`),
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
