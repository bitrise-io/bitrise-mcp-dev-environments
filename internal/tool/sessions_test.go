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

// The nested object parameters on bitrise_devenv_create must tell the client
// which fields the backend cannot default; mcp-go only marks top-level
// parameters as required, so the nested lists are set by hand and easy to lose.
func TestCreateSessionNestedRequired(t *testing.T) {
	raw, err := json.Marshal(CreateSession.Definition.InputSchema)
	if err != nil {
		t.Fatalf("marshal input schema: %v", err)
	}
	var schema struct {
		Properties map[string]struct {
			Type     string   `json:"type"`
			Required []string `json:"required"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal input schema: %v", err)
	}
	for name, want := range map[string][]string{
		"device_spec": nil, // platform is not required on the wire: without a template the handler checks it; with one, omitting it is the per-field override
		"artifact":    {"url"},
	} {
		prop, ok := schema.Properties[name]
		if !ok {
			t.Fatalf("property %s missing from schema", name)
		}
		if prop.Type != "object" {
			t.Errorf("property %s: type %q, want object", name, prop.Type)
		}
		if !reflect.DeepEqual(prop.Required, want) {
			t.Errorf("property %s: required %v, want %v", name, prop.Required, want)
		}
	}
}

// A template-less device_spec without a platform or an artifact without a URL
// must be rejected before any API call is made.
func TestCreateSessionRejectsIncompleteNestedObjects(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"device_spec missing platform", map[string]any{"device_spec": map[string]any{"device_model": "iPhone 16"}}, "device_spec.platform is required"},
		{"device_spec empty platform", map[string]any{"device_spec": map[string]any{"platform": "  "}}, "device_spec.platform is required"},
		{"device_spec not an object", map[string]any{"device_spec": "ios"}, "device_spec must be an object"},
		{"artifact missing url", map[string]any{"device_spec": map[string]any{"platform": "ios"}, "artifact": map[string]any{"app_name": "x"}}, "artifact.url is required"},
		{"artifact empty url", map[string]any{"device_spec": map[string]any{"platform": "ios"}, "artifact": map[string]any{"url": ""}}, "artifact.url is required"},
		{"artifact without device_spec", map[string]any{"stack_id": "s", "machine_type": "m", "artifact": map[string]any{"url": "https://x"}}, "artifact requires a device"},
		{"artifact with template but no_device", map[string]any{"template_id": testTemplateID, "no_device": true, "artifact": map[string]any{"url": "https://x"}}, "artifact requires a device"},
		{"no_device with device_spec", map[string]any{"template_id": testTemplateID, "no_device": true, "device_spec": map[string]any{"platform": "ios"}}, "no_device cannot be combined with device_spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tc.args
			res, err := CreateSession.Handler(context.Background(), req)
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
			if !strings.Contains(text.Text, tc.want) {
				t.Errorf("error %q does not contain %q", text.Text, tc.want)
			}
		})
	}
}

// The happy path: a device session created with only name, device_spec and
// artifact forwards both nested objects verbatim and sends no stack_id /
// machine_type, leaving the platform defaults to the backend.
func TestCreateSessionForwardsDeviceSpecAndArtifact(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workspaces/ws/sessions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal body %q: %v", raw, err)
		}
		_, _ = w.Write([]byte(`{"session":{"id":"sess-1"}}`))
	}))
	defer srv.Close()
	old := devenv.BaseURL
	devenv.BaseURL = srv.URL
	defer func() { devenv.BaseURL = old }()

	deviceSpec := map[string]any{"platform": "ios", "device_model": "iPhone 16", "os_version": "18.2"}
	artifact := map[string]any{"url": "https://example.com/app.zip", "app_name": "Demo"}
	ctx := devenv.ContextWithWorkspace(devenv.ContextWithPAT(context.Background(), "pat"), "ws")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "agent ios", "device_spec": deviceSpec, "artifact": artifact}
	res, err := CreateSession.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %v", res.Content)
	}
	if got == nil {
		t.Fatalf("backend was never called")
	}
	if got["name"] != "agent ios" {
		t.Errorf("name = %v, want %q", got["name"], "agent ios")
	}
	if !reflect.DeepEqual(got["device_spec"], deviceSpec) {
		t.Errorf("device_spec = %v, want %v", got["device_spec"], deviceSpec)
	}
	if !reflect.DeepEqual(got["artifact"], artifact) {
		t.Errorf("artifact = %v, want %v", got["artifact"], artifact)
	}
	for _, key := range []string{"stack_id", "machine_type", "template_id", "no_device"} {
		if v, present := got[key]; present {
			t.Errorf("%s = %v present in body, want absent", key, v)
		}
	}
}

// Creating from a template that declares a device: no_device is forwarded
// only when set, and an artifact is allowed without a device_spec because the
// template's device is the one it lands on.
func TestCreateSessionFromTemplateDeviceOptions(t *testing.T) {
	const path = "/v1/workspaces/ws/sessions"

	t.Run("no_device", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		callOK(t, CreateSession, ctx, map[string]any{"name": "plain", "template_id": testTemplateID, "no_device": true})
		if *got == nil {
			t.Fatalf("backend was never called")
		}
		if (*got)["no_device"] != true {
			t.Errorf("no_device = %v, want true", (*got)["no_device"])
		}
		if v, present := (*got)["device_spec"]; present {
			t.Errorf("device_spec = %v present in body, want absent", v)
		}
	})
	t.Run("no_device false stays off the wire", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		callOK(t, CreateSession, ctx, map[string]any{"name": "inherit", "template_id": testTemplateID, "no_device": false})
		if v, present := (*got)["no_device"]; present {
			t.Errorf("no_device = %v present in body, want absent", v)
		}
	})
	t.Run("artifact rides the template's device", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		artifact := map[string]any{"url": "https://example.com/app.apk"}
		callOK(t, CreateSession, ctx, map[string]any{"name": "with build", "template_id": testTemplateID, "artifact": artifact})
		if !reflect.DeepEqual((*got)["artifact"], artifact) {
			t.Errorf("artifact = %v, want %v", (*got)["artifact"], artifact)
		}
		for _, key := range []string{"device_spec", "no_device"} {
			if v, present := (*got)[key]; present {
				t.Errorf("%s = %v present in body, want absent", key, v)
			}
		}
	})
	t.Run("platform-less device_spec is the per-field override and is forwarded", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		tweak := map[string]any{"device_model": "iPhone 15"}
		callOK(t, CreateSession, ctx, map[string]any{"name": "tweak", "template_id": testTemplateID, "device_spec": tweak})
		if !reflect.DeepEqual((*got)["device_spec"], tweak) {
			t.Errorf("device_spec = %v, want %v", (*got)["device_spec"], tweak)
		}
	})
	t.Run("device_spec override is forwarded as given", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		override := map[string]any{"platform": "android", "device_model": "pixel_8"}
		callOK(t, CreateSession, ctx, map[string]any{"name": "override", "template_id": testTemplateID, "device_spec": override})
		if !reflect.DeepEqual((*got)["device_spec"], override) {
			t.Errorf("device_spec = %v, want %v", (*got)["device_spec"], override)
		}
	})
}

const testSessionID = "5e4d3c2b-1a09-4f8e-8d7c-6b5a4f3e2d1c"

// callRequest builds a tool request with the given arguments.
func callRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// Run again: the hook rides in the body and only on_claim / on_restore /
// personalize pass — On create is refused before any request, in the
// backend's words; Rebuild: a bodyless POST on /rebuild. Both pass the
// backend JSON through.
func TestRunSessionHook(t *testing.T) {
	response := `{"run":{"id":"r-2","hook":"on_claim","attempt":2,"state":"HOOK_RUN_STATE_PENDING","triggered_by":"run_again"}}`
	ctx, got := captureRequest(t, response)
	text := callText(t, RunSessionHook, ctx, map[string]any{"session_id": testSessionID, "hook": "on_claim"})
	assertRequest(t, got, http.MethodPost, "/v1/workspaces/ws/sessions/"+testSessionID+"/hook-runs")
	if got.Body["hook"] != "on_claim" {
		t.Errorf("body hook = %v, want on_claim", got.Body["hook"])
	}
	if text != response {
		t.Errorf("result = %q, want the backend response verbatim", text)
	}
	t.Run("refuses On create and points at rebuild", func(t *testing.T) {
		ctx, got := captureRequest(t, response)
		res, err := RunSessionHook.Handler(ctx, callRequest(map[string]any{"session_id": testSessionID, "hook": "on_create"}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected a tool error for on_create")
		}
		text := res.Content[0].(mcp.TextContent).Text
		for _, want := range []string{"On create cannot be run again on this machine. Use Rebuild to run it on a fresh one.", "bitrise_devenv_rebuild"} {
			if !strings.Contains(text, want) {
				t.Errorf("error %q should contain %q", text, want)
			}
		}
		if got.Called {
			t.Errorf("backend was called (%s) for a refused hook", got.Path)
		}
	})
	t.Run("the schema no longer offers on_create", func(t *testing.T) {
		raw, err := json.Marshal(RunSessionHook.Definition.InputSchema)
		if err != nil {
			t.Fatalf("marshal input schema: %v", err)
		}
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal input schema: %v", err)
		}
		if got := schema.Properties["hook"].Enum; strings.Join(got, ",") != "on_claim,on_restore,personalize" {
			t.Errorf("hook enum = %v, want on_claim, on_restore, personalize", got)
		}
	})
	t.Run("rejects an unknown hook", func(t *testing.T) {
		ctx, got := captureRequest(t, response)
		res, err := RunSessionHook.Handler(ctx, callRequest(map[string]any{"session_id": testSessionID, "hook": "on_boot"}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected a tool error for an unknown hook")
		}
		if got.Path != "" {
			t.Errorf("backend was called (%s) for an invalid request", got.Path)
		}
	})
}

func TestRebuildSession(t *testing.T) {
	response := `{"session":{"id":"` + testSessionID + `","name":"dev","status":"SESSION_STATUS_STARTING"}}`
	ctx, got := captureRequest(t, response)
	text := callText(t, RebuildSession, ctx, map[string]any{"session_id": testSessionID})
	assertRequest(t, got, http.MethodPost, "/v1/workspaces/ws/sessions/"+testSessionID+"/rebuild")
	if text != response {
		t.Errorf("result = %q, want the backend response verbatim", text)
	}
}

// A claim may now carry the personal inputs: they reach the body alongside
// warm_pool_id, while an environment field is still refused locally.
func TestCreateSession_ClaimCarriesPersonalInputs(t *testing.T) {
	response := `{"session":{"id":"` + testSessionID + `","name":"dev","warm_state":"claimed"}}`
	ctx, got := captureRequest(t, response)
	callText(t, CreateSession, ctx, map[string]any{
		"name":                        "dev",
		"warm_pool_id":                testWarmPoolID,
		"session_inputs":              []any{map[string]any{"key": "GIT_TOKEN", "value": "t", "hook": "on_claim"}, map[string]any{"key": "GIT_TOKEN", "value": "r", "hook": "on_restore"}},
		"map_saved_to_session_inputs": true,
		"ai_prompt":                   "hello",
	})
	assertRequest(t, got, http.MethodPost, "/v1/workspaces/ws/sessions")
	if got.Body["warm_pool_id"] != testWarmPoolID {
		t.Errorf("body warm_pool_id = %v", got.Body["warm_pool_id"])
	}
	// The values reach the backend as given, scope included: the same key
	// on two scopes is two entries, not one.
	wantInputs := []any{map[string]any{"key": "GIT_TOKEN", "value": "t", "hook": "on_claim"}, map[string]any{"key": "GIT_TOKEN", "value": "r", "hook": "on_restore"}}
	if !reflect.DeepEqual(got.Body["session_inputs"], wantInputs) {
		t.Errorf("session_inputs = %v, want %v (they must reach the backend on a claim, which restricts them to on_claim and on_restore keys)", got.Body["session_inputs"], wantInputs)
	}
	if got.Body["map_saved_to_session_inputs"] != true || got.Body["ai_prompt"] != "hello" {
		t.Errorf("personal fields missing from the claim body: %v", got.Body)
	}
	t.Run("an environment field is still refused", func(t *testing.T) {
		ctx, got := captureRequest(t, response)
		res, err := CreateSession.Handler(ctx, callRequest(map[string]any{"name": "dev", "warm_pool_id": testWarmPoolID, "stack_id": "osx-27"}))
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected a tool error for stack_id with warm_pool_id")
		}
		if got.Path != "" {
			t.Errorf("backend was called (%s) for an invalid request", got.Path)
		}
	})
}
