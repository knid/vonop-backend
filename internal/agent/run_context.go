package agent

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// SafeFileMap is a mutex-protected map[path]content used as the single source
// of truth for the project's files during a run. Tool executors mutate it in
// memory; only the success-path shutdown writes it back to the git working
// tree and commits.
type SafeFileMap struct {
	mu    sync.RWMutex
	files map[string]string
}

func NewSafeFileMap(seed map[string]string) *SafeFileMap {
	m := &SafeFileMap{files: make(map[string]string, len(seed))}
	for k, v := range seed {
		m.files[k] = v
	}
	return m
}

func (m *SafeFileMap) Get(path string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.files[path]
	return v, ok
}

func (m *SafeFileMap) Set(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = content
}

func (m *SafeFileMap) Delete(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[path]; !ok {
		return false
	}
	delete(m.files, path)
	return true
}

func (m *SafeFileMap) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.files)
}

// Snapshot returns a shallow copy for read-only use (string values are
// immutable in Go so a shallow copy is safe for rollback callers).
func (m *SafeFileMap) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.files))
	for k, v := range m.files {
		out[k] = v
	}
	return out
}

// Match returns paths matching the glob. Uses `filepath.Match` semantics
// plus a `**/` doublestar rewrite that recurses into subdirectories.
func (m *SafeFileMap) Match(glob string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if glob == "" {
		paths := make([]string, 0, len(m.files))
		for p := range m.files {
			paths = append(paths, p)
		}
		return paths
	}
	var out []string
	for p := range m.files {
		if matchGlob(glob, p) {
			out = append(out, p)
		}
	}
	return out
}

// matchGlob handles `filepath.Match` patterns plus a crude `**/` rewrite:
// `components/**/*.tsx` matches `components/ui/Button.tsx` and
// `components/a/b/c/Foo.tsx`, but NOT `app/page.tsx`.
//
// Rewrites: split on the FIRST `**/`, require the prefix to match as a
// directory prefix of the path, then try the tail against every progressive
// filepath.Base suffix of the remainder.
func matchGlob(glob, p string) bool {
	if !strings.Contains(glob, "**/") {
		ok, err := filepath.Match(glob, p)
		return err == nil && ok
	}
	parts := strings.SplitN(glob, "**/", 2)
	prefix := parts[0]
	tail := parts[1]
	if prefix != "" && !strings.HasPrefix(p, prefix) {
		return false
	}
	rest := strings.TrimPrefix(p, prefix)
	// Match `tail` against every suffix of `rest` at directory boundaries.
	// Simplest: walk rest from the start, checking each remaining suffix.
	remaining := rest
	for {
		if ok, err := filepath.Match(tail, remaining); err == nil && ok {
			return true
		}
		idx := strings.Index(remaining, "/")
		if idx < 0 {
			return false
		}
		remaining = remaining[idx+1:]
	}
}

// RunContext is threaded through every tool executor and holds the ambient
// state for a single agent run. It is constructed at the top of Run() and
// never mutated outside the orchestrator.
type RunContext struct {
	RunID      uuid.UUID
	Profile    *AgentProfile
	Skills     []SkillFile
	ReadOnly   bool
	Project    *models.AppProject
	User       *models.User
	FileMap    *SafeFileMap
	BuildState *BuildState
	Todos      *TodoList
	Emitter    EventEmitter
	Tracker    *utils.TokenTracker
	Deps       *RunDependencies
}
