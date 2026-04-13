package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// BuildSystemMessages returns the cached system messages for a run:
//
//  1. BasePrompt           (stable across all projects of this type)
//  2. Variant addendum     (stable across this project's runs, if applicable)
//  3. File tree            (stable within this run, reused across turns)
//
// anthropic.go marks up to 4 system blocks with cache_control, so each block
// becomes its own cache breakpoint for prompt caching.
func BuildSystemMessages(project *models.AppProject, fileMap *SafeFileMap) []utils.Message {
	base := BasePrompt
	addendum := buildAddendum(project)

	msgs := []utils.Message{{Role: "system", Content: base}}
	if addendum != "" {
		msgs = append(msgs, utils.Message{Role: "system", Content: addendum})
	}
	msgs = append(msgs, utils.Message{Role: "system", Content: buildFileTreeBlock(fileMap)})
	return msgs
}

// buildAddendum composes project-specific prompt blocks.
func buildAddendum(project *models.AppProject) string {
	var parts []string
	switch project.ProjectType {
	case "ecommerce":
		parts = append(parts, EcommerceAddendum)
	case "mobile_app":
		parts = append(parts, MobileAddendum)
	}
	if projectHasAuth(project) {
		parts = append(parts, AuthAddendum)
	}
	if projectHasDatabase(project) {
		parts = append(parts, DatabaseAddendum)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// buildFileTreeBlock produces a deterministic "FILE TREE" listing sorted
// by path so cache keys remain stable across runs with identical file sets.
func buildFileTreeBlock(fileMap *SafeFileMap) string {
	files := fileMap.Snapshot()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	sb.WriteString("FILE TREE\n")
	sb.WriteString("=========\n\n")
	if len(paths) == 0 {
		sb.WriteString("(empty — this is a new project, start by creating files)\n")
		return sb.String()
	}
	for _, p := range paths {
		fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(files[p]))
	}
	return sb.String()
}

// projectHasAuth reads the auth flag from project.Settings.
func projectHasAuth(project *models.AppProject) bool {
	if project == nil || project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["needs_auth"].(bool); ok {
		return v
	}
	return false
}
