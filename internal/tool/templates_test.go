package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

const testTemplateID = "0f1e2d3c-4b5a-4978-8877-665544332211"

// captureBody points the API client at a fake backend for the duration of the
// test and returns a context authenticated against workspace "ws" plus a
// pointer that holds the JSON body of the first request matching method and
// path (nil until the backend is called).
func captureBody(t *testing.T, method, path string) (context.Context, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || r.URL.Path != path {
			t.Errorf("unexpected request %s %s, want %s %s", r.Method, r.URL.Path, method, path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal body %q: %v", raw, err)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	old := devenv.BaseURL
	devenv.BaseURL = srv.URL
	t.Cleanup(func() { devenv.BaseURL = old })
	ctx := devenv.ContextWithWorkspace(devenv.ContextWithPAT(context.Background(), "pat"), "ws")
	return ctx, &got
}

// callOK runs the tool handler and fails the test unless it returns a
// successful result.
func callOK(t *testing.T, tool devenv.Tool, ctx context.Context, args map[string]any) {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
}

// callErr runs the tool handler and returns the text of the tool error it
// produced, failing the test if it succeeded instead.
func callErr(t *testing.T, tool devenv.Tool, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error result, got success")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want TextContent", res.Content[0])
	}
	return text.Text
}

// The device_spec object is declared on every tool that accepts one through
// the same helper, so its properties must match bitrise_devenv_create's. The
// advisory platform requirement differs on purpose: a template's device is a
// complete device (platform required), a session's device_spec may omit the
// platform to tweak the template's device per field.
func TestTemplateToolsShareDeviceSpecSchema(t *testing.T) {
	type prop struct {
		Type       string         `json:"type"`
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	schemaOf := func(tool devenv.Tool) prop {
		raw, err := json.Marshal(tool.Definition.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		var schema struct {
			Properties map[string]prop `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		p, ok := schema.Properties["device_spec"]
		if !ok {
			t.Fatalf("%s: device_spec missing from schema", tool.Definition.Name)
		}
		return p
	}
	want := schemaOf(CreateSession)
	if want.Type != "object" || want.Required != nil {
		t.Fatalf("bitrise_devenv_create device_spec: type %q required %v (a session's platform must stay optional for template overrides)", want.Type, want.Required)
	}
	for _, tool := range []devenv.Tool{CreateTemplate, UpdateTemplate} {
		got := schemaOf(tool)
		if got.Type != want.Type || !reflect.DeepEqual(got.Properties, want.Properties) {
			t.Errorf("%s: device_spec schema differs from bitrise_devenv_create's", tool.Definition.Name)
		}
		if !reflect.DeepEqual(got.Required, []string{"platform"}) {
			t.Errorf("%s: device_spec required %v, want [platform] — a template's device is a complete device", tool.Definition.Name, got.Required)
		}
	}
}

// A template created with a device_spec forwards it verbatim; one created
// without sends no device_spec key at all (absent = the template declares no
// device).
func TestCreateTemplateForwardsDeviceSpec(t *testing.T) {
	deviceSpec := map[string]any{"platform": "android", "device_model": "pixel_7", "cold_boot": true}
	base := map[string]any{"name": "android lab", "stack_id": "ubuntu-android", "machine_type": "g2.linux.large"}

	t.Run("with device", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, "/v1/workspaces/ws/templates")
		args := map[string]any{"device_spec": deviceSpec}
		for k, v := range base {
			args[k] = v
		}
		callOK(t, CreateTemplate, ctx, args)
		if *got == nil {
			t.Fatalf("backend was never called")
		}
		if !reflect.DeepEqual((*got)["device_spec"], deviceSpec) {
			t.Errorf("device_spec = %v, want %v", (*got)["device_spec"], deviceSpec)
		}
	})
	t.Run("without device", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, "/v1/workspaces/ws/templates")
		callOK(t, CreateTemplate, ctx, base)
		if v, present := (*got)["device_spec"]; present {
			t.Errorf("device_spec = %v present in body, want absent", v)
		}
	})
	t.Run("missing platform is rejected before the API call", func(t *testing.T) {
		args := map[string]any{"device_spec": map[string]any{"device_model": "pixel_7"}}
		for k, v := range base {
			args[k] = v
		}
		if msg := callErr(t, CreateTemplate, args); !strings.Contains(msg, "device_spec.platform is required") {
			t.Errorf("error %q does not name device_spec.platform", msg)
		}
	})
}

// bitrise_devenv_update_template mirrors the array-field convention for the
// device: device_spec sets update_device_spec alongside the new object,
// clear_device_spec sends the flag alone, and omitting both leaves the wire
// free of either key.
func TestUpdateTemplateDeviceSpecFlag(t *testing.T) {
	path := "/v1/workspaces/ws/templates/" + testTemplateID
	deviceSpec := map[string]any{"platform": "ios", "device_model": "iPhone 16", "os_version": "18.2"}

	t.Run("set", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPatch, path)
		callOK(t, UpdateTemplate, ctx, map[string]any{"template_id": testTemplateID, "device_spec": deviceSpec})
		if *got == nil {
			t.Fatalf("backend was never called")
		}
		if !reflect.DeepEqual((*got)["device_spec"], deviceSpec) {
			t.Errorf("device_spec = %v, want %v", (*got)["device_spec"], deviceSpec)
		}
		if (*got)["update_device_spec"] != true {
			t.Errorf("update_device_spec = %v, want true", (*got)["update_device_spec"])
		}
	})
	t.Run("clear", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPatch, path)
		callOK(t, UpdateTemplate, ctx, map[string]any{"template_id": testTemplateID, "clear_device_spec": true, "name": "renamed"})
		if (*got)["update_device_spec"] != true {
			t.Errorf("update_device_spec = %v, want true", (*got)["update_device_spec"])
		}
		if v, present := (*got)["device_spec"]; present {
			t.Errorf("device_spec = %v present in body, want absent so the backend clears the device", v)
		}
		if (*got)["name"] != "renamed" {
			t.Errorf("name = %v, want %q", (*got)["name"], "renamed")
		}
	})
	t.Run("untouched", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPatch, path)
		callOK(t, UpdateTemplate, ctx, map[string]any{"template_id": testTemplateID, "name": "renamed"})
		for _, key := range []string{"device_spec", "update_device_spec"} {
			if v, present := (*got)[key]; present {
				t.Errorf("%s = %v present in body, want absent", key, v)
			}
		}
	})
	t.Run("set and clear together are rejected", func(t *testing.T) {
		args := map[string]any{"template_id": testTemplateID, "device_spec": deviceSpec, "clear_device_spec": true}
		if msg := callErr(t, UpdateTemplate, args); !strings.Contains(msg, "clear_device_spec cannot be combined with device_spec") {
			t.Errorf("unexpected error %q", msg)
		}
	})
	t.Run("missing platform is rejected before the API call", func(t *testing.T) {
		args := map[string]any{"template_id": testTemplateID, "device_spec": map[string]any{"os_version": "18.2"}}
		if msg := callErr(t, UpdateTemplate, args); !strings.Contains(msg, "device_spec.platform is required") {
			t.Errorf("error %q does not name device_spec.platform", msg)
		}
	})
}

func TestCreateTemplate_ForwardsHooksAndInputHook(t *testing.T) {
	ctx, got := captureBody(t, http.MethodPost, "/v1/workspaces/ws/templates")
	callOK(t, CreateTemplate, ctx, map[string]any{
		"name":         "Hooks",
		"stack_id":     "linux-ubuntu-24.04",
		"machine_type": "standard",
		"hooks":        map[string]any{"on_create": "echo create", "on_claim": "echo claim"},
		"session_inputs": []any{
			map[string]any{"key": "GH_TOKEN", "required": true, "hook": "on_claim"},
		},
	})
	hooks, _ := (*got)["hooks"].(map[string]any)
	if hooks["on_create"] != "echo create" || hooks["on_claim"] != "echo claim" {
		t.Errorf("hooks = %v, want on_create/on_claim forwarded as given", (*got)["hooks"])
	}
	if _, ok := (*got)["update_hooks"]; ok {
		t.Errorf("create must not send update_hooks, body = %v", *got)
	}
	inputs, _ := (*got)["session_inputs"].([]any)
	if len(inputs) != 1 {
		t.Fatalf("session_inputs = %v, want one entry", (*got)["session_inputs"])
	}
	if first, _ := inputs[0].(map[string]any); first["hook"] != "on_claim" {
		t.Errorf("session_inputs[0].hook = %v, want on_claim", first["hook"])
	}
}

func TestUpdateTemplate_HooksCarryReplaceFlag(t *testing.T) {
	ctx, got := captureBody(t, http.MethodPatch, "/v1/workspaces/ws/templates/"+testTemplateID)
	callOK(t, UpdateTemplate, ctx, map[string]any{
		"template_id": testTemplateID,
		"hooks":       map[string]any{"on_create": "echo v2"},
	})
	if (*got)["update_hooks"] != true {
		t.Errorf("update_hooks = %v, want true (body = %v)", (*got)["update_hooks"], *got)
	}
	if hooks, _ := (*got)["hooks"].(map[string]any); hooks["on_create"] != "echo v2" {
		t.Errorf("hooks = %v, want the given object", (*got)["hooks"])
	}
	// Only the hooks were given: no other replace flag may ride along.
	for _, flag := range []string{"update_session_inputs", "update_template_variables", "update_device_spec"} {
		if _, ok := (*got)[flag]; ok {
			t.Errorf("%s should be absent, body = %v", flag, *got)
		}
	}
}

func TestUpdateTemplate_NoHooksNoReplaceFlag(t *testing.T) {
	ctx, got := captureBody(t, http.MethodPatch, "/v1/workspaces/ws/templates/"+testTemplateID)
	callOK(t, UpdateTemplate, ctx, map[string]any{"template_id": testTemplateID, "name": "Renamed"})
	if _, ok := (*got)["update_hooks"]; ok {
		t.Errorf("update_hooks must not be sent when hooks is omitted, body = %v", *got)
	}
	if _, ok := (*got)["hooks"]; ok {
		t.Errorf("hooks must not be sent when omitted, body = %v", *got)
	}
}

// The create and update tools declare the same hooks object and the same
// session input item (with its hook enum), so an agent sees one contract.
func TestTemplateToolsShareHooksAndInputSchemas(t *testing.T) {
	type schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	schemaOf := func(tool devenv.Tool) schema {
		raw, err := json.Marshal(tool.Definition.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		var out schema
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		return out
	}
	create, update := schemaOf(CreateTemplate), schemaOf(UpdateTemplate)
	for _, key := range []string{"hooks", "session_inputs"} {
		if _, ok := create.Properties[key]; !ok {
			t.Fatalf("bitrise_devenv_create_template: %s missing from schema", key)
		}
		if _, ok := update.Properties[key]; !ok {
			t.Fatalf("bitrise_devenv_update_template: %s missing from schema", key)
		}
	}
	var hooks struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(create.Properties["hooks"], &hooks); err != nil {
		t.Fatalf("unmarshal hooks schema: %v", err)
	}
	for _, hook := range []string{"on_create", "on_claim", "on_restore"} {
		if _, ok := hooks.Properties[hook]; !ok {
			t.Errorf("hooks schema lacks %s", hook)
		}
	}
	var inputs struct {
		Items struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(update.Properties["session_inputs"], &inputs); err != nil {
		t.Fatalf("unmarshal session_inputs schema: %v", err)
	}
	if !reflect.DeepEqual(inputs.Items.Properties["hook"].Enum, []string{"shared", "on_create", "on_claim", "on_restore"}) {
		t.Errorf("session_inputs[].hook enum = %v, want [shared on_create on_claim on_restore]", inputs.Items.Properties["hook"].Enum)
	}
	// The two tools describe the array differently (create vs replace) but
	// must agree on what one input looks like.
	itemsOf := func(raw json.RawMessage) string {
		var arr struct {
			Items json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(raw, &arr); err != nil {
			t.Fatalf("unmarshal session_inputs schema: %v", err)
		}
		return string(arr.Items)
	}
	if itemsOf(create.Properties["session_inputs"]) != itemsOf(update.Properties["session_inputs"]) {
		t.Errorf("create and update session_inputs item schemas differ")
	}
}

// A variable's scope (hook) and protected ride along on create and on
// update, next to the legacy keys (an entry without hook is shared on the
// server); the item schema is shared by both tools and no longer knows
// on_create_only.
func TestTemplateTools_ScopedAndProtectedVariables(t *testing.T) {
	ctx, got := captureBody(t, http.MethodPost, "/v1/workspaces/ws/templates")
	callOK(t, CreateTemplate, ctx, map[string]any{
		"name": "Hooks", "stack_id": "linux-ubuntu-24.04", "machine_type": "standard",
		"hooks": map[string]any{"on_create": "echo create"},
		"template_variables": []any{
			map[string]any{"key": "REGISTRY_TOKEN", "value": "tok", "hook": "on_create", "protected": true},
			map[string]any{"key": "APP", "value": "demo", "expose_as_env_var": true},
			map[string]any{"key": "OLD", "value": "x", "is_secret": true},
		},
		"session_inputs": []any{
			map[string]any{"key": "GH_TOKEN", "required": true, "hook": "on_claim"},
			map[string]any{"key": "GH_TOKEN", "required": true, "hook": "on_restore"},
		},
	})
	vars, _ := (*got)["template_variables"].([]any)
	if len(vars) != 3 {
		t.Fatalf("template_variables = %v, want 3 entries", (*got)["template_variables"])
	}
	if v, _ := vars[0].(map[string]any); v["protected"] != true || v["hook"] != "on_create" || v["value"] != "tok" {
		t.Errorf("template_variables[0] = %v, want hook + protected forwarded with the value", vars[0])
	}
	if v, _ := vars[1].(map[string]any); v["expose_as_env_var"] != true || v["hook"] != nil || v["protected"] != nil {
		t.Errorf("template_variables[1] = %v, want the legacy keys only (shared on the server)", vars[1])
	}
	if v, _ := vars[2].(map[string]any); v["is_secret"] != true {
		t.Errorf("template_variables[2] = %v, want is_secret", vars[2])
	}
	inputs, _ := (*got)["session_inputs"].([]any)
	if len(inputs) != 2 {
		t.Fatalf("session_inputs = %v, want the same key on two scopes", (*got)["session_inputs"])
	}
	if in, _ := inputs[1].(map[string]any); in["hook"] != "on_restore" {
		t.Errorf("session_inputs[1] = %v, want hook on_restore", inputs[1])
	}

	ctx, got = captureBody(t, http.MethodPatch, "/v1/workspaces/ws/templates/"+testTemplateID)
	callOK(t, UpdateTemplate, ctx, map[string]any{
		"template_id":        testTemplateID,
		"template_variables": []any{map[string]any{"key": "REGISTRY_TOKEN", "value": "", "hook": "on_create", "protected": true}},
	})
	if (*got)["update_template_variables"] != true {
		t.Errorf("update_template_variables = %v, want true", (*got)["update_template_variables"])
	}
	vars, _ = (*got)["template_variables"].([]any)
	if v, _ := vars[0].(map[string]any); v["protected"] != true || v["hook"] != "on_create" {
		t.Errorf("template_variables[0] = %v, want protected + hook on update (an empty value keeps the stored one)", vars[0])
	}

	type schema struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	for _, tool := range []devenv.Tool{CreateTemplate, UpdateTemplate} {
		raw, err := json.Marshal(tool.Definition.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		var out schema
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		props := out.Properties["template_variables"].Items.Properties
		for _, key := range []string{"key", "value", "is_secret", "protected", "expose_as_env_var", "hook"} {
			if _, ok := props[key]; !ok {
				t.Errorf("%s: template_variables item lacks %s", tool.Definition.Name, key)
			}
		}
		if _, ok := props["on_create_only"]; ok {
			t.Errorf("%s: template_variables item still carries on_create_only", tool.Definition.Name)
		}
		if got := props["hook"].Enum; !reflect.DeepEqual(got, []string{"shared", "on_create", "on_claim", "on_restore"}) {
			t.Errorf("%s: template_variables[].hook enum = %v, want [shared on_create on_claim on_restore]", tool.Definition.Name, got)
		}
	}
}
