package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/constants"
)

// TriggerWebDeployTool queues a fresh Fly web deploy for the project after
// the agent has applied a fix in response to a previously-failed web
// deployment. Pairs with read_deploy_logs to close the "fix-with-AI" loop
// for web deploy failures.
//
// The tool is auth-scoped to the project owner via rc.User; the Controller
// callback (TriggerWebDeploy in RunDependencies) re-runs the usual deploy
// precondition checks so a forged call cannot escape the permission/limits
// gates.
type TriggerWebDeployTool struct {
	Deps *RunDependencies
}

var _ ToolExecutor = (*TriggerWebDeployTool)(nil)

func (t *TriggerWebDeployTool) Name() string { return "trigger_web_deploy" }

func (t *TriggerWebDeployTool) Description() string {
	return "Trigger a new web deployment for this project after applying a fix to the codebase. Use this AFTER you have written the fix to close the read-fix-redeploy loop kicked off by a web deploy failure. Returns the new deployment_id and a status URL the user can follow."
}

func (t *TriggerWebDeployTool) Schema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

type triggerWebDeployOutput struct {
	DeploymentID string `json:"deployment_id"`
	StatusURL    string `json:"status_url"`
	Note         string `json:"note,omitempty"`
}

func (t *TriggerWebDeployTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Deps == nil {
		return "", false, fmt.Errorf("trigger_web_deploy: no dependencies in context")
	}
	if rc.Deps.TriggerWebDeploy == nil {
		return "trigger_web_deploy: deploy trigger callback is not wired in this environment", true, nil
	}
	if rc.Project == nil {
		return "trigger_web_deploy: no project in context", true, nil
	}
	if rc.User == nil {
		return "trigger_web_deploy: no authenticated user in context", true, nil
	}

	deploymentID, err := rc.Deps.TriggerWebDeploy(ctx, rc.Project, rc.User)
	if err != nil {
		return fmt.Sprintf("trigger_web_deploy failed: %v", err), true, nil
	}

	out := triggerWebDeployOutput{
		DeploymentID: deploymentID,
		StatusURL:    fmt.Sprintf("%s/v2/apps/%s/deploy/status", constants.WEMOB_API_BASE_URL, rc.Project.ID.String()),
	}
	if deploymentID == "" {
		out.Note = "Deploy dispatched but no deployment ID returned. Check the studio Deploy tab."
	}

	b, mErr := json.Marshal(out)
	if mErr != nil {
		return "", false, fmt.Errorf("marshal output: %w", mErr)
	}
	return string(b), false, nil
}
