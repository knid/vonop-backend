package gitrepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	defaultBareRoot     = "/data/git/bare"
	defaultWorktreeRoot = "/data/worktrees"
	defaultLegacyRoot   = "/data/repos"

	initCommitMessage      = "chore: initialize Wemob project repository"
	migrationCommitMessage = "migration: persist legacy working tree before bare repo conversion"
)

// BareService manages the Deep Agents project source-of-truth layout:
// a canonical bare repo plus one isolated worktree per agent run.
type BareService struct {
	BareRoot     string
	WorktreeRoot string
	LegacyRoot   string
	locks        sync.Map
}

type BareRepo struct {
	ProjectID     string
	Path          string
	DefaultBranch string
	CurrentCommit string
}

type RunWorktree struct {
	ProjectID     string
	JobID         string
	Path          string
	Branch        string
	BaseCommit    string
	BareRepoPath  string
	DefaultBranch string
	service       *BareService
}

type MigrationResult struct {
	ProjectID          string
	LegacyRepoPath     string
	BareRepoPath       string
	CurrentCommit      string
	CreatedBareRepo    bool
	CreatedDirtyCommit bool
}

func NewBareService(bareRoot, worktreeRoot, legacyRoot string) *BareService {
	if bareRoot == "" {
		bareRoot = defaultBareRoot
	}
	if worktreeRoot == "" {
		worktreeRoot = defaultWorktreeRoot
	}
	if legacyRoot == "" {
		legacyRoot = defaultLegacyRoot
	}
	return &BareService{
		BareRoot:     bareRoot,
		WorktreeRoot: worktreeRoot,
		LegacyRoot:   legacyRoot,
	}
}

func (s *BareService) InitBareProject(ctx context.Context, projectID string) (*BareRepo, error) {
	if err := validatePathID("project_id", projectID); err != nil {
		return nil, err
	}
	unlock := s.lock(projectID)
	defer unlock()

	barePath := s.bareRepoPath(projectID)
	if isBareRepo(barePath) {
		head, err := s.gitOutput(ctx, barePath, "rev-parse", "refs/heads/main")
		if err != nil {
			return nil, fmt.Errorf("gitrepo: read existing bare HEAD: %w", err)
		}
		return &BareRepo{ProjectID: projectID, Path: barePath, DefaultBranch: "main", CurrentCommit: strings.TrimSpace(head)}, nil
	}

	if err := os.MkdirAll(filepath.Dir(barePath), 0o755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir bare root: %w", err)
	}
	if err := s.git(ctx, filepath.Dir(barePath), "init", "--bare", barePath); err != nil {
		return nil, fmt.Errorf("gitrepo: init bare: %w", err)
	}

	tmp, err := os.MkdirTemp("", "wemob-init-"+projectID+"-")
	if err != nil {
		return nil, fmt.Errorf("gitrepo: create init worktree: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := s.git(ctx, "", "clone", barePath, tmp); err != nil {
		return nil, fmt.Errorf("gitrepo: clone init worktree: %w", err)
	}
	if err := s.git(ctx, tmp, "checkout", "-b", "main"); err != nil {
		return nil, fmt.Errorf("gitrepo: checkout main: %w", err)
	}
	if err := writeInitialProjectFiles(tmp); err != nil {
		return nil, err
	}
	if err := commitAll(ctx, tmp, initCommitMessage); err != nil {
		return nil, fmt.Errorf("gitrepo: init commit: %w", err)
	}
	if err := s.git(ctx, tmp, "push", "origin", "main"); err != nil {
		return nil, fmt.Errorf("gitrepo: push initial main: %w", err)
	}

	head, err := s.gitOutput(ctx, barePath, "rev-parse", "refs/heads/main")
	if err != nil {
		return nil, fmt.Errorf("gitrepo: read initialized HEAD: %w", err)
	}
	return &BareRepo{ProjectID: projectID, Path: barePath, DefaultBranch: "main", CurrentCommit: strings.TrimSpace(head)}, nil
}

func (s *BareService) MigrateLegacyRepoToBare(ctx context.Context, projectID string) (*MigrationResult, error) {
	if err := validatePathID("project_id", projectID); err != nil {
		return nil, err
	}
	unlock := s.lock(projectID)
	defer unlock()

	legacyPath := filepath.Join(s.LegacyRoot, projectID)
	barePath := s.bareRepoPath(projectID)
	if _, err := os.Stat(filepath.Join(legacyPath, ".git")); err != nil {
		return nil, fmt.Errorf("gitrepo: legacy repo not found for project %s", projectID)
	}

	result := &MigrationResult{
		ProjectID:      projectID,
		LegacyRepoPath: legacyPath,
		BareRepoPath:   barePath,
	}

	dirty, err := gitDirty(ctx, legacyPath)
	if err != nil {
		return nil, fmt.Errorf("gitrepo: inspect legacy dirty state: %w", err)
	}
	if dirty {
		if err := commitAll(ctx, legacyPath, migrationCommitMessage); err != nil {
			return nil, fmt.Errorf("gitrepo: create migration commit: %w", err)
		}
		result.CreatedDirtyCommit = true
	}

	if !isBareRepo(barePath) {
		if err := os.MkdirAll(filepath.Dir(barePath), 0o755); err != nil {
			return nil, fmt.Errorf("gitrepo: mkdir bare root: %w", err)
		}
		if err := s.git(ctx, "", "clone", "--bare", legacyPath, barePath); err != nil {
			return nil, fmt.Errorf("gitrepo: clone bare: %w", err)
		}
		result.CreatedBareRepo = true
	}

	head, err := s.gitOutput(ctx, barePath, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("gitrepo: read migrated HEAD: %w", err)
	}
	result.CurrentCommit = strings.TrimSpace(head)
	return result, nil
}

func (s *BareService) CreateRunWorktree(ctx context.Context, projectID, jobID, baseCommit string) (*RunWorktree, error) {
	if err := validatePathID("project_id", projectID); err != nil {
		return nil, err
	}
	if err := validatePathID("job_id", jobID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseCommit) == "" {
		return nil, fmt.Errorf("gitrepo: base commit is required")
	}

	unlock := s.lock(projectID)
	defer unlock()

	barePath := s.bareRepoPath(projectID)
	if !isBareRepo(barePath) {
		return nil, fmt.Errorf("gitrepo: bare repo not found for project %s", projectID)
	}

	wtPath := filepath.Join(s.WorktreeRoot, projectID, jobID)
	if err := ensurePathUnder(s.WorktreeRoot, wtPath); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(wtPath); err != nil {
		return nil, fmt.Errorf("gitrepo: clean worktree path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir worktree parent: %w", err)
	}

	if err := s.git(ctx, barePath, "worktree", "add", "--detach", wtPath, baseCommit); err != nil {
		_ = os.RemoveAll(wtPath)
		return nil, fmt.Errorf("gitrepo: add run worktree: %w", err)
	}

	branch := "agent/" + jobID
	if err := s.git(ctx, wtPath, "checkout", "-B", branch); err != nil {
		_ = s.git(ctx, barePath, "worktree", "remove", "--force", wtPath)
		_ = os.RemoveAll(wtPath)
		return nil, fmt.Errorf("gitrepo: checkout run branch: %w", err)
	}

	return &RunWorktree{
		ProjectID:     projectID,
		JobID:         jobID,
		Path:          wtPath,
		Branch:        branch,
		BaseCommit:    baseCommit,
		BareRepoPath:  barePath,
		DefaultBranch: "main",
		service:       s,
	}, nil
}

func (wt *RunWorktree) Remove(ctx context.Context) error {
	if wt == nil || wt.Path == "" {
		return nil
	}
	if wt.service != nil && wt.BareRepoPath != "" {
		_ = wt.service.git(ctx, wt.BareRepoPath, "worktree", "remove", "--force", wt.Path)
	}
	err := os.RemoveAll(wt.Path)
	wt.Path = ""
	return err
}

func (s *BareService) bareRepoPath(projectID string) string {
	return filepath.Join(s.BareRoot, projectID+".git")
}

func (s *BareService) lock(projectID string) func() {
	v, _ := s.locks.LoadOrStore(projectID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *BareService) git(ctx context.Context, dir string, args ...string) error {
	_, err := s.gitOutput(ctx, dir, args...)
	return err
}

func (s *BareService) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err, string(out))
	}
	return string(out), nil
}

func validatePathID(name, value string) error {
	if value == "" {
		return fmt.Errorf("gitrepo: %s is required", name)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("gitrepo: invalid %s %q", name, value)
	}
	return nil
}

func isBareRepo(path string) bool {
	if fi, err := os.Stat(filepath.Join(path, "HEAD")); err != nil || fi.IsDir() {
		return false
	}
	if fi, err := os.Stat(filepath.Join(path, "objects")); err != nil || !fi.IsDir() {
		return false
	}
	return true
}

func writeInitialProjectFiles(path string) error {
	gitignore := strings.Join([]string{
		"node_modules/",
		".next/",
		".env*",
		"dist/",
		"build/",
		".expo/",
		"ios/",
		"android/",
		"web-build/",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(path, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		return fmt.Errorf("gitrepo: write .gitignore: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(path, metaDir), 0o755); err != nil {
		return fmt.Errorf("gitrepo: mkdir _wemob: %w", err)
	}
	if err := os.WriteFile(filepath.Join(path, metaDir, ".keep"), []byte{}, 0o644); err != nil {
		return fmt.Errorf("gitrepo: write _wemob/.keep: %w", err)
	}
	return nil
}

func commitAll(ctx context.Context, dir, message string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %s", err, string(out))
	}
	dirty, err := gitDirty(ctx, dir)
	if err != nil {
		return err
	}
	if !dirty {
		return nil
	}
	cmd = exec.CommandContext(ctx,
		"git",
		"-c", "user.name=Wemob Agent",
		"-c", "user.email=agent@wemob.local",
		"commit",
		"-m", message,
	)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %s: %s", err, string(out))
	}
	return nil
}

func gitDirty(ctx context.Context, dir string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

func ensurePathUnder(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("gitrepo: resolve root: %w", err)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("gitrepo: resolve path: %w", err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("gitrepo: check worktree path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("gitrepo: path %q escapes root %q", path, root)
	}
	return nil
}
