package agent

import (
	"fmt"
	"slices"
	"strings"
)

const (
	SkillLintError   = "error"
	SkillLintWarning = "warning"
)

type SkillLintIssue struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func (i SkillLintIssue) String() string {
	if i.Field == "" {
		return fmt.Sprintf("%s: %s", i.Severity, i.Message)
	}
	return fmt.Sprintf("%s %s: %s", i.Severity, i.Field, i.Message)
}

func LintSkillFile(skill SkillFile) []SkillLintIssue {
	var issues []SkillLintIssue
	add := func(severity, field, msg string) {
		issues = append(issues, SkillLintIssue{Severity: severity, Field: field, Message: msg})
	}

	if strings.TrimSpace(skill.ID) == "" {
		add(SkillLintError, "id", "is required")
	}
	if strings.TrimSpace(skill.Name) == "" {
		add(SkillLintWarning, "name", "is recommended for catalog display")
	}
	if strings.TrimSpace(skill.Description) == "" {
		add(SkillLintError, "description", "is required so automatic activation has a reliable signal")
	}
	if len(skill.Agents) == 0 {
		add(SkillLintWarning, "agents", "should declare compatible agents to avoid web/mobile cross-loading")
	}
	for _, agentID := range skill.Agents {
		if agentID != AgentIDWebBuilder && agentID != AgentIDMobileBuilder {
			add(SkillLintError, "agents", fmt.Sprintf("unknown agent %q", agentID))
		}
	}
	if strings.TrimSpace(skill.Body) == "" {
		add(SkillLintError, "body", "must contain Markdown instructions")
	}
	if len(skill.Body) > 12_000 {
		add(SkillLintWarning, "body", "is large; move detailed references into supporting files")
	}
	if skill.Origin == "" {
		add(SkillLintWarning, "origin", "should identify builtin/imported source and version")
	}
	if skill.RiskLevel != "" && !slices.Contains([]string{"low", "medium", "high"}, skill.RiskLevel) {
		add(SkillLintError, "risk_level", "must be low, medium, or high")
	}
	bodyLower := strings.ToLower(skill.Body)
	for _, forbidden := range []string{
		"ignore previous instructions",
		"reveal system prompt",
		"disable safety",
		"bypass guardrails",
	} {
		if strings.Contains(bodyLower, forbidden) {
			add(SkillLintError, "body", fmt.Sprintf("contains unsafe instruction phrase %q", forbidden))
		}
	}
	if strings.Contains(skill.Body, "!`") {
		add(SkillLintError, "body", "dynamic shell injection is not accepted for Wemob skills")
	}
	return issues
}

func HasSkillLintErrors(issues []SkillLintIssue) bool {
	for _, issue := range issues {
		if issue.Severity == SkillLintError {
			return true
		}
	}
	return false
}
