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

func TestReadFilesToolDescriptionDiscouragesDefaultInspection(t *testing.T) {
	desc := ReadFilesTool{}.Description()
	for _, required := range []string{
		"Do not use as the default inspection tool",
		"Prefer grep, list_files, or read_file_range first",
		"avoid reading files you just wrote",
	} {
		if !strings.Contains(desc, required) {
			t.Fatalf("read_files description missing %q: %s", required, desc)
		}
	}
	if strings.Contains(desc, "Always use this instead of individual file reads") {
		t.Fatalf("read_files description still over-promotes broad reads: %s", desc)
	}
}

func TestWriteFilesToolDescriptionEncouragesBatching(t *testing.T) {
	desc := WriteFilesTool{}.Description()
	for _, required := range []string{
		"Batch many small new files in one call",
		"Do not write one file per round",
	} {
		if !strings.Contains(desc, required) {
			t.Fatalf("write_files description missing %q: %s", required, desc)
		}
	}
}

func TestReadFilesTool_MarksFilesAsReadForLaterEdits(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")

	_, isError, err := ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if isError {
		t.Fatal("expected read_files to succeed")
	}

	out, isError, err := EditFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","old_string":"old","new_string":"new"}`))
	if err != nil {
		t.Fatalf("edit execute: %v", err)
	}
	if isError {
		t.Fatalf("expected edit_file after read_files to succeed, got: %s", out)
	}
}

func TestReadFileRangeTool_ReturnsBoundedNumberedLinesAndMarksFileRead(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "line 1\nline 2\nline 3\nline 4")

	out, isError, err := ReadFileRangeTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","start_line":2,"end_line":3}`))
	if err != nil {
		t.Fatalf("range execute: %v", err)
	}
	if isError {
		t.Fatalf("expected read_file_range success, got: %s", out)
	}
	if !strings.Contains(out, "===== app/page.tsx lines 2-3 =====") ||
		!strings.Contains(out, "   2: line 2") ||
		!strings.Contains(out, "   3: line 3") {
		t.Fatalf("expected numbered bounded output, got: %s", out)
	}
	if strings.Contains(out, "line 1") || strings.Contains(out, "line 4") {
		t.Fatalf("range output included lines outside request: %s", out)
	}

	editOut, editErrFlag, err := EditFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","old_string":"line 3","new_string":"changed"}`))
	if err != nil {
		t.Fatalf("edit execute: %v", err)
	}
	if editErrFlag {
		t.Fatalf("expected range read to satisfy read-before-edit, got: %s", editOut)
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

func TestGrepTool_DefaultsToTwentyFiveContextLines(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	lines := make([]string, 61)
	for i := range lines {
		lines[i] = "line"
	}
	lines[5] = "before-default-window"
	lines[30] = "target-marker"
	lines[56] = "outside-default-window"
	rc.FileMap.Set("frontend/src/App.tsx", strings.Join(lines, "\n"))

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target-marker","glob":"frontend/src/App.tsx"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if !strings.Contains(out, "lines 6-56") {
		t.Fatalf("expected default 25-line context window, got: %s", out)
	}
	if !strings.Contains(out, "before-default-window") {
		t.Fatalf("expected line inside default context window, got: %s", out)
	}
	if strings.Contains(out, "outside-default-window") {
		t.Fatalf("did not expect line outside default context window, got: %s", out)
	}
}

func TestGrepTool_AllowsExplicitZeroContextLines(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("frontend/src/App.tsx", "before\ntarget-marker\nafter")

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target-marker","glob":"frontend/src/App.tsx","context_lines":0}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if !strings.Contains(out, "frontend/src/App.tsx:2:target-marker") {
		t.Fatalf("expected grep -n style output, got: %s", out)
	}
	if strings.Contains(out, "before") || strings.Contains(out, "after") || strings.Contains(out, "--- frontend/src/App.tsx") {
		t.Fatalf("expected no surrounding context when context_lines=0, got: %s", out)
	}
}

func TestGrepTool_CoalescesOverlappingContextWindows(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("frontend/src/App.tsx", strings.Join([]string{
		"one",
		"target first",
		"between",
		"target second",
		"five",
	}, "\n"))

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target","glob":"frontend/src/App.tsx","context_lines":1}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if got := strings.Count(out, "   3: between"); got != 1 {
		t.Fatalf("expected overlapping context line to appear once, got %d in: %s", got, out)
	}
	if got := strings.Count(out, "--- frontend/src/App.tsx"); got != 1 {
		t.Fatalf("expected one merged context block, got %d in: %s", got, out)
	}
	if !strings.Contains(out, "— 2 match(es) across 1 file(s)") {
		t.Fatalf("expected both matches counted, got: %s", out)
	}
}

func TestGrepTool_RespectsCallerMaxMatchesAndBytes(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("frontend/src/App.tsx", strings.Join([]string{
		"target one",
		"target two",
		"target three",
	}, "\n"))

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target","glob":"frontend/src/App.tsx","context_lines":0,"max_matches":2,"max_bytes":1024}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if strings.Contains(out, "target three") {
		t.Fatalf("expected max_matches to stop before third match, got: %s", out)
	}
	if !strings.Contains(out, "truncated") {
		t.Fatalf("expected truncation hint, got: %s", out)
	}
}

func TestGrepTool_FilesModeScansLaterFilesAfterNoisyFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("frontend/src/a.tsx", strings.Join([]string{
		"target one",
		"target two",
		"target three",
	}, "\n"))
	rc.FileMap.Set("frontend/src/b.tsx", "target only")

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target","glob":"frontend/src/*.tsx","mode":"files","max_matches":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if !strings.Contains(out, "frontend/src/a.tsx") || !strings.Contains(out, "frontend/src/b.tsx") {
		t.Fatalf("expected files mode to list both matching files despite noisy first file, got: %s", out)
	}
	if strings.Contains(out, "truncated") {
		t.Fatalf("did not expect files mode to truncate by per-line max_matches, got: %s", out)
	}
}

func TestGrepTool_CountModeScansLaterFilesAfterNoisyFile(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("frontend/src/a.tsx", strings.Join([]string{
		"target one",
		"target two",
		"target three",
	}, "\n"))
	rc.FileMap.Set("frontend/src/b.tsx", "target only")

	out, isError, err := GrepTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"pattern":"target","glob":"frontend/src/*.tsx","mode":"count","max_matches":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected isError=false, got: %s", out)
	}
	if !strings.Contains(out, "frontend/src/a.tsx:3") || !strings.Contains(out, "frontend/src/b.tsx:1") {
		t.Fatalf("expected count mode to count both files despite noisy first file, got: %s", out)
	}
	if strings.Contains(out, "truncated") {
		t.Fatalf("did not expect count mode to truncate by per-line max_matches, got: %s", out)
	}
}

func TestGrepTool_SchemaAdvertisesContextDefaultAndRecommendation(t *testing.T) {
	schema := GrepTool{}.Schema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("grep schema missing properties: %#v", schema)
	}
	contextLines, ok := props["context_lines"].(map[string]any)
	if !ok {
		t.Fatalf("grep schema missing context_lines: %#v", props)
	}
	if got := contextLines["default"]; got != grepDefaultContextLines {
		t.Fatalf("context_lines default = %#v, want %d", got, grepDefaultContextLines)
	}
	desc, _ := contextLines["description"].(string)
	if !strings.Contains(desc, "Default 25") || !strings.Contains(desc, "Set to 0") {
		t.Fatalf("context_lines description should guide larger code-edit context, got: %q", desc)
	}
	for _, name := range []string{"literal", "mode", "max_matches", "max_bytes"} {
		if _, ok := props[name].(map[string]any); !ok {
			t.Fatalf("grep schema missing %s: %#v", name, props)
		}
	}
}

func TestGrepTool_SchemaRequiresNonEmptyPatternAndBoundsContext(t *testing.T) {
	props := GrepTool{}.Schema()["properties"].(map[string]any)
	pattern := props["pattern"].(map[string]any)
	if got := pattern["minLength"]; got != 1 {
		t.Fatalf("grep pattern minLength = %#v, want 1", got)
	}
	contextLines := props["context_lines"].(map[string]any)
	if got := contextLines["minimum"]; got != 0 {
		t.Fatalf("context_lines minimum = %#v, want 0", got)
	}
	if got := contextLines["maximum"]; got != 100 {
		t.Fatalf("context_lines maximum = %#v, want 100", got)
	}
	maxMatches := props["max_matches"].(map[string]any)
	if got := maxMatches["maximum"]; got != grepHardMaxMatches {
		t.Fatalf("max_matches maximum = %#v, want %d", got, grepHardMaxMatches)
	}
	maxBytes := props["max_bytes"].(map[string]any)
	if got := maxBytes["maximum"]; got != grepHardMaxBytes {
		t.Fatalf("max_bytes maximum = %#v, want %d", got, grepHardMaxBytes)
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
	if !strings.Contains(out, "multi_edit") {
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

func TestMultiEditTool_SchemaAdvertisesSingleFileAndMultiFileShapes(t *testing.T) {
	schema := MultiEditTool{}.Schema()
	if _, ok := schema["anyOf"]; !ok {
		t.Fatalf("multi_edit schema should allow single-file or files[] shapes: %#v", schema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("multi_edit schema properties missing: %#v", schema)
	}
	if _, ok := props["path"]; !ok {
		t.Fatalf("multi_edit schema should advertise path: %#v", props)
	}
	if _, ok := props["files"]; !ok {
		t.Fatalf("multi_edit schema should advertise files: %#v", props)
	}
	edits, ok := props["edits"].(map[string]any)
	if !ok {
		t.Fatalf("multi_edit schema should advertise edits: %#v", props)
	}
	if _, ok := edits["anyOf"]; !ok {
		t.Fatalf("multi_edit edits schema should allow array or JSON string: %#v", edits)
	}
}

func TestMultiEditTool_AcceptsMultiFileBatch(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")
	rc.FileMap.Set("app/card.tsx", "card old")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx","app/card.tsx"]}`))

	out, isError, err := MultiEditTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"files":[{"path":"app/page.tsx","edits":"[{\"old_string\":\"old\",\"new_string\":\"new\"}]"},{"path":"app/card.tsx","edits":[{"old_string":"old","new_string":"fresh"}]}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected multi-file multi_edit success, got: %s", out)
	}
	page, _ := rc.FileMap.Get("app/page.tsx")
	card, _ := rc.FileMap.Get("app/card.tsx")
	if page != "hello new" || card != "card fresh" {
		t.Fatalf("contents = %q / %q", page, card)
	}
	if !strings.Contains(out, "edited 2 files") {
		t.Fatalf("expected batch summary, got: %s", out)
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

func TestMultiEditTool_ReplaceAllReplacesEveryMatch(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "text-gray-400 text-gray-400 text-gray-700")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))

	out, isError, err := MultiEditTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","edits":[{"old_string":"text-gray-400","new_string":"text-foreground-muted","replace_all":true}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected replace_all multi_edit success, got: %s", out)
	}
	got, _ := rc.FileMap.Get("app/page.tsx")
	if got != "text-foreground-muted text-foreground-muted text-gray-700" {
		t.Fatalf("file content = %q", got)
	}
	if !strings.Contains(out, "2 replacements") {
		t.Fatalf("expected replacement count in output, got: %s", out)
	}
}

func TestMultiEditTool_SchemaAdvertisesReplaceAll(t *testing.T) {
	schema := editReplacementSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("edit replacement schema properties missing: %#v", schema)
	}
	if _, ok := props["replace_all"]; !ok {
		t.Fatalf("edit replacement schema should advertise replace_all: %#v", props)
	}
}

func TestMultiEditTool_AcceptsStringifiedEdits(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))

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

func TestEditFileTool_RequiresCurrentReadBeforeEditing(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")

	out, isError, err := EditFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","old_string":"old","new_string":"new"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected edit without prior read to fail, got: %s", out)
	}
	if !strings.Contains(out, "must read current file before editing") {
		t.Fatalf("expected read-before-edit guidance, got: %s", out)
	}
	got, _ := rc.FileMap.Get("app/page.tsx")
	if got != "hello old" {
		t.Fatalf("file mutated despite read-before-edit rejection: %q", got)
	}
}

func TestEditFileTool_RejectsStaleRead(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))
	rc.FileMap.Set("app/page.tsx", "hello changed")

	out, isError, err := EditFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","old_string":"changed","new_string":"new"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected stale read rejection, got: %s", out)
	}
	if !strings.Contains(out, "file changed since last read") {
		t.Fatalf("expected stale-read guidance, got: %s", out)
	}
}

func TestEditFileTool_ReturnsStructuredSuccessResult(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old\nsecond line")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))

	out, isError, err := EditFileTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","old_string":"old","new_string":"new","description":"rename marker"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if isError {
		t.Fatalf("expected edit success, got: %s", out)
	}
	var result struct {
		Changed      bool   `json:"changed"`
		Path         string `json:"path"`
		Replacements int    `json:"replacements"`
		Summary      string `json:"summary"`
		OldHash      string `json:"old_hash"`
		NewHash      string `json:"new_hash"`
		ChangedLines []int  `json:"changed_lines"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected structured JSON result, got %q: %v", out, err)
	}
	if !result.Changed || result.Path != "app/page.tsx" || result.Replacements != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.OldHash == "" || result.NewHash == "" || result.OldHash == result.NewHash {
		t.Fatalf("expected distinct old/new hashes: %+v", result)
	}
	if len(result.ChangedLines) == 0 || result.Summary != "rename marker" {
		t.Fatalf("expected changed line and summary: %+v", result)
	}
}

func TestMultiEditTool_ReturnsStructuredOldStringNotFoundFailure(t *testing.T) {
	rc := newPathScopeTestRC(models.ProjectTypeApp)
	rc.FileMap.Set("app/page.tsx", "hello old")
	_, _, _ = ReadFilesTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"paths":["app/page.tsx"]}`))

	out, isError, err := MultiEditTool{}.Execute(context.Background(), rc,
		json.RawMessage(`{"path":"app/page.tsx","edits":[{"old_string":"missing","new_string":"new"}]}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !isError {
		t.Fatalf("expected old_string failure, got: %s", out)
	}
	var result struct {
		Changed       bool   `json:"changed"`
		ErrorCode     string `json:"error_code"`
		NextStep      string `json:"next_step"`
		SuggestedGrep string `json:"suggested_grep"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("expected structured JSON error, got %q: %v", out, err)
	}
	if result.Changed || result.ErrorCode != "old_string_not_found" ||
		result.NextStep != "read_file_range" || result.SuggestedGrep != "missing" {
		t.Fatalf("unexpected structured error: %+v", result)
	}
}
