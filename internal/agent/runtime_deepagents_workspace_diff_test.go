package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/services/gitrepo"
)

func TestEmitWorkspaceDiffEvents_EmitsCreateUpdateAndDeleteEvents(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := &gitrepo.Repo{Path: repoPath}
	runID := uuid.New()

	baseFileMap := NewSafeFileMap(map[string]string{
		"src/main.tsx": "before",
	})

	if err := writeContent(filepath.Join(repoPath, "src/main.tsx"), []byte("before")); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, "src/new.txt"), []byte("fresh")); err != nil {
		t.Fatalf("seed new file: %v", err)
	}

	emitter := &captureEmitter{}
	emitWorkspaceDiffEvents(repo, baseFileMap, runID, emitter, []string{
		"src/main.tsx",
		"src/new.txt",
	})

	events := emitter.Buffer()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	written := []FileWrittenEvent{}
	for _, event := range events {
		if event.Type != EventFileWritten {
			t.Fatalf("expected file_written event, got %s", event.Type)
		}
		payload, ok := event.Data.(FileWrittenEvent)
		if !ok {
			t.Fatalf("expected FileWrittenEvent payload, got %T", event.Data)
		}
		written = append(written, payload)
	}
	if written[0].Path != "src/main.tsx" || written[0].Operation != "update" {
		t.Errorf("expected first event update at src/main.tsx, got path=%s operation=%s", written[0].Path, written[0].Operation)
	}
	if written[1].Path != "src/new.txt" || written[1].Operation != "create" {
		t.Errorf("expected second event create at src/new.txt, got path=%s operation=%s", written[1].Path, written[1].Operation)
	}

	if err := os.Remove(filepath.Join(repoPath, "src/main.tsx")); err != nil {
		t.Fatalf("remove existing file: %v", err)
	}

	emitWorkspaceDiffEvents(repo, baseFileMap, runID, emitter, []string{"src/main.tsx"})
	deleteEvents := emitter.Buffer()[len(events):]
	if len(deleteEvents) != 1 || deleteEvents[0].Type != EventFileDeleted {
		t.Fatalf("expected one delete event after removal, got %#v", deleteEvents)
	}
	deletePayload, ok := deleteEvents[0].Data.(FileDeletedEvent)
	if !ok {
		t.Fatalf("expected FileDeletedEvent payload, got %T", deleteEvents[0].Data)
	}
	if deletePayload.Path != "src/main.tsx" {
		t.Fatalf("expected delete event for src/main.tsx, got %s", deletePayload.Path)
	}
}

func TestEmitWorkspaceDiffEvents_IgnoresProtectedPaths(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := &gitrepo.Repo{Path: repoPath}
	runID := uuid.New()

	if err := writeContent(filepath.Join(repoPath, ".env"), []byte("SECRET=1")); err != nil {
		t.Fatalf(".env: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, ".env.production"), []byte("SECRET=2")); err != nil {
		t.Fatalf(".env.production: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, "node_modules/tool.js"), []byte("bundle")); err != nil {
		t.Fatalf("node module: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, "src/ok.txt"), []byte("ok")); err != nil {
		t.Fatalf("ok file: %v", err)
	}

	emitter := &captureEmitter{}
	emitWorkspaceDiffEvents(repo, NewSafeFileMap(nil), runID, emitter, []string{
		".env",
		".env.production",
		"node_modules/tool.js",
		"src/ok.txt",
	})
	events := emitter.Buffer()
	if len(events) != 1 {
		t.Fatalf("expected only one event for protected path filtering, got %d", len(events))
	}
	if events[0].Type != EventFileWritten {
		t.Fatalf("expected file_written, got %s", events[0].Type)
	}
	payload, ok := events[0].Data.(FileWrittenEvent)
	if !ok {
		t.Fatalf("expected FileWrittenEvent payload, got %T", events[0].Data)
	}
	if payload.Path != "src/ok.txt" {
		t.Fatalf("expected src/ok.txt, got %s", payload.Path)
	}
}

func TestEmitWorkspaceDiffEvents_OmitsLargeFileContents(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := &gitrepo.Repo{Path: repoPath}
	runID := uuid.New()
	large := make([]byte, maxWriteBytes+1)
	for i := range large {
		large[i] = 'x'
	}
	if err := writeContent(filepath.Join(repoPath, "src/huge.txt"), large); err != nil {
		t.Fatalf("write huge file: %v", err)
	}

	emitter := &captureEmitter{}
	emitWorkspaceDiffEvents(repo, NewSafeFileMap(nil), runID, emitter, []string{"src/huge.txt"})
	events := emitter.Buffer()
	if len(events) != 1 {
		t.Fatalf("expected one progress event for large file, got %d", len(events))
	}
	if events[0].Type != EventToolProgress {
		t.Fatalf("expected EventToolProgress for oversized file, got %s", events[0].Type)
	}
	progress, ok := events[0].Data.(ToolProgressEvent)
	if !ok {
		t.Fatalf("expected ToolProgressEvent payload, got %T", events[0].Data)
	}
	if progress.Path != "src/huge.txt" {
		t.Fatalf("expected progress for src/huge.txt, got %s", progress.Path)
	}
	if progress.Bytes != maxWriteBytes+1 {
		t.Fatalf("expected %d bytes, got %d", maxWriteBytes+1, progress.Bytes)
	}
}
