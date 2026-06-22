package agent

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

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

func PackageScriptToolGuardrail(rc *RunContext, toolName string, input json.RawMessage) ToolGuardrailDecision {
	switch toolName {
	case "write_files", "edit_file", "multi_edit":
	default:
		return AllowTool()
	}
	if removesPackageBuildTypecheck(rc, input) {
		return BlockTool("Do not change package.json scripts to work around sandbox/build visibility errors. Verify Git-backed files are synced to the sandbox first, then rerun or restart the preview. package.json scripts must keep existing typecheck/build verification unless the user explicitly asks to change them.")
	}
	return AllowTool()
}

func removesPackageBuildTypecheck(rc *RunContext, input json.RawMessage) bool {
	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return false
	}
	return packageScriptMutationDropsTypecheck(rc, payload, "")
}

func packageScriptMutationDropsTypecheck(rc *RunContext, value any, inheritedPath string) bool {
	switch v := value.(type) {
	case map[string]any:
		path := inheritedPath
		if p, ok := v["path"].(string); ok {
			path = p
		}
		if packageJSONPath(path) {
			if oldString, _ := v["old_string"].(string); oldString != "" {
				newString, _ := v["new_string"].(string)
				if dropsTypecheck(oldString, newString) {
					return true
				}
			}
			if content, _ := v["content"].(string); content != "" {
				if existingPackageBuildHadTypecheck(rc, path) && strings.Contains(content, `"scripts"`) && strings.Contains(content, `"build"`) && !strings.Contains(content, "tsc --noEmit") {
					return true
				}
			}
		}
		for key, child := range v {
			if key == "path" {
				continue
			}
			if packageScriptMutationDropsTypecheck(rc, child, path) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if packageScriptMutationDropsTypecheck(rc, child, inheritedPath) {
				return true
			}
		}
	}
	return false
}

func packageJSONPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return lower == "package.json" || strings.HasSuffix(lower, "/package.json")
}

func existingPackageBuildHadTypecheck(rc *RunContext, path string) bool {
	if rc == nil || rc.FileMap == nil {
		return false
	}
	existing, ok := rc.FileMap.Get(path)
	return ok && strings.Contains(existing, `"build"`) && strings.Contains(existing, "tsc --noEmit")
}

func dropsTypecheck(oldString, newString string) bool {
	return strings.Contains(oldString, "tsc --noEmit") &&
		!strings.Contains(newString, "tsc --noEmit") &&
		(strings.Contains(oldString, `"build"`) || strings.Contains(newString, `"build"`))
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
