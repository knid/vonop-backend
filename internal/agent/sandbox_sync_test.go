package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func TestSyncSandboxAfterCommitPassesChangedPaths(t *testing.T) {
	state := NewBuildState("build app", false)
	state.RecordFileWrite("frontend/src/App.tsx", "update")
	state.RecordFileDelete("frontend/src/old.tsx")

	var gotProjectID uuid.UUID
	var gotPaths []string
	deps := &RunDependencies{
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			gotProjectID = project.ID
			gotPaths = append([]string(nil), paths...)
			return nil
		},
	}
	project := &models.AppProject{ID: uuid.New()}

	syncSandboxAfterCommit(context.Background(), deps, project, state)

	if gotProjectID != project.ID {
		t.Fatalf("synced project ID = %s, want %s", gotProjectID, project.ID)
	}
	wantPaths := []string{"frontend/src/App.tsx", "frontend/src/old.tsx"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("synced paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestSyncSandboxAfterCommitSkipsEmptyChanges(t *testing.T) {
	calls := 0
	deps := &RunDependencies{
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			calls++
			return nil
		},
	}

	syncSandboxAfterCommit(context.Background(), deps, &models.AppProject{ID: uuid.New()}, NewBuildState("build app", false))

	if calls != 0 {
		t.Fatalf("sync called %d times, want 0", calls)
	}
}
