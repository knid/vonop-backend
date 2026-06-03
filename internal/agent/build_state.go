package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type BuildPhase string

const (
	BuildPhaseFoundation BuildPhase = "foundation"
	BuildPhaseMainUI     BuildPhase = "main_ui"
	BuildPhaseImages     BuildPhase = "images"
	BuildPhaseWiring     BuildPhase = "wiring"
	BuildPhaseValidation BuildPhase = "validation"
	BuildPhaseFinal      BuildPhase = "final"
)

type RequirementState string

const (
	RequirementPending  RequirementState = "pending"
	RequirementComplete RequirementState = "complete"
)

type RequirementStatus struct {
	Text  string
	State RequirementState
}

type BuildState struct {
	mu sync.RWMutex

	OriginalRequest string
	Phase           BuildPhase
	Requirements    []RequirementStatus
	Created         map[string]bool
	Updated         map[string]bool
	Deleted         map[string]bool
}

func NewBuildState(originalRequest string, isFirstGen bool) *BuildState {
	phase := BuildPhaseMainUI
	if isFirstGen {
		phase = BuildPhaseFoundation
	}
	return &BuildState{
		OriginalRequest: originalRequest,
		Phase:           phase,
		Requirements:    extractRequirementHints(originalRequest),
		Created:         map[string]bool{},
		Updated:         map[string]bool{},
		Deleted:         map[string]bool{},
	}
}

func (s *BuildState) RecordFileWrite(path, op string) {
	if s == nil || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Deleted, path)
	if op == "create" {
		s.Created[path] = true
		delete(s.Updated, path)
		return
	}
	if !s.Created[path] {
		s.Updated[path] = true
	}
}

func (s *BuildState) RecordFileDelete(path string) {
	if s == nil || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Created, path)
	delete(s.Updated, path)
	s.Deleted[path] = true
}

func (s *BuildState) SetPhase(phase BuildPhase) {
	if s == nil || phase == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Phase = phase
}

func (s *BuildState) MarkRequirementComplete(match string) {
	if s == nil || strings.TrimSpace(match) == "" {
		return
	}
	needle := strings.ToLower(match)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Requirements {
		if strings.Contains(strings.ToLower(s.Requirements[i].Text), needle) {
			s.Requirements[i].State = RequirementComplete
		}
	}
}

func (s *BuildState) Render(fileMap *SafeFileMap) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	phase := s.Phase
	created := sortedKeys(s.Created)
	updated := sortedKeys(s.Updated)
	deleted := sortedKeys(s.Deleted)
	s.mu.RUnlock()

	if phase == BuildPhaseFoundation && hasAppShell(fileMap) {
		phase = BuildPhaseMainUI
	}

	var sb strings.Builder
	sb.WriteString("BUILD STATE\n")
	sb.WriteString("===========\n\n")
	fmt.Fprintf(&sb, "Phase: %s\n", phase)
	writePathList(&sb, "Created this run", created)
	writePathList(&sb, "Updated this run", updated)
	writePathList(&sb, "Deleted this run", deleted)
	if len(created)+len(updated) > 0 {
		allWritten := append(append([]string{}, created...), updated...)
		sort.Strings(allWritten)
		writePathList(&sb, "Do not recreate; edit only if more changes are needed", allWritten)
	}
	sb.WriteString("\nUse this BUILD STATE plus the FILE TREE as the source of truth for files. Use TODO STATE as the source of truth for task progress; do not restart completed work.\n")
	return sb.String()
}

func hasAppShell(fileMap *SafeFileMap) bool {
	if fileMap == nil {
		return false
	}
	_, hasLayout := fileMap.Get("app/layout.tsx")
	_, hasPage := fileMap.Get("app/page.tsx")
	return hasLayout && hasPage
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func writePathList(sb *strings.Builder, label string, paths []string) {
	if len(paths) == 0 {
		return
	}
	sb.WriteString(label)
	sb.WriteString(":\n")
	for _, p := range paths {
		fmt.Fprintf(sb, "- %s\n", p)
	}
}

func extractRequirementHints(prompt string) []RequirementStatus {
	lower := strings.ToLower(prompt)
	candidates := []struct {
		needle string
		label  string
	}{
		{"admin", "admin/owner dashboard and management flow"},
		{"captain", "captain-only assigned-yacht interface"},
		{"role", "role-specific access boundaries"},
		{"dashboard", "dashboard-oriented primary screen"},
		{"form", "requested forms with validation and success states"},
		{"history", "chronological log/history view"},
		{"alert", "smart alert/visual indicator behavior"},
		{"database", "relational data model or local state simulation"},
		{"auth", "authentication/session flow"},
		{"mobile-first", "mobile-first responsive experience"},
		{"settings", "settings or management surface"},
	}

	seen := map[string]bool{}
	var out []RequirementStatus
	for _, c := range candidates {
		if strings.Contains(lower, c.needle) && !seen[c.label] {
			out = append(out, RequirementStatus{Text: c.label, State: RequirementPending})
			seen[c.label] = true
		}
	}
	if len(out) == 0 {
		out = append(out, RequirementStatus{Text: "main user-requested experience", State: RequirementPending})
	}
	return out
}

func renderRunPrefix(fileMap *SafeFileMap, state *BuildState, todos *TodoList) string {
	parts := []string{}
	if tree := BuildFileTreeUserPrefix(fileMap); strings.TrimSpace(tree) != "" {
		parts = append(parts, tree)
	}
	if rendered := state.Render(fileMap); strings.TrimSpace(rendered) != "" {
		parts = append(parts, rendered)
	}
	if rendered := todos.Render(); strings.TrimSpace(rendered) != "" {
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n")
}
