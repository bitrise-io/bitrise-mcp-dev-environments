package tool

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Execute dials the VM itself, so the backend's access gate during On create
// is applied client-side: no dial while an On create run is pending or
// running, in the backend's words, pointing at lifecycle.phase.
func TestExecute_RefusedWhileOnCreateRuns(t *testing.T) {
	const running = `{"session":{"id":"` + testSessionID + `","status":"SESSION_STATUS_RUNNING",
		"sshAddress":"ssh vagrant@example.invalid -p 22","sshPassword":"pw","sshConnectionOpen":true,
		"lifecycle":{"mode":"hooks","phase":"LIFECYCLE_PHASE_SETTING_UP",
			"runs":[{"id":"r-1","hook":"on_create","attempt":1,"state":"HOOK_RUN_STATE_RUNNING","triggeredBy":"create","ranIn":"session"}]}}}`
	ctx, got := captureRequest(t, running)
	res, err := Execute.Handler(ctx, callRequest(map[string]any{"session_id": testSessionID, "command": "true"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !got.Called {
		t.Fatal("the session should have been read first")
	}
	if !res.IsError {
		t.Fatal("expected execute to be refused while On create runs")
	}
	text := res.Content[0].(mcp.TextContent).Text
	for _, want := range []string{"Session is setting up: On create is still running. Access opens when it finishes.", "LIFECYCLE_PHASE_READY"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q should contain %q", text, want)
		}
	}
}

func TestSessionSSHFields_OnCreateInFlight(t *testing.T) {
	type run = struct {
		Hook  string `json:"hook"`
		State string `json:"state"`
	}
	lifecycle := func(runs ...run) *struct {
		Runs []run `json:"runs"`
	} {
		return &struct {
			Runs []run `json:"runs"`
		}{Runs: runs}
	}
	cases := []struct {
		name string
		s    sessionSSHFields
		want bool
	}{
		{"legacy session", sessionSSHFields{}, false},
		{"on create pending", sessionSSHFields{Lifecycle: lifecycle(run{"on_create", "HOOK_RUN_STATE_PENDING"})}, true},
		{"on create running", sessionSSHFields{Lifecycle: lifecycle(run{"on_create", "HOOK_RUN_STATE_RUNNING"})}, true},
		{"on create done, on claim running", sessionSSHFields{Lifecycle: lifecycle(run{"on_create", "HOOK_RUN_STATE_SUCCEEDED"}, run{"on_claim", "HOOK_RUN_STATE_RUNNING"})}, false},
		{"on create failed at the limit", sessionSSHFields{Lifecycle: lifecycle(run{"on_create", "HOOK_RUN_STATE_FAILED"})}, false},
		{"rebuild queued a second on create", sessionSSHFields{Lifecycle: lifecycle(run{"on_create", "HOOK_RUN_STATE_FAILED"}, run{"on_create", "HOOK_RUN_STATE_PENDING"})}, true},
	}
	for _, tc := range cases {
		if got := tc.s.onCreateInFlight(); got != tc.want {
			t.Errorf("%s: onCreateInFlight = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// An unclaimed warm session never shows its connection details. Execute
// says it has to be claimed first, in the backend's words, instead of
// "SSH is not ready yet", which would have the agent poll forever.
func TestExecute_RefusedOnUnclaimedWarmSession(t *testing.T) {
	for _, state := range []string{"warming", "ready", "retiring"} {
		t.Run(state, func(t *testing.T) {
			warm := `{"session":{"id":"` + testSessionID + `","status":"SESSION_STATUS_RUNNING",
				"sshAddress":"","sshPassword":"","sshConnectionOpen":false,
				"warmState":"` + state + `","warmPoolId":"pool-1"}}`
			ctx, _ := captureRequest(t, warm)
			res, err := Execute.Handler(ctx, callRequest(map[string]any{"session_id": testSessionID, "command": "true"}))
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !res.IsError {
				t.Fatal("expected execute to be refused on an unclaimed warm session")
			}
			text := res.Content[0].(mcp.TextContent).Text
			for _, want := range []string{"warm session held by warm pool pool-1", "warm_pool_id"} {
				if !strings.Contains(text, want) {
					t.Errorf("error %q should contain %q", text, want)
				}
			}
		})
	}
}
