package agent

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestApplyRawFileBlocksCreatesLargeNewFile(t *testing.T) {
	emitter := &captureEmitter{}
	rc := &RunContext{
		RunID:      uuid.New(),
		Project:    &models.AppProject{ProjectType: models.ProjectTypeApp},
		FileMap:    NewSafeFileMap(nil),
		BuildState: NewBuildState("create app", true),
		Emitter:    emitter,
	}
	content := strings.Repeat("export const x = 1\n", 4*1024)
	text := "Created the file.\nBEGIN_WEMOB_FILE path=app/page.tsx\n" + content + "END_WEMOB_FILE\nDone."

	written, err := applyRawFileBlocks(rc, text)
	if err != nil {
		t.Fatalf("applyRawFileBlocks: %v", err)
	}
	if written != 1 {
		t.Fatalf("written = %d, want 1", written)
	}
	got, ok := rc.FileMap.Get("app/page.tsx")
	if !ok {
		t.Fatal("raw block file was not created")
	}
	if got != content {
		t.Fatal("raw block content mismatch")
	}
	if !rc.BuildState.Created["app/page.tsx"] {
		t.Fatal("raw block create was not recorded in build state")
	}
	if len(emitter.events) != 1 || emitter.events[0].Type != EventFileWritten {
		t.Fatalf("expected one file_written event, got %#v", emitter.events)
	}
}

func TestApplyRawFileBlocksRejectsExistingFile(t *testing.T) {
	rc := &RunContext{
		RunID:   uuid.New(),
		Project: &models.AppProject{ProjectType: models.ProjectTypeApp},
		FileMap: NewSafeFileMap(map[string]string{"app/page.tsx": "old"}),
		Emitter: noopEmitter{},
	}
	text := "BEGIN_WEMOB_FILE path=app/page.tsx\nnew\nEND_WEMOB_FILE"

	_, err := applyRawFileBlocks(rc, text)
	if err == nil {
		t.Fatal("expected existing-file raw block to fail")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing-file error, got %v", err)
	}
	if got, _ := rc.FileMap.Get("app/page.tsx"); got != "old" {
		t.Fatal("existing file was mutated")
	}
}

func TestApplyRawFileBlocksRejectsSuspiciousPath(t *testing.T) {
	rc := &RunContext{
		RunID:   uuid.New(),
		Project: &models.AppProject{ProjectType: models.ProjectTypeApp},
		FileMap: NewSafeFileMap(nil),
		Emitter: noopEmitter{},
	}

	_, err := applyRawFileBlocks(rc, "BEGIN_WEMOB_FILE path=../page.tsx\nx\nEND_WEMOB_FILE")
	if err == nil {
		t.Fatal("expected suspicious path to fail")
	}
	if rc.FileMap.Len() != 0 {
		t.Fatal("file map mutated after invalid raw block")
	}
}
