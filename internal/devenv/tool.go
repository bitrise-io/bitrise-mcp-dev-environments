package devenv

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// Tool wraps an MCP tool definition with its handler.
type Tool struct {
	Definition mcp.Tool
	Handler    func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
}
