package agent

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

type fakeChangedPathsRepo struct {
	paths []string
	err   error
}

func (f fakeChangedPathsRepo) ChangedPathsSince(fromHash string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.paths...), nil
}

func TestSyncSandboxAfterCommitPrefersGitDiffChangedPaths(t *testing.T) {
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
	repo := fakeChangedPathsRepo{paths: []string{
		"backend/lib/wemob-ai.ts",
		"frontend/src/App.tsx",
	}}

	syncSandboxAfterCommit(context.Background(), deps, repo, project, state, "pre-commit")

	if gotProjectID != project.ID {
		t.Fatalf("synced project ID = %s, want %s", gotProjectID, project.ID)
	}
	wantPaths := []string{"backend/lib/wemob-ai.ts", "frontend/src/App.tsx"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("synced paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestSyncSandboxAfterCommitFallsBackToBuildState(t *testing.T) {
	state := NewBuildState("build app", false)
	state.RecordFileWrite("frontend/src/App.tsx", "update")

	var gotPaths []string
	deps := &RunDependencies{
		SyncSandboxFiles: func(ctx context.Context, project *models.AppProject, paths []string) error {
			gotPaths = append([]string(nil), paths...)
			return nil
		},
	}

	syncSandboxAfterCommit(
		context.Background(),
		deps,
		fakeChangedPathsRepo{err: fmt.Errorf("diff failed")},
		&models.AppProject{ID: uuid.New()},
		state,
		"pre-commit",
	)

	wantPaths := []string{"frontend/src/App.tsx"}
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

	syncSandboxAfterCommit(context.Background(), deps, fakeChangedPathsRepo{}, &models.AppProject{ID: uuid.New()}, NewBuildState("build app", false), "pre-commit")

	if calls != 0 {
		t.Fatalf("sync called %d times, want 0", calls)
	}
}
