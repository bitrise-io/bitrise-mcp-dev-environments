package devenv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCallAPIQueryParams(t *testing.T) {
	// callWith performs a CallAPI against a stub server and returns the raw
	// query string the server received.
	callWith := func(t *testing.T, p CallAPIParams) string {
		t.Helper()
		var query string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		old := BaseURL
		BaseURL = srv.URL
		defer func() { BaseURL = old }()

		p.Method = http.MethodGet
		p.Path = "/v1/x"
		_, err := CallAPI(ContextWithPAT(context.Background(), "pat"), p)
		assert.NoError(t, err)
		return query
	}

	t.Run("no params yields no query string", func(t *testing.T) {
		assert.Equal(t, "", callWith(t, CallAPIParams{}))
	})

	t.Run("single-value params", func(t *testing.T) {
		q := callWith(t, CallAPIParams{Params: map[string]string{"include_secrets": "true", "skipped": ""}})
		assert.Equal(t, "include_secrets=true", q)
	})

	t.Run("repeated params appear once per value", func(t *testing.T) {
		q := callWith(t, CallAPIParams{RepeatedParams: map[string][]string{
			"label_selectors": {"team=mobile", "branch=main", ""},
		}})
		assert.Equal(t, "label_selectors=team%3Dmobile&label_selectors=branch%3Dmain", q)
	})

	t.Run("single and repeated params combine", func(t *testing.T) {
		q := callWith(t, CallAPIParams{
			Params:         map[string]string{"a": "1"},
			RepeatedParams: map[string][]string{"b": {"2", "3"}},
		})
		assert.Equal(t, "a=1&b=2&b=3", q)
	})
}

// A refusal — RESOURCE_EXHAUSTED with a codespaces.v1.WarmPoolRefusal
// detail — comes back as text an agent can act on: the relay message, the
// caller's own sessions with their IDs, the next steps. Any other error
// keeps the raw envelope.
func TestCallAPIRefusalRendersAsText(t *testing.T) {
	callWith := func(t *testing.T, status int, body string) error {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		old := BaseURL
		BaseURL = srv.URL
		defer func() { BaseURL = old }()
		_, err := CallAPI(ContextWithPAT(context.Background(), "pat"), CallAPIParams{Method: http.MethodPost, Path: "/v1/x"})
		return err
	}

	t.Run("pool full", func(t *testing.T) {
		body := `{"code":8,"message":"The agents pool is full: 2 of 2 sessions are in use. Your sessions in this pool: \"migrate-auth\" (running). Use one of yours, end one you're done with, or try again when a session ends. A workspace admin can raise this pool's max sessions.","details":[` +
			`{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"POOL_FULL","domain":"rde.bitrise.io","metadata":{"action":"claim"}},` +
			`{"@type":"type.googleapis.com/codespaces.v1.WarmPoolRefusal","reason":"POOL_FULL","action":"claim","warmPoolId":"p-1","warmPoolName":"agents","inUse":2,"maxSessions":2,"family":"","quotaVcpu":0,` +
			`"yourSessions":[{"sessionId":"s-1","name":"migrate-auth","status":"running"}],` +
			`"nextSteps":["Use one of your sessions in use from this pool.","Try again once one of the pool's sessions ends."]}]}`
		err := callWith(t, http.StatusTooManyRequests, body)
		if err == nil {
			t.Fatal("expected an error on 429")
		}
		var apiErr *APIError
		if !assert.ErrorAs(t, err, &apiErr) {
			return
		}
		assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
		if assert.NotNil(t, apiErr.Refusal) {
			assert.Equal(t, "POOL_FULL", apiErr.Refusal.Reason)
			assert.Equal(t, "agents", apiErr.Refusal.WarmPoolName)
			assert.Equal(t, 2, apiErr.Refusal.MaxSessions)
		}
		text := err.Error()
		assert.Equal(t, "Refused (POOL_FULL): The agents pool is full: 2 of 2 sessions are in use. Your sessions in this pool: \"migrate-auth\" (running). Use one of yours, end one you're done with, or try again when a session ends. A workspace admin can raise this pool's max sessions.\n\n"+
			"Your sessions in use from this pool:\n- migrate-auth (running) — session_id s-1\n\n"+
			"Next steps:\n1. Use one of your sessions in use from this pool.\n2. Try again once one of the pool's sessions ends.", text)
		assert.NotContains(t, text, "@type", "the raw envelope must not leak into the text")
	})

	t.Run("quota exhausted without sessions", func(t *testing.T) {
		body := `{"code":8,"message":"The workspace is out of m4pro CPUs: 60 of 60 vCPUs are in use. End a session you're done with or try again later. A workspace admin manages the workspace's CPUs.","details":[` +
			`{"@type":"type.googleapis.com/codespaces.v1.WarmPoolRefusal","reason":"CPU_QUOTA_EXHAUSTED","action":"create","family":"m4pro","usedVcpu":60,"quotaVcpu":60,"yourSessions":[],"nextSteps":["Try again later."]}]}`
		err := callWith(t, http.StatusTooManyRequests, body)
		if err == nil {
			t.Fatal("expected an error on 429")
		}
		assert.Equal(t, "Refused (CPU_QUOTA_EXHAUSTED): The workspace is out of m4pro CPUs: 60 of 60 vCPUs are in use. End a session you're done with or try again later. A workspace admin manages the workspace's CPUs.\n\nNext steps:\n1. Try again later.", err.Error())
	})

	t.Run("other errors keep the raw body", func(t *testing.T) {
		body := `{"code":3,"message":"Bad request.","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"name","description":"required"}]}]}`
		err := callWith(t, http.StatusBadRequest, body)
		if err == nil {
			t.Fatal("expected an error on 400")
		}
		assert.Equal(t, "API error (status 400): "+body, err.Error())
	})
}
