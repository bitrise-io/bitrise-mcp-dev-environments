package tool

import (
	"context"
	"testing"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
)

func toolNames(b *Belt) map[string]bool {
	m := map[string]bool{}
	for _, t := range b.tools {
		m[t.Definition.Name] = true
	}
	return m
}

func newReq(name string) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Name = name
	return r
}

// TestClassificationKeysAreRealTools guards against typos in the userScoped /
// localOnly maps: every key must correspond to a registered tool, otherwise a
// tool would be silently mis-classified (e.g. a user-scoped tool treated as
// workspace-scoped and triggering a spurious GET /organizations).
func TestClassificationKeysAreRealTools(t *testing.T) {
	b := NewBelt()
	names := toolNames(b)
	for k := range b.userScoped {
		assert.Truef(t, names[k], "userScoped key %q is not a registered tool", k)
	}
	for k := range b.localOnly {
		assert.Truef(t, names[k], "localOnly key %q is not a registered tool", k)
	}
}

func TestFilterTools(t *testing.T) {
	b := NewBelt()
	tools := []mcp.Tool{
		{Name: "bitrise_devenv_upload"},
		{Name: "bitrise_devenv_download"},
		{Name: "bitrise_devenv_list"},
		{Name: "bitrise_devenv_me"},
	}

	t.Run("stdio lists every tool", func(t *testing.T) {
		got := b.FilterTools(context.Background(), tools)
		assert.Len(t, got, 4)
	})

	t.Run("hosted hides local-only tools", func(t *testing.T) {
		got := b.FilterTools(devenv.ContextWithHostedMode(context.Background()), tools)
		var got_names []string
		for _, tl := range got {
			got_names = append(got_names, tl.Name)
		}
		assert.ElementsMatch(t, []string{"bitrise_devenv_list", "bitrise_devenv_me"}, got_names)
	})
}

func TestGateAndResolveWorkspace(t *testing.T) {
	b := NewBelt()

	t.Run("local-only tool is rejected on the hosted transport", func(t *testing.T) {
		ctx := devenv.ContextWithHostedMode(context.Background())
		_, errRes := b.GateAndResolveWorkspace(ctx, newReq("bitrise_devenv_upload"))
		assert.NotNil(t, errRes)
		assert.True(t, errRes.IsError)
	})

	t.Run("local-only tool is allowed in stdio with a workspace set", func(t *testing.T) {
		ctx := devenv.ContextWithWorkspace(context.Background(), "ws")
		_, errRes := b.GateAndResolveWorkspace(ctx, newReq("bitrise_devenv_upload"))
		assert.Nil(t, errRes)
	})

	t.Run("user-scoped tool skips workspace resolution", func(t *testing.T) {
		// No workspace configured and MainAPIBaseURL unset: if resolution ran it
		// would error. A nil result proves it was skipped.
		_, errRes := b.GateAndResolveWorkspace(context.Background(), newReq("bitrise_devenv_me"))
		assert.Nil(t, errRes)
	})

	t.Run("workspace-scoped tool with a configured workspace does not auto-detect", func(t *testing.T) {
		ctx := devenv.ContextWithWorkspace(context.Background(), "ws-1")
		gotCtx, errRes := b.GateAndResolveWorkspace(ctx, newReq("bitrise_devenv_list"))
		assert.Nil(t, errRes)
		assert.Equal(t, "ws-1", devenv.WorkspaceFromCtx(gotCtx))
	})

	t.Run("workspace-scoped tool with no workspace and no discovery config errors gracefully", func(t *testing.T) {
		old := devenv.MainAPIBaseURL
		devenv.MainAPIBaseURL = ""
		defer func() { devenv.MainAPIBaseURL = old }()

		ctx := devenv.ContextWithPAT(context.Background(), "pat")
		_, errRes := b.GateAndResolveWorkspace(ctx, newReq("bitrise_devenv_list"))
		assert.NotNil(t, errRes)
		assert.True(t, errRes.IsError)
	})
}

func newReqWithArgs(name string, args map[string]any) mcp.CallToolRequest {
	r := newReq(name)
	r.Params.Arguments = args
	return r
}

// TestWorkspaceIDParamInjection asserts every workspace-scoped tool exposes the
// optional workspace_id parameter and user-scoped tools do not.
func TestWorkspaceIDParamInjection(t *testing.T) {
	b := NewBelt()
	for _, tl := range b.tools {
		name := tl.Definition.Name
		_, has := tl.Definition.InputSchema.Properties["workspace_id"]
		if b.userScoped[name] {
			assert.Falsef(t, has, "%s is user-scoped and must NOT expose workspace_id", name)
		} else {
			assert.Truef(t, has, "%s is workspace-scoped and must expose workspace_id", name)
		}
	}
}

func TestGateAndResolveWorkspace_ParamWins(t *testing.T) {
	b := NewBelt()
	// An explicit workspace_id param beats the per-connection default in ctx.
	ctx := devenv.ContextWithWorkspace(context.Background(), "default-ws")
	req := newReqWithArgs("bitrise_devenv_list", map[string]any{"workspace_id": "param-ws"})
	gotCtx, errRes := b.GateAndResolveWorkspace(ctx, req)
	assert.Nil(t, errRes)
	assert.Equal(t, "param-ws", devenv.WorkspaceFromCtx(gotCtx))
}

// TestGateAndResolveWorkspace_RemembersParamPerClientSession asserts an
// explicit workspace_id is remembered for the MCP client session that passed
// it, beats the per-connection default on later calls, is scoped to that
// session, and is not remembered when no client session is attached.
func TestGateAndResolveWorkspace_RemembersParamPerClientSession(t *testing.T) {
	b := NewBelt()
	srv := server.NewMCPServer("test", "0.0.0")
	sessA := server.NewInProcessSession("sess-a", nil)
	sessB := server.NewInProcessSession("sess-b", nil)
	base := devenv.ContextWithWorkspace(context.Background(), "default-ws")
	ctxA := srv.WithContext(base, sessA)
	ctxB := srv.WithContext(base, sessB)

	// First call in session A names the workspace explicitly.
	gotCtx, errRes := b.GateAndResolveWorkspace(ctxA, newReqWithArgs("bitrise_devenv_list", map[string]any{"workspace_id": "param-ws"}))
	assert.Nil(t, errRes)
	assert.Equal(t, "param-ws", devenv.WorkspaceFromCtx(gotCtx))

	// A later call in session A without the parameter inherits it.
	gotCtx, errRes = b.GateAndResolveWorkspace(ctxA, newReq("bitrise_devenv_get"))
	assert.Nil(t, errRes)
	assert.Equal(t, "param-ws", devenv.WorkspaceFromCtx(gotCtx))

	// Passing a different value later replaces the remembered one.
	_, _ = b.GateAndResolveWorkspace(ctxA, newReqWithArgs("bitrise_devenv_list", map[string]any{"workspace_id": "other-ws"}))
	gotCtx, _ = b.GateAndResolveWorkspace(ctxA, newReq("bitrise_devenv_get"))
	assert.Equal(t, "other-ws", devenv.WorkspaceFromCtx(gotCtx))

	// Session B never passed one and falls through to the connection default.
	gotCtx, errRes = b.GateAndResolveWorkspace(ctxB, newReq("bitrise_devenv_list"))
	assert.Nil(t, errRes)
	assert.Equal(t, "default-ws", devenv.WorkspaceFromCtx(gotCtx))

	// Without a client session nothing is remembered: the next call falls back.
	_, _ = b.GateAndResolveWorkspace(base, newReqWithArgs("bitrise_devenv_list", map[string]any{"workspace_id": "anon-ws"}))
	gotCtx, _ = b.GateAndResolveWorkspace(base, newReq("bitrise_devenv_list"))
	assert.Equal(t, "default-ws", devenv.WorkspaceFromCtx(gotCtx))
}
