package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// templateHooksDoc explains lifecycle hooks once; the template tools share
// it so every description tells the same story.
const templateHooksDoc = `Lifecycle hooks are the alternative to startup_script / warmup_script: bash scripts the platform runs on the booted machine as the session user, in a login shell, with the values of the hook's scope exported. Every hook is optional. on_create runs once, when the session is created — on a warm pool, when the pool warms the machine. on_claim runs on every claim, when the session is assigned to its owner: at claim time for a pool, right after on_create for a session created directly. on_restore runs every time a stopped session is restored — background processes do not survive a stop, so anything that must run again after a restore belongs here too. A hook left empty runs nothing: with no on_create a session (or a warm pool machine) is set up as soon as it boots, and a template with no hooks at all is valid. A hook is done when its script exits (0 = done, non-zero = failed): start long-running work (a dev server) in the background so the script can exit, without the values it must not keep ("env -u SECRET cmd &"). An on_create that has not exited after 20 minutes is ended by RDE (exit code 124). While an On create run is pending or running the session cannot be reached: bitrise_devenv_execute, screenshot, click/type/scroll/mouse_drag, upload, download and open_remote_access are refused with "Session is setting up: On create is still running. Access opens when it finishes." — poll bitrise_devenv_get until lifecycle.phase is LIFECYCLE_PHASE_READY.

SCOPES: every template variable and session input has a hook — its scope. "shared" (the default when omitted) reaches every hook; "on_create", "on_claim" or "on_restore" reach that hook only and are accepted only on a template with hooks. A hook receives the shared values plus its own, nothing else: on_restore does NOT see the claim values — give it its own on_restore inputs, or make them shared. The hook's own value wins over a shared value of the same key. Keys are unique within a scope; the same key may exist on several scopes, and one supplied value (or one saved input) fills all of them. Who supplies inputs: bitrise_devenv_create with template_id needs every required input of every scope; bitrise_devenv_create_warm_pool takes the shared and on_create inputs; a claim (bitrise_devenv_create with warm_pool_id) supplies the on_claim and on_restore inputs. Legacy templates keep every value shared — exactly the delivery they had.

VARIABLES: is_secret encrypts the value at rest and omits it from responses (include_secrets reveals it to members). protected is secret plus write-only: the API never returns the value (reads give value "" with protected true, even with include_secrets), it can be replaced but never viewed, and an empty value on update keeps the stored one — so a round-trip from bitrise_devenv_get_template keeps it. Use protected for secrets every warm pool of the template reuses, such as a registry credential. A protected value is never written to the machine, so expose_as_env_var is refused with it.

"ALSO IN TERMINAL" (expose_as_env_var, on every variable and input, any scope): hooks always receive this value. Also in terminal additionally writes it to the session's shell profile when this step runs, so it is available in SSH and terminal sessions afterwards. Later steps override earlier ones for the same name. Exposed Shared and On create values stay on a pool machine for whoever claims it. A hook with no script but with exposed values still gets a run, so the values are applied.

A template has either hooks or the legacy scripts, never both, and saving hooks requires lifecycle hooks to be available on the workspace (the server rejects it otherwise). On a template with hooks, startup_script reads back as a generated, read-only equivalent of the hooks — change hooks instead.`

// valueScopes are the four scopes a variable or session input can belong to;
// an omitted hook means "shared".
var valueScopes = []string{"shared", "on_create", "on_claim", "on_restore"}

// templateVariableItemSchema is one template variable, shared by the create
// and update template tools.
var templateVariableItemSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"key":               map[string]any{"type": "string", "description": "Key/name of the variable. Unique within its scope; the same key may exist on another scope."},
		"value":             map[string]any{"type": "string", "description": "Value of the variable. For a protected variable an empty value on update keeps the stored one."},
		"is_secret":         map[string]any{"type": "boolean", "description": "Whether this is a secret value (encrypted at rest, omitted from responses unless include_secrets)"},
		"protected":         map[string]any{"type": "boolean", "description": "Write-only: secret, never returned by the API (reads give value \"\"), replaced but never viewed; an empty value on update keeps the stored one. Implies is_secret. Any scope, hooks templates only. For secrets every warm pool of the template reuses. expose_as_env_var is refused with it."},
		"expose_as_env_var": map[string]any{"type": "boolean", "description": "\"Also in terminal\": hooks always receive the value; this additionally writes it to the session's shell profile when its scope's step runs, so it is available in SSH and terminal sessions afterwards (later steps override earlier ones for the same name; exposed shared and on_create values stay on a pool machine for whoever claims it). Refused with protected."},
		"hook":              map[string]any{"type": "string", "enum": valueScopes, "description": `The scope that receives the variable: "shared" (the default when omitted) reaches every hook; "on_create", "on_claim" or "on_restore" reach that hook only (hooks templates only). The hook's own value wins over a shared one of the same key.`},
	},
	"required": []string{"key", "value"},
}

// templateHooksSchema is the hooks object of the create/update template tools.
func templateHooksSchema(description string) []mcp.PropertyOption {
	return []mcp.PropertyOption{
		mcp.Description(description),
		mcp.Properties(map[string]any{
			"on_create":  map[string]any{"type": "string", "description": "Optional bash script run once when the session is created (on a warm pool: when the machine is warmed) with the shared and on_create values. Empty = no On create hook: the session is set up as soon as it boots."},
			"on_claim":   map[string]any{"type": "string", "description": "Optional bash script run on every claim, when the session is assigned to its owner, with the shared and on_claim values. Empty = no On claim hook."},
			"on_restore": map[string]any{"type": "string", "description": "Optional bash script run every time a stopped session is restored, with the shared and on_restore values (never the claim values). Empty = no On restore hook."},
		}),
	}
}

// templateSessionInputItemSchema is one session input definition, shared by
// the create and update template tools.
var templateSessionInputItemSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"key":               map[string]any{"type": "string", "description": "Key/name of the input. Unique within its scope; the same key may exist on another scope (one supplied value or saved input then fills both)."},
		"description":       map[string]any{"type": "string", "description": "Description explaining what this input is for"},
		"required":          map[string]any{"type": "boolean", "description": "Whether this input is required (default: false)"},
		"default_value":     map[string]any{"type": "string", "description": "Default value for the input. REQUIRED when required is false (must be a non-empty string). Omit or leave empty when required is true."},
		"expose_as_env_var": map[string]any{"type": "boolean", "description": "\"Also in terminal\": hooks always receive the value; this additionally writes it to the session's shell profile when its scope's step runs, so it is available in SSH and terminal sessions afterwards (later steps override earlier ones for the same name; exposed shared and on_create values stay on a pool machine for whoever claims it)."},
		"hook":              map[string]any{"type": "string", "enum": valueScopes, "description": `The scope that receives the input and says who supplies it: "shared" (the default when omitted) reaches every hook and is given when the session or the warm pool is created; "on_create" is given with the pool (or the direct create) and reaches on_create only; "on_claim" and "on_restore" are given by whoever claims (or at a direct create) and reach that hook only — never stored on a pool. The per-hook scopes are only valid on a template with hooks.`},
	},
	"required": []string{"key"},
}

// ListTemplates lists all available templates.
var ListTemplates = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_list_templates",
		mcp.WithTitleAnnotation("List templates"),
		mcp.WithDescription("List all available devenv templates. Each template defines the stack, startup/warmup scripts, template variables, session inputs (required and optional), feature flags, workspace links, and optionally a device_spec — the iOS simulator / Android emulator every session created from it boots unless the create request overrides it with its own device_spec or no_device. lifecycle_mode says how its scripts run: \"legacy\" (startup_script / warmup_script) or \"hooks\" (the hooks object: on_create, on_claim, on_restore — see bitrise_devenv_create_template; on such a template startup_script is a generated read-only equivalent). By default, secret template variable values are omitted from the response; set include_secrets=true to include them."),
		mcp.WithBoolean("include_secrets",
			mcp.Description("When true, secret template variable values are included in the response. Defaults to false (secret values are omitted)."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := map[string]string{}
		if request.GetBool("include_secrets", false) {
			params["include_secrets"] = "true"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/templates"),
			Params: params,
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
		mcp.WithTitleAnnotation("Get template"),
		mcp.WithDescription("Get details of a specific template including startup/warmup scripts, stack, working directory, template variables, session inputs (with required/default_value/expose_as_env_var fields), feature flags, workspace links, and device_spec (the virtual device sessions created from it boot by default; absent when the template declares none — see bitrise_devenv_create for how a create request inherits, overrides or skips it). lifecycle_mode is \"legacy\" (startup_script / warmup_script run by the platform's two boot stages) or \"hooks\": the template's scripts are the hooks object (on_create, on_claim, on_restore — see bitrise_devenv_create_template for what each one is) and startup_script is a generated, read-only equivalent of them — edit hooks, never that. Each session input and template variable carries hook — its scope: \"shared\" reaches every hook, \"on_create\" / \"on_claim\" / \"on_restore\" that hook only (see bitrise_devenv_create_template) — and a variable may be protected (write-only: its value is always \"\"). By default, secret template variable values are omitted from the response; set include_secrets=true to include them (a protected value stays \"\" regardless)."),
		mcp.WithString("template_id",
			mcp.Description("The unique identifier (UUID) of the template"),
			mcp.Required(),
		),
		mcp.WithBoolean("include_secrets",
			mcp.Description("When true, secret template variable values are included in the response. Defaults to false (secret values are omitted)."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		params := map[string]string{}
		if request.GetBool("include_secrets", false) {
			params["include_secrets"] = "true"
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/templates/%s", templateID)),
			Params: params,
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
		mcp.WithTitleAnnotation("Create template"),
		mcp.WithDescription(`Create a new devenv template. Use bitrise_devenv_list_stacks to find a valid stack_id and bitrise_devenv_list_machine_types to find a valid machine_type name. IMPORTANT: Provide the stack id (the 'id' field from bitrise_devenv_list_stacks) for stack_id, and the machine type name (not UUID) for machine_type. Optionally declare a device_spec so every session created from the template boots an iOS simulator / Android emulator; the stack and machine type must then fit the platform (iOS: a macOS stack; Android: a dockerless Android Linux stack such as ubuntu-resolute-26.04-bitrise-2026-android, not linux-docker-*; both: >= 4 vCPU / 6-8 GB) or the request is rejected with a device_spec.* field violation.

Scripts: give either the legacy pair (warmup_script runs once at creation, startup_script on every start) or hooks. `+templateHooksDoc+`

Every session input and template variable carries hook — its scope (shared, on_create, on_claim, on_restore; see session_inputs and template_variables); omit it for shared.`),
		mcp.WithString("name", mcp.Description("Template name"), mcp.Required()),
		mcp.WithString("description", mcp.Description("Template description")),
		mcp.WithString("startup_script", mcp.Description("Legacy: bash script that runs every time a session starts. Cannot be combined with hooks.")),
		mcp.WithString("warmup_script", mcp.Description("Legacy: bash script that runs once during initial session creation. Cannot be combined with hooks.")),
		mcp.WithObject("hooks",
			templateHooksSchema("Lifecycle hooks (on_create, on_claim, on_restore). Setting this makes the template a hooks template: cannot be combined with startup_script / warmup_script, and requires lifecycle hooks to be available on the workspace.")...,
		),
		mcp.WithString("stack_id", mcp.Description(`Stack ID (use bitrise_devenv_list_stacks to find the id, e.g. 'osx-xcode-16.0.x-edge').`), mcp.Required()),
		mcp.WithString("machine_type", mcp.Description("Machine type name (use bitrise_devenv_list_machine_types to find the name, e.g. 'g2.mac.m2pro.4c')"), mcp.Required()),
		mcp.WithString("working_directory", mcp.Description("Working directory for terminal sessions (absolute path)")),
		mcp.WithArray("template_variables",
			mcp.Description("Template variables baked into this template (delivered to the boot scripts / lifecycle hooks of their scope). Each has a hook (its scope; omitted = shared) and may be is_secret or protected (write-only); hooks templates only for the per-hook scopes and protected."),
			mcp.Items(templateVariableItemSchema),
		),
		mcp.WithArray("session_inputs",
			mcp.Description("Session inputs that callers provide when creating (or claiming) sessions from this template. Each has a hook — its scope (omitted = shared), which also says who supplies it: shared and on_create with the session or the pool, on_claim and on_restore by whoever claims."),
			mcp.Items(templateSessionInputItemSchema),
		),
		mcp.WithArray("feature_flags",
			mcp.Description("Feature flags to toggle optional behaviors"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Name of the feature flag"},
					"description": map[string]any{"type": "string", "description": "Description of what this flag enables"},
				},
				"required": []string{"name"},
			}),
		),
		mcp.WithArray("workspace_links",
			mcp.Description("IDE workspace folder links for quick access"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":             map[string]any{"type": "string", "description": "Display label for the button"},
					"folder_path":       map[string]any{"type": "string", "description": "Remote folder path to open"},
					"feature_flag_name": map[string]any{"type": "string", "description": "Optional: feature flag name that controls visibility"},
				},
				"required": []string{"label", "folder_path"},
			}),
		),
		mcp.WithObject("device_spec",
			deviceSpecSchema(`Optional virtual device declared on the template: sessions created from this template boot this device unless the create request overrides it (a device_spec without a platform tweaks it per field; one with a platform replaces it whole) or skips it (no_device=true). `+deviceSpecFieldsDoc+` platform is required here. The template's stack_id and machine_type must fit the platform.`, "platform")...,
		),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{
			"name":         request.GetString("name", ""),
			"stack_id":     request.GetString("stack_id", ""),
			"machine_type": request.GetString("machine_type", ""),
		}
		for _, key := range []string{"description", "startup_script", "warmup_script", "working_directory"} {
			if v := request.GetString(key, ""); v != "" {
				body[key] = v
			}
		}
		for _, key := range []string{"template_variables", "session_inputs", "feature_flags", "workspace_links"} {
			if v, ok := request.GetArguments()[key]; ok {
				body[key] = v
			}
		}
		// Forwarded as given; the server decides whether hooks may be saved
		// for the workspace and refuses them next to the legacy scripts.
		if hooks, ok := request.GetArguments()["hooks"]; ok {
			body["hooks"] = hooks
		}
		if deviceSpec, ok := request.GetArguments()["device_spec"]; ok {
			if err := requireNonEmptyString(deviceSpec, "device_spec", "platform"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body["device_spec"] = deviceSpec
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(ctx, "/templates"),
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
		mcp.WithTitleAnnotation("Update template"),
		mcp.WithDescription("Update an existing devenv template. Only provided fields are updated. For array fields (template_variables, session_inputs, feature_flags, workspace_links), providing a new array replaces ALL existing entries. Omit an array field to leave it unchanged. The template's device works the same way: pass device_spec to replace the declared device as a whole (there is no per-field merge on the template itself), pass clear_device_spec=true to remove it so sessions boot without a device, or omit both to leave it unchanged. Existing sessions keep the device they were created with. Lifecycle hooks: pass hooks to replace the template's hooks as a whole (include every hook you want kept) — this also turns a legacy template into a hooks template, after which startup_script / warmup_script are refused on it (the conversion is one-way); omit hooks to leave them unchanged. "+templateHooksDoc+" Existing sessions keep the hooks they were created with."),
		mcp.WithString("template_id", mcp.Description("The unique identifier of the template to update"), mcp.Required()),
		mcp.WithString("name", mcp.Description("Updated template name")),
		mcp.WithString("description", mcp.Description("Updated description")),
		mcp.WithString("startup_script", mcp.Description("Legacy: updated startup script. Refused on a template with hooks (update hooks instead).")),
		mcp.WithString("warmup_script", mcp.Description("Legacy: updated warmup script. Refused on a template with hooks (update hooks instead).")),
		mcp.WithObject("hooks",
			templateHooksSchema("Replace the template's lifecycle hooks with these (the whole object is replaced, so include every hook you want kept). Omit to leave the hooks unchanged. Requires lifecycle hooks to be available on the workspace.")...,
		),
		mcp.WithString("stack_id", mcp.Description(`Updated stack ID (use bitrise_devenv_list_stacks to find the id, e.g. 'osx-xcode-16.0.x-edge').`)),
		mcp.WithString("machine_type", mcp.Description("Updated machine type name (use bitrise_devenv_list_machine_types to find the name)")),
		mcp.WithString("working_directory", mcp.Description("Updated working directory")),
		mcp.WithArray("template_variables",
			mcp.Description("Replace ALL template variables with this list. Omit to leave unchanged. Pass empty array to clear all. A protected variable resent with an empty value keeps its stored value (reads never return it), so a round-trip from bitrise_devenv_get_template keeps it; changing a protected variable's key needs a value again. An entry without hook is shared."),
			mcp.Items(templateVariableItemSchema),
		),
		mcp.WithArray("session_inputs",
			mcp.Description("Replace ALL session inputs with this list. Omit to leave unchanged. Pass empty array to clear all. An entry without hook is shared."),
			mcp.Items(templateSessionInputItemSchema),
		),
		mcp.WithArray("feature_flags",
			mcp.Description("Replace ALL feature flags with this list. Omit to leave unchanged. Pass empty array to clear all."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "Name of the feature flag"},
					"description": map[string]any{"type": "string", "description": "Description of what this flag enables"},
				},
				"required": []string{"name"},
			}),
		),
		mcp.WithArray("workspace_links",
			mcp.Description("Replace ALL workspace links with this list. Omit to leave unchanged. Pass empty array to clear all."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"label":             map[string]any{"type": "string", "description": "Display label for the button"},
					"folder_path":       map[string]any{"type": "string", "description": "Remote folder path to open"},
					"feature_flag_name": map[string]any{"type": "string", "description": "Optional: feature flag name that controls visibility"},
				},
				"required": []string{"label", "folder_path"},
			}),
		),
		mcp.WithObject("device_spec",
			deviceSpecSchema(`Replace the template's declared virtual device with this one (the whole object is replaced, so include every field you want kept). Omit to leave the device unchanged; use clear_device_spec to remove it. `+deviceSpecFieldsDoc+` platform is required here. The template's stack_id and machine_type (after this update) must fit the platform.`, "platform")...,
		),
		mcp.WithBoolean("clear_device_spec",
			mcp.Description("When true, remove the template's declared device so new sessions boot without one. Cannot be combined with device_spec."),
		),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{}
		for _, key := range []string{"name", "description", "startup_script", "warmup_script", "stack_id", "machine_type", "working_directory"} {
			if v := request.GetString(key, ""); v != "" {
				body[key] = v
			}
		}
		// Array fields: auto-set the corresponding update_* flag when the array is provided.
		// Backend requires the flag to be true for array changes to take effect.
		arrayFields := map[string]string{
			"template_variables": "update_template_variables",
			"session_inputs":     "update_session_inputs",
			"feature_flags":      "update_feature_flags",
			"workspace_links":    "update_workspace_links",
		}
		for arrayKey, flagKey := range arrayFields {
			if v, ok := request.GetArguments()[arrayKey]; ok {
				body[arrayKey] = v
				body[flagKey] = true
			}
		}
		// hooks follows the same convention: update_hooks says the object
		// replaces the template's hooks (an empty on_claim removes that hook).
		hooks, hasHooks := request.GetArguments()["hooks"]
		if hasHooks {
			body["hooks"] = hooks
			body["update_hooks"] = true
		}
		// The device follows the same convention: update_device_spec is set
		// whenever the caller asks for a change. With device_spec the template's
		// device becomes that object; with clear_device_spec the flag goes out
		// alone, which the backend reads as "no device".
		deviceSpec, hasDevice := request.GetArguments()["device_spec"]
		clearDevice := request.GetBool("clear_device_spec", false)
		if hasDevice && clearDevice {
			return mcp.NewToolResultError("clear_device_spec cannot be combined with device_spec — either replace the template's device or remove it"), nil
		}
		if hasDevice {
			if err := requireNonEmptyString(deviceSpec, "device_spec", "platform"); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			body["device_spec"] = deviceSpec
			body["update_device_spec"] = true
		}
		if clearDevice {
			body["update_device_spec"] = true
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPatch,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/templates/%s", templateID)),
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
		mcp.WithTitleAnnotation("Delete template"),
		mcp.WithDescription("Delete a devenv template. Existing sessions continue to work from their snapshotted template configuration, but will be marked with template_deleted=true."),
		mcp.WithString("template_id", mcp.Description("The unique identifier of the template to delete"), mcp.Required()),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		templateID, err := requireUUID(request, "template_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodDelete,
			Path:   devenv.WsPath(ctx, fmt.Sprintf("/templates/%s", templateID)),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("delete template", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
