package agent

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/knid/wemob/internal/models"
)

const (
	ToolRiskLow         = "low"
	ToolRiskMedium      = "medium"
	ToolRiskHigh        = "high"
	ToolRiskDestructive = "destructive"
)

type ToolGuardrailDecision struct {
	Allowed bool
	Message string
}

type ToolGuardrail func(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision

type AgentValidationPolicy func(fileMap *SafeFileMap, state *BuildState, prompt string, isFirstGen bool, project *models.AppProject, skills []SkillFile) error

func AllowTool() ToolGuardrailDecision {
	return ToolGuardrailDecision{Allowed: true}
}

func BlockTool(message string) ToolGuardrailDecision {
	return ToolGuardrailDecision{Allowed: false, Message: message}
}

func ApplyToolGuardrails(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision {
	if rc == nil || rc.Profile == nil {
		return AllowTool()
	}
	for _, guardrail := range rc.Profile.ToolGuardrails {
		decision := guardrail(rc, toolName, input)
		if !decision.Allowed {
			return decision
		}
	}
	return AllowTool()
}

func ReadOnlyToolGuardrail(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision {
	if rc == nil || !rc.ReadOnly {
		return AllowTool()
	}
	if toolName == "execute_sql" {
		if readOnlySQLRequested(input) {
			return AllowTool()
		}
		return BlockTool("tool blocked by read-only command policy")
	}
	if slices.Contains(mutatingToolNames, toolName) {
		return BlockTool("tool blocked by read-only command policy")
	}
	return AllowTool()
}

func readOnlySQLRequested(input json.RawMessage) bool {
	var args struct {
		ReadOnly *bool `json:"readonly"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return false
	}
	return args.ReadOnly != nil && *args.ReadOnly
}

func HumanApprovalToolGuardrail(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision {
	if rc == nil || rc.Project == nil || rc.Project.Settings == nil {
		return AllowTool()
	}
	requireApproval, _ := rc.Project.Settings["agent_require_approval"].(bool)
	if !requireApproval {
		return AllowTool()
	}
	risk := ClassifyToolRisk(toolName)
	if risk == ToolRiskHigh || risk == ToolRiskDestructive {
		return BlockTool(fmt.Sprintf("tool %q requires human approval before execution", toolName))
	}
	return AllowTool()
}

func SkillAllowedToolsGuardrail(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision {
	if slices.Contains(skillRuntimeToolNames, toolName) {
		return AllowTool()
	}
	if rc == nil || rc.Project == nil || rc.Project.Settings == nil {
		return AllowTool()
	}
	enforce, _ := rc.Project.Settings["agent_enforce_skill_tool_allowlist"].(bool)
	if !enforce {
		return AllowTool()
	}
	allowed := map[string]bool{}
	for _, skill := range rc.Skills {
		for _, t := range skill.AllowedTools {
			allowed[t] = true
		}
	}
	if len(allowed) == 0 || allowed[toolName] {
		return AllowTool()
	}
	return BlockTool(fmt.Sprintf("tool %q is not allowed by the active skill tool allowlist", toolName))
}

func ValidateAgentOutput(profile *AgentProfile, fileMap *SafeFileMap, state *BuildState, prompt string, isFirstGen bool, project *models.AppProject, skills []SkillFile) error {
	if profile != nil && profile.ValidationPolicy != nil {
		return profile.ValidationPolicy(fileMap, state, prompt, isFirstGen, project, skills)
	}
	return validateGeneratedApp(fileMap, state, prompt, isFirstGen, project)
}

func DefaultValidationPolicy(fileMap *SafeFileMap, state *BuildState, prompt string, isFirstGen bool, project *models.AppProject, skills []SkillFile) error {
	return validateGeneratedApp(fileMap, state, prompt, isFirstGen, project)
}

var mutatingToolNames = []string{
	"write_file",
	"write_files",
	"edit_file",
	"multi_edit",
	"delete_file",
	"provision_database",
	"provision_local_database",
	"setup_wemob_ai_gateway",
	"setup_store",
	"configure_mobile_bridge",
	"trigger_build_after_fix",
	"trigger_web_deploy",
	"sandbox_exec",
	"process_restart",
	"sandbox_serve_artifact",
	"preview_set_url",
	"preview_open_route",
	"preview_open_artifact",
}

var skillRuntimeToolNames = []string{"list_skills", "search_skills", "load_skill"}

func ClassifyToolRisk(toolName string) string {
	switch toolName {
	case "read_files", "list_files", "grep", "read_attachment", "read_mobile_config", "read_build_logs", "read_deploy_logs", "web_search", "list_skills", "search_skills", "load_skill", "preview_status", "preview_logs", "process_logs":
		return ToolRiskLow
	case "write_file", "write_files", "edit_file", "multi_edit", "generate_image", "process_restart", "sandbox_serve_artifact", "preview_set_url", "preview_open_route", "preview_open_artifact":
		return ToolRiskMedium
	case "execute_sql", "provision_database", "provision_local_database", "setup_wemob_ai_gateway", "setup_store", "configure_mobile_bridge", "trigger_build_after_fix", "trigger_web_deploy", "sandbox_exec":
		return ToolRiskHigh
	case "delete_file":
		return ToolRiskDestructive
	default:
		return ToolRiskMedium
	}
}
