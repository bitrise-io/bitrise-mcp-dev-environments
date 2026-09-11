package tool

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Device-session guides, served as MCP resources so an agent that creates a
// session with a device_spec can read the know-how (readiness, connectivity,
// accessibility tree, input, screenshots, do-nots, recovery) instead of
// re-deriving it. The markdown mirrors the RDE backend's device-session guide
// verbatim; the backend is the source of truth — keep these in sync with the
// backend release this server targets rather than editing here.
//
// Resources rather than tool output: they cost no context until an agent asks
// for them, and the create/get tool descriptions point at them by URI. Not
// every MCP client can read resources, so DeviceGuide serves the very same
// bodies as a tool — a fallback, sharing this slice so the two cannot drift.

//go:embed guides/device-sessions.md
var guideDeviceSessions string

//go:embed guides/ios.md
var guideIOS string

//go:embed guides/android.md
var guideAndroid string

// GuideURIDeviceSessions is the platform-independent device session guide.
const GuideURIDeviceSessions = "bitrise-devenv://guides/device-sessions"

// GuideURIIOS / GuideURIAndroid are the per-platform driving guides.
const (
	GuideURIIOS     = "bitrise-devenv://guides/device-sessions/ios"
	GuideURIAndroid = "bitrise-devenv://guides/device-sessions/android"
)

type guide struct {
	// key selects the guide in the DeviceGuide tool ("device-sessions", "ios", "android").
	key                          string
	uri, name, description, body string
}

// guideByKey returns the guide the DeviceGuide tool serves for key.
func guideByKey(key string) (guide, bool) {
	for _, g := range guides {
		if g.key == key {
			return g, true
		}
	}
	return guide{}, false
}

// guideKeys lists the DeviceGuide tool's accepted keys in guide order.
func guideKeys() []string {
	keys := make([]string, 0, len(guides))
	for _, g := range guides {
		keys = append(keys, g.key)
	}
	return keys
}

// Guides lists the resources RegisterResources serves (exported for tests and
// the README table).
func Guides() []struct{ URI, Name string } {
	out := make([]struct{ URI, Name string }, 0, len(guides))
	for _, g := range guides {
		out = append(out, struct{ URI, Name string }{g.uri, g.name})
	}
	return out
}

var guides = []guide{
	{
		key:         "device-sessions",
		uri:         GuideURIDeviceSessions,
		name:        "Device sessions — agent guide",
		description: "How to create an RDE session that boots an iOS simulator or Android emulator (bitrise_devenv_create with device_spec), wait for the device to be READY, connect (SSH tunnel or bitrise_devenv_execute), drive it efficiently (accessibility tree first, then input), let a human watch from the session page in the RDE web UI, and what never to do. Read this before driving a device session.",
		body:        guideDeviceSessions,
	},
	{
		key:         "ios",
		uri:         GuideURIIOS,
		name:        "Device sessions — iOS simulator specifics",
		description: "Driving the iOS simulator on a device session: simctl, serve-sim's CLI (tap/type/button/gesture) and its /ax accessibility JSON endpoint, screenshots, logs, and recovery.",
		body:        guideIOS,
	},
	{
		key:         "android",
		uri:         GuideURIAndroid,
		name:        "Device sessions — Android emulator specifics",
		description: "Driving the Android emulator on a device session: adb over the tunnel or in-band, uiautomator dump for the accessibility tree, input, install/launch, screenshots, logs, and recovery.",
		body:        guideAndroid,
	},
}

// RegisterResources registers the guide resources with the MCP server.
func (b *Belt) RegisterResources(s *server.MCPServer) {
	for _, g := range guides {
		g := g
		s.AddResource(
			mcp.NewResource(g.uri, g.name, mcp.WithResourceDescription(g.description), mcp.WithMIMEType("text/markdown")),
			func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				return []mcp.ResourceContents{mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "text/markdown",
					Text:     g.body,
				}}, nil
			},
		)
	}
}

// DeviceGuide serves the device-session guides as a tool, for MCP clients that
// cannot read resources. It returns exactly the markdown the resource of the
// same guide returns — both read the guides slice — so an agent on a
// tools-only client gets the same know-how, just paid for in context up front.
var DeviceGuide = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_device_guide",
		mcp.WithDescription(`Fallback for clients that cannot read MCP resources: returns the device-session agent guide as markdown — the exact same content as the resources bitrise-devenv://guides/device-sessions, .../ios and .../android. If your client supports MCP resources, read those instead (they cost no context until requested); use this tool only when the resources are not available to you.

Read "device-sessions" before creating or driving a session with a device_spec (readiness contract, connecting, accessibility tree, input, screenshots, letting a human watch, do-nots, recovery), then the platform guide for the device you boot.`),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("guide",
			mcp.Description(`Which guide to return: "device-sessions" (platform-independent, read first), "ios" (simulator specifics: simctl, serve-sim CLI, /ax accessibility endpoint) or "android" (emulator specifics: adb, uiautomator dump, input, install).`),
			mcp.Required(),
			mcp.Enum(guideKeys()...),
		),
	),
	Handler: func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		key := request.GetString("guide", "")
		g, ok := guideByKey(key)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("unknown guide %q; one of %s", key, strings.Join(guideKeys(), ", "))), nil
		}
		return mcp.NewToolResultText(g.body), nil
	},
}
