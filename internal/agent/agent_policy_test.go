package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

type policyTestTool struct{}

func (policyTestTool) Name() string        { return "write_file" }
func (policyTestTool) Description() string { return "test tool" }
func (policyTestTool) Schema() map[string]any {
	return map[string]any{"type": "object"}
}
func (policyTestTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	return "executed", false, nil
}

func TestReadOnlyToolGuardrailBlocksMutations(t *testing.T) {
	rc := &RunContext{ReadOnly: true}
	if got := ReadOnlyToolGuardrail(rc, "read_files", json.RawMessage(`{}`)); !got.Allowed {
		t.Fatalf("read_files should be allowed in read-only mode: %+v", got)
	}
	if got := ReadOnlyToolGuardrail(rc, "write_file", json.RawMessage(`{}`)); got.Allowed {
		t.Fatalf("write_file should be blocked in read-only mode")
	}
}

func TestReadOnlyToolGuardrailAllowsReadonlySQL(t *testing.T) {
	rc := &RunContext{ReadOnly: true}
	if got := ReadOnlyToolGuardrail(rc, "execute_sql", json.RawMessage(`{"query":"SELECT 1","readonly":true}`)); !got.Allowed {
		t.Fatalf("readonly execute_sql should be allowed in read-only mode: %+v", got)
	}
}

func TestReadOnlyToolGuardrailBlocksMutatingSQL(t *testing.T) {
	rc := &RunContext{ReadOnly: true}
	for name, input := range map[string]json.RawMessage{
		"explicit write": json.RawMessage(`{"query":"UPDATE users SET name = 'x'","readonly":false}`),
		"missing flag":   json.RawMessage(`{"query":"SELECT 1"}`),
		"invalid input":  json.RawMessage(`{`),
	} {
		t.Run(name, func(t *testing.T) {
			if got := ReadOnlyToolGuardrail(rc, "execute_sql", input); got.Allowed {
				t.Fatalf("execute_sql should be blocked in read-only mode for %s", name)
			}
		})
	}
}

func TestHumanApprovalToolGuardrailOptIn(t *testing.T) {
	rc := &RunContext{
		Project: &models.AppProject{Settings: map[string]any{"agent_require_approval": true}},
	}
	if got := HumanApprovalToolGuardrail(rc, "read_files", json.RawMessage(`{}`)); !got.Allowed {
		t.Fatalf("low risk tool should not require approval: %+v", got)
	}
	if got := HumanApprovalToolGuardrail(rc, "trigger_web_deploy", json.RawMessage(`{}`)); got.Allowed {
		t.Fatal("high risk tool should be blocked when approval is required")
	}
}

func TestClassifyToolRisk(t *testing.T) {
	if got := ClassifyToolRisk("read_files"); got != ToolRiskLow {
		t.Fatalf("read_files risk = %q", got)
	}
	if got := ClassifyToolRisk("delete_file"); got != ToolRiskDestructive {
		t.Fatalf("delete_file risk = %q", got)
	}
}

func TestSkillAllowedToolsGuardrailOptIn(t *testing.T) {
	rc := &RunContext{
		Project: &models.AppProject{Settings: map[string]any{"agent_enforce_skill_tool_allowlist": true}},
		Skills: []SkillFile{{
			ID:           "read-only-skill",
			AllowedTools: []string{"read_files", "grep"},
		}},
	}
	if got := SkillAllowedToolsGuardrail(rc, "grep", json.RawMessage(`{}`)); !got.Allowed {
		t.Fatalf("grep should be allowed: %+v", got)
	}
	if got := SkillAllowedToolsGuardrail(rc, "write_file", json.RawMessage(`{}`)); got.Allowed {
		t.Fatal("write_file should be blocked by skill tool allowlist")
	}
}

func TestPackageScriptGuardrailBlocksRemovingTypecheckFromBuildScript(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	got := PackageScriptToolGuardrail(rc, "edit_file", json.RawMessage(`{
		"path": "frontend/package.json",
		"old_string": "\"build\": \"tsc --noEmit && vite build\"",
		"new_string": "\"build\": \"vite build\""
	}`))
	if got.Allowed {
		t.Fatal("expected package script guardrail to block removing typecheck from build script")
	}
	if !strings.Contains(got.Message, "sandbox") || !strings.Contains(got.Message, "package.json scripts") {
		t.Fatalf("expected sandbox/package guidance, got: %s", got.Message)
	}
}

func TestRegistryExecuteAppliesProfileGuardrails(t *testing.T) {
	r := NewRegistry()
	r.Register(policyTestTool{})
	rc := &RunContext{
		Profile:  &AgentProfile{ToolGuardrails: []ToolGuardrail{ReadOnlyToolGuardrail}},
		ReadOnly: true,
	}
	result, isErr, err := r.Execute(context.Background(), rc, "write_file", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !isErr {
		t.Fatal("expected guardrail-blocked tool to return isErr=true")
	}
	if result == "executed" {
		t.Fatal("guardrail did not block tool execution")
	}
}
