package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func runCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", strings.Join(args, " "), err, string(output))
	}
}

func writeFile(t *testing.T, dir, rel string, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func createRepoForWorkingTreeChanges(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runCmd(t, dir, "init", "-q")
	runCmd(t, dir, "config", "user.email", "test@localhost")
	runCmd(t, dir, "config", "user.name", "Test User")

	writeFile(t, dir, "src/active.tsx", "before")
	writeFile(t, dir, "src/to-delete.tsx", "remove me")
	writeFile(t, dir, "src/legacy.tsx", "legacy")
	writeFile(t, dir, "web-build/bundle.js", "bundle")
	writeFile(t, dir, "_wemob/meta.json", "{}")
	runCmd(t, dir, "add", "src", "web-build", "_wemob")
	runCmd(t, dir, "commit", "-m", "seed")
	return dir
}

func TestWorkingTreeChangedPaths_TracksRelevantChangesAndFiltersNoisyPaths(t *testing.T) {
	dir := createRepoForWorkingTreeChanges(t)
	repo := &Repo{Path: dir, ProjectID: "p", service: nil}

	baseCommit, err := repo.Head()
	if err != nil {
		t.Fatalf("base commit: %v", err)
	}

	writeFile(t, dir, "src/active.tsx", "updated")
	writeFile(t, dir, "src/new.tsx", "new file")
	if err := os.Rename(filepath.Join(dir, "src/legacy.tsx"), filepath.Join(dir, "src/renamed.tsx")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	runCmd(t, dir, "add", "-A")
	writeFile(t, dir, "web-build/new-bundle.js", "another")
	writeFile(t, dir, "src/untracked.txt", "scratch")
	runCmd(t, dir, "rm", "-q", "src/to-delete.tsx")

	// Add a noise-path untracked file; should be ignored by filtering.
	writeFile(t, dir, "web-build/untracked.js", "ignored untracked")
	writeFile(t, dir, "_wemob/noise.txt", "ignored tracked/untracked helper")
	runCmd(t, dir, "add", ".")

	paths, err := repo.WorkingTreeChangedPaths(baseCommit)
	if err != nil {
		t.Fatalf("WorkingTreeChangedPaths: %v", err)
	}

	expected := []string{
		"src/active.tsx",
		"src/new.tsx",
		"src/legacy.tsx",
		"src/renamed.tsx",
		"src/to-delete.tsx",
		"src/untracked.txt",
	}
	sort.Strings(expected)
	sort.Strings(paths)

	if len(paths) != len(expected) {
		t.Fatalf("expected %d changed paths, got %d: %#v", len(expected), len(paths), paths)
	}
	for i := range expected {
		if paths[i] != expected[i] {
			t.Fatalf("path mismatch at %d: expected %q got %q", i, expected[i], paths[i])
		}
	}
}

func TestShouldIgnorePath(t *testing.T) {
	cases := []struct {
		path       string
		shouldSkip bool
	}{
		{".git/config", true},
		{"_wemob/theme.json", true},
		{"dist/file.txt", true},
		{"build/artifacts/app.js", true},
		{"android/main.kt", true},
		{"src/app.tsx", false},
		{"mobile/src/index.tsx", false},
	}
	for _, tc := range cases {
		if got := shouldIgnorePath(tc.path); got != tc.shouldSkip {
			t.Fatalf("shouldIgnorePath(%q) = %v, want %v", tc.path, got, tc.shouldSkip)
		}
	}
}
