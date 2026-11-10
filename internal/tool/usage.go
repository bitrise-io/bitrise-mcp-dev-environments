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
  - sessions: the active sessions behind this row's totals, oldest first. Filled ONLY when terminating any session is enabled for the workspace (a per-workspace capability, enabled on request); otherwise empty or missing, and the rest of the report is the same. The ids feed bitrise_devenv_terminate, which with that capability and this permission stops any of these sessions, whoever owns it: read this report, pick ids, terminate. Each session has:
    - id: the session's UUID, the session_id bitrise_devenv_terminate takes.
    - name: the session's name.
    - os: "linux" or "macos"; empty when it could not be determined (the session is counted in the unknown bucket).
    - machineType: the machine type the session runs on.
    - vcpu / memoryGb: the machine's vCPUs and memory in GB; 0 when the machine type's spec is unknown.
    - status: SESSION_STATUS_STARTING, SESSION_STATUS_RUNNING, SESSION_STATUS_TERMINATING or SESSION_STATUS_DRAINING.
    - createdAt: when the session was created, i.e. its age. A session terminated and restored since keeps its original creation time.
    - heldByWarmPoolId: non-empty when a warm pool holds the session rather than anyone using it (an unclaimed warm session, or the machine a pool keeps after its setup failed); the value is that pool's id. bitrise_devenv_terminate refuses such a session: lower the pool's pool_size with bitrise_devenv_update_warm_pool instead (0 empties it), or bitrise_devenv_retry_warm_pool for a pool paused after a failed setup. Empty for every other session, including one already claimed from a pool.
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
