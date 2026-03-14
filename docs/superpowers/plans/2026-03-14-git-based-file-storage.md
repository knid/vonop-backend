# Git-Based File Storage Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace PostgreSQL-based file storage (AppGeneratedFile table + AST blobs) with per-project git repositories on the local filesystem.

**Architecture:** New `internal/services/gitrepo/` package wraps `os/exec` git CLI calls. Controller gets `*gitrepo.Service` injected. All file reads/writes go through git repos at `/data/repos/{projectId}/`. Snapshots replaced by git commit history. Theme stored as `_wemob/theme.json` in repo.

**Tech Stack:** Go, git CLI via `os/exec`, local filesystem

**Spec:** `docs/superpowers/specs/2026-03-14-git-based-file-storage-design.md`

---

## Chunk 1: Git Repo Service

### Task 1: Create gitrepo Service struct and Init/Open/Delete

**Files:**
- Create: `internal/services/gitrepo/service.go`

- [ ] **Step 1: Create the Service struct with per-project locking**

```go
package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	commitTimeout = 30 * time.Second
	readTimeout   = 10 * time.Second
	metaDir       = "_wemob"
)

type Service struct {
	BasePath string
	locks    sync.Map // map[projectID]*sync.Mutex
}

func NewService(basePath string) *Service {
	return &Service{BasePath: basePath}
}

// lock acquires a per-project mutex and returns an unlock function.
func (s *Service) lock(projectID string) func() {
	v, _ := s.locks.LoadOrStore(projectID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
```

- [ ] **Step 2: Implement Init — creates a new git repo for a project**

```go
// Init creates a new git repository for the given project.
func (s *Service) Init(projectID string) (*Repo, error) {
	repoPath := filepath.Join(s.BasePath, projectID)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir: %w", err)
	}

	// Initialize git repo
	if err := s.git(repoPath, "init"); err != nil {
		return nil, fmt.Errorf("gitrepo: init: %w", err)
	}

	// Create .gitignore
	gitignore := "node_modules/\n.next/\n.env*\n"
	if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return nil, fmt.Errorf("gitrepo: write .gitignore: %w", err)
	}

	// Create _wemob metadata directory
	if err := os.MkdirAll(filepath.Join(repoPath, metaDir), 0755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir _wemob: %w", err)
	}

	return &Repo{Path: repoPath, ProjectID: projectID, service: s}, nil
}
```

- [ ] **Step 3: Implement Open — opens an existing repo (or initializes if missing)**

```go
// Open returns a handle to an existing repo. Returns error if it doesn't exist.
func (s *Service) Open(projectID string) (*Repo, error) {
	repoPath := filepath.Join(s.BasePath, projectID)
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return nil, fmt.Errorf("gitrepo: repo not found for project %s", projectID)
	}
	return &Repo{Path: repoPath, ProjectID: projectID, service: s}, nil
}

// OpenOrInit opens a repo if it exists, otherwise initializes a new one.
func (s *Service) OpenOrInit(projectID string) (*Repo, error) {
	repo, err := s.Open(projectID)
	if err == nil {
		return repo, nil
	}
	return s.Init(projectID)
}
```

- [ ] **Step 4: Implement Delete and Clone**

```go
// Delete removes a project's git repository from disk.
func (s *Service) Delete(projectID string) error {
	repoPath := filepath.Join(s.BasePath, projectID)
	return os.RemoveAll(repoPath)
}

// Clone copies the working tree (no .git history) from source into a new repo.
// Locks the source project during copy to prevent inconsistent state.
func (s *Service) Clone(sourceID, targetID string) (*Repo, error) {
	srcPath := filepath.Join(s.BasePath, sourceID)
	if _, err := os.Stat(srcPath); err != nil {
		return nil, fmt.Errorf("gitrepo: source repo not found: %s", sourceID)
	}

	// Lock source to prevent concurrent writes during copy
	unlockSrc := s.lock(sourceID)
	defer unlockSrc()

	target, err := s.Init(targetID)
	if err != nil {
		return nil, err
	}

	// Copy working tree files (skip .git)
	err = filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcPath, path)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".gitignore" {
			return nil // already created by Init
		}
		destPath := filepath.Join(target.Path, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0644)
	})
	if err != nil {
		return nil, fmt.Errorf("gitrepo: clone copy: %w", err)
	}

	if _, err := target.Commit("clone: copied from " + sourceID); err != nil {
		return nil, fmt.Errorf("gitrepo: clone commit: %w", err)
	}

	return target, nil
}

// git runs a git command in the given directory.
func (s *Service) git(dir string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}

// Note: use strings.HasPrefix from stdlib, no custom helper needed.
```

- [ ] **Step 5: Commit**

```bash
git add internal/services/gitrepo/service.go
git commit -m "feat(gitrepo): add Service with Init, Open, Delete, Clone"
```

---

### Task 2: Create Repo struct with file and git operations

**Files:**
- Create: `internal/services/gitrepo/repo.go`

- [ ] **Step 1: Create Repo struct and file operations**

```go
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
```

- [ ] **Step 2: Implement git operations (Commit, Revert, Tag, Log)**

```go
// commit is the internal, lock-free version. Caller must hold the project lock.
func (r *Repo) commit(message string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commitTimeout)
	defer cancel()

	// Stage all changes
	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gitrepo: git add: %s: %s", err, out)
	}

	// Check if there's anything to commit
	cmd = exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = r.Path
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gitrepo: git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		// Nothing to commit — return current HEAD
		return r.head()
	}

	// Commit
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", message)
	cmd.Dir = r.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gitrepo: git commit: %s: %s", err, out)
	}

	return r.head()
}

// Commit stages all changes and creates a commit. Returns the commit hash.
// Acquires the per-project lock. For sequences of multiple git ops, use
// WithLock() + commit() instead to avoid deadlock.
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
// Uses git read-tree for full tree replacement (deletes files added after the target commit).
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

	// Use git archive to get a tar of the tree at that commit
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
		// Skip metadata files
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
```

- [ ] **Step 3: Add theme helpers**

```go
// WriteTheme writes the theme JSON to _wemob/theme.json.
func (r *Repo) WriteTheme(themeJSON []byte) error {
	return r.WriteFile(filepath.Join(metaDir, "theme.json"), themeJSON)
}

// ReadTheme reads the theme JSON from _wemob/theme.json.
func (r *Repo) ReadTheme() ([]byte, error) {
	return r.ReadFile(filepath.Join(metaDir, "theme.json"))
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/services/gitrepo/repo.go
git commit -m "feat(gitrepo): add Repo with file ops, commit, revert, tag, log"
```

---

## Chunk 2: Controller & Infrastructure Integration

### Task 3: Inject GitRepoService into Controller

**Files:**
- Modify: `internal/controllers/controller.go:23-65`
- Modify: `cmd/main.go` (Controller initialization + Docker setup)

- [ ] **Step 1: Add GitRepoService to Controller struct and NewController**

In `internal/controllers/controller.go`:

Add import:
```go
"github.com/knid/wemob/internal/services/gitrepo"
```

Add field to Controller struct (after `NeonService`):
```go
GitRepoService *gitrepo.Service
```

Add parameter to `NewController`:
```go
func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage, secretManager secret.SecretManager,
	vercelService *vercel.Service,
	neonService *neon.Service,
	posthogClient posthog.Client,
	gitRepoService *gitrepo.Service) *Controller {
```

Add to return struct:
```go
GitRepoService: gitRepoService,
```

- [ ] **Step 2: Initialize GitRepoService in main.go**

In `cmd/main.go`, after neon service initialization, add:

```go
// Git repo service for project file storage
repoBasePath := "/data/repos"
if envPath := os.Getenv("REPO_BASE_PATH"); envPath != "" {
	repoBasePath = envPath
}
gitRepoService := gitrepo.NewService(repoBasePath)
slog.Info("Git repo service initialized", "base_path", repoBasePath)
```

Pass `gitRepoService` to `NewController(...)`.

- [ ] **Step 3: Update Dockerfile to install git**

In `Dockerfile`, change the runtime `apk add` line:
```dockerfile
RUN apk add --no-cache ca-certificates tzdata curl git && \
    git config --global user.email "system@wemob.io" && \
    git config --global user.name "wemob" && \
    git config --global init.defaultBranch main
```

- [ ] **Step 4: Update docker-compose.yaml to add repo volume**

```yaml
services:
  api:
    build:
      context: .
      dockerfile: Dockerfile
    restart: unless-stopped
    expose:
      - "8001"
      - "9001"
    healthcheck:
      test: [ "CMD", "curl", "-f", "http://localhost:8001/health" ]
      interval: 10s
      timeout: 5s
      retries: 5
    env_file:
      - ./.env
    volumes:
      - repo-data:/data/repos
```

Add to volumes section:
```yaml
volumes:
  caddy_data:
  caddy_config:
  repo-data:
```

- [ ] **Step 5: Commit**

```bash
git add internal/controllers/controller.go cmd/main.go Dockerfile docker-compose.yaml
git commit -m "feat: inject GitRepoService into Controller and update Docker config"
```

---

### Task 4: Update saveGeneratedFile to write to git repo

**Files:**
- Modify: `internal/controllers/app_builder_controller.go:986-1117`

- [ ] **Step 1: Update saveGeneratedFile to use git repo**

The function needs a `*gitrepo.Repo` parameter. Change the signature and body:

```go
func (c *Controller) saveGeneratedFile(
	project *models.AppProject,
	repo *gitrepo.Repo, // NEW
	path string,
	content string,
	fileType string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
	allowProtected ...bool,
) {
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("saveGeneratedFile: rejecting suspicious file path", "path", path)
		return
	}
	path = cleanPath

	// Guard: never let the LLM overwrite template-managed files during initial generation.
	canOverride := len(allowProtected) > 0 && allowProtected[0]
	if !canOverride && isProtectedTemplateFile(path) {
		slog.Warn("saveGeneratedFile: skipping protected template file", "path", path)
		if existing, err := repo.ReadFile(path); err == nil {
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: "modify", FilePath: path,
				FileType: fileType, Content: string(existing),
			})
		}
		return
	}

	// Guard: block seed routes
	if strings.HasPrefix(path, "app/api/seed") || strings.HasPrefix(path, "app/api/migrate") {
		slog.Warn("saveGeneratedFile: blocking seed/migrate route", "path", path)
		return
	}

	// Guard: e-commerce restrictions
	if project.ProjectType == "ecommerce" {
		if strings.HasPrefix(path, "app/api/") && !strings.HasPrefix(path, "app/api/auth/") {
			slog.Warn("saveGeneratedFile: blocking API route in e-commerce project", "path", path)
			return
		}
		if path == "lib/db.ts" || path == "lib/schema.ts" {
			slog.Warn("saveGeneratedFile: blocking drizzle file in e-commerce project", "path", path)
			return
		}
	}

	// Determine action
	action := "create"
	if repo.FileExists(path) {
		existing, _ := repo.ReadFile(path)
		if normalizeContent(content) == normalizeContent(string(existing)) {
			sendEvent("file_complete", responses.FileCompleteEvent{
				Action: "modify", FilePath: path,
				FileType: fileType, Content: content,
			})
			return
		}
		action = "modify"
	}

	// Write to disk
	if err := repo.WriteFile(path, []byte(content)); err != nil {
		slog.Error("saveGeneratedFile: failed to write", "path", path, "error", err)
		return
	}

	fileMap[path] = content
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: action, FilePath: path,
		FileType: fileType, Content: content,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: action, Path: path, Type: fileType,
	})
}
```

- [ ] **Step 2: Update deleteGeneratedFile to use git repo**

```go
func (c *Controller) deleteGeneratedFile(
	project *models.AppProject,
	repo *gitrepo.Repo, // NEW
	path string,
	fileMap map[string]string,
	sendEvent func(string, any) bool,
	ops *[]appBuilderOperation,
) {
	cleanPath, valid := utils.SanitizeFilePath(path)
	if !valid {
		slog.Warn("deleteGeneratedFile: rejecting suspicious file path", "path", path)
		return
	}
	path = cleanPath

	if !repo.FileExists(path) {
		slog.Warn("deleteGeneratedFile: file not found", "path", path)
		return
	}

	if err := repo.DeleteFile(path); err != nil {
		slog.Error("deleteGeneratedFile: failed to delete", "path", path, "error", err)
		return
	}

	delete(fileMap, path)
	sendEvent("file_complete", responses.FileCompleteEvent{
		Action: "delete", FilePath: path,
	})
	*ops = append(*ops, appBuilderOperation{
		Action: "delete", Path: path,
	})
}
```

- [ ] **Step 3: Remove FileID from FileCompleteEvent**

In `internal/responses/sse_events.go`, remove the `FileID` field:

```go
type FileCompleteEvent struct {
	Action   string `json:"action"`
	FilePath string `json:"file_path"`
	FileType string `json:"file_type,omitempty"`
	Content  string `json:"content,omitempty"`
}
```

- [ ] **Step 4: Update all callers of saveGeneratedFile and deleteGeneratedFile**

Search for all calls to `saveGeneratedFile` and `deleteGeneratedFile` in `app_builder_controller.go` and pass the `repo` parameter. These callers need `repo` available in their scope — this means `StreamBuildApp` must open/init the repo at the start and pass it through.

At the top of `StreamBuildApp` (after project validation), add:

```go
repo, err := c.GitRepoService.OpenOrInit(project.ID.String())
if err != nil {
	sendError("system", "failed to open project repository")
	return
}
```

Then pass `repo` to all `saveGeneratedFile` / `deleteGeneratedFile` calls.

Also update the `FileCompleteEvent` emission at line 831 (package.json augmentation) to remove `FileID`.

- [ ] **Step 5: Add git commit at end of generation**

After all files are written (end of successful generation), add:

```go
commitMsg := "chat: " + message
if isInitialGeneration {
	commitMsg = "init: initial generation"
}
if _, err := repo.Commit(commitMsg); err != nil {
	slog.Error("Failed to commit after generation", "error", err, "project_id", project.ID)
}
```

Also write theme to repo before commit:
```go
if len(project.Theme) > 0 {
	if err := repo.WriteTheme(project.Theme); err != nil {
		slog.Warn("Failed to write theme to repo", "error", err)
	}
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/app_builder_controller.go internal/responses/sse_events.go
git commit -m "feat: switch saveGeneratedFile/deleteGeneratedFile to git repo"
```

---

## Chunk 3: Snapshot, Deploy, Preview, Export Migration

### Task 5: Replace snapshot system with git commits

**Files:**
- Modify: `internal/controllers/app_project_controller.go:217-328`
- Modify: `internal/controllers/app_builder_controller.go` (pre-change snapshot calls)

- [ ] **Step 1: Replace CreateAppSnapshot with git commit**

```go
// CreateAppSnapshot creates a git commit as a snapshot point.
// Returns the commit hash as the snapshot identifier.
func (c *Controller) CreateAppSnapshot(project *models.AppProject, description string, source string) (string, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return "", fmt.Errorf("failed to open repo: %w", err)
	}

	// Write current theme to repo so it's captured in the commit
	if len(project.Theme) > 0 {
		if err := repo.WriteTheme(project.Theme); err != nil {
			return "", fmt.Errorf("failed to write theme: %w", err)
		}
	}

	commitMsg := fmt.Sprintf("%s: %s", source, description)
	hash, err := repo.Commit(commitMsg)
	if err != nil {
		return "", fmt.Errorf("failed to commit snapshot: %w", err)
	}
	return hash, nil
}
```

- [ ] **Step 2: Replace RestoreAppSnapshot with git revert**

```go
// RestoreAppSnapshot reverts the working tree to a previous commit.
// Uses WithLock to hold a single lock across commit + revert + commit (avoids deadlock).
func (c *Controller) RestoreAppSnapshot(project *models.AppProject, commitHash string) (*models.AppProject, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open repo: %w", err)
	}

	// All git ops under a single lock to avoid deadlock
	var restoreErr error
	repo.WithLock(func() error {
		// Safety commit of current state
		if _, err := repo.commit("auto: before restore"); err != nil {
			restoreErr = fmt.Errorf("failed to create safety commit: %w", err)
			return restoreErr
		}
		// Revert to target
		if err := repo.revert(commitHash); err != nil {
			restoreErr = fmt.Errorf("failed to revert: %w", err)
			return restoreErr
		}
		// Commit reverted state
		if _, err := repo.commit("undo: restored to " + commitHash[:8]); err != nil {
			restoreErr = fmt.Errorf("failed to commit after revert: %w", err)
			return restoreErr
		}
		return nil
	})
	if restoreErr != nil {
		return nil, restoreErr
	}

	// Restore theme from repo
	if themeData, err := repo.ReadTheme(); err == nil && len(themeData) > 0 {
		project.Theme = themeData
	}

	project.UpdatedAt = time.Now()
	updated, err := c.DB.UpdateAppProject(project)
	if err != nil {
		return nil, err
	}
	return updated, nil
}
```

**Note:** `repo.commit()` and `repo.revert()` are the unexported, lock-free variants. The `WithLock` method holds the lock for the entire sequence, preventing deadlock.

- [ ] **Step 3: Replace GetAppSnapshots with git log**

```go
func (c *Controller) GetAppSnapshots(projectID string) ([]CommitHistoryEntry, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, err
	}
	commits, err := repo.Log(50)
	if err != nil {
		return nil, err
	}
	var entries []CommitHistoryEntry
	for _, c := range commits {
		entries = append(entries, CommitHistoryEntry{
			Hash:      c.Hash,
			Message:   c.Message,
			CreatedAt: c.Timestamp,
		})
	}
	return entries, nil
}

type CommitHistoryEntry struct {
	Hash      string    `json:"hash"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
```

- [ ] **Step 4: Remove DeleteAppSnapshot (no longer needed)**

Delete the `DeleteAppSnapshot` method — git history is immutable.

- [ ] **Step 5: Update StreamBuildApp snapshot calls**

In `app_builder_controller.go`, the pre-change snapshot call:

```go
// Old:
preSnapshot, snapErr := c.CreateAppSnapshot(project, "Auto-save before changes", "auto")

// New:
preCommitHash, snapErr := c.CreateAppSnapshot(project, "before changes", "auto")
```

Update rollback logic to use `repo.Revert(preCommitHash)` instead of `RestoreAppSnapshot`.

The `preSnapshotID` variable becomes `preCommitHash string`. All references to `preSnapshot.ID.String()` become `preCommitHash`.

Auto-rollback on failure (use `RevertAndCommit` to avoid deadlock):
```go
if preCommitHash != "" {
	if _, err := repo.RevertAndCommit(preCommitHash, "auto: rollback after failed generation"); err != nil {
		slog.Error("Auto-rollback failed", "error", err)
	}
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/controllers/app_project_controller.go internal/controllers/app_builder_controller.go
git commit -m "feat: replace snapshot system with git commits"
```

---

### Task 6: Update deploy, preview, export, file CRUD to use git

**Files:**
- Modify: `internal/controllers/app_deploy_controller.go:20-140, 462-480`
- Modify: `internal/controllers/app_project_controller.go:330-384`
- Modify: `internal/models/app_deployment.go:21`
- Modify: `internal/views/app_views.go` (init endpoint, export)

- [ ] **Step 1: Update DeployToVercel to read from git**

In `app_deploy_controller.go`, replace DB file reads with git:

```go
func (c *Controller) DeployToVercel(project *models.AppProject, user *models.User, target string) (*models.AppDeployment, error) {
	// ... existing Vercel project setup code unchanged ...

	// Read files from git repo
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo")
	}

	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}

	// ... rest of method unchanged (inject .env.local, analytics, deploy) ...

	// Get commit hash for deployment record
	commitHash, _ := repo.Head()

	// Tag this deployment
	repo.Tag(fmt.Sprintf("deploy-%d", time.Now().Unix()))

	deployment := &models.AppDeployment{
		// ... existing fields ...
		CommitHash: commitHash, // NEW: replaces ASTSnapshot
	}
	// ... rest unchanged ...
}
```

- [ ] **Step 2: Update GetPreviewBundle to read from git**

```go
func (c *Controller) GetPreviewBundle(project *models.AppProject) (map[string]interface{}, error) {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open project repo")
	}

	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}

	envVars := c.CollectAllEnvVars(project)

	return map[string]interface{}{
		"files":    fileMap,
		"env_vars": envVars,
	}, nil
}
```

- [ ] **Step 3: Update AppDeployment model**

In `internal/models/app_deployment.go`, replace `ASTSnapshot` with `CommitHash`:

```go
CommitHash   string         `bun:",notnull,default:''"` // git commit hash at deploy time
// Remove: ASTSnapshot  []byte `bun:",type:jsonb,default:'[]'"`
```

- [ ] **Step 4: Update file CRUD methods**

In `app_project_controller.go`, replace DB operations:

```go
func (c *Controller) GetAppGeneratedFiles(projectID string) (map[string]string, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, err
	}
	return repo.ReadAllFiles()
}

func (c *Controller) CreateAppFile(project *models.AppProject, filePath string, content string, fileType string) error {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return err
	}
	if repo.FileExists(filePath) {
		return fmt.Errorf("a file already exists at this path")
	}
	if err := repo.WriteFile(filePath, []byte(content)); err != nil {
		return err
	}
	_, err = repo.Commit(fmt.Sprintf("chat: add %s", filePath))
	return err
}

func (c *Controller) UpdateAppFile(project *models.AppProject, filePath string, content string) error {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return err
	}
	if !repo.FileExists(filePath) {
		return fmt.Errorf("file not found")
	}
	if err := repo.WriteFile(filePath, []byte(content)); err != nil {
		return err
	}
	_, err = repo.Commit(fmt.Sprintf("chat: update %s", filePath))
	return err
}

func (c *Controller) DeleteAppFile(project *models.AppProject, filePath string) error {
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return err
	}
	if !repo.FileExists(filePath) {
		return fmt.Errorf("file not found")
	}
	if err := repo.DeleteFile(filePath); err != nil {
		return err
	}
	_, err = repo.Commit(fmt.Sprintf("chat: delete %s", filePath))
	return err
}

func (c *Controller) ExportAppAsZIP(projectID string) (map[string]string, error) {
	repo, err := c.GitRepoService.Open(projectID)
	if err != nil {
		return nil, err
	}
	return repo.ReadAllFiles()
}
```

- [ ] **Step 5: Update view handlers that use changed signatures**

The init endpoint and export endpoint in `app_views.go` need to use the new return types. These methods now return `map[string]string` instead of `[]models.AppGeneratedFile`, so the view code that maps DB records to response structs needs updating.

For init endpoint — replace the file mapping loop:
```go
// Old: maps []models.AppGeneratedFile to []AppFileResponse
// New: maps map[string]string to []AppFileResponse
files, err := v.Controller.GetAppGeneratedFiles(project.ID.String())
if err == nil {
	for path, content := range files {
		if path == ".env.local" {
			continue
		}
		fileType := inferFileType(path) // helper to get extension
		resp.Files = append(resp.Files, responses.AppFileResponse{
			FilePath: path,
			Content:  content,
			FileType: fileType,
		})
	}
}
```

For export endpoint — update to use `map[string]string`:
```go
files, err := v.Controller.ExportAppAsZIP(project.ID.String())
// ... create ZIP from map[string]string ...
```

- [ ] **Step 6: Update CloneAppProjectAsMobile to use git clone**

In `app_project_controller.go`:
```go
// Replace file copy via DB with git clone
if _, err := c.GitRepoService.Clone(source.ID.String(), clone.ID.String()); err != nil {
	slog.Error("Failed to clone repo for mobile project", "error", err)
	return nil, fmt.Errorf("failed to copy files to mobile project")
}
```

Remove the `AST: source.AST` line from the clone struct.

- [ ] **Step 7: Update DeleteAppProject to clean up repo**

In `app_project_controller.go`, add before the DB delete:
```go
// Clean up git repo (best-effort)
if c.GitRepoService != nil {
	if err := c.GitRepoService.Delete(project.ID.String()); err != nil {
		slog.Warn("Failed to delete git repo during app deletion", "error", err, "project_id", project.ID)
	}
}
```

- [ ] **Step 8: Commit**

```bash
git add internal/controllers/app_deploy_controller.go internal/controllers/app_project_controller.go internal/models/app_deployment.go internal/views/app_views.go
git commit -m "feat: migrate deploy, preview, export, file CRUD to git repo"
```

---

## Chunk 4: Cleanup & Migration

### Task 7: Remove dead code and update responses

**Files:**
- Modify: `internal/models/app_project.go:27` (remove AST field)
- Remove: `internal/models/app_snapshot.go` (entire file — or keep SnapshotFile struct temporarily for migration)
- Remove: `internal/database/app_generated_files.go` (entire file)
- Remove: `internal/database/app_snapshots.go` (entire file)
- Modify: `internal/responses/app_project.go:26` (remove AST field)
- Modify: `cmd/main.go` (remove from createSchema)
- Modify: `internal/views/app_views.go` (snapshot endpoints)

- [ ] **Step 1: Remove AST field from AppProject model**

In `internal/models/app_project.go`, remove:
```go
AST             []byte         `bun:",type:jsonb,default:'[]'"`
```

- [ ] **Step 2: Remove AST and DataModels from response**

In `internal/responses/app_project.go`, remove:
```go
AST          []map[string]any     `json:"ast,omitempty"`
```

- [ ] **Step 3: Update AppSnapshotResponse for git-based history**

In `internal/responses/app_project.go`, replace `AppSnapshotResponse`:
```go
type AppSnapshotResponse struct {
	Hash        string    `json:"hash"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
```

- [ ] **Step 4: Update snapshot view handlers**

In `app_views.go`, update `GetAppSnapshots` to return commit history and `RestoreAppSnapshot` to accept commit hash instead of snapshot ID.

- [ ] **Step 5: Remove AppGeneratedFile and AppSnapshot from createSchema**

In `cmd/main.go`, remove these from the `createSchema` function:
```go
(*models.AppGeneratedFile)(nil),
(*models.AppSnapshot)(nil),
```

Also remove the dedup statement and unique index for `app_generated_files`.

- [ ] **Step 6: Delete dead files**

Remove:
- `internal/database/app_generated_files.go`
- `internal/database/app_snapshots.go`

Keep `internal/models/app_snapshot.go` temporarily (for migration script to read old data).

- [ ] **Step 7: Verify build compiles**

```bash
go build -o /dev/null cmd/main.go
```

Fix any remaining compilation errors from references to removed types.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "refactor: remove AppGeneratedFile, AppSnapshot, AST field"
```

---

### Task 8: Migration script

**Files:**
- Create: `cmd/migrate_to_git/main.go`

- [ ] **Step 1: Write migration script**

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

type LegacyFile struct {
	AppProjectID string `bun:"app_project_id"`
	FilePath     string `bun:"file_path"`
	Content      string `bun:"content"`
	FileType     string `bun:"file_type"`
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL required")
	}
	basePath := os.Getenv("REPO_BASE_PATH")
	if basePath == "" {
		basePath = "/data/repos"
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())
	defer db.Close()

	svc := gitrepo.NewService(basePath)

	// Get distinct project IDs
	var projectIDs []string
	err := db.NewSelect().
		TableExpr("app_generated_files").
		ColumnExpr("DISTINCT app_project_id::text").
		Scan(context.Background(), &projectIDs)
	if err != nil {
		log.Fatalf("Failed to get project IDs: %v", err)
	}

	log.Printf("Migrating %d projects...", len(projectIDs))

	for i, pid := range projectIDs {
		var files []LegacyFile
		err := db.NewSelect().
			TableExpr("app_generated_files").
			Where("app_project_id = ?", pid).
			Order("file_path ASC").
			Scan(context.Background(), &files)
		if err != nil {
			log.Printf("  [%d/%d] SKIP %s: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		repo, err := svc.Init(pid)
		if err != nil {
			log.Printf("  [%d/%d] SKIP %s: init failed: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		for _, f := range files {
			if err := repo.WriteFile(f.FilePath, []byte(f.Content)); err != nil {
				log.Printf("    WARN: failed to write %s: %v", f.FilePath, err)
			}
		}

		// Also migrate theme from app_projects
		var theme []byte
		err = db.NewSelect().
			TableExpr("app_projects").
			Column("theme").
			Where("id = ?", pid).
			Scan(context.Background(), &theme)
		if err == nil && len(theme) > 2 {
			repo.WriteTheme(theme)
		}

		if _, err := repo.Commit("init: migrated from database"); err != nil {
			log.Printf("  [%d/%d] SKIP %s: commit failed: %v", i+1, len(projectIDs), pid, err)
			continue
		}

		log.Printf("  [%d/%d] OK %s (%d files)", i+1, len(projectIDs), pid, len(files))
	}

	fmt.Println("Migration complete.")
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/migrate_to_git/main.go
git commit -m "feat: add migration script from DB files to git repos"
```

---

### Task 9: Update all remaining DB call sites in builder

**Files:**
- Modify: `internal/controllers/app_builder_controller.go` (multiple locations)
- Modify: `internal/controllers/app_deploy_controller.go` (ValidateAppFiles)
- Modify: `internal/controllers/app_dashboard_controller.go`
- Modify: `internal/controllers/app_env_var_controller.go`
- Modify: `internal/responses/app_project.go` (AppFileResponse)

These are DB call sites beyond `saveGeneratedFile`/`deleteGeneratedFile` that also need migration:

- [ ] **Step 1: Update `injectTemplateFile` (~line 960) to use repo**

This function uses `c.DB.UpsertAppGeneratedFile`. Replace with `repo.WriteFile`. It needs a `repo` parameter.

- [ ] **Step 2: Update package.json augmenter (~lines 822-834, 2022-2033)**

Uses `c.DB.GetAppGeneratedFileByPath` and `c.DB.UpdateAppGeneratedFile` to read/update `package.json`. Replace with `repo.ReadFile` / `repo.WriteFile`. Remove `FileID` from the SSE event emission.

- [ ] **Step 3: Update `.ts → .tsx` auto-fix (~lines 2929-2963)**

Uses `c.DB.GetAppGeneratedFileByPath` and `c.DB.UpdateAppGeneratedFile`. Replace with `repo.ReadFile` / `repo.WriteFile` / `repo.DeleteFile`.

- [ ] **Step 4: Update deterministic auto-fix path fixer (~lines 3036-3067)**

Uses `c.DB.GetAppGeneratedFileByPath`. Replace with `repo.FileExists` / `repo.ReadFile`.

- [ ] **Step 5: Update tool executor file operations (~lines 3706-3748)**

Uses `c.DB.GetAppGeneratedFileByPath`, `c.DB.CreateAppGeneratedFile`, `c.DB.UpdateAppGeneratedFile`. Replace with repo operations.

- [ ] **Step 6: Update `StreamBuildApp` initial file loading (~line 333)**

Currently loads `fileMap` from `c.DB.GetAppGeneratedFilesByProjectID`. Replace with `repo.ReadAllFiles()` to populate the in-memory `fileMap` for chat edits.

- [ ] **Step 7: Update `StreamOptimizeMobileApp` (~lines 5700-5882)**

This entire flow loads files from DB, creates snapshots, and rolls back on failure. It also calls `selectMobileOptimizeFiles` which takes `[]models.AppGeneratedFile`. Update the function signature to accept `map[string]string` and update the flow to use repo.

- [ ] **Step 8: Update `ValidateAppFiles` in app_deploy_controller.go**

Uses `c.DB.GetAppGeneratedFilesByProjectID`. Replace with repo read.

- [ ] **Step 9: Update `app_dashboard_controller.go` and `app_env_var_controller.go`**

Any DB file reads in these controllers need to use repo.

- [ ] **Step 10: Update `AppFileResponse` struct**

In `internal/responses/app_project.go`, remove DB-specific fields:
```go
type AppFileResponse struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	FileType  string `json:"file_type"`
}
```

Remove `ID`, `Platform`, `CreatedAt`, `UpdatedAt` fields — these came from the DB model and no longer apply.

- [ ] **Step 11: Update `CreateAppProject` in app_project_controller.go**

Remove `AST: []byte("[]")` from the project creation struct (line 43). Initialize a git repo instead:
```go
if _, err := c.GitRepoService.Init(project.ID.String()); err != nil {
	slog.Warn("Failed to init git repo for new project", "error", err)
}
```

- [ ] **Step 12: Search for any remaining references**

Run:
```bash
grep -rn "AppGeneratedFile\|AppSnapshot\|ASTSnapshot\|\.AST\b\|FileID\|GetAppGeneratedFile\|UpsertAppGeneratedFile\|BulkCreateAppGeneratedFiles\|DeleteAppGeneratedFilesByProjectPlatform\|DeleteOldAutoSnapshots\|GetAppSnapshotByID\|DeleteAppSnapshot\|GetAppSnapshotsByProjectID" internal/ cmd/
```

Fix each remaining reference.

- [ ] **Step 2: Verify clean build**

```bash
go build -o /dev/null cmd/main.go
```

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "fix: resolve all remaining references to removed types"
```

---

### Task 10: Verify end-to-end locally

- [ ] **Step 1: Build and run Docker**

```bash
docker compose build && docker compose up
```

- [ ] **Step 2: Test file generation flow**

Create a project → trigger initial generation → verify files appear in `/data/repos/{projectId}/` → verify git log shows commit.

- [ ] **Step 3: Test preview bundle**

Hit `GET /v2/apps/{appId}/preview-bundle` → verify files returned.

- [ ] **Step 4: Test undo**

Trigger chat edit → verify new commit → trigger undo → verify revert.

- [ ] **Step 5: Test deploy**

Trigger Vercel deploy → verify files uploaded and `CommitHash` saved on deployment.

- [ ] **Step 6: Final commit**

```bash
git add -A
git commit -m "chore: verified git-based file storage end-to-end"
```
