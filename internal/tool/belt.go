package tool

import (
	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/server"
)

// Belt holds all registered tools.
type Belt struct {
	tools []devenv.Tool
}

// NewBelt creates a new tool belt with all tools registered.
func NewBelt() *Belt {
	return &Belt{
		tools: []devenv.Tool{
			// User
			Me,

			// Sessions
			ListSessions,
			GetSession,
			CreateSession,
			UpdateSession,
			StartSession,
			StopSession,
			DeleteSession,
			DeleteArchivedSessions,

			// Templates
			ListTemplates,
			GetTemplate,
			CreateTemplate,
			UpdateTemplate,
			DeleteTemplate,

			// User Inputs
			ListUserInputs,
			GetUserInput,
			CreateUserInput,
			UpdateUserInput,
			DeleteUserInput,

			// Instance Manager Proxy
			ListImages,
			ListMachineTypes,

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
	}
}

// RegisterAll registers all tools with the MCP server.
func (b *Belt) RegisterAll(s *server.MCPServer) {
	for _, t := range b.tools {
		s.AddTool(t.Definition, t.Handler)
	}
}
