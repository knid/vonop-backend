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

func TestWriteFilesTool_RejectsAccidentalRewriteOfExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", strings.Repeat("a", 6*1024))

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/page.tsx","content":"new content"}]}`))
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

func TestWriteFilesTool_AllowsIntentionalRewriteOfExistingFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/stub.tsx", "stub")

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/stub.tsx","content":"export default function () { return null }","overwrite":true,"reason":"replace generated stub with full file"}]}`))
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

func TestWriteFilesTool_RejectsLargeExistingFileRewriteEvenWithOverwrite(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	original := strings.Repeat("a", 31*1024)
	rc.FileMap.Set("app/panel/linkler/page.tsx", original)

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/panel/linkler/page.tsx","content":"new content","overwrite":true,"reason":"add campaign types"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected isError=true for large existing-file rewrite; out=%s", out)
	}
	if !strings.Contains(out, "existing file is too large for full rewrite") {
		t.Fatalf("expected large-file rewrite guidance; got: %s", out)
	}
	if !strings.Contains(out, "multi_edit") || !strings.Contains(out, "patch_files") {
		t.Fatalf("expected edit-tool guidance; got: %s", out)
	}
	if got, _ := rc.FileMap.Get("app/panel/linkler/page.tsx"); got != original {
		t.Fatal("large existing file was mutated despite rewrite rejection")
	}
}

func TestWriteFilesTool_AllowsCreationOfNewFileAtAnySize(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	body, _ := json.Marshal(map[string]any{
		"files": []map[string]any{
			{
				"path":    "app/big-new.tsx",
				"content": strings.Repeat("a", 24*1024),
			},
		},
	})
	_, isError, err := WriteFilesTool{}.Execute(context.Background(), rc, body)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Error("expected isError=false: new file creation should succeed regardless of size")
	}
}

func TestWriteFilesTool_RejectsQuotedPathWithoutMutatingState(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.BuildState = NewBuildState("create landing page", false)

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/landing/page.tsx\"","content":"export default function Page() { return null }"}]}`))
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

func TestWriteFilesTool_IsAtomicWhenAnyEntryFails(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[
			{"path":"DESIGN.md","content":"# Design Brief\n\n## Identity\nMissing required sections\n"},
			{"path":"app/page.tsx","content":"export default function Page(){ return null }"}
		]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected batch validation failure, got: %s", out)
	}
	if !strings.Contains(out, "failed before writing any files") {
		t.Fatalf("expected atomic failure message, got: %s", out)
	}
	if _, ok := rc.FileMap.Get("app/page.tsx"); ok {
		t.Fatal("valid sibling file was written despite batch failure")
	}
}

func TestWriteFilesTool_RejectsDuplicatePathsBeforeWriting(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[
			{"path":"app/page.tsx","content":"first"},
			{"path":"./app/page.tsx","content":"second"}
		]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected duplicate path failure, got: %s", out)
	}
	if !strings.Contains(out, "duplicate path") {
		t.Fatalf("expected duplicate path message, got: %s", out)
	}
	if _, ok := rc.FileMap.Get("app/page.tsx"); ok {
		t.Fatal("duplicate batch wrote a file")
	}
}

func TestWriteFilesTool_ReturnsBatchSummary(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/existing.tsx", "old")

	out, isError, err := WriteFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[
			{"path":"app/new.tsx","content":"new"},
			{"path":"app/existing.tsx","content":"updated","overwrite":true,"reason":"replace generated stub"}
		]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected success, got: %s", out)
	}
	if !strings.Contains(out, "wrote 2 files: created 1, updated 1") {
		t.Fatalf("expected batch summary, got: %s", out)
	}
}

func TestPatchFilesTool_AcceptsSingleFileShorthand(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")

	out, isError, err := PatchFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","edits":[{"old_string":"old","new_string":"new"}],"description":"fix copy"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected shorthand patch_files success, got: %s", out)
	}
	got, _ := rc.FileMap.Get("app/page.tsx")
	if got != "hello new" {
		t.Fatalf("file content = %q, want %q", got, "hello new")
	}
}

func TestPatchFilesTool_SchemaAdvertisesSingleFileShorthandAndStringEdits(t *testing.T) {
	schema := PatchFilesTool{}.Schema()
	if _, ok := schema["anyOf"]; !ok {
		t.Fatalf("patch_files schema should allow files[] or shorthand path+edits: %#v", schema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("patch_files schema properties missing: %#v", schema)
	}
	if _, ok := props["path"]; !ok {
		t.Fatalf("patch_files schema should advertise shorthand path: %#v", props)
	}
	edits, ok := props["edits"].(map[string]any)
	if !ok {
		t.Fatalf("patch_files schema should advertise shorthand edits: %#v", props)
	}
	if _, ok := edits["anyOf"]; !ok {
		t.Fatalf("patch_files edits schema should allow array or JSON string: %#v", edits)
	}
}

func TestPatchFilesTool_AcceptsStringifiedEdits(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")

	out, isError, err := PatchFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/page.tsx","edits":"[{\"old_string\":\"old\",\"new_string\":\"new\"}]"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected stringified patch_files edits success, got: %s", out)
	}
	got, _ := rc.FileMap.Get("app/page.tsx")
	if got != "hello new" {
		t.Fatalf("file content = %q, want %q", got, "hello new")
	}
}

func TestMultiEditTool_SchemaAdvertisesStringEdits(t *testing.T) {
	schema := MultiEditTool{}.Schema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("multi_edit schema properties missing: %#v", schema)
	}
	edits, ok := props["edits"].(map[string]any)
	if !ok {
		t.Fatalf("multi_edit schema should advertise edits: %#v", props)
	}
	if _, ok := edits["anyOf"]; !ok {
		t.Fatalf("multi_edit edits schema should allow array or JSON string: %#v", edits)
	}
}

func TestMultiEditTool_AcceptsStringifiedEdits(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")

	out, isError, err := MultiEditTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","edits":"[{\"old_string\":\"old\",\"new_string\":\"new\"}]"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected stringified multi_edit edits success, got: %s", out)
	}
	got, _ := rc.FileMap.Get("app/page.tsx")
	if got != "hello new" {
		t.Fatalf("file content = %q, want %q", got, "hello new")
	}
}
