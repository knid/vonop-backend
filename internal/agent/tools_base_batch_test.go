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

func TestReadFilesTool_MissingPathReportsErrorInline(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("a.tsx", "AAA")

	out, isError, err := ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["a.tsx","missing.tsx"]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false (per-path errors are inline), got: %s", out)
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

func TestWriteFileTool_AllowsRewriteOfLargeExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	// Pre-seed an existing file larger than the old 5 KB rewrite cap.
	// write_file should now allow wholesale overwrites of any size.
	rc.FileMap.Set("app/page.tsx", strings.Repeat("a", 6*1024))

	out, isError, err := WriteFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","content":"new content"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false for wholesale rewrite of large existing file; out=%s", out)
	}
	if !strings.Contains(out, "update") {
		t.Errorf("expected update confirmation; got: %s", out)
	}
}

func TestWriteFileTool_AllowsRewriteOfSmallExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/stub.tsx", "stub")

	_, isError, err := WriteFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/stub.tsx","content":"export default function () { return null }"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false when overwriting a small existing file")
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
