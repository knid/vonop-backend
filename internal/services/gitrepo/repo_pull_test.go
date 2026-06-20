package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestPullFromURLFastForwardsWithoutPersistingRemote(t *testing.T) {
	base := t.TempDir()
	remotePath := filepath.Join(base, "remote.git")
	workPath := filepath.Join(base, "work")
	clonePath := filepath.Join(base, "clone")

	runGit(t, base, "init", "--bare", remotePath)
	runGit(t, base, "clone", remotePath, workPath)
	runGit(t, workPath, "checkout", "-b", "main")
	runGit(t, workPath, "config", "user.email", "test@example.com")
	runGit(t, workPath, "config", "user.name", "Test User")
	mustWrite(t, filepath.Join(workPath, "app.txt"), []byte("v1\n"))
	runGit(t, workPath, "add", "app.txt")
	runGit(t, workPath, "commit", "-m", "initial")
	runGit(t, workPath, "push", "-u", "origin", "main")

	runGit(t, base, "clone", remotePath, clonePath)
	runGit(t, clonePath, "checkout", "main")
	repo := &Repo{
		Path:      clonePath,
		ProjectID: "project-1",
		service:   NewService(filepath.Join(base, "repos")),
	}

	mustWrite(t, filepath.Join(workPath, "app.txt"), []byte("v2\n"))
	runGit(t, workPath, "add", "app.txt")
	runGit(t, workPath, "commit", "-m", "remote update")
	runGit(t, workPath, "push", "origin", "main")

	if err := repo.PullFromURL(remotePath, "main"); err != nil {
		t.Fatalf("PullFromURL: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(clonePath, "app.txt"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if string(got) != "v2\n" {
		t.Fatalf("expected pulled content v2, got %q", got)
	}

	remotes := runGit(t, clonePath, "remote", "-v")
	if strings.Contains(remotes, remotePath) && strings.Contains(remotes, "wemob") {
		t.Fatalf("PullFromURL persisted temporary remote: %s", remotes)
	}
}
