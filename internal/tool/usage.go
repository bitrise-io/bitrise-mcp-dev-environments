package tool

import (
	"context"
	"net/http"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// GetWorkspaceUsage reports the workspace's active-session resource usage.
var GetWorkspaceUsage = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_workspace_usage",
		mcp.WithTitleAnnotation("Get workspace usage"),
		mcp.WithDescription(`Get a point-in-time snapshot of the workspace's active devenv sessions: session counts and vCPU/memory totals split by OS, workspace-wide and per user, plus the workspace's vCPU quota per machine family.

This reports sessions currently consuming resources (starting, running, terminating, or draining). It is NOT a historical or billing-period report — poll it over time if you need trends.

Requires the workspace's billing-view permission (workspace owners and billing-managing custom roles); other members get a permission-denied error.

Response shape (zero-valued fields and empty objects may be omitted from the JSON — treat a missing field as 0/false/empty):
- totals: workspace-wide usage, split into linux, macos, and unknown (OS could not be determined) buckets. Each bucket has:
  - sessionCount: number of active sessions.
  - vcpu: total vCPUs across those sessions.
  - memoryGb: total memory in GB across those sessions.
- users: per-user breakdown, sorted by email. Each row has:
  - userSlug: the user's Bitrise slug, usable with other Bitrise APIs. Best-effort: may be empty even on user rows, so use isWorkspace (not an empty userSlug) to detect the workspace bucket.
  - email / username: the user's identity (username is best-effort and may be empty).
  - userId: internal identifier; prefer userSlug for cross-referencing.
  - isWorkspace: true for the single aggregate row of sessions owned by the workspace itself rather than by an individual user (for example sessions spawned by a preview link); that row has no userSlug/email.
  - totals: this row's usage, same linux/macos/unknown bucket shape as the workspace-wide totals.
- unknownMachineTypeCount: number of active sessions whose machine type had no resolvable vCPU/RAM spec. Those sessions are counted in sessionCount but contribute 0 to the vcpu/memory sums, so totals undercount when this is non-zero — mention that caveat when presenting the numbers.
- computeQuotas: the workspace's vCPU quota per machine family its plan includes, sorted by displayName. Empty when the quota could not be read; say so rather than reporting no quota. Each row has:
  - family: the machine family id, e.g. "m2pro".
  - displayName: the family's human-readable name, e.g. "M2 Pro".
  - usedVcpu: vCPUs held right now by the workspace's machines in the family, warm pool sessions included. Counted from machines rather than sessions, so it can briefly differ from the session totals.
  - quotaVcpu: the family's vCPU quota from the workspace's plan. Families the plan does not include are not listed.
  - os: "linux" or "macos", the operating system of the family's machines. Empty when it could not be determined. Most plans have one family per OS.
  A new session cannot start on a family once usedVcpu plus the new machine's vCPUs would exceed quotaVcpu.`),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := devenv.CallAPI(ctx, devenv.CallAPIParams{
			Method: http.MethodGet,
			Path:   devenv.WsPath(ctx, "/usage"),
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("get workspace usage", err), nil
		}
		return mcp.NewToolResultText(res), nil
	},
}
