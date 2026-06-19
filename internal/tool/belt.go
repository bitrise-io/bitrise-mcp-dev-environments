package tool

import (
	"context"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Belt holds all registered tools plus the classification used to resolve a
// workspace per request and to hide host-dependent tools when hosted.
type Belt struct {
	tools []devenv.Tool
	// userScoped lists tools that are NOT workspace-scoped (their backend path
	// has no /v1/workspaces/{id} segment). Every other tool is workspace-scoped,
	// so a new session/template tool is treated as workspaced by default.
	userScoped map[string]bool
	// localOnly lists tools that depend on the host the server runs on (the
	// local filesystem). They are hidden and rejected on the hosted HTTP
	// transport, where "local" would mean the server, not the user's machine.
	localOnly map[string]bool
}

// NewBelt creates a new tool belt with all tools registered.
func NewBelt() *Belt {
	return &Belt{
		tools: []devenv.Tool{
			// User / account
			Me,
			ListWorkspaces,

			// Sessions
			ListSessions,
			GetSession,
			CreateSession,
			UpdateSession,
			RestoreSession,
			TerminateSession,
			DeleteSession,
			DeleteTerminatedSessions,
			CompareSessionTemplate,

			// Templates
			ListTemplates,
			GetTemplate,
			CreateTemplate,
			UpdateTemplate,
			DeleteTemplate,

			// Saved Inputs
			ListSavedInputs,
			GetSavedInput,
			CreateSavedInput,
			UpdateSavedInput,
			DeleteSavedInput,

			// Instance Manager Proxy
			ListImages,
			ListMachineTypes,
			ResolveClusters,

			// Session Notifications
			ListSessionNotifications,

			// Session Interaction
			Execute,
			Screenshot,
			Click,
			Type,
			Scroll,
			MouseDrag,

			// File Transfer
			Upload,
			Download,

			// Remote Access
			OpenRemoteAccess,
		},
		// User-scoped tools hit /v1/me or /v1/saved-inputs (no workspace
		// segment), or the main API (list_workspaces). Everything else is
		// workspace-scoped.
		userScoped: map[string]bool{
			"bitrise_devenv_me":                 true,
			"bitrise_devenv_list_workspaces":    true,
			"bitrise_devenv_list_saved_inputs":  true,
			"bitrise_devenv_get_saved_input":    true,
			"bitrise_devenv_create_saved_input": true,
			"bitrise_devenv_update_saved_input": true,
			"bitrise_devenv_delete_saved_input": true,
		},
		// upload reads the local filesystem; download writes it. Neither makes
		// sense on a hosted server.
		localOnly: map[string]bool{
			"bitrise_devenv_upload":   true,
			"bitrise_devenv_download": true,
		},
	}
}

// RegisterAll registers all tools with the MCP server.
func (b *Belt) RegisterAll(s *server.MCPServer) {
	for _, t := range b.tools {
		s.AddTool(t.Definition, t.Handler)
	}
}

// FilterTools hides host-dependent (local-only) tools when the request is
// served over the hosted HTTP transport. Wired via server.WithToolFilter; in
// stdio mode (no hosted marker) every tool is listed.
func (b *Belt) FilterTools(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	if !devenv.HostedModeFromCtx(ctx) {
		return tools
	}
	filtered := make([]mcp.Tool, 0, len(tools))
	for _, t := range tools {
		if !b.localOnly[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// GateAndResolveWorkspace enforces transport-level tool availability and
// resolves the workspace for workspace-scoped tools. It returns the (possibly
// updated) context to use for the handler, or a non-nil error result that the
// caller should return to the client.
//
// PAT (and any per-connection default workspace) must already be in ctx: the
// stdio middleware injects them from env, the HTTP context func from the bearer
// token and the x-bitrise-workspace-id header.
func (b *Belt) GateAndResolveWorkspace(ctx context.Context, request mcp.CallToolRequest) (context.Context, *mcp.CallToolResult) {
	name := request.Params.Name

	// Host-dependent tools are unavailable when hosted (defense in depth — they
	// are also hidden from the tool list by FilterTools).
	if devenv.HostedModeFromCtx(ctx) && b.localOnly[name] {
		return ctx, mcp.NewToolResultError("this tool needs a locally-run MCP server (it reads or writes your machine's filesystem) and is unavailable on the hosted server; run the MCP server locally to use it")
	}

	// Workspace-scoped tools need a workspace. If none was configured, try to
	// auto-detect the user's sole workspace. The result is cached per PAT so
	// this doesn't issue a discovery request on every call.
	if !b.userScoped[name] && devenv.WorkspaceFromCtx(ctx) == "" {
		slug, err := devenv.ResolveSoleWorkspace(ctx)
		if err != nil {
			return ctx, mcp.NewToolResultError(err.Error())
		}
		ctx = devenv.ContextWithWorkspace(ctx, slug)
	}

	return ctx, nil
}
