package devenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// BaseURL is set at init from config.
var BaseURL string

// WorkspaceID is set at init from config.
var WorkspaceID string

// WsPath prepends the workspace-scoped /v1 base to a resource path.
// "/sessions" → "/v1/workspaces/{id}/sessions"
func WsPath(path string) string {
	return "/v1/workspaces/" + WorkspaceID + path
}

// CallAPIParams configures an API call.
type CallAPIParams struct {
	Method string
	Path   string
	Params map[string]string
	Body   any
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// CallAPI makes an authenticated HTTP request to the Dev Environments backend.
func CallAPI(ctx context.Context, p CallAPIParams) (string, error) {
	authHeader, authValue, err := AuthFromCtx(ctx)
	if err != nil {
		return "", err
	}

	fullURL := BaseURL + p.Path
	if len(p.Params) > 0 {
		params := url.Values{}
		for k, v := range p.Params {
			if v != "" {
				params.Set(k, v)
			}
		}
		if encoded := params.Encode(); encoded != "" {
			fullURL += "?" + encoded
		}
	}

	var reqBody io.Reader
	if p.Body != nil {
		bodyBytes, err := json.Marshal(p.Body)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, p.Method, fullURL, reqBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(authHeader, authValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bitrise-mcp-dev-environments/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// CallAPILongTimeout makes an API call with a longer timeout for operations like file transfers.
func CallAPILongTimeout(ctx context.Context, p CallAPIParams) (string, error) {
	authHeader, authValue, err := AuthFromCtx(ctx)
	if err != nil {
		return "", err
	}

	fullURL := BaseURL + p.Path

	var reqBody io.Reader
	if p.Body != nil {
		bodyBytes, err := json.Marshal(p.Body)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, p.Method, fullURL, reqBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(authHeader, authValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bitrise-mcp-dev-environments/1.0")

	longClient := &http.Client{Timeout: 10 * time.Minute}
	resp, err := longClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}
