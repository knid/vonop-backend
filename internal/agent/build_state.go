package agent

import (
	"fmt"
	"path/filepath"
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
	NeedsRewrite    map[string]string
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
		NeedsRewrite:    map[string]string{},
	}
}

func (s *BuildState) RecordFileWrite(path, op, content string) string {
	if s == nil || path == "" {
		return ""
	}
	rewriteReason := suspiciousGeneratedFileReason(path, content)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Deleted, path)
	if rewriteReason != "" {
		s.NeedsRewrite[path] = rewriteReason
	} else {
		delete(s.NeedsRewrite, path)
	}
	if op == "create" {
		s.Created[path] = true
		delete(s.Updated, path)
		return rewriteReason
	}
	if !s.Created[path] {
		s.Updated[path] = true
	}
	return rewriteReason
}

func (s *BuildState) RecordFileDelete(path string) {
	if s == nil || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Created, path)
	delete(s.Updated, path)
	delete(s.NeedsRewrite, path)
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
	requirements := append([]RequirementStatus(nil), s.Requirements...)
	created := sortedKeys(s.Created)
	updated := sortedKeys(s.Updated)
	deleted := sortedKeys(s.Deleted)
	needsRewrite := sortedRewriteItems(s.NeedsRewrite)
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
		allWritten = filterRewritePaths(allWritten, needsRewrite)
		writePathList(&sb, "Do not recreate; edit only if more changes are needed", allWritten)
	}
	writeRewriteList(&sb, needsRewrite)
	if len(requirements) > 0 {
		sb.WriteString("Requirement checklist:\n")
		for _, req := range requirements {
			if strings.TrimSpace(req.Text) == "" {
				continue
			}
			fmt.Fprintf(&sb, "- [%s] %s\n", req.State, req.Text)
		}
	}
	sb.WriteString("\nUse this BUILD STATE plus the FILE TREE as the source of truth. Continue pending requirements and rewrite files listed under Needs rewrite; do not restart completed work.\n")
	return sb.String()
}

type rewriteItem struct {
	Path   string
	Reason string
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

func sortedRewriteItems(m map[string]string) []rewriteItem {
	out := make([]rewriteItem, 0, len(m))
	for path, reason := range m {
		if strings.TrimSpace(reason) == "" {
			continue
		}
		out = append(out, rewriteItem{Path: path, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

func filterRewritePaths(paths []string, rewriteItems []rewriteItem) []string {
	if len(paths) == 0 || len(rewriteItems) == 0 {
		return paths
	}
	needsRewrite := map[string]bool{}
	for _, item := range rewriteItems {
		needsRewrite[item.Path] = true
	}
	out := paths[:0]
	for _, path := range paths {
		if !needsRewrite[path] {
			out = append(out, path)
		}
	}
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

func writeRewriteList(sb *strings.Builder, items []rewriteItem) {
	if len(items) == 0 {
		return
	}
	sb.WriteString("Needs rewrite; too small or placeholder-like, not complete:\n")
	for _, item := range items {
		fmt.Fprintf(sb, "- %s (%s)\n", item.Path, item.Reason)
	}
}

func suspiciousGeneratedFileReason(path, content string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if !generatedSourceExts[ext] || strings.HasSuffix(strings.ToLower(path), ".d.ts") {
		return ""
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "empty source file"
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range placeholderMarkers {
		if strings.Contains(lower, marker) {
			return "placeholder-like source file"
		}
	}
	if len(trimmed) < suspiciousGeneratedSourceMinBytes {
		return fmt.Sprintf("very small source file (%d bytes)", len(trimmed))
	}
	return ""
}

const suspiciousGeneratedSourceMinBytes = 120

var generatedSourceExts = map[string]bool{
	".js":  true,
	".jsx": true,
	".ts":  true,
	".tsx": true,
}

var placeholderMarkers = []string{
	"todo",
	"stub",
	"placeholder",
	"coming soon",
	"not implemented",
	"return null",
	"lorem ipsum",
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

func renderRunPrefix(fileMap *SafeFileMap, state *BuildState) string {
	parts := []string{}
	if tree := BuildFileTreeUserPrefix(fileMap); strings.TrimSpace(tree) != "" {
		parts = append(parts, tree)
	}
	if rendered := state.Render(fileMap); strings.TrimSpace(rendered) != "" {
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n")
}
