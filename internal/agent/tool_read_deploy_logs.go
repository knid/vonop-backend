package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// ReadDeployLogsTool exposes the latest failed web deployment's status,
// error message, and structured error details to the chat agent so it can
// diagnose Fly deploy failures from inside the conversation.
type ReadDeployLogsTool struct {
	Deps *RunDependencies
}

var _ ToolExecutor = (*ReadDeployLogsTool)(nil)

func (t *ReadDeployLogsTool) Name() string { return "read_deploy_logs" }

func (t *ReadDeployLogsTool) Description() string {
	return "Read context for the most recent FAILED web deployment of this project: deployment status, failure stage, error message, and build log when available. Use this before proposing a code fix for a failed web deployment."
}

func (t *ReadDeployLogsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"deployment_id": map[string]any{
				"type":        "string",
				"description": "Optional UUID of the AppDeployment to inspect. When omitted, returns the most recent failed web deployment for this project.",
			},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

type readDeployLogsOutput struct {
	Found      bool              `json:"found"`
	Deployment *deployLogInfo    `json:"deployment,omitempty"`
	Events     []deployEventInfo `json:"events,omitempty"`
	Note       string            `json:"note,omitempty"`
}

type deployLogInfo struct {
	DeploymentID string         `json:"deployment_id"`
	Status       string         `json:"status"`
	Stage        string         `json:"stage,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	BuildLog     string         `json:"build_log,omitempty"`
	ProviderData map[string]any `json:"provider_data,omitempty"`
	CreatedAt    string         `json:"created_at,omitempty"`
	UpdatedAt    string         `json:"updated_at,omitempty"`
}

type deployEventInfo struct {
	EventType  string `json:"event_type"`
	Message    string `json:"message,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

func (t *ReadDeployLogsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Deps == nil || rc.Deps.DB == nil {
		return "", false, fmt.Errorf("read_deploy_logs: no database available")
	}
	if rc.Project == nil {
		return "read_deploy_logs: no project in context", true, nil
	}
	if rc.User == nil {
		return "read_deploy_logs: no authenticated user in context", true, nil
	}

	var args struct {
		DeploymentID string `json:"deployment_id"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}

	deployments, err := rc.Deps.DB.GetAppDeploymentsByProjectID(rc.Project.ID.String())
	if err != nil {
		return fmt.Sprintf("read_deploy_logs: failed to load deployments: %v", err), true, nil
	}

	var picked *models.AppDeployment
	if args.DeploymentID != "" {
		for i := range deployments {
			if deployments[i].ID.String() == args.DeploymentID {
				picked = &deployments[i]
				break
			}
		}
		if picked == nil {
			return fmt.Sprintf("deployment_id %s not found among this project's deployments", args.DeploymentID), true, nil
		}
	} else {
		for i := range deployments {
			if deployments[i].Status == "failed" || deployments[i].Status == "error" {
				picked = &deployments[i]
				break
			}
		}
		if picked == nil {
			out := readDeployLogsOutput{
				Found: false,
				Note:  "No failed web deployments found for this project. The most recent deployment may still be in progress or may have succeeded.",
			}
			b, _ := json.Marshal(out)
			return string(b), false, nil
		}
	}

	info := &deployLogInfo{
		DeploymentID: picked.ID.String(),
		Status:       picked.Status,
		ErrorMessage: picked.ErrorMessage,
		ProviderData: picked.ProviderData,
		CreatedAt:    picked.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    picked.UpdatedAt.UTC().Format(time.RFC3339),
	}

	if picked.ProviderData != nil {
		if errDetail, ok := picked.ProviderData["error"].(map[string]any); ok {
			if stage, ok := errDetail["stage"].(string); ok {
				info.Stage = stage
			}
			if log, ok := errDetail["build_log"].(string); ok {
				info.BuildLog = log
			}
		}
	}

	if info.Stage == "" && info.ErrorMessage != "" {
		if idx := indexOfFirstColon(info.ErrorMessage); idx > 0 {
			info.Stage = info.ErrorMessage[:idx]
		}
	}

	const maxLogChars = 4000
	if len(info.BuildLog) > maxLogChars {
		info.BuildLog = "...(truncated)...\n" + info.BuildLog[len(info.BuildLog)-maxLogChars:]
	}
	const maxErrChars = 2000
	if len(info.ErrorMessage) > maxErrChars {
		info.ErrorMessage = info.ErrorMessage[:maxErrChars] + "\n...(truncated)"
	}

	events, eerr := database.ListDeploymentEvents(ctx, rc.Deps.DB.DB, picked.ID)
	infos := make([]deployEventInfo, 0, len(events))
	if eerr == nil {
		for _, ev := range events {
			msg := ""
			if ev.Metadata != nil {
				if m, ok := ev.Metadata["message"].(string); ok {
					msg = m
				}
			}
			infos = append(infos, deployEventInfo{
				EventType:  ev.EventType,
				Message:    msg,
				OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339),
			})
		}
	}

	out := readDeployLogsOutput{
		Found:      true,
		Deployment: info,
		Events:     infos,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}

func indexOfFirstColon(s string) int {
	for i, r := range s {
		if r == ':' {
			return i
		}
	}
	return -1
}
