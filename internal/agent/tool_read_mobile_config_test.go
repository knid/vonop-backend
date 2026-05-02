package agent

import (
	"context"
	"encoding/json"
	"testing"
)

// TestReadMobileConfigTool_NameAndSchema verifies the static contract of the
// tool: name, non-empty description, and a valid JSON-schema object shape.
// These run without any database.
func TestReadMobileConfigTool_NameAndSchema(t *testing.T) {
	tool := &ReadMobileConfigTool{}
	if tool.Name() != "read_mobile_config" {
		t.Errorf("Name() = %q, want read_mobile_config", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() must not be empty")
	}
	schema := tool.Schema()
	if schema["type"] != "object" {
		t.Errorf("Schema type = %v, want object", schema["type"])
	}
	if _, ok := schema["properties"]; !ok {
		t.Error("Schema missing 'properties' key")
	}
}

// TestReadMobileConfigTool_NilDeps verifies that calling Execute with nil deps
// returns an infrastructure error (not isError=true) — callers treat infra
// errors differently from tool-usage errors.
func TestReadMobileConfigTool_NilDeps(t *testing.T) {
	tool := &ReadMobileConfigTool{}
	rc := &RunContext{
		Deps: nil,
	}
	_, _, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error when Deps is nil, got nil")
	}
}

// TestReadMobileConfigTool_ReturnsEnabledMethods and
// TestReadMobileConfigTool_HandlesMissingConfig require a real PostgreSQL
// connection and are skipped unless integration test infrastructure is wired.
// TODO(P1.1): wire sqlite-in-memory or test-db fixture so these execute.
func TestReadMobileConfigTool_ReturnsEnabledMethods(t *testing.T) {
	t.Skip("requires real DB connection — re-enable in P1.1 when integration test infra is wired")
}

func TestReadMobileConfigTool_HandlesMissingConfig(t *testing.T) {
	t.Skip("requires real DB connection — re-enable in P1.1 when integration test infra is wired")
}
