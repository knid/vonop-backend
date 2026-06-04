package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type MCPToolExecutor struct {
	Tool MCPRuntimeTool
}

func (t MCPToolExecutor) Name() string { return t.Tool.PrefixedName }

func (t MCPToolExecutor) Description() string {
	if t.Tool.Description != "" {
		return t.Tool.Description
	}
	if t.Tool.DisplayName != "" {
		return "Call the " + t.Tool.DisplayName + " MCP tool."
	}
	return "Call an MCP tool connected to this project."
}

func (t MCPToolExecutor) Schema() map[string]any {
	if len(t.Tool.InputSchema) > 0 {
		return t.Tool.InputSchema
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t MCPToolExecutor) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Project == nil || rc.User == nil || rc.Profile == nil || rc.Deps == nil {
		return "MCP tool unavailable: missing run context", true, nil
	}
	if rc.Deps.ExecuteMCPTool == nil {
		return "MCP tool unavailable: backend executor is not configured", true, nil
	}

	policy := normalizeMCPPolicy(t.Tool.Policy)
	if policy == "blocked" {
		return fmt.Sprintf("MCP tool %q is blocked for agent %s", t.Name(), rc.Profile.ID), true, nil
	}

	var approvalID string
	var approvedByUserID string
	if policy == "ask" {
		if rc.Deps.CreateMCPToolApproval == nil || rc.Deps.WaitForMCPApproval == nil {
			return "MCP approval is required but the approval service is not configured", true, nil
		}
		approval, err := rc.Deps.CreateMCPToolApproval(ctx, MCPToolApprovalRequest{
			JobID:        rc.Deps.JobID,
			RunID:        rc.RunID.String(),
			ProjectID:    rc.Project.ID.String(),
			UserID:       rc.User.ID.String(),
			AgentID:      rc.Profile.ID,
			ConnectionID: t.Tool.ConnectionID,
			ToolID:       t.Tool.ToolID,
			ToolName:     t.Tool.ToolName,
			PrefixedName: t.Tool.PrefixedName,
			RiskLevel:    t.Tool.RiskLevel,
			Arguments:    input,
		})
		if err != nil {
			return fmt.Sprintf("Failed to create MCP approval: %v", err), true, nil
		}
		approvalID = approval.ID
		if rc.Emitter != nil {
			rc.Emitter.Emit(EventToolApprovalRequired, ToolApprovalRequiredEvent{
				RunID:        rc.RunID,
				ApprovalID:   approval.ID,
				ToolName:     t.Tool.PrefixedName,
				ConnectionID: t.Tool.ConnectionID,
				RiskLevel:    t.Tool.RiskLevel,
				InputPreview: stripToolInputForPreview(t.Tool.PrefixedName, string(input)),
				ExpiresAt:    approval.ExpiresAtUnixMs,
			})
		}
		decision, err := rc.Deps.WaitForMCPApproval(ctx, approval.ID)
		if err != nil {
			return fmt.Sprintf("MCP approval failed: %v", err), true, nil
		}
		approvedByUserID = decision.UserID
		if rc.Emitter != nil {
			rc.Emitter.Emit(EventToolApprovalResolved, ToolApprovalResolvedEvent{
				RunID:        rc.RunID,
				ApprovalID:   approval.ID,
				ToolName:     t.Tool.PrefixedName,
				ConnectionID: t.Tool.ConnectionID,
				Status:       decision.Status,
				Approved:     decision.Approved,
			})
		}
		if !decision.Approved {
			return fmt.Sprintf("MCP tool %q was not approved by the user", t.Name()), true, nil
		}
	}

	runID := ""
	if rc.RunID != uuid.Nil {
		runID = rc.RunID.String()
	}
	return rc.Deps.ExecuteMCPTool(ctx, MCPToolExecutionRequest{
		JobID:            rc.Deps.JobID,
		RunID:            runID,
		ProjectID:        rc.Project.ID.String(),
		UserID:           rc.User.ID.String(),
		AgentID:          rc.Profile.ID,
		ConnectionID:     t.Tool.ConnectionID,
		ToolID:           t.Tool.ToolID,
		ApprovalID:       approvalID,
		ToolName:         t.Tool.ToolName,
		PrefixedName:     t.Tool.PrefixedName,
		RiskLevel:        t.Tool.RiskLevel,
		Policy:           policy,
		Arguments:        input,
		ApprovedByUserID: approvedByUserID,
	})
}

func normalizeMCPPolicy(policy string) string {
	switch policy {
	case "auto", "ask", "blocked":
		return policy
	default:
		return "ask"
	}
}
