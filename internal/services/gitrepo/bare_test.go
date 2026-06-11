package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBareServiceInitAndRunWorktree(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc := NewBareService(
		filepath.Join(root, "bare"),
		filepath.Join(root, "worktrees"),
		filepath.Join(root, "legacy"),
	)

	repo, err := svc.InitBareProject(ctx, "project-1")
	if err != nil {
		t.Fatalf("InitBareProject: %v", err)
	}
	if repo.CurrentCommit == "" {
		t.Fatal("expected initialized current commit")
	}
	if !isBareRepo(repo.Path) {
		t.Fatalf("expected bare repo at %s", repo.Path)
	}

	wt, err := svc.CreateRunWorktree(ctx, "project-1", "job-1", repo.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree: %v", err)
	}
	if got, want := gitOutputForTest(t, wt.Path, "branch", "--show-current"), "agent/job-1"; got != want {
		t.Fatalf("branch mismatch: got %q want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".gitignore")); err != nil {
		t.Fatalf("expected initialized files in worktree: %v", err)
	}

	if err := wt.Remove(ctx); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "worktrees", "project-1", "job-1")); !os.IsNotExist(err) {
		t.Fatalf("expected worktree removed, stat err=%v", err)
	}
}

func TestBareServiceMigrateLegacyRepoToBareCommitsDirtyTree(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacyRoot := filepath.Join(root, "legacy")
	legacyPath := filepath.Join(legacyRoot, "project-2")
	if err := os.MkdirAll(legacyPath, 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	gitForTest(t, legacyPath, "init", "-b", "main")
	mustWrite(t, filepath.Join(legacyPath, "README.md"), []byte("# Project\n"))
	if err := commitAll(ctx, legacyPath, "initial commit"); err != nil {
		t.Fatalf("initial commit: %v", err)
	}
	mustWrite(t, filepath.Join(legacyPath, "dirty.txt"), []byte("persist me\n"))

	svc := NewBareService(
		filepath.Join(root, "bare"),
		filepath.Join(root, "worktrees"),
		legacyRoot,
	)
	result, err := svc.MigrateLegacyRepoToBare(ctx, "project-2")
	if err != nil {
		t.Fatalf("MigrateLegacyRepoToBare: %v", err)
	}
	if !result.CreatedBareRepo {
		t.Fatal("expected migration to create bare repo")
	}
	if !result.CreatedDirtyCommit {
		t.Fatal("expected migration to commit dirty legacy changes")
	}
	if result.CurrentCommit == "" {
		t.Fatal("expected migrated current commit")
	}

	wt, err := svc.CreateRunWorktree(ctx, "project-2", "job-2", result.CurrentCommit)
	if err != nil {
		t.Fatalf("CreateRunWorktree from migrated commit: %v", err)
	}
	defer wt.Remove(ctx)
	data, err := os.ReadFile(filepath.Join(wt.Path, "dirty.txt"))
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if string(data) != "persist me\n" {
		t.Fatalf("dirty file content mismatch: %q", data)
	}
}

func gitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitOutputForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
