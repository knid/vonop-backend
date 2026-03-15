package gitrepo

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Repo struct {
	Path      string
	ProjectID string
	service   *Service
}

type CommitInfo struct {
	Hash      string
	Message   string
	Timestamp time.Time
}

// WriteFile writes content to the given path within the repo.
func (r *Repo) WriteFile(path string, content []byte) error {
	fullPath := filepath.Join(r.Path, path)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("gitrepo: mkdir: %w", err)
	}
	return os.WriteFile(fullPath, content, 0644)
}

// ReadFile reads a file from the working tree.
func (r *Repo) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(r.Path, path))
}

// DeleteFile removes a file from the working tree.
func (r *Repo) DeleteFile(path string) error {
	return os.Remove(filepath.Join(r.Path, path))
}

// FileExists checks if a file exists in the working tree.
func (r *Repo) FileExists(path string) bool {
	_, err := os.Stat(filepath.Join(r.Path, path))
	return err == nil
}

// ListFiles returns all file paths in the working tree, excluding .git/ and _wemob/.
func (r *Repo) ListFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(r.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(r.Path, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (rel == ".git" || rel == metaDir) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if rel == ".gitignore" {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

// ReadAllFiles reads all files from the working tree into a map, excluding .git/, _wemob/, .gitignore.
func (r *Repo) ReadAllFiles() (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(r.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(r.Path, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() && (rel == ".git" || rel == metaDir) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if rel == ".gitignore" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	return files, err
}

// commit is the internal, lock-free version. Caller must hold the project lock.
func (r *Repo) commit(message string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gitrepo: git add: %s: %s", err, out)
	}

	cmd = exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return r.head()
	}

	cmd = exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gitrepo: git commit: %s: %s", err, out)
	}

	return r.head()
}

// Commit stages all changes and creates a commit. Returns the commit hash.
// Acquires the per-project lock.
func (r *Repo) Commit(message string) (string, error) {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()
	return r.commit(message)
}

// revert is the internal, lock-free version. Caller must hold the project lock.
func (r *Repo) revert(commitHash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "read-tree", "--reset", "-u", commitHash)
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gitrepo: read-tree: %s: %s", err, out)
	}
	return nil
}

// Revert restores the working tree to the state at the given commit hash.
// Acquires the per-project lock.
func (r *Repo) Revert(commitHash string) error {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()
	return r.revert(commitHash)
}

// RevertAndCommit atomically reverts to a commit and commits the result.
// Uses a single lock acquisition to avoid deadlock.
func (r *Repo) RevertAndCommit(targetHash, commitMessage string) (string, error) {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()
	if err := r.revert(targetHash); err != nil {
		return "", err
	}
	return r.commit(commitMessage)
}

// WithLock acquires the project lock and calls fn. Use for multi-step
// git operations that need atomicity (e.g., commit + revert + commit).
func (r *Repo) WithLock(fn func() error) error {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()
	return fn()
}

// Tag creates a lightweight tag at the current HEAD.
func (r *Repo) Tag(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "tag", name)
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gitrepo: tag: %s: %s", err, out)
	}
	return nil
}

// Log returns the last N commits.
func (r *Repo) Log(limit int) ([]CommitInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "log",
		fmt.Sprintf("--max-count=%d", limit),
		"--format=%H\t%s\t%aI",
	)
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gitrepo: log: %w", err)
	}

	var commits []CommitInfo
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, parts[2])
		commits = append(commits, CommitInfo{
			Hash:      parts[0],
			Message:   parts[1],
			Timestamp: ts,
		})
	}
	return commits, nil
}

// ReadAllFilesAtCommit reads all files at a specific commit using git archive.
func (r *Repo) ReadAllFilesAtCommit(commitHash string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar", commitHash)
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gitrepo: archive: %w", err)
	}

	files := make(map[string]string)
	tr := tar.NewReader(bytes.NewReader(out))
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.HasPrefix(hdr.Name, metaDir+"/") || hdr.Name == ".gitignore" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			continue
		}
		files[hdr.Name] = string(data)
	}
	return files, nil
}

// head returns the current HEAD commit hash.
func (r *Repo) head() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Head is the exported version.
func (r *Repo) Head() (string, error) {
	return r.head()
}

// WriteTheme writes the theme JSON to _wemob/theme.json.
func (r *Repo) WriteTheme(themeJSON []byte) error {
	return r.WriteFile(filepath.Join(metaDir, "theme.json"), themeJSON)
}

// ReadTheme reads the theme JSON from _wemob/theme.json.
func (r *Repo) ReadTheme() ([]byte, error) {
	return r.ReadFile(filepath.Join(metaDir, "theme.json"))
}

// CurrentBranch returns the name of the current checked-out branch.
func (r *Repo) CurrentBranch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PushToURL pushes the current branch to a remote URL. The URL is not persisted
// as a named remote — this prevents tokens from being stored in .git/config.
// Acquires the per-project lock to prevent concurrent pushes.
func (r *Repo) PushToURL(url, branch string) error {
	unlock := r.service.lock(r.ProjectID)
	defer unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "push", url, branch)
	cmd.Dir = r.Path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gitrepo: push: %s: %s", err, out)
	}
	return nil
}
