package devenv

import (
	"context"
	"fmt"
)

type ctxKey int

const (
	keyPAT ctxKey = iota
	keyWorkspace
	keyHostedMode
)

// ContextWithPAT returns a new context with the Bitrise token stored.
func ContextWithPAT(ctx context.Context, pat string) context.Context {
	return context.WithValue(ctx, keyPAT, pat)
}

// AuthFromCtx returns the Authorization header name and value from context.
func AuthFromCtx(ctx context.Context) (headerName, headerValue string, err error) {
	if v, ok := ctx.Value(keyPAT).(string); ok && v != "" {
		return "Authorization", "Bearer " + v, nil
	}
	return "", "", fmt.Errorf("missing authentication - complete the OAuth flow, or set the BITRISE_TOKEN env var (stdio) / Authorization header (http) to a Bitrise personal access token")
}

// ContextWithWorkspace returns a new context with the resolved workspace ID
// (slug) stored. Tools read it via WorkspaceFromCtx / WsPath.
func ContextWithWorkspace(ctx context.Context, workspaceID string) context.Context {
	return context.WithValue(ctx, keyWorkspace, workspaceID)
}

// WorkspaceFromCtx returns the resolved workspace ID from context, or "" if none.
func WorkspaceFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(keyWorkspace).(string); ok {
		return v
	}
	return ""
}

// ContextWithHostedMode marks the request as served over the hosted HTTP
// transport. The tool filter uses this to hide host-dependent (Local) tools
// that only work when the server runs on the user's own machine.
func ContextWithHostedMode(ctx context.Context) context.Context {
	return context.WithValue(ctx, keyHostedMode, true)
}

// HostedModeFromCtx reports whether the request is served over the hosted HTTP
// transport.
func HostedModeFromCtx(ctx context.Context) bool {
	v, ok := ctx.Value(keyHostedMode).(bool)
	return ok && v
}
