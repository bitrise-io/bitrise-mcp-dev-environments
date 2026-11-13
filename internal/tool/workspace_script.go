package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// workspaceScriptConceptDoc explains what a workspace script is and how it
// runs. It is shared by the tool descriptions so an agent reads the same
// model wherever the script is mentioned.
const workspaceScriptConceptDoc = `A workspace script is one bash script per workspace that runs first on every session boot: every kind of session (template, ad hoc, warm pool, device preview), every boot (restore and rebuild included), macOS and Linux. It runs as the session user from $HOME via ` + "`bash -l`" + `, with RDE_OS (macos|linux), RDE_SESSION_ID, RDE_WORKSPACE_ID and the script's variables in its environment. The template's lifecycle hooks / startup script wait until it exits, at most 10 minutes: past that they start anyway and the script keeps running (it is never killed); a line ` + "`# rde: no-wait-limit`" + ` in the script lifts that limit. Start long-running processes in the background (` + "`my-agent &`" + `) so the script can exit. Its output goes to the session's System log (bitrise_devenv_get_logs stage "system"; "startup" on a session created before lifecycle hooks), each line prefixed "[workspace] ", and to ~/.rde/workspace.log on the machine; ` + "`rde-agent session info [--format json|env]`" + ` on the machine describes the session live.

A disabled script is a draft: it runs only on test sessions — personal sessions created with bitrise_devenv_create workspace_script_test=true.

Variables have a visibility: plain (value returned), secret (stored encrypted; value returned only with include_secrets=true, otherwise ""), protected (stored encrypted and write-only; the value is never returned, value_stored=true says one is stored). Keys starting with RDE_ are reserved. Limits: script 64 KiB, 64 variables, keys up to 128 characters, script + all keys and values up to 256 KiB, no NUL characters.

Admin only: reading and editing need either a personal access token of someone with the workspace's Usage permission (owners, billing managers), or a Workspace API Token (bitwat_) created with Remote Dev Environments access and a workspace role that can manage the workspace (Manager, or a custom role with "Modify workspace settings"); any other Workspace API Token is refused with 403. A save made with a Workspace API Token shows "Workspace API token" as updated_by instead of an email. A Workspace API Token cannot create a test session: those are personal. The feature must be enabled for the workspace by Bitrise; otherwise the API answers 403 "The workspace script is not available for this workspace."`

// Variable visibilities accepted by bitrise_devenv_update_workspace_script.
const (
	visibilityPlain     = "plain"
	visibilitySecret    = "secret"
	visibilityProtected = "protected"
)

// workspaceScriptVariable is one variable as the API stores it.
type workspaceScriptVariable struct {
	Key       string
	Value     string
	IsSecret  bool
	Protected bool
}

func (v workspaceScriptVariable) visibility() string {
	switch {
	case v.Protected:
		return visibilityProtected
	case v.IsSecret:
		return visibilitySecret
	default:
		return visibilityPlain
	}
}

// workspaceScriptState is the script as read back from the API.
type workspaceScriptState struct {
	Enabled   bool
	Script    string
	Variables []workspaceScriptVariable
}

// jsonField returns m[snake] or, failing that, m[camel]: the API answers in
// snake_case, but reading the camelCase spelling too keeps the
// read-modify-write correct whichever one the gateway emits.
func jsonField(m map[string]any, snake, camel string) any {
	if v, ok := m[snake]; ok {
		return v
	}
	return m[camel]
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// parseWorkspaceScript decodes a GET /workspace-script response. Absent
// fields are zero values (an unconfigured script reads as disabled, empty,
// with no variables).
func parseWorkspaceScript(raw string) (workspaceScriptState, error) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return workspaceScriptState{}, fmt.Errorf("decode workspace script: %w", err)
	}
	ws, ok := jsonField(envelope, "workspace_script", "workspaceScript").(map[string]any)
	if !ok {
		return workspaceScriptState{}, fmt.Errorf("decode workspace script: response has no workspace_script object")
	}
	state := workspaceScriptState{
		Enabled: asBool(ws["enabled"]),
		Script:  asString(ws["script"]),
	}
	vars, _ := ws["variables"].([]any)
	for _, item := range vars {
		m, ok := item.(map[string]any)
		if !ok {
			return workspaceScriptState{}, fmt.Errorf("decode workspace script: variable is not an object")
		}
		state.Variables = append(state.Variables, workspaceScriptVariable{
			Key:       asString(m["key"]),
			Value:     asString(m["value"]),
			IsSecret:  asBool(jsonField(m, "is_secret", "isSecret")),
			Protected: asBool(m["protected"]),
		})
	}
	return state, nil
}

// GetWorkspaceScript reads the workspace script.
var GetWorkspaceScript = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_workspace_script",
		mcp.WithTitleAnnotation("Get workspace script"),
		mcp.WithDescription(`Get the workspace script: its bash script, whether it is enabled, and its variables. `+workspaceScriptConceptDoc+`

Returns {"workspace_script": {configured, enabled, script, variables: [{key, value, is_secret, protected, value_stored}], updated_by, updated_at}}. configured=false means no script was ever saved. Secret values are "" unless include_secrets=true; protected values are always "".`),
		mcp.WithBoolean("include_secrets",
			mcp.Description("When true, secret (non-protected) variable values are returned in plaintext. Protected values are never returned. Defaults to false."),
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
			Path:   devenv.WsPath(ctx, "/workspace-script"),
			Params: params,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get workspace script", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// variableChange is one entry of set_variables.
type variableChange struct {
	key        string
	value      string
	hasValue   bool
	visibility string // "" when omitted
}

func parseSetVariables(raw any) ([]variableChange, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("set_variables must be an array of {key, value, visibility} objects")
	}
	seen := map[string]bool{}
	changes := make([]variableChange, 0, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("set_variables[%d] must be an object with key, value and visibility", i)
		}
		key, _ := m["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("set_variables[%d].key is required", i)
		}
		if strings.HasPrefix(key, "RDE_") {
			return nil, fmt.Errorf("set_variables[%d].key %q: keys starting with RDE_ are reserved", i, key)
		}
		if seen[key] {
			return nil, fmt.Errorf("set_variables lists %q more than once", key)
		}
		seen[key] = true
		c := variableChange{key: key}
		if v, present := m["value"]; present && v != nil {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("set_variables[%d].value must be a string", i)
			}
			c.value, c.hasValue = s, true
		}
		if v, present := m["visibility"]; present && v != nil {
			s, _ := v.(string)
			switch s {
			case visibilityPlain, visibilitySecret, visibilityProtected:
				c.visibility = s
			default:
				return nil, fmt.Errorf("set_variables[%d].visibility must be one of plain, secret, protected", i)
			}
		}
		changes = append(changes, c)
	}
	return changes, nil
}

func parseRemoveVariables(raw any) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("remove_variables must be an array of variable keys")
	}
	keys := make([]string, 0, len(items))
	for i, item := range items {
		key, ok := item.(string)
		if !ok || key == "" {
			return nil, fmt.Errorf("remove_variables[%d] must be a non-empty variable key", i)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// applyWorkspaceScriptChanges applies a partial update to the current state
// and returns the full variable list to PUT plus whether anything changed.
// Protected variables are sent with value "" unless a new value is given,
// which keeps their stored value.
func applyWorkspaceScriptChanges(current workspaceScriptState, set []variableChange, remove []string) ([]workspaceScriptVariable, bool, error) {
	changed := false
	index := map[string]int{}
	for i, v := range current.Variables {
		index[v.Key] = i
	}

	removed := map[string]bool{}
	for _, key := range remove {
		if _, ok := index[key]; !ok {
			return nil, false, fmt.Errorf("remove_variables: the workspace script has no variable %q", key)
		}
		removed[key] = true
		changed = true
	}

	// Start from the stored variables; a protected value cannot be read, so
	// "" is what keeps it.
	vars := make([]workspaceScriptVariable, 0, len(current.Variables)+len(set))
	for _, v := range current.Variables {
		if removed[v.Key] {
			continue
		}
		if v.Protected {
			v.Value = ""
		}
		vars = append(vars, v)
	}
	pos := map[string]int{}
	for i, v := range vars {
		pos[v.Key] = i
	}

	for _, c := range set {
		if removed[c.key] {
			return nil, false, fmt.Errorf("%q is in both set_variables and remove_variables", c.key)
		}
		i, exists := pos[c.key]
		if !exists {
			if !c.hasValue {
				return nil, false, fmt.Errorf("set_variables: %q is a new variable, so value is required", c.key)
			}
			visibility := c.visibility
			if visibility == "" {
				visibility = visibilityPlain
			}
			vars = append(vars, newWorkspaceScriptVariable(c.key, c.value, visibility))
			changed = true
			continue
		}

		old := vars[i]
		visibility := c.visibility
		if visibility == "" {
			visibility = old.visibility()
		}
		value := c.value
		if !c.hasValue {
			if old.Protected && visibility != visibilityProtected {
				return nil, false, fmt.Errorf("set_variables: %q is protected and its value cannot be read back, so changing its visibility needs a value", c.key)
			}
			value = old.Value // "" for a protected variable: keeps the stored value
		}
		next := newWorkspaceScriptVariable(c.key, value, visibility)
		switch {
		case visibility != old.visibility():
			changed = true
		case old.Protected:
			// The stored value is unknown; only a new value changes it.
			if value != "" {
				changed = true
			}
		case value != old.Value:
			changed = true
		}
		vars[i] = next
	}
	return vars, changed, nil
}

func newWorkspaceScriptVariable(key, value, visibility string) workspaceScriptVariable {
	return workspaceScriptVariable{
		Key:       key,
		Value:     value,
		IsSecret:  visibility != visibilityPlain,
		Protected: visibility == visibilityProtected,
	}
}

// UpdateWorkspaceScript changes the workspace script: a partial update done as
// read (with secrets) → apply the changes → write the full script back.
var UpdateWorkspaceScript = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_update_workspace_script",
		mcp.WithTitleAnnotation("Update workspace script"),
		mcp.WithDescription(`Change the workspace script. Only the given fields change: script replaces the whole script, enabled turns it on or off, set_variables adds or replaces variables by key, remove_variables deletes variables. Everything else is kept as stored (protected values included). Returns the saved state, with secret and protected values masked. A call that changes nothing is refused.

CAUTION — this affects every member of the workspace: saving changes the script that every session runs at its next boot, and enabling it makes it run on all sessions. Test first: save the change with the script disabled, create a test session (bitrise_devenv_create workspace_script_test=true — it runs the draft), read its System log (bitrise_devenv_get_logs stage "system") and ~/.rde/workspace.log, and only then set enabled=true. While the script is enabled, every save goes live for everyone.

`+workspaceScriptConceptDoc),
		mcp.WithString("script",
			mcp.Description("The new bash script; replaces the whole stored script. Omit to keep it."),
		),
		mcp.WithBoolean("enabled",
			mcp.Description("true: the script runs on every session boot in the workspace. false: it is a draft that runs only on test sessions (bitrise_devenv_create workspace_script_test=true). Omit to keep the current state."),
		),
		mcp.WithArray("set_variables",
			mcp.Description("Variables to add, or to replace by key. Others are kept."),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key":   map[string]any{"type": "string", "description": "Variable name, exported to the script's environment. Keys starting with RDE_ are reserved."},
					"value": map[string]any{"type": "string", "description": "The value. Required for a new variable. Omit on an existing variable to keep its value (e.g. to change only its visibility); a protected variable's value cannot be read back, so turning it into plain or secret needs the value."},
					"visibility": map[string]any{
						"type":        "string",
						"enum":        []string{visibilityPlain, visibilitySecret, visibilityProtected},
						"description": "plain: value readable by admins. secret: stored encrypted, readable with include_secrets=true. protected: stored encrypted and write-only, never returned. Omitted: an existing variable keeps its visibility, a new one is plain.",
					},
				},
				"required": []string{"key"},
			}),
		),
		mcp.WithArray("remove_variables",
			mcp.Description("Keys of the variables to delete."),
			mcp.WithStringItems(),
		),
		mcp.WithDestructiveHintAnnotation(true),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		var (
			set    []variableChange
			remove []string
			err    error
		)
		scriptRaw, hasScript := args["script"]
		script, ok := scriptRaw.(string)
		if hasScript && !ok {
			return mcp.NewToolResultError("script must be a string"), nil
		}
		enabledRaw, hasEnabled := args["enabled"]
		enabled, ok := enabledRaw.(bool)
		if hasEnabled && !ok {
			return mcp.NewToolResultError("enabled must be a boolean"), nil
		}
		if raw, present := args["set_variables"]; present {
			if set, err = parseSetVariables(raw); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		if raw, present := args["remove_variables"]; present {
			if remove, err = parseRemoveVariables(raw); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
		}
		if !hasScript && !hasEnabled && len(set) == 0 && len(remove) == 0 {
			return mcp.NewToolResultError("nothing to update — pass at least one of script, enabled, set_variables or remove_variables"), nil
		}

		// Read with secrets: the write is a full replacement, so every
		// secret value has to be sent back as it is.
		raw, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/workspace-script"),
			Params: map[string]string{"include_secrets": "true"},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("read workspace script", err), nil
		}
		current, err := parseWorkspaceScript(raw)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("read workspace script", err), nil
		}

		vars, changed, err := applyWorkspaceScriptChanges(current, set, remove)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		next := current
		if hasScript && script != current.Script {
			next.Script = script
			changed = true
		}
		if hasEnabled && enabled != current.Enabled {
			next.Enabled = enabled
			changed = true
		}
		if !changed {
			return mcp.NewToolResultError("nothing to update — the workspace script already has the given script, enabled state and variables"), nil
		}

		bodyVars := make([]map[string]any, 0, len(vars))
		for _, v := range vars {
			bodyVars = append(bodyVars, map[string]any{
				"key":       v.Key,
				"value":     v.Value,
				"is_secret": v.IsSecret,
				"protected": v.Protected,
			})
		}
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPut,
			Path:   devenv.WsPath(ctx, "/workspace-script"),
			Body: map[string]any{
				"enabled":   next.Enabled,
				"script":    next.Script,
				"variables": bodyVars,
			},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("update workspace script", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
