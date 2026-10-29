package tool

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bitrise-io/bitrise-mcp-dev-environments/internal/devenv"
	"github.com/mark3labs/mcp-go/mcp"
)

// logStages maps the tool's stage names to the backend's LogStage numbers.
var logStages = map[string]string{
	"warmup":      "1",
	"startup":     "2",
	"on_create":   "3",
	"on_claim":    "4",
	"on_restore":  "5",
	"system":      "6",
	"personalize": "7",
}

// The log read stops after a quiet gap (the backend replays the log, then
// holds the stream open for new output and never signals its end) or at the
// budget, kept well under the hosted server's request timeout.
var (
	logIdleGap     = 2 * time.Second
	logFirstOutput = 8 * time.Second
	logBudget      = 20 * time.Second
	logMaxBytes    = 64 * 1024
)

// GetSessionLogs returns what one log stage of a session holds so far.
var GetSessionLogs = devenv.Tool{
	Definition: mcp.NewTool("bitrise_devenv_get_logs",
		mcp.WithTitleAnnotation("Get session logs"),
		mcp.WithDescription(`Read a session's setup log: what one lifecycle hook (or, on a legacy session, the warmup or startup script) printed so far. Returns the log text: every run of the hook in order, or only one run with attempt.

WHEN TO USE THIS: a hook failed (a run in lifecycle.runs of bitrise_devenv_get has state HOOK_RUN_STATE_FAILED, or lifecycle.phase is LIFECYCLE_PHASE_SETUP_FAILED / PERSONALIZATION_FAILED / RESTORE_FAILED) and you need to know why, or setup is slow and you want to see where it is. It works while On create is still running, when the terminal and bitrise_devenv_execute are refused. Read the log before deciding between bitrise_devenv_run_hook (transient cause) and fixing the template (script bug).

stage: on_create, on_claim, on_restore — that hook's output; personalize — the platform's system setup step; system — the platform's own boot output, system setup included. On a session without lifecycle hooks (lifecycle.mode "legacy"): warmup or startup.

The read returns once the log has been quiet for a moment, so output written after that is not included; call again to see more. Only the last 64 KB are returned, with a note when earlier output was cut.`),
		mcp.WithString("session_id", mcp.Description("The unique identifier (UUID) of the session"), mcp.Required()),
		mcp.WithString("stage",
			mcp.Description("Which log to read: on_create, on_claim, on_restore, personalize or system on a session with lifecycle hooks; warmup or startup on a legacy session."),
			mcp.Enum("on_create", "on_claim", "on_restore", "personalize", "system", "warmup", "startup"),
			mcp.Required(),
		),
		mcp.WithNumber("attempt", mcp.Description("Optional, hook stages only: read only this attempt of the hook (1-based, the attempt numbers in lifecycle.runs). Omit for every attempt in order.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	),
	Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sessionID, err := requireUUID(request, "session_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		stage := request.GetString("stage", "")
		num, ok := logStages[stage]
		if !ok {
			return mcp.NewToolResultError("stage must be one of on_create, on_claim, on_restore, personalize, system, warmup, startup"), nil
		}
		params := map[string]string{}
		if v, ok := request.GetArguments()["attempt"]; ok {
			n, isNum := v.(float64)
			if !isNum || n < 1 || n != float64(int(n)) {
				return mcp.NewToolResultError("attempt must be a whole number, 1 or more"), nil
			}
			params["attempt"] = strconv.Itoa(int(n))
		}
		read, err := devenv.ReadLogStream(ctx, devenv.LogReadParams{
			Path:            devenv.WsPath(ctx, fmt.Sprintf("/sessions/%s/logs/%s", sessionID, num)),
			Params:          params,
			Idle:            logIdleGap,
			FirstOutputWait: logFirstOutput,
			Budget:          logBudget,
			MaxBytes:        logMaxBytes,
		})
		if err != nil {
			// The backend answers 404 until the stage has produced output.
			if strings.Contains(err.Error(), "status 404") {
				return mcp.NewToolResultText(formatLogRead(stage, devenv.LogRead{})), nil
			}
			return mcp.NewToolResultErrorFromErr("get session logs", err), nil
		}
		return mcp.NewToolResultText(formatLogRead(stage, read)), nil
	},
}

// formatLogRead adds what the agent needs to know about the read to the log.
func formatLogRead(stage string, read devenv.LogRead) string {
	var sb strings.Builder
	if read.Truncated {
		sb.WriteString(fmt.Sprintf("[earlier output cut: showing the last %d KB]\n", logMaxBytes/1024))
	}
	if read.Text == "" {
		sb.WriteString(fmt.Sprintf("[no %s output yet]", stage))
	} else {
		sb.WriteString(read.Text)
	}
	if read.Incomplete {
		sb.WriteString("\n[still printing: this is the output so far; call again for more]")
	}
	return sb.String()
}
