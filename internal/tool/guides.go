package tool

import (
	"context"
	_ "embed"

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
// for them, and the create/get tool descriptions point at them by URI.

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
	uri, name, description, body string
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
		uri:         GuideURIDeviceSessions,
		name:        "Device sessions — agent guide",
		description: "How to create an RDE session that boots an iOS simulator or Android emulator (bitrise_devenv_create with device_spec), wait for the device to be READY, connect (SSH tunnel or bitrise_devenv_execute), drive it efficiently (accessibility tree first, then input), let a human watch from the session page in the RDE web UI, and what never to do. Read this before driving a device session.",
		body:        guideDeviceSessions,
	},
	{
		uri:         GuideURIIOS,
		name:        "Device sessions — iOS simulator specifics",
		description: "Driving the iOS simulator on a device session: simctl, serve-sim's CLI (tap/type/button/gesture) and its /ax accessibility JSON endpoint, screenshots, logs, and recovery.",
		body:        guideIOS,
	},
	{
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
