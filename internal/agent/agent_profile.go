package agent

import (
	"slices"

	"github.com/knid/wemob/internal/models"
)

const (
	AgentIDWebBuilder    = "web-builder"
	AgentIDMobileBuilder = "mobile-builder"
)

// AgentProfile is the stable product-level contract for one Wemob builder
// agent. Prompts, tools, skills, and later validation policies hang off this
// profile instead of being selected ad hoc throughout Run().
type AgentProfile struct {
	ID               string
	Name             string
	ProjectTypes     []string
	BasePrompt       string
	FirstGenPrompt   string
	ChatPrompt       string
	DefaultSkillIDs  []string
	HandoffTargets   []string
	BuildRegistry    func(project *models.AppProject, deps *RunDependencies, model string) *Registry
	ToolGuardrails   []ToolGuardrail
	ValidationPolicy AgentValidationPolicy
}

func (p *AgentProfile) SupportsProjectType(projectType string) bool {
	return slices.Contains(p.ProjectTypes, projectType)
}

func (p *AgentProfile) ModePrompt(isFirstGen bool) string {
	if isFirstGen {
		return p.FirstGenPrompt
	}
	return p.ChatPrompt
}

var webBuilderProfile = &AgentProfile{
	ID:               AgentIDWebBuilder,
	Name:             "Wemob Web Builder",
	ProjectTypes:     []string{models.ProjectTypeApp, models.ProjectTypeEcommerce},
	BasePrompt:       CompactBasePromptWeb,
	FirstGenPrompt:   BasePromptWebFirstGen,
	ChatPrompt:       BasePromptWebChat,
	DefaultSkillIDs:  []string{"nextjs-app-router", "tailwind-v4", "web-builder-foundation"},
	HandoffTargets:   []string{"debug-agent", "seo-agent", "store-data-agent", "release-agent"},
	BuildRegistry:    buildWebBuilderRegistry,
	ToolGuardrails:   []ToolGuardrail{ReadOnlyToolGuardrail, HumanApprovalToolGuardrail, SkillAllowedToolsGuardrail},
	ValidationPolicy: DefaultValidationPolicy,
}

var mobileBuilderProfile = &AgentProfile{
	ID:               AgentIDMobileBuilder,
	Name:             "Wemob Mobile Builder",
	ProjectTypes:     []string{models.ProjectTypeMobileAppNative},
	BasePrompt:       CompactBasePromptMobileNative,
	FirstGenPrompt:   BasePromptMobileNativeFirstGen,
	ChatPrompt:       BasePromptMobileNativeChat,
	DefaultSkillIDs:  []string{"expo-router", "mobile-builder-foundation", "nativewind-ui"},
	HandoffTargets:   []string{"debug-agent", "release-agent"},
	BuildRegistry:    buildMobileBuilderRegistry,
	ToolGuardrails:   []ToolGuardrail{ReadOnlyToolGuardrail, HumanApprovalToolGuardrail, SkillAllowedToolsGuardrail},
	ValidationPolicy: DefaultValidationPolicy,
}

var agentProfiles = []*AgentProfile{
	webBuilderProfile,
	mobileBuilderProfile,
}

func ListAgentProfiles() []*AgentProfile {
	out := make([]*AgentProfile, len(agentProfiles))
	copy(out, agentProfiles)
	return out
}

// ResolveAgentProfile chooses the builder agent for the project. Unknown or
// nil project types intentionally fall back to web for backward compatibility
// with older rows whose project_type defaults to "app".
func ResolveAgentProfile(project *models.AppProject) *AgentProfile {
	projectType := models.ProjectTypeApp
	if project != nil && project.ProjectType != "" {
		projectType = project.ProjectType
	}
	for _, profile := range agentProfiles {
		if profile.SupportsProjectType(projectType) {
			return profile
		}
	}
	return webBuilderProfile
}
