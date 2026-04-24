package tool

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// Click performs a mouse click at specified coordinates.
var Click = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_click",
		mcp.WithDescription(`Click at specific coordinates on a running devenv session's macOS display.

PREFER SCRIPTED AUTOMATION WHEN POSSIBLE: for scriptable UI actions (opening
System Settings panes, launching apps, menu navigation, keystrokes, defaults),
use bitrise_devenv_execute with "open x-apple.systempreferences:<pane-id>",
"open -a <app>", or "osascript ..." — a single deterministic call vs. a
screenshot + coordinate-estimation + click chain. Wrap osascript in a short
"timeout 15s" so an unexpected TCC permission dialog fails fast (common
scopes are pre-approved, but not all). Reach for click only when no scriptable
path exists (e.g. inside a third-party app's custom canvas).

Use bitrise_devenv_screenshot first to identify the target coordinates.
Coordinates must be in the actual screen coordinate space (typically 1920x1080), NOT in screenshot
image pixel coordinates. The screenshot tool response includes the screen resolution.
NOTE: This tool only works on macOS sessions.`),
		mcp.WithString("session_id", mcp.Description("The unique identifier of the running session"), mcp.Required()),
		mcp.WithNumber("x", mcp.Description("X coordinate in screen space (0-1920 for a 1920-wide screen)"), mcp.Required()),
		mcp.WithNumber("y", mcp.Description("Y coordinate in screen space (0-1080 for a 1080-tall screen)"), mcp.Required()),
		mcp.WithString("button", mcp.Description("Mouse button: left (default), right, or middle"), mcp.Enum("left", "right", "middle"), mcp.DefaultString("left")),
		mcp.WithBoolean("double_click", mcp.Description("Whether to perform a double-click (default: false)")),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		body := map[string]any{
			"x":      request.GetInt("x", 0),
			"y":      request.GetInt("y", 0),
			"button": request.GetString("button", "left"),
		}
		if dc, ok := request.GetArguments()["double_click"]; ok {
			body["double_click"] = dc
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/click", sessionID)),
			Body:   body,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("click", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// Type types text on the session's machine.
var Type = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_type",
		mcp.WithDescription(`Type text on a running devenv session's macOS display.

PREFER SCRIPTED AUTOMATION WHEN POSSIBLE: for keystrokes, shortcuts, and text
input you can drive programmatically, use bitrise_devenv_execute with osascript
and System Events, e.g.:
  timeout 15s osascript -e 'tell application "System Events" to keystroke "hello"'
  timeout 15s osascript -e 'tell application "System Events" to keystroke "t" using {command down}'
The "timeout 15s" prefix is cheap insurance — common TCC scopes are
pre-approved on session images, but an unexpected permission prompt would
otherwise hang the command until the 2-minute execute cap. Reach for this tool
only when the target app can't be driven via AppleScript / shell commands.

The text is typed character by character as keyboard input. Special characters and
control sequences are supported.
NOTE: This tool only works on macOS sessions.`),
		mcp.WithString("session_id", mcp.Description("The unique identifier of the running session"), mcp.Required()),
		mcp.WithString("text", mcp.Description("The text to type"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := request.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/type", sessionID)),
			Body:   map[string]any{"text": text},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("type", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// Scroll performs a scroll action at the current mouse position.
var Scroll = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_scroll",
		mcp.WithDescription(`Scroll at the current mouse position on a running devenv session's macOS display.

PREFER SCRIPTED AUTOMATION WHEN POSSIBLE: many "scroll to reveal X" flows can
be avoided entirely by driving the app directly via bitrise_devenv_execute
(e.g. "open x-apple.systempreferences:<pane-id>" jumps straight to a pane, or
AppleScript navigates menus without scrolling). Use this tool only when no
scriptable path exists.
NOTE: This tool only works on macOS sessions.`),
		mcp.WithString("session_id", mcp.Description("The unique identifier of the running session"), mcp.Required()),
		mcp.WithString("direction", mcp.Description("Scroll direction"), mcp.Enum("up", "down"), mcp.Required()),
		mcp.WithNumber("amount", mcp.Description("Number of lines to scroll (default: 3)"), mcp.DefaultNumber(3)),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/scroll", sessionID)),
			Body: map[string]any{
				"direction": request.GetString("direction", "down"),
				"amount":    request.GetInt("amount", 3),
			},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("scroll", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}

// MouseDrag performs a mouse drag between two points.
var MouseDrag = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_mouse_drag",
		mcp.WithDescription(`Drag the mouse between two points on a running devenv session's macOS display.

PREFER SCRIPTED AUTOMATION WHEN POSSIBLE: most drag-to-move, drag-to-select,
and drag-to-resize actions can be done via bitrise_devenv_execute — e.g. "mv"
for files, "osascript" + System Events for window positioning, "defaults
write" for settings. Use drag only when no scriptable path exists.

Coordinates must be in the actual screen coordinate space (typically 1920x1080), NOT in screenshot
image pixel coordinates. The screenshot tool response includes the screen resolution.
NOTE: This tool only works on macOS sessions.`),
		mcp.WithString("session_id", mcp.Description("The unique identifier of the running session"), mcp.Required()),
		mcp.WithNumber("start_x", mcp.Description("Starting X coordinate"), mcp.Required()),
		mcp.WithNumber("start_y", mcp.Description("Starting Y coordinate"), mcp.Required()),
		mcp.WithNumber("end_x", mcp.Description("Ending X coordinate"), mcp.Required()),
		mcp.WithNumber("end_y", mcp.Description("Ending Y coordinate"), mcp.Required()),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodPost,
			Path:   devenv.WsPath(fmt.Sprintf("/sessions/%s/mouse-drag", sessionID)),
			Body: map[string]any{
				"start_x": request.GetInt("start_x", 0),
				"start_y": request.GetInt("start_y", 0),
				"end_x":   request.GetInt("end_x", 0),
				"end_y":   request.GetInt("end_y", 0),
			},
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("mouse drag", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
