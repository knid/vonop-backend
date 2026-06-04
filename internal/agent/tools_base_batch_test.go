package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestReadFilesTool_ReturnsContentsForAllPaths(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("a.tsx", "AAA")
	rc.FileMap.Set("b.tsx", "BBB")

	out, isError, err := ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["a.tsx","b.tsx"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if !strings.Contains(out, "===== a.tsx =====") || !strings.Contains(out, "AAA") {
		t.Errorf("missing a.tsx in output: %s", out)
	}
	if !strings.Contains(out, "===== b.tsx =====") || !strings.Contains(out, "BBB") {
		t.Errorf("missing b.tsx in output: %s", out)
	}
}

func TestReadFilesTool_MissingPathReportsToolErrorAndInlineDetail(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("a.tsx", "AAA")

	out, isError, err := ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["a.tsx","missing.tsx"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected isError=true for missing path, got: %s", out)
	}
	if !strings.Contains(out, "AAA") {
		t.Errorf("expected a.tsx content in output: %s", out)
	}
	if !strings.Contains(out, "ERROR: file not found") {
		t.Errorf("expected inline not-found error for missing.tsx: %s", out)
	}
}

func TestReadFilesTool_RejectsEmptyAndOversizedBatches(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	_, isError, _ := ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":[]}`))
	if !isError {
		t.Error("expected isError=true for empty paths array")
	}

	paths := make([]string, readFilesMaxPaths+1)
	for i := range paths {
		paths[i] = "x.tsx"
	}
	body, _ := json.Marshal(map[string]any{"paths": paths})
	_, isError, _ = ReadFilesTool{}.Execute(context.Background(), rc, body)
	if !isError {
		t.Error("expected isError=true when exceeding readFilesMaxPaths")
	}
}

func TestWriteFileTool_RejectsAccidentalRewriteOfExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", strings.Repeat("a", 6*1024))

	out, isError, err := WriteFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","content":"new content"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected isError=true for accidental rewrite; out=%s", out)
	}
	if !strings.Contains(out, "file already exists") {
		t.Errorf("expected existing-file warning; got: %s", out)
	}
}

func TestWriteFileTool_AllowsIntentionalRewriteOfExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/stub.tsx", "stub")

	out, isError, err := WriteFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/stub.tsx","content":"export default function () { return null }","overwrite":true,"reason":"replace generated stub with full file"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false when overwrite is explicit; out=%s", out)
	}
	if !strings.Contains(out, "update") {
		t.Errorf("expected update confirmation; got: %s", out)
	}
}

func TestWriteFileTool_AllowsCreationOfNewFileAtAnySize(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	body, _ := json.Marshal(map[string]any{
		"path":    "app/big-new.tsx",
		"content": strings.Repeat("a", 24*1024),
	})
	_, isError, err := WriteFileTool{}.Execute(context.Background(), rc, body)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false: new file creation should succeed regardless of size")
	}
}

func TestWriteFileTool_RejectsQuotedPathWithoutMutatingState(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.BuildState = NewBuildState("create landing page", false)

	out, isError, err := WriteFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/landing/page.tsx\"","content":"export default function Page() { return null }"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected isError=true for quoted path; out=%s", out)
	}
	if !strings.Contains(out, "invalid or suspicious path") {
		t.Fatalf("expected suspicious path error; got: %s", out)
	}
	if _, ok := rc.FileMap.Get(`app/landing/page.tsx"`); ok {
		t.Fatal("quoted path was added to file map")
	}
	if rc.BuildState.Created[`app/landing/page.tsx"`] {
		t.Fatal("quoted path was recorded in build state")
	}
}
