package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/integrations"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)

// --- Import graph for smarter context selection ---

// tsImportRegex matches TypeScript/JavaScript import statements to extract the import path.
// Handles: import X from '...', import { X } from '...', import '...', import('...')
var tsImportRegex = regexp.MustCompile(`(?:import\s+(?:[\w{},\s*]+\s+from\s+)?|import\s*\()['"](@/[^'"]+|\.\.?/[^'"]+)['"]`)

// importGraph maps each file to the set of project files it imports (forward edges)
// and the set of project files that import it (reverse edges).
type importGraph struct {
	imports   map[string]map[string]bool // file → set of files it imports
	importers map[string]map[string]bool // file → set of files that import it
}

// buildImportGraph parses all TS/TSX/JS files in fileMap and constructs a
// bidirectional import graph. Runs in O(files × imports) with pure regex —
// no LLM calls. Only tracks project-internal imports (@/ and relative paths).
func buildImportGraph(fileMap map[string]string) *importGraph {
	g := &importGraph{
		imports:   make(map[string]map[string]bool, len(fileMap)),
		importers: make(map[string]map[string]bool, len(fileMap)),
	}

	// Build a lookup from path variants to canonical file paths.
	// e.g. "lib/utils" → "lib/utils.ts", "components/ui/button" → "components/ui/button.tsx"
	pathLookup := make(map[string]string, len(fileMap)*3)
	for p := range fileMap {
		pathLookup[p] = p
		// Strip extensions for bare import resolution
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			if strings.HasSuffix(p, ext) {
				pathLookup[strings.TrimSuffix(p, ext)] = p
			}
		}
		// Handle index file imports: "components/ui" → "components/ui/index.tsx"
		if strings.HasSuffix(p, "/index.tsx") || strings.HasSuffix(p, "/index.ts") {
			dir := p[:strings.LastIndex(p, "/")]
			pathLookup[dir] = p
		}
	}

	for filePath, content := range fileMap {
		if !strings.HasSuffix(filePath, ".ts") && !strings.HasSuffix(filePath, ".tsx") &&
			!strings.HasSuffix(filePath, ".js") && !strings.HasSuffix(filePath, ".jsx") {
			continue
		}

		matches := tsImportRegex.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			importPath := match[1]

			// Resolve @/ alias
			if strings.HasPrefix(importPath, "@/") {
				importPath = strings.TrimPrefix(importPath, "@/")
			} else if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
				// Resolve relative path from the importing file's directory
				importPath = resolveRelativeImport(filePath, importPath)
			} else {
				continue // node_modules or other non-project import
			}

			// Look up canonical path
			target, ok := pathLookup[importPath]
			if !ok {
				continue
			}
			if target == filePath {
				continue // self-import
			}

			// Forward edge: filePath imports target
			if g.imports[filePath] == nil {
				g.imports[filePath] = make(map[string]bool)
			}
			g.imports[filePath][target] = true

			// Reverse edge: target is imported by filePath
			if g.importers[target] == nil {
				g.importers[target] = make(map[string]bool)
			}
			g.importers[target][filePath] = true
		}
	}

	return g
}

// maxExpandFileSize is the maximum character size for a file added by graph
// expansion. Files larger than this (e.g. SDK wrappers, i18n dictionaries)
// are skipped during expansion — the LLM can still select them explicitly.
const maxExpandFileSize = 15000 // ~3,750 tokens

// expandWithDependencies takes a set of selected files and expands it with
// 1-hop imports (files they depend on) and 1-hop importers (files that depend
// on them). Caps expansion to prevent blowing up context.
// fileMap is used to skip oversized files during expansion (e.g. SDK wrappers).
// Pass nil to disable size filtering (backward-compatible).
func (g *importGraph) expandWithDependencies(selected map[string]bool, maxTotal int, fileMap map[string]string) map[string]bool {
	expanded := make(map[string]bool, len(selected)*2)
	for p := range selected {
		expanded[p] = true
	}

	// First pass: add direct imports of selected files (dependencies)
	for p := range selected {
		for dep := range g.imports[p] {
			if len(expanded) >= maxTotal {
				return expanded
			}
			// Skip oversized files during graph expansion — they bloat context
			// without being directly relevant. The LLM can still select them.
			if fileMap != nil && len(fileMap[dep]) > maxExpandFileSize {
				continue
			}
			expanded[dep] = true
		}
	}

	// Second pass: add direct importers of selected files (callers/impact zone)
	for p := range selected {
		for imp := range g.importers[p] {
			if len(expanded) >= maxTotal {
				return expanded
			}
			if fileMap != nil && len(fileMap[imp]) > maxExpandFileSize {
				continue
			}
			expanded[imp] = true
		}
	}

	return expanded
}

// resolveRelativeImport resolves a relative import path (./foo or ../foo)
// relative to the directory of the importing file.
func resolveRelativeImport(fromFile, relPath string) string {
	// Get directory of the importing file
	dir := fromFile
	if idx := strings.LastIndex(dir, "/"); idx >= 0 {
		dir = dir[:idx]
	} else {
		dir = ""
	}

	parts := strings.Split(relPath, "/")
	dirParts := strings.Split(dir, "/")
	if dir == "" {
		dirParts = nil
	}

	for _, part := range parts {
		switch part {
		case ".":
			// current dir, no change
		case "..":
			if len(dirParts) > 0 {
				dirParts = dirParts[:len(dirParts)-1]
			}
		default:
			dirParts = append(dirParts, part)
		}
	}

	return strings.Join(dirParts, "/")
}

// detectInstalledUIComponents scans the fileMap for files under components/ui/
// and extracts component names. Returns a sorted list like ["button", "card", "dialog"].
func detectInstalledUIComponents(fileMap map[string]string) []string {
	seen := make(map[string]bool)
	for path, content := range fileMap {
		if !strings.HasPrefix(path, "components/ui/") {
			continue
		}
		// Extract component name from path: "components/ui/button.tsx" → "button"
		name := strings.TrimPrefix(path, "components/ui/")
		for _, ext := range []string{".tsx", ".ts", ".jsx", ".js"} {
			name = strings.TrimSuffix(name, ext)
		}
		if name == "" || strings.Contains(name, "/") {
			continue // skip nested dirs
		}

		// Extract exported component names for a richer description
		exports := extractFileSummary(content)
		if exports != "" {
			seen[name+" ("+exports+")"] = true
		} else {
			seen[name] = true
		}
	}

	components := make([]string, 0, len(seen))
	for name := range seen {
		components = append(components, name)
	}
	sort.Strings(components)
	return components
}

// extractHTMLTitle extracts the <title> content from HTML.
func extractHTMLTitle(htmlContent string) string {
	lower := strings.ToLower(htmlContent)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	// Skip past the opening tag
	closeTag := strings.Index(lower[start:], ">")
	if closeTag < 0 {
		return ""
	}
	contentStart := start + closeTag + 1
	end := strings.Index(lower[contentStart:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(htmlContent[contentStart : contentStart+end])
}

// extractHTMLMeta extracts a <meta name="..." content="..."> value from HTML.
func extractHTMLMeta(htmlContent string, name string) string {
	lower := strings.ToLower(htmlContent)
	search := fmt.Sprintf(`name="%s"`, name)
	idx := strings.Index(lower, search)
	if idx < 0 {
		search = fmt.Sprintf(`name='%s'`, name)
		idx = strings.Index(lower, search)
	}
	if idx < 0 {
		return ""
	}
	// Find the enclosing <meta ...> tag
	tagStart := strings.LastIndex(lower[:idx], "<meta")
	if tagStart < 0 {
		return ""
	}
	tagEnd := strings.Index(lower[tagStart:], ">")
	if tagEnd < 0 {
		return ""
	}
	tag := htmlContent[tagStart : tagStart+tagEnd+1]
	// Extract content="..."
	contentIdx := strings.Index(strings.ToLower(tag), `content="`)
	if contentIdx < 0 {
		contentIdx = strings.Index(strings.ToLower(tag), `content='`)
	}
	if contentIdx < 0 {
		return ""
	}
	quote := tag[contentIdx+8] // " or '
	valStart := contentIdx + 9
	valEnd := strings.Index(tag[valStart:], string(quote))
	if valEnd < 0 {
		return ""
	}
	return tag[valStart : valStart+valEnd]
}

// truncate returns the first n characters of s, or s if shorter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- Helper: build modular system prompt ---

// projectHasDataModels returns true if the project has non-empty data_models.
func projectHasDataModels(project *models.AppProject) bool {
	if len(project.DataModels) == 0 {
		return false
	}
	models, _ := parseDataModels(project.DataModels)
	return len(models) > 0
}

// buildSystemPrompt assembles the system prompt for the tool-calling path,
// only including e-commerce and auth rules when the project actually uses those features.
func buildSystemPrompt(project *models.AppProject, mobileConfig *models.MobileAppConfig) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_TOOL_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_SYSTEM_PROMPT
		if projectHasDataModels(project) {
			prompt += constants.NEXTJS_BUILDER_DATABASE_RULES
		}
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
		prompt += constants.NEXTJS_BUILDER_MOBILE_CONFIG_TOOL_RULES
		prompt += constants.BuildBridgeSDKRules(constants.ALL_BRIDGE_METHODS)
	}

	return prompt
}

// buildChatSystemPrompt assembles the system prompt for the simplified file-block
// chat path. Uses CHAT_INSTRUCTIONS instead of TOOL_INSTRUCTIONS — no tool loop.
// NOTE: Does NOT include MOBILE_CONFIG_TOOL_RULES because the chat Phase 2 path
// uses StreamTextFromLLMWithMessages (no tools). Bridge config is handled by

func buildChatSystemPrompt(project *models.AppProject, mobileConfig *models.MobileAppConfig) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_CHAT_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_CHAT_SYSTEM_PROMPT
		if projectHasDataModels(project) {
			prompt += constants.NEXTJS_BUILDER_DATABASE_RULES
		}
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
		prompt += constants.BuildBridgeSDKRules(constants.ALL_BRIDGE_METHODS)
	}

	return prompt
}

// buildSingleShotSystemPrompt assembles the system prompt for single-shot initial
// generation using <file> block output format instead of tool calls.
// NOTE: Does NOT include MOBILE_CONFIG_TOOL_RULES because single-shot uses
// StreamTextFromLLMWithMessages (no tools). Bridge config is auto-detected

func buildSingleShotSystemPrompt(project *models.AppProject, mobileConfig *models.MobileAppConfig) string {
	var prompt string
	if project.ProjectType == "ecommerce" {
		prompt = constants.NEXTJS_ECOMMERCE_COMMON_RULES + constants.NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS
	} else {
		prompt = constants.NEXTJS_BUILDER_COMMON_RULES + constants.NEXTJS_BUILDER_SINGLESHOT_INSTRUCTIONS
		if projectHasDataModels(project) {
			prompt += constants.NEXTJS_BUILDER_DATABASE_RULES
		}
	}

	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			prompt += constants.NEXTJS_BUILDER_AUTH_RULES
		}
	}

	if project.ProjectType == "mobile_app" {
		prompt += constants.NEXTJS_BUILDER_MOBILE_RULES
		prompt += constants.BuildBridgeSDKRules(constants.ALL_BRIDGE_METHODS)
	}

	return prompt
}

// --- Helper: extract file summary for project context ---

// exportPattern matches common TypeScript/JavaScript export patterns.

var exportPattern = regexp.MustCompile(`(?m)^export\s+(?:default\s+)?(?:function|const|class|type|interface|enum)\s+(\w+)`)

// extractFileSummary produces a brief structural summary of a file's exports.

// buildAvailableImportsSection creates a prominent section listing all exports
// from lib/ files so the LLM knows exactly what can be imported.
// Placed at the end of project context for recency bias.
func buildAvailableImportsSection(fileMap map[string]string) string {
	type libExport struct {
		path    string
		exports []string
		note    string
	}

	var libs []libExport
	for fp, content := range fileMap {
		if !strings.HasPrefix(fp, "lib/") {
			continue
		}
		if !strings.HasSuffix(fp, ".ts") && !strings.HasSuffix(fp, ".tsx") {
			continue
		}

		// Extract named exports
		var exports []string
		matches := exportPattern.FindAllStringSubmatch(content, 30)
		for _, m := range matches {
			if len(m) > 1 {
				exports = append(exports, m[1])
			}
		}
		if len(exports) == 0 {
			continue
		}

		sort.Strings(exports)

		// Determine note
		importPath := strings.TrimSuffix(strings.TrimSuffix(fp, ".tsx"), ".ts")
		note := ""
		if strings.Contains(fp, "wemob-auth.ts") {
			note = "SERVER-ONLY — never import in 'use client' files"
		} else if strings.Contains(fp, "wemob-auth-client") {
			note = "CLIENT-ONLY — never import in API routes"
		} else if strings.Contains(fp, "wemob-store") {
			note = "E-commerce SDK"
		}

		libs = append(libs, libExport{path: "@/" + importPath, exports: exports, note: note})
	}

	if len(libs) == 0 {
		return ""
	}

	sort.Slice(libs, func(i, j int) bool { return libs[i].path < libs[j].path })

	var sb strings.Builder
	sb.WriteString("\n### ⚠️ Available Imports — ONLY use these exports from lib/ files\n")
	for _, lib := range libs {
		sb.WriteString(fmt.Sprintf("- `%s`: %s", lib.path, strings.Join(lib.exports, ", ")))
		if lib.note != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", lib.note))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Do NOT import functions or types not listed above from these modules.\n")
	return sb.String()
}

func extractFileSummary(content string) string {
	var names []string
	hasDefault := false

	matches := exportPattern.FindAllStringSubmatch(content, 20)
	for _, m := range matches {
		if len(m) > 1 {
			names = append(names, m[1])
		}
	}
	if strings.Contains(content, "export default") {
		hasDefault = true
	}
	if strings.HasPrefix(strings.TrimSpace(content), `"use client"`) || strings.HasPrefix(strings.TrimSpace(content), `'use client'`) {
		if len(names) > 0 {
			return fmt.Sprintf("\"use client\"; exports: %s", strings.Join(names, ", "))
		}
		if hasDefault {
			return "\"use client\"; default export"
		}
		return "\"use client\""
	}
	if len(names) > 0 {
		return fmt.Sprintf("exports: %s", strings.Join(names, ", "))
	}
	if hasDefault {
		return "default export"
	}
	return ""
}

// --- Helper: build project context (system block 2) ---

// contextFileSelection holds the result of file selection, preserving which
// files were directly chosen by the LLM (primary) vs added by graph expansion
// (secondary). buildProjectContextForChat uses this to prioritize primary files
// when the character budget is tight.
type contextFileSelection struct {
	// All returns the full set of selected files (primary + secondary).
	primary   map[string]bool // LLM-selected + forced files
	secondary map[string]bool // added by import graph expansion
}

// All returns the combined set of primary and secondary files.
func (s contextFileSelection) All() map[string]bool {
	all := make(map[string]bool, len(s.primary)+len(s.secondary))
	for p := range s.primary {
		all[p] = true
	}
	for p := range s.secondary {
		all[p] = true
	}
	return all
}

// selectContextFiles uses a fast LLM call to pick the most relevant files for
// the user's current message. Returns a contextFileSelection that distinguishes
// LLM-selected files (primary) from graph-expanded files (secondary) so the
// context builder can prioritize primary files within the character budget.
// On error or when there are few files, returns all paths as primary (safe fallback).

func (c *Controller) selectContextFiles(ctx context.Context, provider *utils.LLMProvider, fileMap map[string]string, userMessage string, recentMessages []models.AppConversationMessage) contextFileSelection {
	maxAutoInline := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxAutoInline, remoteconfig.Global)

	if len(fileMap) <= maxAutoInline {
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return contextFileSelection{primary: all}
	}

	// Force-include files explicitly mentioned in error messages or user text.
	// This ensures build errors like "./project/app/products/page.tsx (167:11)"
	// always get the actual file content in the LLM context.
	forcedFiles := extractFilePathsFromMessage(userMessage, fileMap)

	// Build file list with sizes
	paths := make([]string, 0, len(fileMap))
	for p := range fileMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var fileList strings.Builder
	for _, p := range paths {
		fileList.WriteString(fmt.Sprintf("- %s (%d chars)\n", p, len(fileMap[p])))
	}

	// Include last 3 assistant messages for context
	var recentContext string
	start := len(recentMessages) - 6
	if start < 0 {
		start = 0
	}
	for _, msg := range recentMessages[start:] {
		if msg.Role == "assistant" && len(msg.Content) > 0 {
			summary := msg.Content
			if len(summary) > 200 {
				summary = summary[:200] + "..."
			}
			recentContext += fmt.Sprintf("Assistant: %s\n", summary)
		}
	}

	prompt := fmt.Sprintf(`You are a software engineer reviewing a project's files to determine which ones are relevant to the user's current request.

Available files:
%s
%sUser's message:
%s

Select the files that are most relevant to this task. You can select up to 10 files. Always include:
- Files directly mentioned in the message or error traces
- Files that import/depend on mentioned files
- Layout/page files if UI changes are requested
- The main layout and page files (app/layout.tsx, app/page.tsx) when in doubt

Respond with ONLY a list of file paths, one per line. No explanation, no markdown, no numbering.`, fileList.String(), recentContext, userMessage)

	fastModel := provider.FastModel()
	result, err := utils.GetTextFromLLM(ctx, fastModel, "", prompt, provider)
	if err != nil {
		slog.Warn("selectContextFiles: LLM call failed, falling back to all files", "error", err)
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return contextFileSelection{primary: all}
	}

	selected := make(map[string]bool)
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if _, exists := fileMap[line]; exists {
			selected[line] = true
		}
	}

	// Fallback: if selection is empty or too small, inline all
	if len(selected) < 2 {
		slog.Warn("selectContextFiles: too few files selected, falling back to all", "selected", len(selected))
		all := make(map[string]bool, len(fileMap))
		for p := range fileMap {
			all[p] = true
		}
		return contextFileSelection{primary: all}
	}

	// Always include files extracted from error messages / user text
	for _, fp := range forcedFiles {
		if !selected[fp] {
			selected[fp] = true
			slog.Info("selectContextFiles: force-included file from message", "path", fp)
		}
	}

	// Snapshot primary selection before graph expansion
	primary := make(map[string]bool, len(selected))
	for p := range selected {
		primary[p] = true
	}

	// Expand selection with 1-hop import dependencies and reverse importers.
	// This catches files the LLM missed that are structurally connected to the
	// selected files — e.g. a lib/config.ts imported by the selected component,
	// or a page.tsx that imports the selected component (impact zone).
	graph := buildImportGraph(fileMap)
	maxExpandedFiles := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxExpandedFiles, remoteconfig.Global)
	expanded := graph.expandWithDependencies(selected, maxExpandedFiles, fileMap)

	// Secondary files = expanded minus primary
	secondary := make(map[string]bool)
	for p := range expanded {
		if !primary[p] {
			secondary[p] = true
		}
	}

	slog.Info("selectContextFiles: selected files for context",
		"total", len(fileMap), "primary", len(primary),
		"secondary", len(secondary))
	return contextFileSelection{primary: primary, secondary: secondary}
}

// extractFilePathsFromMessage scans a user message for file paths that exist in
// the project. Handles patterns like "./project/app/page.tsx (167:11)" from Next.js
// build errors, bare paths like "app/page.tsx", and common error trace formats.

var filePathInMessageRe = regexp.MustCompile(`(?:\.?/project/)?([a-zA-Z0-9_\-./]+\.(?:tsx?|jsx?|css|json|mjs))`)


func extractFilePathsFromMessage(message string, fileMap map[string]string) []string {
	matches := filePathInMessageRe.FindAllStringSubmatch(message, -1)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		candidate := m[1]
		// Strip leading "./" if present
		candidate = strings.TrimPrefix(candidate, "./")
		if _, exists := fileMap[candidate]; exists && !seen[candidate] {
			seen[candidate] = true
			paths = append(paths, candidate)
		}
	}
	return paths
}

// buildProjectContextForChat builds project context with selected file contents
// inlined and remaining files listed as paths only.
// chatMessagesToLLM converts stored conversation messages to LLM messages.
// currentImageDataURIs contains base64 data URIs for the current (latest) user
// message's images. Stored ImageURLs are S3 URLs and cannot be sent directly
// to the LLM — only the current turn's data URIs are attached as vision content.
// Older user messages with images get a text placeholder instead.

func chatMessagesToLLM(msgs []models.AppConversationMessage, provider *utils.LLMProvider, currentImageDataURIs []string) []utils.Message {
	supportsVision := provider.SupportsVision()

	// The last message is the current user message (just appended before this call).
	// Only attach actual base64 data URIs on the last user message.
	lastIdx := len(msgs) - 1

	out := make([]utils.Message, 0, len(msgs))
	for i, msg := range msgs {
		m := utils.Message{Role: msg.Role, Content: msg.Content}
		if msg.Role == "user" && len(msg.ImageURLs) > 0 {
			if i == lastIdx && supportsVision && len(currentImageDataURIs) > 0 {
				m.Images = currentImageDataURIs
			} else {
				m.Content += fmt.Sprintf("\n\n[User previously attached %d image(s)]", len(msg.ImageURLs))
			}
		}
		out = append(out, m)
	}
	return out
}


func (c *Controller) getProjectLogoURL(project *models.AppProject) string {
	if project.LogoS3Key == "" {
		return ""
	}
	// Return a permanent URL that never expires. The /v2/apps/{id}/logo endpoint
	// generates a fresh pre-signed S3 URL on each request and redirects to it.
	return fmt.Sprintf("%s/v2/apps/%s/logo", constants.WEMOB_API_BASE_URL, project.ID)
}


func (c *Controller) buildProjectContextForChat(project *models.AppProject, fileMap map[string]string, selection contextFileSelection) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("App: %s (slug: %s)\n", project.Name, project.Slug))
	sb.WriteString(fmt.Sprintf("Project type: %s\n", project.ProjectType))

	// Inject persisted project memory (design preferences, decisions, etc.)
	if memCtx := c.buildProjectMemoryContext(project.ID.String()); memCtx != "" {
		sb.WriteString(memCtx)
	}

	// Include logo URL if available
	if logoURL := c.getProjectLogoURL(project); logoURL != "" {
		sb.WriteString(fmt.Sprintf("\nApp Logo URL (use this as the site logo in the header/navbar via next/image and as the favicon in metadata — do not use text for the brand name, always use this logo image):\n%s\n", logoURL))
	}

	// List installed UI components so the LLM reuses them instead of building from scratch
	if components := detectInstalledUIComponents(fileMap); len(components) > 0 {
		sb.WriteString("\n### Installed UI Components (USE THESE — do not rebuild from scratch)\n")
		sb.WriteString("Import from @/components/ui/<name>. Available:\n")
		for _, comp := range components {
			sb.WriteString(fmt.Sprintf("- %s\n", comp))
		}
		sb.WriteString("Always prefer these components over building custom equivalents.\n")
	}

	if len(fileMap) > 0 {
		paths := make([]string, 0, len(fileMap))
		for p := range fileMap {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		// List all files as paths (so LLM knows project structure)
		sb.WriteString("\nAll project files:\n")
		for _, fp := range paths {
			sb.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(fileMap[fp])))
		}

		// Inline selected files with a character budget to avoid exceeding context limits.
		// ~4 chars ≈ 1 token; 150k chars ≈ 37.5k tokens, leaving room for system prompt,
		// conversation history, and tool context within a 200k token window.
		// Primary files (LLM-selected) are inlined first; secondary files (graph-expanded)
		// use the remaining budget. This prevents large dependency files from crowding out
		// the files the LLM actually chose as relevant.
		maxFileChars := c.RemoteConfig.GetInt(remoteconfig.AIBuilderMaxFileChars, remoteconfig.Global)
		usedChars := 0
		sb.WriteString("\nFile contents (relevant to current task):\n")

		inlineFile := func(fp string) {
			content := fileMap[fp]
			if strings.Contains(content, fallbackSentinel) || strings.Contains(content, "Generation failed") {
				sb.WriteString(fmt.Sprintf("\n--- %s [BROKEN — needs regeneration] ---\n", fp))
			} else if usedChars+len(content) > maxFileChars {
				remaining := maxFileChars - usedChars
				if remaining > 500 {
					sb.WriteString(fmt.Sprintf("\n--- %s [TRUNCATED — %d/%d chars] ---\n%s\n", fp, remaining, len(content), content[:remaining]))
					usedChars += remaining
				} else {
					sb.WriteString(fmt.Sprintf("\n--- %s [OMITTED — %d chars, budget exhausted] ---\n", fp, len(content)))
				}
			} else {
				sb.WriteString(fmt.Sprintf("\n--- %s ---\n%s\n", fp, content))
				usedChars += len(content)
			}
		}

		// Pass 1: inline primary files (LLM-selected) — these get budget priority
		for _, fp := range paths {
			if !selection.primary[fp] {
				continue
			}
			inlineFile(fp)
		}
		// Pass 2: inline secondary files (graph-expanded) with remaining budget
		for _, fp := range paths {
			if !selection.secondary[fp] {
				continue
			}
			inlineFile(fp)
		}
	}

	// User-uploaded assets
	chatAssets, _ := c.DB.GetAppAssetsByProjectID(project.ID.String())
	if len(chatAssets) > 0 {
		sb.WriteString("\nUser-uploaded assets (use these URLs directly in code):\n")
		for _, a := range chatAssets {
			sb.WriteString(fmt.Sprintf("- %s (%s) → %s\n", a.FileName, a.ContentType, a.PublicURL))
		}
	} else {
		sb.WriteString("\nNo user-uploaded assets. If the user's request requires custom files (3D models, fonts, images, videos, etc.), tell them to upload the files first using the Assets button (paperclip icon) in the chat toolbar. Do NOT use placeholder URLs or try to generate these files.\n")
	}

	// Available Imports section — placed at the end for recency bias
	sb.WriteString(buildAvailableImportsSection(fileMap))

	return sb.String()
}


func (c *Controller) buildProjectContext(project *models.AppProject, fileMap map[string]string, isInitialGeneration bool) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("App: %s (slug: %s)\n", project.Name, project.Slug))
	sb.WriteString(fmt.Sprintf("Project type: %s\n", project.ProjectType))

	// List installed UI components so the LLM reuses them
	if !isInitialGeneration {
		if components := detectInstalledUIComponents(fileMap); len(components) > 0 {
			sb.WriteString("\n### Installed UI Components (USE THESE — do not rebuild from scratch)\n")
			sb.WriteString("Import from @/components/ui/<name>. Available:\n")
			for _, comp := range components {
				sb.WriteString(fmt.Sprintf("- %s\n", comp))
			}
			sb.WriteString("Always prefer these components over building custom equivalents.\n")
		}
	}

	// File tree with structural summaries
	if len(fileMap) > 0 {
		sb.WriteString("\nCurrent project files:\n")
		paths := make([]string, 0, len(fileMap))
		for p := range fileMap {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		brokenCount := 0
		for _, fp := range paths {
			content := fileMap[fp]
			if strings.Contains(content, fallbackSentinel) || strings.Contains(content, "Generation failed") {
				sb.WriteString(fmt.Sprintf("- %s (%d chars) [BROKEN]\n", fp, len(content)))
				brokenCount++
			} else if len(content) <= 500 {
				// Inline small files so the LLM doesn't need read_file
				sb.WriteString(fmt.Sprintf("- %s (%d chars) [inline]:\n```\n%s\n```\n", fp, len(content), content))
			} else {
				// Extract exports summary for larger files
				summary := extractFileSummary(content)
				if summary != "" {
					sb.WriteString(fmt.Sprintf("- %s (%d chars) — %s\n", fp, len(content), summary))
				} else {
					sb.WriteString(fmt.Sprintf("- %s (%d chars)\n", fp, len(content)))
				}
			}
		}
		if brokenCount > 0 {
			sb.WriteString(fmt.Sprintf("\nWARNING: %d file(s) marked [BROKEN] need full regeneration.\n", brokenCount))
		}
		sb.WriteString("\nUse read_file to view any file's content before modifying it.")
	} else if isInitialGeneration {
		sb.WriteString("\nNo files yet — you are building this app from scratch.\n")
		sb.WriteString("User prompt: " + utils.SanitizePromptContent(project.Prompt) + "\n")
	}

	// Inject file plan from structure phase if available (for initial gen regardless of fileMap state,
	// since template files may already be injected before the LLM runs).
	if isInitialGeneration {
		if raw, ok := project.Settings["file_plan"]; ok {
			b, marshalErr := json.Marshal(raw)
			if marshalErr != nil {
				slog.Warn("failed to marshal file_plan setting", "error", marshalErr, "project_id", project.ID)
			}
			var filePlan []struct {
				Path        string `json:"path"`
				Type        string `json:"type"`
				Description string `json:"description"`
			}
			if marshalErr == nil && json.Unmarshal(b, &filePlan) == nil && len(filePlan) > 0 {
				sb.WriteString("\nPlanned file structure (create these files):\n")
				for _, f := range filePlan {
					sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.Path, f.Type, f.Description))
				}
				sb.WriteString("\nFollow this plan. You may add extra files if needed, but create all planned files.\n")
			}
		}
	}

	// Database schema context
	if len(project.DataModels) > 0 {
		dataModels, _ := parseDataModels(project.DataModels)
		if len(dataModels) > 0 {
			sb.WriteString("\nDatabase schema (Drizzle ORM via lib/schema.ts):\n")
			for _, dm := range dataModels {
				sb.WriteString(fmt.Sprintf("  Table: %s (", dm.Name))
				var cols []string
				for _, col := range dm.Columns {
					cols = append(cols, fmt.Sprintf("%s:%s", col.Name, col.Type))
				}
				sb.WriteString(strings.Join(cols, ", "))
				sb.WriteString(")\n")
			}
		}
	}

	// Integration context
	appIntegrations, _ := c.DB.GetAppIntegrationsByProjectID(project.ID.String())
	hasPaymentGateway := false
	if len(appIntegrations) > 0 {
		sb.WriteString("\nConnected integrations:\n")
		for _, integ := range appIntegrations {
			config, err := integrations.GetProviderConfig(integ.Provider)
			if err != nil {
				sb.WriteString(fmt.Sprintf("- %s (status: %s)\n", integ.Provider, integ.Status))
				continue
			}
			sb.WriteString(fmt.Sprintf("- %s (category: %s, status: %s)\n", integ.Provider, config.Category, integ.Status))
			var envNames []string
			for _, envName := range config.EnvVarMapping {
				envNames = append(envNames, envName)
			}
			if len(envNames) > 0 {
				sb.WriteString(fmt.Sprintf("  Env vars: %s\n", strings.Join(envNames, ", ")))
			}
			if config.Category == "payment" {
				hasPaymentGateway = true
			}
		}
	}

	// E-commerce store context
	if project.ProjectType == "ecommerce" {
		store, err := c.DB.GetStoreByAppProjectID(project.ID.String())
		if err == nil && store != nil {
			sb.WriteString(fmt.Sprintf("\nE-commerce store: %s (store_id: %s)\n", store.Status, store.ID.String()))
			sb.WriteString(fmt.Sprintf("Storefront API: %s/v2/storefront/%s\n", constants.WEMOB_API_BASE_URL, store.ID.String()))
			sb.WriteString(fmt.Sprintf("Currency: %s, Locale: %s, Tax rate: %.2f%%\n", store.Currency, store.Locale, store.TaxRate))

			products, _ := c.DB.GetProductsByStoreID(store.ID.String())
			sb.WriteString(fmt.Sprintf("Products: %d\n", len(products)))

			categories, _ := c.DB.GetCategoriesByStoreID(store.ID.String())
			if len(categories) > 0 {
				var catNames []string
				for _, cat := range categories {
					catNames = append(catNames, cat.Name)
				}
				sb.WriteString(fmt.Sprintf("Categories: %s\n", strings.Join(catNames, ", ")))
			}

			var paymentProviders []string
			for _, integ := range appIntegrations {
				if integ.Status == "connected" {
					for _, cfg := range constants.IntegrationProviders {
						if cfg.Provider == integ.Provider && cfg.Category == "payment" {
							paymentProviders = append(paymentProviders, cfg.Name)
						}
					}
				}
			}
			if len(paymentProviders) > 0 {
				sb.WriteString(fmt.Sprintf("Payment providers: %s\n", strings.Join(paymentProviders, ", ")))
			}

			sb.WriteString("SDK: lib/wemob-store.ts provides CartProvider, useCart, useProducts, etc. Wrap layout.tsx with <CartProvider>.\n")
		}
		if !hasPaymentGateway {
			sb.WriteString("\nWARNING: No payment gateway connected. Tell user to connect Stripe or Iyzico.\n")
		}
	}

	// Auth context
	if settings := project.Settings; settings != nil {
		if needsAuth, ok := settings["needs_auth"].(bool); ok && needsAuth {
			sb.WriteString("\nAuthentication: enabled\n")
			sb.WriteString("- Server: import { register, login, logout, getSession, requireAuth, getCurrentUser } from '@/lib/wemob-auth'\n")
			sb.WriteString("- Client: import { AuthProvider, useAuth } from '@/lib/wemob-auth-client'\n")
			sb.WriteString("- lib/wemob-auth.ts, lib/wemob-auth-client.tsx, and app/api/auth/[...action]/route.ts must exist.\n")
		}
	}

	// Custom env vars
	customEnvVars, _ := c.DB.GetAppEnvVarsByProjectID(project.ID.String())
	if len(customEnvVars) > 0 {
		sb.WriteString("\nCustom environment variables:\n")
		for _, ev := range customEnvVars {
			key := ev.Key
			if ev.IsPublic && !strings.HasPrefix(key, "NEXT_PUBLIC_") {
				key = "NEXT_PUBLIC_" + key
			}
			sb.WriteString(fmt.Sprintf("- %s\n", key))
		}
	}

	// User-uploaded assets
	assets, _ := c.DB.GetAppAssetsByProjectID(project.ID.String())
	if len(assets) > 0 {
		sb.WriteString("\nUser-uploaded assets (use these URLs directly in code):\n")
		for _, a := range assets {
			sb.WriteString(fmt.Sprintf("- %s (%s) → %s\n", a.FileName, a.ContentType, a.PublicURL))
		}
	} else {
		sb.WriteString("\nNo user-uploaded assets. If the user's request requires custom files (3D models, fonts, images, videos, etc.), tell them to upload the files first using the Assets button (paperclip icon) in the chat toolbar. Do NOT use placeholder URLs or try to generate these files.\n")
	}

	// Available Imports section — placed at the end for recency bias
	sb.WriteString(buildAvailableImportsSection(fileMap))

	return sb.String()
}

// --- Helper: run deterministic auto-fixes ---

