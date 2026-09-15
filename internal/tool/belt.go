package tool

import (
	"context"
	"sync"

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
	// lastWorkspace remembers, per MCP client session (keyed by the session
	// id), the workspace_id most recently passed explicitly, so a
	// multi-workspace user states it once instead of on every call.
	lastWorkspace sync.Map
}

// workspaceIDParamDesc documents the optional workspace_id parameter injected
// onto every workspace-scoped tool.
const workspaceIDParamDesc = "Workspace ID (slug) to operate in. Optional. If omitted, the server reuses the workspace_id you last passed in this MCP session, then BITRISE_WORKSPACE_ID (local stdio) or the x-bitrise-workspace-id header (hosted), then auto-detects when you belong to a single workspace. If you belong to multiple workspaces, pass the chosen workspace's ID (from bitrise_devenv_list_workspaces) once; later calls in the same session inherit it."

// NewBelt creates a new tool belt with all tools registered.
func NewBelt() *Belt {
	b := &Belt{
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

			// Stacks & Machine Types
			ListStacks,
			ListMachineTypes,

			// Workspace Usage
			GetWorkspaceUsage,

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

			// Guides (fallback for clients without MCP resource support)
			DeviceGuide,
		},
		// User-scoped tools hit /v1/me or /v1/saved-inputs (no workspace
		// segment), or the main API (list_workspaces), or no API at all
		// (device_guide serves embedded text). Everything else is
		// workspace-scoped.
		userScoped: map[string]bool{
			"bitrise_devenv_device_guide":       true,
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

	// Inject an optional workspace_id parameter on every workspace-scoped tool
	// (centralised so it isn't repeated across ~27 tool definitions). It is the
	// top rung of the workspace-resolution ladder in GateAndResolveWorkspace.
	for i := range b.tools {
		if b.userScoped[b.tools[i].Definition.Name] {
			continue
		}
		if b.tools[i].Definition.InputSchema.Properties == nil {
			b.tools[i].Definition.InputSchema.Properties = map[string]any{}
		}
		b.tools[i].Definition.InputSchema.Properties["workspace_id"] = map[string]any{
			"type":        "string",
			"description": workspaceIDParamDesc,
		}
	}

	return b
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

	// Resolve the workspace for workspace-scoped tools. Ladder (highest first):
	//   1. an explicit workspace_id tool parameter (remembered for this client session)
	//   2. the workspace_id last passed explicitly in this client session
	//   3. the per-connection default (BITRISE_WORKSPACE_ID env / x-bitrise-workspace-id header)
	//   4. auto-detection of the user's sole workspace (cached per PAT)
	if !b.userScoped[name] {
		sessionKey := clientSessionKey(ctx)
		ws := request.GetString("workspace_id", "")
		if ws != "" && sessionKey != "" {
			b.lastWorkspace.Store(sessionKey, ws)
		}
		if ws == "" && sessionKey != "" {
			if v, ok := b.lastWorkspace.Load(sessionKey); ok {
				ws, _ = v.(string)
			}
		}
		if ws == "" {
			ws = devenv.WorkspaceFromCtx(ctx)
		}
		if ws == "" {
			detected, err := devenv.ResolveSoleWorkspace(ctx)
			if err != nil {
				return ctx, mcp.NewToolResultError(err.Error())
			}
			ws = detected
		}
		ctx = devenv.ContextWithWorkspace(ctx, ws)
	}

	return ctx, nil
}

// clientSessionKey identifies the MCP client session a call belongs to, or ""
// when the transport attached none (nothing is remembered then).
func clientSessionKey(ctx context.Context) string {
	cs := server.ClientSessionFromContext(ctx)
	if cs == nil {
		return ""
	}
	return cs.SessionID()
}
