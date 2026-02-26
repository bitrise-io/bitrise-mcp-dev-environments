package tool

import (
	"context"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// Me returns the currently authenticated user.
var Me = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_me",
		mcp.WithDescription("Get the currently authenticated Bitrise user information (email, user ID)."),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   "/v1/me",
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get user info", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
