package devenv

import (
	"encoding/json"
	"fmt"
	"strings"
)

// APIError is a non-2xx response from the Dev Environments backend. Refusal
// is set when the backend refused the request rather than rejecting it — a
// full pool, a workspace out of CPUs — and renders as text an agent can act
// on instead of the raw JSON envelope.
type APIError struct {
	StatusCode int
	Body       string
	Refusal    *WarmPoolRefusal
}

func (e *APIError) Error() string {
	if e.Refusal != nil {
		return e.Refusal.Text()
	}
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Body)
}

// WarmPoolRefusal is the detail of a RESOURCE_EXHAUSTED refusal: why the
// session was not handed out, the caller's own sessions in use from the
// pool, and the next steps.
type WarmPoolRefusal struct {
	// Message is the relay sentence, written to be repeated to a person.
	Message string `json:"-"`
	// Reason is POOL_FULL or CPU_QUOTA_EXHAUSTED.
	Reason string `json:"reason"`
	// Action is what was refused: claim, restore or create.
	Action       string `json:"action"`
	WarmPoolID   string `json:"warmPoolId"`
	WarmPoolName string `json:"warmPoolName"`
	InUse        int    `json:"inUse"`
	MaxSessions  int    `json:"maxSessions"`
	Family       string `json:"family"`
	UsedVCPU     *int   `json:"usedVcpu"`
	QuotaVCPU    int    `json:"quotaVcpu"`
	YourSessions []struct {
		SessionID string `json:"sessionId"`
		Name      string `json:"name"`
		Status    string `json:"status"`
	} `json:"yourSessions"`
	NextSteps []string `json:"nextSteps"`
}

// Text renders the refusal: the relay message, then the caller's own
// sessions with their IDs (the message names them without IDs), then the
// next steps in order.
func (r *WarmPoolRefusal) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Refused (%s): %s", r.Reason, r.Message)
	if len(r.YourSessions) > 0 {
		b.WriteString("\n\nYour sessions in use from this pool:")
		for _, s := range r.YourSessions {
			fmt.Fprintf(&b, "\n- %s (%s) — session_id %s", s.Name, s.Status, s.SessionID)
		}
	}
	if len(r.NextSteps) > 0 {
		b.WriteString("\n\nNext steps:")
		for i, step := range r.NextSteps {
			fmt.Fprintf(&b, "\n%d. %s", i+1, step)
		}
	}
	return b.String()
}

const warmPoolRefusalType = "type.googleapis.com/codespaces.v1.WarmPoolRefusal"

// newAPIError reads the gRPC-gateway status envelope out of a non-2xx body.
// A refusal (a codespaces.v1.WarmPoolRefusal among the details) is kept
// structured; anything else keeps the raw body.
func newAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode, Body: string(body)}
	var envelope struct {
		Message string            `json:"message"`
		Details []json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return apiErr
	}
	for _, raw := range envelope.Details {
		var typed struct {
			Type string `json:"@type"`
		}
		if json.Unmarshal(raw, &typed) != nil || typed.Type != warmPoolRefusalType {
			continue
		}
		var refusal WarmPoolRefusal
		if json.Unmarshal(raw, &refusal) != nil || refusal.Reason == "" {
			continue
		}
		refusal.Message = envelope.Message
		apiErr.Refusal = &refusal
		return apiErr
	}
	return apiErr
}
