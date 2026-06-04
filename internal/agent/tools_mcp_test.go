package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

type mcpCaptureEmitter struct {
	events []Event
}

func (e *mcpCaptureEmitter) Emit(eventType string, data any) bool {
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}

func (e *mcpCaptureEmitter) Buffer() []Event { return e.events }
func (e *mcpCaptureEmitter) Close()          {}

func TestMCPToolExecutorBlockedPolicy(t *testing.T) {
	executed := false
	tool := MCPToolExecutor{Tool: MCPRuntimeTool{
		ConnectionID: "conn",
		ToolID:       "tool",
		ToolName:     "send_email",
		PrefixedName: "mcp_email__send_email",
		Policy:       "blocked",
		RiskLevel:    ToolRiskHigh,
	}}
	rc := &RunContext{
		Project: &models.AppProject{ID: uuid.New()},
		User:    &models.User{ID: uuid.New()},
		Profile: &AgentProfile{ID: "web-builder"},
		Deps: &RunDependencies{
			ExecuteMCPTool: func(context.Context, MCPToolExecutionRequest) (string, bool, error) {
				executed = true
				return "executed", false, nil
			},
		},
	}

	result, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !isErr {
		t.Fatalf("isErr = false, want true")
	}
	if executed {
		t.Fatalf("blocked MCP tool executed")
	}
	if result == "" {
		t.Fatalf("result is empty")
	}
}

func TestMCPToolExecutorApprovalFlow(t *testing.T) {
	var executedReq MCPToolExecutionRequest
	emitter := &mcpCaptureEmitter{}
	projectID := uuid.New()
	userID := uuid.New()
	tool := MCPToolExecutor{Tool: MCPRuntimeTool{
		ConnectionID: "11111111-1111-1111-1111-111111111111",
		ToolID:       "22222222-2222-2222-2222-222222222222",
		ToolName:     "send_email",
		PrefixedName: "mcp_email__send_email",
		Policy:       "ask",
		RiskLevel:    ToolRiskHigh,
	}}
	rc := &RunContext{
		RunID:   uuid.New(),
		Project: &models.AppProject{ID: projectID},
		User:    &models.User{ID: userID},
		Profile: &AgentProfile{ID: "web-builder"},
		Emitter: emitter,
		Deps: &RunDependencies{
			JobID: "33333333-3333-3333-3333-333333333333",
			CreateMCPToolApproval: func(_ context.Context, req MCPToolApprovalRequest) (*MCPToolApproval, error) {
				if req.ProjectID != projectID.String() {
					t.Fatalf("approval project id = %s", req.ProjectID)
				}
				return &MCPToolApproval{ID: "approval-1", Status: "pending", ExpiresAtUnixMs: 123}, nil
			},
			WaitForMCPApproval: func(context.Context, string) (MCPApprovalDecision, error) {
				return MCPApprovalDecision{Approved: true, Status: "approved", UserID: userID.String()}, nil
			},
			ExecuteMCPTool: func(_ context.Context, req MCPToolExecutionRequest) (string, bool, error) {
				executedReq = req
				return "sent", false, nil
			},
		},
	}

	result, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{"to":"a@example.com"}`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if isErr || result != "sent" {
		t.Fatalf("Execute = (%q, %v), want sent/false", result, isErr)
	}
	if executedReq.ApprovalID != "approval-1" {
		t.Fatalf("approval id = %q", executedReq.ApprovalID)
	}
	if executedReq.ApprovedByUserID != userID.String() {
		t.Fatalf("approved by = %q", executedReq.ApprovedByUserID)
	}
	if len(emitter.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(emitter.events))
	}
	if emitter.events[0].Type != EventToolApprovalRequired || emitter.events[1].Type != EventToolApprovalResolved {
		t.Fatalf("events = %#v", emitter.events)
	}
}
