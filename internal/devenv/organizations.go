package devenv

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MainAPIBaseURL is the base URL of the main Bitrise API (e.g.
// https://api.bitrise.io/v0.1), used for workspace discovery. The codespaces
// backend has no list-workspaces endpoint (only /v1/me, /v1/saved-inputs and
// /v1/workspaces/{id}/...), so GET /organizations is served by the main API.
var MainAPIBaseURL string

// Organization is a workspace the authenticated user belongs to. Field names
// match v0.OrganizationResponseModel in the Bitrise API.
type Organization struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ListOrganizations returns the workspaces (organizations) the authenticated
// user can access via GET {MainAPIBaseURL}/organizations.
func ListOrganizations(ctx context.Context) ([]Organization, error) {
	pat, _ := ctx.Value(keyPAT).(string)
	if pat == "" {
		return nil, fmt.Errorf("missing authentication - complete the OAuth flow, or set the BITRISE_TOKEN env var (stdio) / Authorization header (http)")
	}
	if MainAPIBaseURL == "" {
		return nil, fmt.Errorf("workspace discovery is unavailable: main Bitrise API base URL is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, MainAPIBaseURL+"/organizations", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	// The main Bitrise API (v0.1) authenticates with the RAW PAT in the
	// Authorization header — no "Bearer " prefix. (The codespaces backend used
	// by CallAPI is the opposite: it expects "Bearer <pat>".)
	req.Header.Set("Authorization", pat)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bitrise-mcp-dev-environments/1.0")
	req.Header.Set("X-Request-Source", "mcp")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list workspaces failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Bitrise v0.1 list responses wrap items under a top-level "data" array.
	var page struct {
		Data []Organization `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse organizations response: %w", err)
	}
	return page.Data, nil
}

// SoleWorkspace returns the user's workspace when they have exactly one. With
// zero or 2+ workspaces it returns a friendly error listing them, since no
// default can be picked unambiguously.
func SoleWorkspace(orgs []Organization) (Organization, error) {
	switch len(orgs) {
	case 0:
		return Organization{}, fmt.Errorf("no workspaces found for this account — create one in the Bitrise dashboard, then pass workspace_id")
	case 1:
		return orgs[0], nil
	default:
		var b strings.Builder
		for _, o := range orgs {
			if o.Name != "" {
				fmt.Fprintf(&b, "\n  %s (%s)", o.Name, o.Slug)
			} else {
				fmt.Fprintf(&b, "\n  %s", o.Slug)
			}
		}
		return Organization{}, fmt.Errorf("you belong to multiple Bitrise workspaces, so one can't be chosen automatically. ASK THE USER which workspace to use, then pass its slug as the workspace_id argument and reuse that same value on every following call this session. Do NOT retry across workspaces or list them one by one, and do NOT guess. Available workspaces:%s", b.String())
	}
}

type wsCacheEntry struct {
	slug      string
	expiresAt time.Time
}

var (
	workspaceCache    sync.Map // wsCacheKey(pat) -> wsCacheEntry
	workspaceCacheTTL = 5 * time.Minute
)

// ResolveSoleWorkspace returns the slug of the user's only workspace,
// auto-detecting it via GET /organizations. The result is cached per PAT for a
// short window so that, when no workspace is configured, repeated
// workspace-scoped tool calls don't each issue a discovery request.
//
// The zero- and multiple-workspace cases (SoleWorkspace's errors) are never
// cached, so a freshly created workspace is picked up promptly and the
// multi-workspace error always reflects current state.
func ResolveSoleWorkspace(ctx context.Context) (string, error) {
	pat, _ := ctx.Value(keyPAT).(string)
	key := wsCacheKey(pat)
	if pat != "" {
		if v, ok := workspaceCache.Load(key); ok {
			entry := v.(wsCacheEntry) //nolint:forcetypeassert
			if time.Now().Before(entry.expiresAt) {
				return entry.slug, nil
			}
			workspaceCache.Delete(key)
		}
	}

	orgs, err := ListOrganizations(ctx)
	if err != nil {
		return "", err
	}
	ws, err := SoleWorkspace(orgs)
	if err != nil {
		return "", err
	}
	if pat != "" {
		workspaceCache.Store(key, wsCacheEntry{slug: ws.Slug, expiresAt: time.Now().Add(workspaceCacheTTL)})
	}
	return ws.Slug, nil
}

// wsCacheKey hashes the PAT so the full token is not kept in memory.
func wsCacheKey(pat string) string {
	h := sha256.Sum256([]byte(pat))
	return fmt.Sprintf("%x", h[:8])
}
