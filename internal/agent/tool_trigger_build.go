package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

// TriggerBuildAfterFixTool kicks off a fresh Codemagic build for the
// mobile_app_native project after the agent has applied a fix in response
// to a previously-failed build. Pairs with read_build_logs to close the
// "fix-with-AI" loop kicked off from the BuildRow "Fix with AI" button
// (P1 #17 mobile pipeline audit).
//
// The tool is auth-scoped to the project owner via rc.User; the Controller
// callback (TriggerMobileBuild in RunDependencies) re-runs the
// BuildExpoMobile precondition checks so a forged call cannot escape the
// usual permission/limits gates. Returns the new build ID + per-platform
// task IDs so the agent (and the user, via the chat output) can follow
// the build progress.
type TriggerBuildAfterFixTool struct {
	Deps *RunDependencies
}

var _ ToolExecutor = (*TriggerBuildAfterFixTool)(nil)

func (t *TriggerBuildAfterFixTool) Name() string { return "trigger_build_after_fix" }

func (t *TriggerBuildAfterFixTool) Description() string {
	return "Trigger a new mobile build for this project. Use this ONLY when the user explicitly asks you to build, or AFTER you have written a fix to close the read-fix-rebuild loop kicked off by a previously-failed build. Do NOT call this automatically during initial app creation or when the user has not asked for a build. Returns the new build_id, per-platform task_id(s), and a status URL the user can follow. Pass platform='android', 'ios', or 'both' (default 'both') to control which platforms re-build."
}

func (t *TriggerBuildAfterFixTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"platform": map[string]any{
				"type":        "string",
				"enum":        []string{"ios", "android", "both"},
				"description": "Which platform(s) to rebuild. Defaults to 'both' so the rebuilt binary is consistent across iOS and Android.",
			},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

// triggerBuildOutput is the JSON shape returned to the agent.
type triggerBuildOutput struct {
	BuildID        string `json:"build_id"`
	IOSTaskID      string `json:"ios_task_id,omitempty"`
	AndroidTaskID  string `json:"android_task_id,omitempty"`
	StatusURL      string `json:"status_url"`
	PlatformsFired []string `json:"platforms_fired"`
	Note           string `json:"note,omitempty"`
}

func (t *TriggerBuildAfterFixTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Deps == nil {
		return "", false, fmt.Errorf("trigger_build_after_fix: no dependencies in context")
	}
	if rc.Deps.TriggerMobileBuild == nil {
		return "trigger_build_after_fix: build trigger callback is not wired in this environment", true, nil
	}
	if rc.Project == nil {
		return "trigger_build_after_fix: no project in context", true, nil
	}
	if rc.User == nil {
		return "trigger_build_after_fix: no authenticated user in context", true, nil
	}
	if rc.Project.ProjectType != models.ProjectTypeMobileAppNative {
		return "trigger_build_after_fix is only available for mobile_app_native projects", true, nil
	}

	var args struct {
		Platform string `json:"platform"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}

	platforms := normalizeBuildPlatforms(args.Platform)

	buildID, taskIDs, err := rc.Deps.TriggerMobileBuild(ctx, rc.Project, rc.User, platforms)
	if err != nil {
		// Surface the controller-side error message (already includes
		// "subscription does not allow", "already in progress", etc.) as
		// a tool-usage error so the model can react instead of crashing.
		return fmt.Sprintf("trigger_build_after_fix failed: %v", err), true, nil
	}

	out := triggerBuildOutput{
		BuildID:        buildID,
		PlatformsFired: platforms,
		// Status URL points at the studio's Builds tab so the user — and
		// the agent's reply — has a single clickable destination. The
		// frontend route is /apps/{id} (org slug is resolved client-side
		// from the user's session).
		StatusURL: fmt.Sprintf("%s/v2/apps/%s/builds", constants.WEMOB_API_BASE_URL, rc.Project.ID.String()),
	}
	if taskIDs != nil {
		if id, ok := taskIDs["ios"]; ok {
			out.IOSTaskID = id
		}
		if id, ok := taskIDs["android"]; ok {
			out.AndroidTaskID = id
		}
	}
	if out.IOSTaskID == "" && out.AndroidTaskID == "" {
		out.Note = "Build dispatched but no per-platform task IDs returned. Check the studio Builds tab."
	}

	b, mErr := json.Marshal(out)
	if mErr != nil {
		return "", false, fmt.Errorf("marshal output: %w", mErr)
	}
	return string(b), false, nil
}

// normalizeBuildPlatforms converts the schema input ("ios"/"android"/"both"
// — empty string defaults to "both") into the slice shape the
// MobileBuildTrigger callback expects.
func normalizeBuildPlatforms(arg string) []string {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "ios":
		return []string{"ios"}
	case "android":
		return []string{"android"}
	case "", "both":
		return []string{"android", "ios"}
	}
	// Unknown value — fall back to "both" rather than reject; the
	// controller layer is the source of truth for what's allowed.
	return []string{"android", "ios"}
}
