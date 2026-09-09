package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// The guides are embedded copies of the RDE backend's docs; an empty or
// truncated copy would silently ship an agent nothing to read.
func TestGuideResources(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0")
	NewBelt().RegisterResources(s)
	got := s.ListResources()
	if len(got) != len(guides) {
		t.Fatalf("registered %d resources, want %d", len(got), len(guides))
	}
	for _, g := range guides {
		res, ok := got[g.uri]
		if !ok {
			t.Fatalf("resource %s not registered", g.uri)
		}
		contents, err := res.Handler(context.Background(), mcp.ReadResourceRequest{Params: mcp.ReadResourceParams{URI: g.uri}})
		if err != nil {
			t.Fatalf("read %s: %v", g.uri, err)
		}
		if len(contents) != 1 {
			t.Fatalf("read %s: %d contents, want 1", g.uri, len(contents))
		}
		text, ok := contents[0].(mcp.TextResourceContents)
		if !ok {
			t.Fatalf("read %s: contents are %T, want TextResourceContents", g.uri, contents[0])
		}
		if len(text.Text) < 1000 || !strings.Contains(text.Text, "device") {
			t.Errorf("guide %s looks truncated (%d bytes)", g.uri, len(text.Text))
		}
	}
	if !strings.Contains(guideDeviceSessions, "PREVIEW_DEVICE_STATE_READY") {
		t.Errorf("the main guide must document the readiness contract")
	}
}
