package gitrepo

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizeRunWorktreeSuccessUpdatesMainAndRunRef(t *testing.T) {
	ctx := context.Background()
	svc, repo := newBareServiceForFinalizerTest(t)
	wt, err := svc.CreateRunWorktree(ctx, repo.ProjectID, "job-success", repo.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree: %v", err)
	}
	defer wt.Remove(ctx)
	mustWrite(t, filepath.Join(wt.Path, "src/App.tsx"), []byte("hello\n"))

	result, err := svc.FinalizeRunWorktree(ctx, wt, FinalizeRunOptions{
		Successful:            true,
		Summary:               "update app",
		ExpectedCurrentCommit: repo.CurrentCommit,
	})
	if err != nil {
		t.Fatalf("FinalizeRunWorktree: %v", err)
	}
	if result.Noop || result.RunCommit == "" || result.FinalCommit == "" || !result.FinalizedToProject {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := gitOutputForTest(t, repo.Path, "rev-parse", "refs/heads/main"); got != result.FinalCommit {
		t.Fatalf("main = %q want %q", got, result.FinalCommit)
	}
	if got := gitOutputForTest(t, repo.Path, "rev-parse", result.RunRef); got != result.RunCommit {
		t.Fatalf("run ref = %q want %q", got, result.RunCommit)
	}
}

func TestFinalizeRunWorktreeFailedCreatesCheckpointOnly(t *testing.T) {
	ctx := context.Background()
	svc, repo := newBareServiceForFinalizerTest(t)
	wt, err := svc.CreateRunWorktree(ctx, repo.ProjectID, "job-failed", repo.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree: %v", err)
	}
	defer wt.Remove(ctx)
	mustWrite(t, filepath.Join(wt.Path, "partial.txt"), []byte("partial\n"))

	result, err := svc.FinalizeRunWorktree(ctx, wt, FinalizeRunOptions{
		Successful:            false,
		Summary:               "validation failed",
		ExpectedCurrentCommit: repo.CurrentCommit,
	})
	if err != nil {
		t.Fatalf("FinalizeRunWorktree: %v", err)
	}
	if result.CheckpointCommit == "" || result.FinalizedToProject {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := gitOutputForTest(t, repo.Path, "rev-parse", "refs/heads/main"); got != repo.CurrentCommit {
		t.Fatalf("main moved on failed run: got %q want %q", got, repo.CurrentCommit)
	}
}

func TestFinalizeRunWorktreeNoopCreatesNoCommit(t *testing.T) {
	ctx := context.Background()
	svc, repo := newBareServiceForFinalizerTest(t)
	wt, err := svc.CreateRunWorktree(ctx, repo.ProjectID, "job-noop", repo.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree: %v", err)
	}
	defer wt.Remove(ctx)

	result, err := svc.FinalizeRunWorktree(ctx, wt, FinalizeRunOptions{
		Successful:            true,
		Summary:               "noop",
		ExpectedCurrentCommit: repo.CurrentCommit,
	})
	if err != nil {
		t.Fatalf("FinalizeRunWorktree: %v", err)
	}
	if !result.Noop || result.RunCommit != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if out := gitOutputForTestAllowError(t, repo.Path, "rev-parse", "refs/wemob/runs/job-noop"); out != "" {
		t.Fatalf("expected no run ref, got %q", out)
	}
}

func TestFinalizeRunWorktreeConflictKeepsRunRefAndDoesNotMoveMain(t *testing.T) {
	ctx := context.Background()
	svc, repo := newBareServiceForFinalizerTest(t)
	wt, err := svc.CreateRunWorktree(ctx, repo.ProjectID, "job-conflict", repo.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree: %v", err)
	}
	defer wt.Remove(ctx)
	mustWrite(t, filepath.Join(wt.Path, "change.txt"), []byte("change\n"))

	result, err := svc.FinalizeRunWorktree(ctx, wt, FinalizeRunOptions{
		Successful:            true,
		Summary:               "conflicting change",
		ExpectedCurrentCommit: "different-current-commit",
	})
	if !errors.Is(err, ErrFinalizationConflict) {
		t.Fatalf("expected conflict, got result=%+v err=%v", result, err)
	}
	if result == nil || result.RunCommit == "" || result.CheckpointCommit == "" {
		t.Fatalf("expected saved run checkpoint, got %+v", result)
	}
	if got := gitOutputForTest(t, repo.Path, "rev-parse", "refs/heads/main"); got != repo.CurrentCommit {
		t.Fatalf("main moved on conflict: got %q want %q", got, repo.CurrentCommit)
	}
}

func newBareServiceForFinalizerTest(t *testing.T) (*BareService, *BareRepo) {
	t.Helper()
	root := t.TempDir()
	svc := NewBareService(
		filepath.Join(root, "bare"),
		filepath.Join(root, "worktrees"),
		filepath.Join(root, "legacy"),
	)
	repo, err := svc.InitBareProject(context.Background(), "project-finalizer")
	if err != nil {
		t.Fatalf("InitBareProject: %v", err)
	}
	return svc, repo
}

func gitOutputForTestAllowError(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := testGitCommand(dir, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func testGitCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}
