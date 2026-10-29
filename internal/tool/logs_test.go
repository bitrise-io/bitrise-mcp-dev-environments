package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const logSessionID = "38e859b1-0d7d-4714-8521-dc51bed30841"

func callGetLogs(t *testing.T, args map[string]any, handler http.HandlerFunc) *mcp.CallToolResult {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	old := devenv.BaseURL
	devenv.BaseURL = srv.URL
	defer func() { devenv.BaseURL = old }()
	oldIdle := logIdleGap
	logIdleGap = 100 * time.Millisecond
	defer func() { logIdleGap = oldIdle }()

	ctx := devenv.ContextWithWorkspace(devenv.ContextWithPAT(context.Background(), "pat"), "ws")
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := GetSessionLogs.Handler(ctx, req)
	require.NoError(t, err)
	return res
}

func resultText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// Each stage name reaches the backend as its LogStage number, with the
// attempt as a query parameter.
func TestGetSessionLogs_StagePathsAndAttempt(t *testing.T) {
	for stage, num := range logStages {
		t.Run(stage, func(t *testing.T) {
			res := callGetLogs(t, map[string]any{"session_id": logSessionID, "stage": stage, "attempt": float64(2)}, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, fmt.Sprintf("/v1/workspaces/ws/sessions/%s/logs/%s", logSessionID, num), r.URL.Path)
				assert.Equal(t, "attempt=2", r.URL.RawQuery)
				_, _ = fmt.Fprintln(w, `{"result":{"logContent":"hello\n"}}`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			assert.False(t, res.IsError)
			assert.Equal(t, "hello\n", resultText(res))
		})
	}
}

// Until a stage has output the backend answers 404; that is "no output
// yet", not an error the agent has to interpret.
func TestGetSessionLogs_NoOutputYet(t *testing.T) {
	res := callGetLogs(t, map[string]any{"session_id": logSessionID, "stage": "on_claim"}, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":5}`, http.StatusNotFound)
	})
	assert.False(t, res.IsError)
	assert.Equal(t, "[no on_claim output yet]", resultText(res))
}

func TestGetSessionLogs_RejectsBadArguments(t *testing.T) {
	never := func(w http.ResponseWriter, r *http.Request) { t.Errorf("backend called for a bad request") }
	for name, args := range map[string]map[string]any{
		"unknown stage":   {"session_id": logSessionID, "stage": "main"},
		"zero attempt":    {"session_id": logSessionID, "stage": "on_create", "attempt": float64(0)},
		"partial attempt": {"session_id": logSessionID, "stage": "on_create", "attempt": 1.5},
		"bad session id":  {"session_id": "nope", "stage": "on_create"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, callGetLogs(t, args, never).IsError)
		})
	}
}

func TestFormatLogRead(t *testing.T) {
	assert.Equal(t, "[no on_create output yet]", formatLogRead("on_create", devenv.LogRead{}))
	assert.Equal(t, "[earlier output cut: showing the last 64 KB]\nend\n\n[still printing: this is the output so far; call again for more]",
		formatLogRead("on_create", devenv.LogRead{Text: "end\n", Truncated: true, Incomplete: true}))
}
