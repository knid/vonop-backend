package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knid/wemob/internal/services/gitrepo"
)

func TestWorkspaceDiffPoller_EmitsChangesForModifiedAndUpdatedContent(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	repo, err := initRepoWithSeed(t, repoPath)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}
	base, err := repo.Head()
	if err != nil {
		t.Fatalf("repo head: %v", err)
	}

	events := make(chan []string, 8)
	poller := NewWorkspaceDiffPoller(repo, base, func(paths []string) {
		events <- paths
	})
	poller.interval = 80 * time.Millisecond
	poller.Start()
	t.Cleanup(func() {
		poller.Stop()
	})

	if err := writeAndStage(repoPath, "src/main.tsx", "updated"); err != nil {
		t.Fatalf("write: %v", err)
	}
	runCmd(t, repoPath, "add", "src/main.tsx")

	got := waitForEvent(t, events, 300*time.Millisecond)
	if len(got) != 1 || got[0] != "src/main.tsx" {
		t.Fatalf("expected first event [src/main.tsx], got %#v", got)
	}

	// Re-write same content as current diff to ensure de-duping by content hash.
	if err := writeAndStage(repoPath, "src/main.tsx", "updated"); err != nil {
		t.Fatalf("write same: %v", err)
	}
	runCmd(t, repoPath, "add", "src/main.tsx")
	time.Sleep(220 * time.Millisecond)
	select {
	case extra := <-events:
		t.Fatalf("did not expect deduped event, got %#v", extra)
	default:
	}

	if err := writeAndStage(repoPath, "src/main.tsx", "updated again"); err != nil {
		t.Fatalf("write again: %v", err)
	}
	runCmd(t, repoPath, "add", "src/main.tsx")
	got = waitForEvent(t, events, 300*time.Millisecond)
	if len(got) != 1 || got[0] != "src/main.tsx" {
		t.Fatalf("expected second event [src/main.tsx], got %#v", got)
	}
}

func TestWorkspaceDiffPoller_StopsAndDoesNotEmitAfterStop(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	repo, err := initRepoWithSeed(t, repoPath)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	base, err := repo.Head()
	if err != nil {
		t.Fatalf("repo head: %v", err)
	}

	calls := 0
	poller := NewWorkspaceDiffPoller(repo, base, func([]string) {
		calls++
	})
	poller.interval = 80 * time.Millisecond
	poller.Start()
	poller.Stop()
	poller.Stop()

	if err := writeAndStage(repoPath, "src/main.tsx", "again"); err != nil {
		t.Fatalf("write: %v", err)
	}
	runCmd(t, repoPath, "add", "src/main.tsx")

	time.Sleep(250 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("expected no callbacks after stop, got %d", calls)
	}
}

func TestWorkspaceDiffPoller_FiltersDuplicatePathsByContentHash(t *testing.T) {
	dir := t.TempDir()
	repo := &gitrepo.Repo{Path: dir}
	poller := NewWorkspaceDiffPoller(repo, "base", nil)

	if err := writeContent(filepath.Join(dir, "src", "main.tsx"), []byte("v1")); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	out := poller.filterDuplicatePaths([]string{"src/main.tsx"})
	if len(out) != 1 || out[0] != "src/main.tsx" {
		t.Fatalf("expected first hash emit, got %#v", out)
	}

	out = poller.filterDuplicatePaths([]string{"src/main.tsx"})
	if len(out) != 0 {
		t.Fatalf("expected duplicate hash suppress, got %#v", out)
	}

	if err := writeContent(filepath.Join(dir, "src", "main.tsx"), []byte("v2")); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	out = poller.filterDuplicatePaths([]string{"src/main.tsx"})
	if len(out) != 1 || out[0] != "src/main.tsx" {
		t.Fatalf("expected updated content emit, got %#v", out)
	}
}

func TestWorkspaceDiffPoller_IgnoresProtectedPathsFromRepoChangeDetection(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo")
	repo, err := initRepoWithSeed(t, repoPath)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	base, err := repo.Head()
	if err != nil {
		t.Fatalf("repo head: %v", err)
	}

	if err := writeContent(filepath.Join(repoPath, "src/ok.ts"), []byte("change")); err != nil {
		t.Fatalf("write src/ok.ts: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, ".env"), []byte("SECRET=1")); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, "node_modules", "tool.js"), []byte("ignored")); err != nil {
		t.Fatalf("write node_modules tool: %v", err)
	}

	poller := NewWorkspaceDiffPoller(repo, base, nil)
	poller.interval = 80 * time.Millisecond

	changed, err := repo.WorkingTreeChangedPaths(base)
	if err != nil {
		t.Fatalf("working tree changed paths: %v", err)
	}
	filtered := poller.filterDuplicatePaths(changed)
	if len(filtered) != 1 {
		t.Fatalf("expected only src/ok.ts, got %#v", filtered)
	}
	if filtered[0] != "src/ok.ts" {
		t.Fatalf("expected first path to be src/ok.ts, got %s", filtered[0])
	}
}

func initRepoWithSeed(t *testing.T, dir string) (*gitrepo.Repo, error) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, err
	}

	runCmd(t, dir, "init", "-q")
	runCmd(t, dir, "config", "user.email", "test@localhost")
	runCmd(t, dir, "config", "user.name", "Test User")

	if err := writeContent(filepath.Join(dir, "src", "main.tsx"), []byte("before")); err != nil {
		return nil, err
	}
	if err := writeContent(filepath.Join(dir, "web-build", "old.js"), []byte("ignored")); err != nil {
		return nil, err
	}
	runCmd(t, dir, "add", "src", "web-build")
	runCmd(t, dir, "commit", "-m", "seed")

	return &gitrepo.Repo{Path: dir}, nil
}

func writeAndStage(dir, rel, content string) error {
	return writeContent(filepath.Join(dir, rel), []byte(content))
}

func waitForEvent(t *testing.T, events <-chan []string, timeout time.Duration) []string {
	t.Helper()
	select {
	case ev := <-events:
		return ev
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event after %s", timeout)
	}
	return nil
}

func runCmd(t *testing.T, dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", strings.Join(args, " "), err, string(output))
	}
}

func writeContent(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return err
	}
	return nil
}
