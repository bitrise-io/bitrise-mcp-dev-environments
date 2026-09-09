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
		"device_spec": {"platform"},
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

// A device_spec without a platform, an artifact without a URL, or no_device
// alongside a device_spec must be rejected before any API call is made.
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
		{"artifact without device_spec", map[string]any{"stack_id": "s", "machine_type": "m", "artifact": map[string]any{"url": "https://x"}}, "artifact requires device_spec"},
		{"no_device with device_spec", map[string]any{"template_id": "t", "no_device": true, "device_spec": map[string]any{"platform": "android"}}, "no_device and device_spec are mutually exclusive"},
		{"no_device false with device_spec", map[string]any{"template_id": "t", "no_device": false, "device_spec": map[string]any{"platform": "android"}}, "no_device and device_spec are mutually exclusive"},
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

// no_device is forwarded to the backend verbatim when supplied and left out of
// the body otherwise, so the backend's own default applies.
func TestCreateSessionForwardsNoDevice(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want any // nil = key must be absent
	}{
		{"omitted", map[string]any{"name": "s", "template_id": "t"}, nil},
		{"true", map[string]any{"name": "s", "template_id": "t", "no_device": true}, true},
		{"false", map[string]any{"name": "s", "template_id": "t", "no_device": false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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

			ctx := devenv.ContextWithWorkspace(devenv.ContextWithPAT(context.Background(), "pat"), "ws")
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tc.args
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
			v, present := got["no_device"]
			if tc.want == nil {
				if present {
					t.Errorf("no_device %v present in body, want absent", v)
				}
				return
			}
			if !present || v != tc.want {
				t.Errorf("no_device = %v (present=%v), want %v", v, present, tc.want)
			}
		})
	}
}
