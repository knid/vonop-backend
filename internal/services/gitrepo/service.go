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

// Init creates a new git repository for the given project.
func (s *Service) Init(projectID string) (*Repo, error) {
	repoPath := filepath.Join(s.BasePath, projectID)
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir: %w", err)
	}

	if err := s.git(repoPath, "init"); err != nil {
		return nil, fmt.Errorf("gitrepo: init: %w", err)
	}

	gitignore := "node_modules/\n.next/\n.env*\n"
	if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return nil, fmt.Errorf("gitrepo: write .gitignore: %w", err)
	}

	if err := os.MkdirAll(filepath.Join(repoPath, metaDir), 0755); err != nil {
		return nil, fmt.Errorf("gitrepo: mkdir _wemob: %w", err)
	}

	return &Repo{Path: repoPath, ProjectID: projectID, service: s}, nil
}

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

	unlockSrc := s.lock(sourceID)
	defer unlockSrc()

	target, err := s.Init(targetID)
	if err != nil {
		return nil, err
	}

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
			return nil
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
