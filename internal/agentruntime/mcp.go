package agentruntime

import (
	"context"
	"encoding/json"
)

const (
	ToolRiskLow         = "low"
	ToolRiskMedium      = "medium"
	ToolRiskHigh        = "high"
	ToolRiskDestructive = "destructive"
)

type MCPRuntimeTool struct {
	ConnectionID string
	ToolID       string
	ToolName     string
	PrefixedName string
	DisplayName  string
	Description  string
	InputSchema  map[string]any
	RiskLevel    string
	Policy       string
}

type MCPToolApprovalRequest struct {
	JobID        string
	RunID        string
	ProjectID    string
	UserID       string
	AgentID      string
	ConnectionID string
	ToolID       string
	ToolName     string
	PrefixedName string
	RiskLevel    string
	Arguments    json.RawMessage
}

type MCPToolApproval struct {
	ID              string
	Status          string
	ExpiresAtUnixMs int64
}

type MCPApprovalDecision struct {
	Approved bool
	Status   string
	UserID   string
}

type MCPToolExecutionRequest struct {
	JobID            string
	RunID            string
	ProjectID        string
	UserID           string
	AgentID          string
	ConnectionID     string
	ToolID           string
	ApprovalID       string
	ToolName         string
	PrefixedName     string
	RiskLevel        string
	Policy           string
	Arguments        json.RawMessage
	ApprovedByUserID string
}

type MCPRuntimeToolLister func(projectID, agentID string) ([]MCPRuntimeTool, error)
type MCPToolApprovalCreator func(ctx context.Context, req MCPToolApprovalRequest) (*MCPToolApproval, error)
type MCPToolApprovalWaiter func(ctx context.Context, approvalID string) (MCPApprovalDecision, error)
type MCPToolExecutorFn func(ctx context.Context, req MCPToolExecutionRequest) (string, bool, error)

func ClassifyToolRisk(toolName string) string {
	switch toolName {
	case "read_files", "list_files", "grep", "read_attachment", "read_mobile_config", "read_build_logs", "read_deploy_logs", "web_search", "list_skills", "search_skills", "load_skill", "sandbox_status", "sandbox_read_file", "sandbox_list_files", "sandbox_read_logs":
		return ToolRiskLow
	case "write_file", "write_files", "edit_file", "multi_edit", "patch_files", "generate_image", "update_todos", "sandbox_write_file", "sandbox_serve_artifact", "preview_set_url", "preview_open_route", "preview_open_artifact":
		return ToolRiskMedium
	case "execute_sql", "provision_database", "provision_local_database", "setup_wemob_ai_gateway", "setup_store", "configure_mobile_bridge", "trigger_build_after_fix", "trigger_web_deploy", "sandbox_exec":
		return ToolRiskHigh
	case "delete_file":
		return ToolRiskDestructive
	default:
		return ToolRiskMedium
	}
}
