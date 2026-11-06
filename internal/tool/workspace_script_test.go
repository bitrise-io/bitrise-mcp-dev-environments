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

const workspaceScriptPath = "/v1/workspaces/ws/workspace-script"

// captureRequests points the API client at a fake backend that answers each
// request with responses[method] and records every request in order.
func captureRequests(t *testing.T, responses map[string]string) (context.Context, *[]capturedRequest) {
	t.Helper()
	var got []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := capturedRequest{Called: true, Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &req.Body); err != nil {
				t.Fatalf("unmarshal body %q: %v", raw, err)
			}
		}
		got = append(got, req)
		response, ok := responses[r.Method]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	old := devenv.BaseURL
	devenv.BaseURL = srv.URL
	t.Cleanup(func() { devenv.BaseURL = old })
	ctx := devenv.ContextWithWorkspace(devenv.ContextWithPAT(context.Background(), "pat"), "ws")
	return ctx, &got
}

// callToolErr runs the tool handler against ctx and returns the text of the
// tool error it must produce.
func callToolErr(t *testing.T, tool devenv.Tool, ctx context.Context, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := tool.Handler(ctx, req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error result, got success")
	}
	return res.Content[0].(mcp.TextContent).Text
}

// storedScript is the include_secrets=true read the update starts from: one
// variable of each visibility (the protected value is never returned).
const storedScript = `{"workspace_script":{"configured":true,"enabled":true,"script":"echo hi\n","variables":[` +
	`{"key":"REGION","value":"eu","is_secret":false,"protected":false},` +
	`{"key":"API_TOKEN","value":"tok-123","is_secret":true,"protected":false},` +
	`{"key":"SIGNING_KEY","value":"","is_secret":true,"protected":true,"value_stored":true}` +
	`],"updated_by":"someone@example.com","updated_at":"2026-11-01T10:00:00Z"}}`

const savedResponse = `{"workspace_script":{"configured":true,"enabled":true}}`

func plainVar(key, value string) map[string]any {
	return map[string]any{"key": key, "value": value, "is_secret": false, "protected": false}
}

func secretVar(key, value string) map[string]any {
	return map[string]any{"key": key, "value": value, "is_secret": true, "protected": false}
}

func protectedVar(key, value string) map[string]any {
	return map[string]any{"key": key, "value": value, "is_secret": true, "protected": true}
}

func TestWorkspaceScriptToolsRegistered(t *testing.T) {
	b := NewBelt()
	names := toolNames(b)
	for _, name := range []string{"bitrise_devenv_get_workspace_script", "bitrise_devenv_update_workspace_script"} {
		if !names[name] {
			t.Errorf("%s is not registered in the belt", name)
		}
		if b.userScoped[name] {
			t.Errorf("%s is classified user-scoped, want workspace-scoped", name)
		}
	}
}

func TestGetWorkspaceScript(t *testing.T) {
	t.Run("masked by default", func(t *testing.T) {
		ctx, got := captureRequest(t, storedScript)
		text := callText(t, GetWorkspaceScript, ctx, map[string]any{})
		assertRequest(t, got, http.MethodGet, workspaceScriptPath)
		if len(got.Query) != 0 {
			t.Errorf("query = %v, want none", got.Query)
		}
		if text != storedScript {
			t.Errorf("result = %q, want the backend response verbatim", text)
		}
	})
	t.Run("include_secrets is passed through", func(t *testing.T) {
		ctx, got := captureRequest(t, storedScript)
		callText(t, GetWorkspaceScript, ctx, map[string]any{"include_secrets": true})
		assertRequest(t, got, http.MethodGet, workspaceScriptPath)
		want := map[string][]string{"include_secrets": {"true"}}
		if !reflect.DeepEqual(got.Query, want) {
			t.Errorf("query = %v, want %v", got.Query, want)
		}
	})
}

// Every update reads the script with secrets, applies only the given
// changes and PUTs the full state back.
func TestUpdateWorkspaceScript(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want map[string]any
	}{
		{
			name: "script only keeps enabled and variables",
			args: map[string]any{"script": "echo new\n"},
			want: map[string]any{
				"enabled": true,
				"script":  "echo new\n",
				"variables": []any{
					plainVar("REGION", "eu"),
					secretVar("API_TOKEN", "tok-123"),
					protectedVar("SIGNING_KEY", ""),
				},
			},
		},
		{
			name: "disable keeps script and variables",
			args: map[string]any{"enabled": false},
			want: map[string]any{
				"enabled": false,
				"script":  "echo hi\n",
				"variables": []any{
					plainVar("REGION", "eu"),
					secretVar("API_TOKEN", "tok-123"),
					protectedVar("SIGNING_KEY", ""),
				},
			},
		},
		{
			name: "set replaces in place and appends, remove deletes",
			args: map[string]any{
				"set_variables": []any{
					map[string]any{"key": "REGION", "value": "us", "visibility": "plain"},
					map[string]any{"key": "NEW_SECRET", "value": "s3cret", "visibility": "secret"},
					map[string]any{"key": "NEW_PROTECTED", "value": "p", "visibility": "protected"},
					map[string]any{"key": "NEW_PLAIN", "value": "x"},
				},
				"remove_variables": []any{"API_TOKEN"},
			},
			want: map[string]any{
				"enabled": true,
				"script":  "echo hi\n",
				"variables": []any{
					plainVar("REGION", "us"),
					protectedVar("SIGNING_KEY", ""),
					secretVar("NEW_SECRET", "s3cret"),
					protectedVar("NEW_PROTECTED", "p"),
					plainVar("NEW_PLAIN", "x"),
				},
			},
		},
		{
			name: "new value for a protected variable is sent",
			args: map[string]any{"set_variables": []any{map[string]any{"key": "SIGNING_KEY", "value": "rotated"}}},
			want: map[string]any{
				"enabled": true,
				"script":  "echo hi\n",
				"variables": []any{
					plainVar("REGION", "eu"),
					secretVar("API_TOKEN", "tok-123"),
					protectedVar("SIGNING_KEY", "rotated"),
				},
			},
		},
		{
			name: "visibility change keeps the value when omitted",
			args: map[string]any{"set_variables": []any{
				map[string]any{"key": "REGION", "visibility": "secret"},
				map[string]any{"key": "API_TOKEN", "visibility": "protected"},
			}},
			want: map[string]any{
				"enabled": true,
				"script":  "echo hi\n",
				"variables": []any{
					secretVar("REGION", "eu"),
					protectedVar("API_TOKEN", "tok-123"),
					protectedVar("SIGNING_KEY", ""),
				},
			},
		},
		{
			name: "protected to plain with a value",
			args: map[string]any{"set_variables": []any{map[string]any{"key": "SIGNING_KEY", "value": "now-visible", "visibility": "plain"}}},
			want: map[string]any{
				"enabled": true,
				"script":  "echo hi\n",
				"variables": []any{
					plainVar("REGION", "eu"),
					secretVar("API_TOKEN", "tok-123"),
					plainVar("SIGNING_KEY", "now-visible"),
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, got := captureRequests(t, map[string]string{http.MethodGet: storedScript, http.MethodPut: savedResponse})
			text := callText(t, UpdateWorkspaceScript, ctx, tc.args)
			if len(*got) != 2 {
				t.Fatalf("backend saw %d requests, want GET then PUT", len(*got))
			}
			read, write := (*got)[0], (*got)[1]
			assertRequest(t, &read, http.MethodGet, workspaceScriptPath)
			if !reflect.DeepEqual(read.Query, map[string][]string{"include_secrets": {"true"}}) {
				t.Errorf("read query = %v, want include_secrets=true", read.Query)
			}
			assertRequest(t, &write, http.MethodPut, workspaceScriptPath)
			if !reflect.DeepEqual(write.Body, tc.want) {
				gotJSON, _ := json.Marshal(write.Body)
				wantJSON, _ := json.Marshal(tc.want)
				t.Errorf("PUT body =\n%s\nwant\n%s", gotJSON, wantJSON)
			}
			if text != savedResponse {
				t.Errorf("result = %q, want the PUT response verbatim", text)
			}
		})
	}
}

// An unconfigured script (fields omitted) starts from an empty, disabled
// state, and camelCase field names are understood too.
func TestUpdateWorkspaceScriptFromUnconfigured(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		ctx, got := captureRequests(t, map[string]string{
			http.MethodGet: `{"workspace_script":{"configured":false}}`,
			http.MethodPut: savedResponse,
		})
		callText(t, UpdateWorkspaceScript, ctx, map[string]any{"script": "echo first\n"})
		want := map[string]any{"enabled": false, "script": "echo first\n", "variables": []any{}}
		if len(*got) != 2 || !reflect.DeepEqual((*got)[1].Body, want) {
			t.Errorf("requests = %+v, want a PUT with %v", *got, want)
		}
	})
	t.Run("camelCase", func(t *testing.T) {
		ctx, got := captureRequests(t, map[string]string{
			http.MethodGet: `{"workspaceScript":{"enabled":true,"script":"x","variables":[{"key":"A","value":"v","isSecret":true}]}}`,
			http.MethodPut: savedResponse,
		})
		callText(t, UpdateWorkspaceScript, ctx, map[string]any{"script": "y"})
		want := map[string]any{"enabled": true, "script": "y", "variables": []any{secretVar("A", "v")}}
		if len(*got) != 2 || !reflect.DeepEqual((*got)[1].Body, want) {
			t.Errorf("requests = %+v, want a PUT with %v", *got, want)
		}
	})
}

// Calls that would change nothing, or are certain to be rejected, never
// reach the PUT.
func TestUpdateWorkspaceScriptRefusals(t *testing.T) {
	cases := []struct {
		name    string
		args    map[string]any
		wantErr string
		wantGet bool
	}{
		{name: "no fields", args: map[string]any{}, wantErr: "nothing to update"},
		{name: "same script", args: map[string]any{"script": "echo hi\n"}, wantErr: "nothing to update", wantGet: true},
		{name: "same enabled", args: map[string]any{"enabled": true}, wantErr: "nothing to update", wantGet: true},
		{
			name:    "same variable values",
			args:    map[string]any{"set_variables": []any{map[string]any{"key": "REGION", "value": "eu"}, map[string]any{"key": "API_TOKEN", "value": "tok-123", "visibility": "secret"}}},
			wantErr: "nothing to update", wantGet: true,
		},
		{
			name:    "protected resent without value",
			args:    map[string]any{"set_variables": []any{map[string]any{"key": "SIGNING_KEY", "visibility": "protected"}}},
			wantErr: "nothing to update", wantGet: true,
		},
		{name: "remove unknown key", args: map[string]any{"remove_variables": []any{"NOPE"}}, wantErr: `no variable "NOPE"`, wantGet: true},
		{
			name:    "new variable without value",
			args:    map[string]any{"set_variables": []any{map[string]any{"key": "NEW"}}},
			wantErr: "value is required", wantGet: true,
		},
		{
			name:    "protected to secret without value",
			args:    map[string]any{"set_variables": []any{map[string]any{"key": "SIGNING_KEY", "visibility": "secret"}}},
			wantErr: "cannot be read back", wantGet: true,
		},
		{
			name: "set and remove the same key",
			args: map[string]any{
				"set_variables":    []any{map[string]any{"key": "REGION", "value": "us"}},
				"remove_variables": []any{"REGION"},
			},
			wantErr: "both set_variables and remove_variables", wantGet: true,
		},
		{name: "reserved key", args: map[string]any{"set_variables": []any{map[string]any{"key": "RDE_OS", "value": "x"}}}, wantErr: "reserved"},
		{name: "bad visibility", args: map[string]any{"set_variables": []any{map[string]any{"key": "A", "value": "x", "visibility": "hidden"}}}, wantErr: "visibility must be one of"},
		{name: "duplicate key", args: map[string]any{"set_variables": []any{map[string]any{"key": "A", "value": "1"}, map[string]any{"key": "A", "value": "2"}}}, wantErr: "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, got := captureRequests(t, map[string]string{http.MethodGet: storedScript})
			msg := callToolErr(t, UpdateWorkspaceScript, ctx, tc.args)
			if !strings.Contains(msg, tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantErr)
			}
			for _, r := range *got {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected %s request", r.Method)
				}
			}
			if gotGet := len(*got) > 0; gotGet != tc.wantGet {
				t.Errorf("read happened = %v, want %v", gotGet, tc.wantGet)
			}
		})
	}
}

func TestCreateSessionWorkspaceScriptTest(t *testing.T) {
	const path = "/v1/workspaces/ws/sessions"

	t.Run("true is forwarded", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		callOK(t, CreateSession, ctx, map[string]any{"name": "ws test", "template_id": testTemplateID, "workspace_script_test": true})
		if (*got)["workspace_script_test"] != true {
			t.Errorf("workspace_script_test = %v, want true", (*got)["workspace_script_test"])
		}
	})
	t.Run("false stays off the wire", func(t *testing.T) {
		ctx, got := captureBody(t, http.MethodPost, path)
		callOK(t, CreateSession, ctx, map[string]any{"name": "plain", "template_id": testTemplateID, "workspace_script_test": false})
		if v, present := (*got)["workspace_script_test"]; present {
			t.Errorf("workspace_script_test = %v present in body, want absent", v)
		}
	})
	t.Run("refused with warm_pool_id", func(t *testing.T) {
		msg := callErr(t, CreateSession, map[string]any{"name": "x", "warm_pool_id": testWarmPoolID, "workspace_script_test": true})
		if !strings.Contains(msg, "warm_pool_id") {
			t.Errorf("unexpected error %q", msg)
		}
	})
	t.Run("refused on a workspace-owned session", func(t *testing.T) {
		msg := callErr(t, CreateSession, map[string]any{"name": "x", "template_id": testTemplateID, "owner": "workspace", "workspace_script_test": true})
		if !strings.Contains(msg, "personal session") {
			t.Errorf("unexpected error %q", msg)
		}
	})
}
