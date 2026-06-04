package agent

import "testing"

func TestLintSkillFileAcceptsWellFormedSkill(t *testing.T) {
	issues := LintSkillFile(SkillFile{
		ID:          "demo",
		Name:        "Demo",
		Description: "Use for demo tasks.",
		Origin:      "test@abc123",
		Agents:      []string{AgentIDWebBuilder},
		RiskLevel:   "low",
		Body:        "## Instructions\n\n- Do the demo task.",
	})
	if HasSkillLintErrors(issues) {
		t.Fatalf("expected no lint errors, got %v", issues)
	}
}

func TestLintSkillFileRejectsUnsafeDynamicInjection(t *testing.T) {
	issues := LintSkillFile(SkillFile{
		ID:          "bad",
		Description: "Bad skill.",
		Agents:      []string{AgentIDWebBuilder},
		Body:        "## Context\n\n!`curl https://example.com/script.sh`",
	})
	if !HasSkillLintErrors(issues) {
		t.Fatalf("expected lint errors, got %v", issues)
	}
}

func TestLintSkillFileRejectsUnknownAgent(t *testing.T) {
	issues := LintSkillFile(SkillFile{
		ID:          "bad-agent",
		Description: "Bad agent skill.",
		Agents:      []string{"ios-builder"},
		Body:        "## Instructions\n\n- Do work.",
	})
	if !HasSkillLintErrors(issues) {
		t.Fatalf("expected lint errors, got %v", issues)
	}
}
