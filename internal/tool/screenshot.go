package tool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

type screenshotResp struct {
	SignedURL string `json:"signedUrl"`
}

// Screenshot takes a screenshot of the session's screen.
var Screenshot = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_screenshot",
		mcp.WithDescription(`Take a screenshot of a running devenv session's macOS display.

Returns the screenshot as an embedded image along with the actual screen resolution.
Use this to verify the current state of the GUI, identify coordinates for click/drag operations,
and debug visual issues.

NOTE: This tool only works on macOS sessions. Linux sessions do not have a graphical display.`),
		mcp.WithString("session_id",
			mcp.Description("The unique identifier of the running session"),
			mcp.Required(),
		),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := devenv.CallAPILongTimeout(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/screenshot", sessionID)),
			Body:   map[string]any{},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("take screenshot", err), nil
		}

		var resp screenshotResp
		if err := json.Unmarshal([]byte(res), &resp); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("parse screenshot response: %v", err)), nil
		}

		imageData, err := downloadImage(ctx, resp.SignedURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("download screenshot: %v", err)), nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewImageContent(base64.StdEncoding.EncodeToString(imageData), "image/jpeg"),
				mcp.NewTextContent(
					"Screenshot of session display. The actual screen resolution is 1920x1080 pixels.\n" +
						"IMPORTANT: When using click or drag tools, you MUST provide coordinates in the actual screen " +
						"coordinate space (1920x1080), NOT in the pixel coordinates of this image (which may have been " +
						"resized). Estimate where elements are positioned on the full 1920x1080 screen.\n" +
						"If the screen resolution is not 1920x1080, you can verify it by running via the execute tool:\n" +
						`swift -e 'import CoreGraphics; let id = CGMainDisplayID(); print("\(CGDisplayPixelsWide(id))x\(CGDisplayPixelsHigh(id))")'`,
				),
			},
		}, nil
	},
}

func downloadImage(ctx context.Context, signedURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	client := http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (status %d): %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}
