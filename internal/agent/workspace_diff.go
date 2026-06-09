package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/knid/wemob/internal/services/gitrepo"
)

// ReloadFileMapFromRepo loads the repository's tracked user files into
// a SafeFileMap, using Repo.ReadAllFiles filtering rules.
func ReloadFileMapFromRepo(repo *gitrepo.Repo) (*SafeFileMap, error) {
	files, err := repo.ReadAllFiles()
	if err != nil {
		return nil, err
	}
	return NewSafeFileMap(files), nil
}

// WorkspaceDiffPoller periodically re-runs workspace diff detection. It's a
// lightweight scaffold for future live auto-deploy hooks and editor sync
// updates without changing run-time semantics today.
type WorkspaceDiffPoller struct {
	repo     *gitrepo.Repo
	interval time.Duration
	base     string
	onChange func([]string)
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	running  bool
	lastHash map[string]string
}

const defaultWorkspacePollInterval = 1500 * time.Millisecond

// NewWorkspaceDiffPoller creates a new poller scaffold. The poller is inert
// until Start is called.
func NewWorkspaceDiffPoller(repo *gitrepo.Repo, baseCommit string, onChange func([]string)) *WorkspaceDiffPoller {
	return &WorkspaceDiffPoller{
		repo:     repo,
		interval: defaultWorkspacePollInterval,
		base:     baseCommit,
		onChange: onChange,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
		lastHash: make(map[string]string),
	}
}

// Start begins polling in a background goroutine.
func (p *WorkspaceDiffPoller) Start() {
	p.mu.Lock()
	if p == nil || p.running {
		p.mu.Unlock()
		return
	}
	p.running = true
	p.mu.Unlock()

	go func() {
		defer close(p.done)
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				paths, err := p.Poll()
				if err != nil {
					continue
				}
				if len(paths) > 0 && p.onChange != nil {
					p.onChange(paths)
				}
			case <-p.stop:
				return
			}
		}
	}()
}

// Poll checks for new workspace changes and returns deduplicated changed paths.
func (p *WorkspaceDiffPoller) Poll() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p == nil || p.repo == nil || p.base == "" {
		return nil, nil
	}

	paths, err := p.repo.WorkingTreeChangedPaths(p.base)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	paths = p.filterDuplicatePaths(paths)
	return paths, nil
}

// Stop requests graceful poller shutdown and waits for the background worker.
func (p *WorkspaceDiffPoller) Stop() {
	p.mu.Lock()
	if p == nil || !p.running {
		p.mu.Unlock()
		return
	}
	p.running = false
	p.mu.Unlock()

	close(p.stop)
	<-p.done
}

// WithContext is a temporary helper for compatibility with future polling
// callsites that need a cancel hook.
func (p *WorkspaceDiffPoller) WithContext(ctx context.Context) {
	if p == nil {
		return
	}
	go func() {
		<-ctx.Done()
		p.Stop()
	}()
}

func (p *WorkspaceDiffPoller) filterDuplicatePaths(paths []string) []string {
	if p == nil {
		return nil
	}
	next := make(map[string]string, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		hash, err := p.hashPath(path)
		if err != nil {
			continue
		}
		if hash == "" {
			continue
		}
		next[path] = hash
		if prev, ok := p.lastHash[path]; !ok || prev != hash {
			out = append(out, path)
		}
	}
	p.lastHash = next
	return out
}

func (p *WorkspaceDiffPoller) hashPath(path string) (string, error) {
	if p == nil || p.repo == nil {
		return "", fmt.Errorf("workspace diff poller is not initialized")
	}
	if isIgnoredWorkspaceDiffPath(path) {
		return "", nil
	}
	fullPath := filepath.Join(p.repo.Path, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "deleted", nil
		}
		return "", fmt.Errorf("read file for poller hash: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

func isIgnoredWorkspaceDiffPath(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	if normalized == "" {
		return true
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" {
			continue
		}
		switch part {
		case ".git", "_wemob", "node_modules", ".next", "dist", "build", "web-build", "ios", "android":
			return true
		}
		if strings.HasPrefix(part, ".env") {
			return true
		}
	}
	return false
}
