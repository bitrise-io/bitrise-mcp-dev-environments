package devenv

import (
	"context"
	"fmt"
)

type ctxKey int

const keyPAT ctxKey = iota

// ContextWithPAT returns a new context with the Bitrise token stored.
func ContextWithPAT(ctx context.Context, pat string) context.Context {
	return context.WithValue(ctx, keyPAT, pat)
}

// AuthFromCtx returns the Authorization header name and value from context.
func AuthFromCtx(ctx context.Context) (headerName, headerValue string, err error) {
	if v, ok := ctx.Value(keyPAT).(string); ok && v != "" {
		return "Authorization", "Bearer " + v, nil
	}
	return "", "", fmt.Errorf("missing authentication - set BITRISE_TOKEN env var")
}
