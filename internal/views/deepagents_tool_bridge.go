package views

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/utils"
)

// DeepAgentsToolResult is the canonical response body for the Python
// tool-execute bridge. Python wraps one tool function per ToolSpec; each
// call returns a single DeepAgentsToolResult that the model sees verbatim.
type DeepAgentsToolResult struct {
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
	Summary    string `json:"summary"`
	DurationMs int    `json:"duration_ms"`
}

// HandleDeepAgentsToolExecute is the Go-side bridge for first-class Wemob
// tools called by the Python DeepAgents runtime. The Python side binds one
// Python function per ToolSpec and POSTs the model's raw arguments here.
//
// Path: POST /internal/v1/agent-runs/{run_id}/tools/{tool_name}/execute
//
// Auth: Authorization: Bearer <DEEPAGENTS_INTERNAL_TOKEN> (X-Internal-Token
// is also accepted for symmetry with other internal endpoints).
//
// The endpoint intentionally does NOT emit generic tool_call_start /
// tool_call_result events — Python emits those into the stream itself so the
// SSE timeline stays consistent and there is no double-emit.
//
// The only events this endpoint publishes are tool_approval_required and
// tool_approval_resolved for MCP tools whose policy is "ask". The Python
// tool blocks on the synchronous wait, but the UI must still see the
// approval prompt mid-stream so the user can click yes/no.
func (v *HttpViews) HandleDeepAgentsToolExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.JSONErrorCode(w, "method_not_allowed", http.StatusMethodNotAllowed, nil)
		return
	}

	expected := strings.TrimSpace(v.Controller.SecretManager.GetSecret("DEEPAGENTS_INTERNAL_TOKEN"))
	if expected == "" {
		slog.Warn("deepagents tool execute: DEEPAGENTS_INTERNAL_TOKEN not configured; rejecting")
		utils.JSONErrorCode(w, "not_configured", http.StatusServiceUnavailable, nil)
		return
	}

	presented := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(presented), "bearer ") {
		presented = strings.TrimSpace(presented[len("bearer "):])
	} else if x := strings.TrimSpace(r.Header.Get("X-Internal-Token")); x != "" {
		presented = x
	}
	if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
		utils.JSONErrorCode(w, "unauthorized", http.StatusUnauthorized, nil)
		return
	}

	runIDStr := strings.TrimSpace(chi.URLParam(r, "run_id"))
	toolName := strings.TrimSpace(chi.URLParam(r, "tool_name"))
	if runIDStr == "" || toolName == "" {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, nil)
		return
	}
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, map[string]any{"reason": "run_id is not a uuid"})
		return
	}

	var body struct {
		JobID      string          `json:"job_id"`
		ProjectID  string          `json:"project_id"`
		UserID     string          `json:"user_id"`
		ToolCallID string          `json:"tool_call_id"`
		Arguments  json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, map[string]any{"reason": "invalid json body"})
		return
	}
	if body.JobID == "" || body.ProjectID == "" || body.UserID == "" {
		utils.JSONErrorCode(w, "bad_request", http.StatusBadRequest, map[string]any{"reason": "job_id, project_id, user_id are required"})
		return
	}

	// Run must still be active and the IDs must match what Python sent.
	run, err := v.Controller.DB.GetAgentRun(runID)
	if err != nil || run == nil {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, map[string]any{"reason": "run not found"})
		return
	}
	if run.Status != models.AgentRunStatusRunning {
		utils.JSONErrorCode(w, "conflict", http.StatusConflict, map[string]any{"reason": "run is not active"})
		return
	}
	if run.AppProjectID.String() != body.ProjectID || run.UserID.String() != body.UserID {
		utils.JSONErrorCode(w, "forbidden", http.StatusForbidden, map[string]any{"reason": "project_id or user_id mismatch"})
		return
	}
	// Note: the GenerationJob row has no FK to AppAgentRun today, so
	// job_id is treated as a hint and not cross-checked against the DB.
	// The token, run state, and project/user ID match are the source of
	// truth. Python learns about job_id from the run_start envelope.

	// Load the project + user fresh; never trust Python's claim.
	project, err := v.Controller.DB.GetAppProjectByID(body.ProjectID)
	if err != nil || project == nil {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, map[string]any{"reason": "project not found"})
		return
	}
	user, err := v.Controller.DB.GetUserById(body.UserID)
	if err != nil || user == nil {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, map[string]any{"reason": "user not found"})
		return
	}

	// Build a per-call registry. The Python runtime must NEVER see file
	// tools, skill tools, update_todos, or create_design — see the
	// FilterRegistryForDeepAgents rationale in registry.go.
	modelName := strings.TrimSpace(v.Controller.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL"))
	deps := v.Controller.AgentDeps(user, body.JobID, "deepagents-bridge")
	profile := agent.ResolveAgentProfile(project)
	rc := agent.NewRunContext(runID, profile, project, user, deps)

	registry := agent.BuildRegistryFor(project, deps, modelName)
	registry = agent.FilterRegistryForDeepAgents(registry)
	allowed := map[string]bool{}
	for _, n := range registry.Names() {
		allowed[n] = true
	}
	if !allowed[toolName] {
		slog.Warn("deepagents tool execute: tool not in allowed registry", "tool", toolName, "run_id", runID)
		utils.JSONError(w, constants.ErrFeatureNotAvailable,
			fmt.Sprintf("tool %q is not allowed for the DeepAgents runtime", toolName),
			http.StatusForbidden, nil)
		return
	}

	// MCP tools get a specialized executor that goes through approval
	// flow and publishes approval events. Anything else dispatches
	// through the standard registry path.
	if isMCPToolName(toolName) {
		executeMCPToolForDeepAgents(w, v, rc, toolName, body.Arguments, runID, body.JobID, body.ToolCallID, project, user, r)
		return
	}

	// Apply Go guardrails before executing the tool. This catches
	// read-only-mode blocking, human-approval requirements, and skill-
	// scoped tool allowlists.
	if decision := agent.ApplyToolGuardrails(rc, toolName, body.Arguments); !decision.Allowed {
		slog.Warn("deepagents tool execute: blocked by guardrail", "run_id", runID, "tool", toolName, "message", decision.Message)
		writeToolResult(w, DeepAgentsToolResult{
			Result:     decision.Message,
			IsError:    true,
			Summary:    decision.Message,
			DurationMs: 0,
		})
		return
	}

	startedAt := time.Now()
	result, isError, execErr := registry.Execute(r.Context(), rc, toolName, body.Arguments)
	duration := time.Since(startedAt)

	if execErr != nil {
		slog.Error("deepagents tool execute: registry error", "run_id", runID, "tool", toolName, "error", execErr)
		writeToolResult(w, DeepAgentsToolResult{
			Result:     fmt.Sprintf("tool %q failed: %v", toolName, execErr),
			IsError:    true,
			Summary:    firstLine(fmt.Sprintf("tool %q failed: %v", toolName, execErr)),
			DurationMs: int(duration.Milliseconds()),
		})
		return
	}

	writeToolResult(w, DeepAgentsToolResult{
		Result:     truncateToolResult(result, 16_000),
		IsError:    isError,
		Summary:    firstLine(result),
		DurationMs: int(duration.Milliseconds()),
	})
}

// executeMCPToolForDeepAgents runs an MCP-prefixed tool through the same
// approval path the native runtime uses, and publishes approval events to
// the jobstream so the UI can render them while the Python tool blocks.
func executeMCPToolForDeepAgents(
	w http.ResponseWriter,
	v *HttpViews,
	rc *agent.RunContext,
	toolName string,
	arguments json.RawMessage,
	runID uuid.UUID,
	jobID, toolCallID string,
	project *models.AppProject,
	user *models.User,
	r *http.Request,
) {
	startedAt := time.Now()
	tool, ok := findMCPRuntimeTool(rc.Deps, project, toolName)
	if !ok {
		utils.JSONErrorCode(w, "not_found", http.StatusNotFound, map[string]any{"reason": "mcp tool not found"})
		return
	}
	policy := normalizeMCPPolicy(tool.Policy)

	approvalID := ""
	approvedBy := ""
	if policy == "ask" {
		approval, err := rc.Deps.CreateMCPToolApproval(r.Context(), agent.MCPToolApprovalRequest{
			JobID:        jobID,
			RunID:        runID.String(),
			ProjectID:    project.ID.String(),
			UserID:       user.ID.String(),
			AgentID:      rc.Profile.ID,
			ConnectionID: tool.ConnectionID,
			ToolID:       tool.ToolID,
			ToolName:     tool.ToolName,
			PrefixedName: tool.PrefixedName,
			RiskLevel:    tool.RiskLevel,
			Arguments:    arguments,
		})
		if err != nil {
			writeToolResult(w, DeepAgentsToolResult{
				Result:     fmt.Sprintf("MCP approval create failed: %v", err),
				IsError:    true,
				Summary:    "approval create failed",
				DurationMs: int(time.Since(startedAt).Milliseconds()),
			})
			return
		}
		approvalID = approval.ID
		publishToolApproval(v, jobID, agent.ToolApprovalRequiredEvent{
			RunID:        runID,
			ApprovalID:   approval.ID,
			ToolCallID:   toolCallID,
			ToolName:     tool.PrefixedName,
			ConnectionID: tool.ConnectionID,
			RiskLevel:    tool.RiskLevel,
			InputPreview: arguments,
			ExpiresAt:    approval.ExpiresAtUnixMs,
		})

		decision, err := rc.Deps.WaitForMCPApproval(r.Context(), approval.ID)
		if err != nil {
			publishToolApproval(v, jobID, agent.ToolApprovalResolvedEvent{
				RunID:        runID,
				ApprovalID:   approval.ID,
				ToolName:     tool.PrefixedName,
				ConnectionID: tool.ConnectionID,
				Status:       "error",
				Approved:     false,
			})
			writeToolResult(w, DeepAgentsToolResult{
				Result:     fmt.Sprintf("MCP approval wait failed: %v", err),
				IsError:    true,
				Summary:    "approval wait failed",
				DurationMs: int(time.Since(startedAt).Milliseconds()),
			})
			return
		}
		approvedBy = decision.UserID
		publishToolApproval(v, jobID, agent.ToolApprovalResolvedEvent{
			RunID:        runID,
			ApprovalID:   approval.ID,
			ToolName:     tool.PrefixedName,
			ConnectionID: tool.ConnectionID,
			Status:       decision.Status,
			Approved:     decision.Approved,
		})
		if !decision.Approved {
			writeToolResult(w, DeepAgentsToolResult{
				Result:     fmt.Sprintf("MCP tool %q was not approved by the user", toolName),
				IsError:    true,
				Summary:    "not approved",
				DurationMs: int(time.Since(startedAt).Milliseconds()),
			})
			return
		}
	}

	result, isError, err := rc.Deps.ExecuteMCPTool(r.Context(), agent.MCPToolExecutionRequest{
		JobID:            jobID,
		RunID:            runID.String(),
		ProjectID:        project.ID.String(),
		UserID:           user.ID.String(),
		AgentID:          rc.Profile.ID,
		ConnectionID:     tool.ConnectionID,
		ToolID:           tool.ToolID,
		ApprovalID:       approvalID,
		ToolName:         tool.ToolName,
		PrefixedName:     tool.PrefixedName,
		RiskLevel:        tool.RiskLevel,
		Policy:           policy,
		Arguments:        arguments,
		ApprovedByUserID: approvedBy,
	})
	if err != nil {
		writeToolResult(w, DeepAgentsToolResult{
			Result:     fmt.Sprintf("MCP tool %q failed: %v", toolName, err),
			IsError:    true,
			Summary:    "mcp tool failed",
			DurationMs: int(time.Since(startedAt).Milliseconds()),
		})
		return
	}

	writeToolResult(w, DeepAgentsToolResult{
		Result:     truncateToolResult(result, 16_000),
		IsError:    isError,
		Summary:    firstLine(result),
		DurationMs: int(time.Since(startedAt).Milliseconds()),
	})
}

func writeToolResult(w http.ResponseWriter, res DeepAgentsToolResult) {
	utils.JSONResponse(w, res, http.StatusOK)
}

func publishToolApproval(v *HttpViews, jobID string, payload any) {
	if v == nil || v.Controller == nil || v.Controller.RedisPubSub == nil || jobID == "" {
		return
	}
	evType := ""
	switch payload.(type) {
	case agent.ToolApprovalRequiredEvent:
		evType = agent.EventToolApprovalRequired
	case agent.ToolApprovalResolvedEvent:
		evType = agent.EventToolApprovalResolved
	default:
		return
	}
	_ = jobstream.PublishEvent(context.Background(), v.Controller.RedisPubSub, jobID, responses.SSEEvent{
		Event: evType,
		Data:  payload,
	})
}

// findMCPRuntimeTool looks up an MCP tool by its prefixed name on the
// project. Mirrors registerMCPTools on the agent side so the runtime and
// bridge agree on what is allowed.
func findMCPRuntimeTool(deps *agent.RunDependencies, project *models.AppProject, name string) (agent.MCPRuntimeTool, bool) {
	if deps == nil || deps.ListMCPTools == nil || project == nil {
		return agent.MCPRuntimeTool{}, false
	}
	tools, err := deps.ListMCPTools(project.ID.String(), "")
	if err != nil {
		return agent.MCPRuntimeTool{}, false
	}
	for _, t := range tools {
		if t.PrefixedName == name {
			return t, true
		}
	}
	return agent.MCPRuntimeTool{}, false
}

func isMCPToolName(name string) bool {
	return strings.Contains(name, ":") || strings.Contains(name, "mcp_")
}

func normalizeMCPPolicy(policy string) string {
	switch policy {
	case "auto", "ask", "blocked":
		return policy
	default:
		return "ask"
	}
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func truncateToolResult(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}
