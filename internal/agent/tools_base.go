package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

const maxReadBytes = 200 * 1024 // 200 KB

// protectedPaths cannot be written, edited, or deleted by the agent. Keys
// MUST be lowercase — isProtected normalises the input before lookup.
//
// package.json is intentionally NOT on this list: the agent owns its own
// imports, so it must be able to add new dependencies when it introduces a
// new third-party package. Lock files stay protected so the agent can't
// invent fake hashes.
var protectedPaths = map[string]bool{
	// Lock files — npm/yarn/pnpm/bun manage these, not the LLM
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"bun.lockb":         true,
	// Build/config files where a wrong edit silently breaks the build pipeline
	// (Next.js / TS / PostCSS / Drizzle drivers) without a clear error path.
	// tailwind.config.* is intentionally NOT here — the agent legitimately
	// extends the theme (fonts, custom colors). middleware.ts is also NOT
	// here even though the template ships one; per-app CORS/header tweaks
	// are valid agent work.
	"tsconfig.json":      true,
	"next.config.ts":     true,
	"next.config.js":     true,
	"next.config.mjs":    true,
	"next.config.cjs":    true,
	"postcss.config.ts":  true,
	"postcss.config.js":  true,
	"postcss.config.mjs": true,
	"drizzle.config.ts":  true,
	"drizzle.config.js":  true,
	// Env files — all variants
	".env":             true,
	".env.local":       true,
	".env.production":  true,
	".env.development": true,
	".env.example":     true,
}

// protectedPrefixes blocks any path under one of these directories.
// All prefixes must end with "/" and be lowercase.
var protectedPrefixes = []string{
	".git/",
	"node_modules/",
	".next/",
	".turbo/",
	"dist/",
	"build/",
	".vercel/",
}

// sanitizePath wraps utils.SanitizeFilePath in the (string, error) shape the
// tool executors expect internally.
func sanitizePath(raw string) (string, error) {
	cleaned, ok := utils.SanitizeFilePath(raw)
	if !ok {
		return "", fmt.Errorf("invalid or suspicious path: %q", raw)
	}
	return cleaned, nil
}

// isProtected returns true if the path refers to a file the agent must not
// modify (protected config files at the project root or anything under a
// protected prefix).
func isProtected(path string) bool {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(lower))
	if protectedPaths[lower] || protectedPaths[base] {
		return true
	}
	// Protected path prefixes — block anything under .git/, node_modules/, .next/, dist/, build/
	for _, pfx := range protectedPrefixes {
		if strings.HasPrefix(lower, pfx) {
			return true
		}
	}
	return false
}

// isProtectedRead returns true if the path refers to a file the agent must
// not read (secrets, git internals). Less strict than isProtected — the
// agent can READ package.json et al., it just can't write them.
func isProtectedRead(path string) bool {
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(lower))
	envNames := map[string]bool{
		".env":             true,
		".env.local":       true,
		".env.production":  true,
		".env.development": true,
		".env.example":     true,
	}
	if envNames[base] {
		return true
	}
	if strings.HasPrefix(lower, ".git/") {
		return true
	}
	return false
}

// Allowed top-level subtrees for mobile_app_native projects. The AI agent
// writes Expo source into mobile/ and Next.js API routes into backend/;
// any write at the repo root or under another prefix is almost certainly
// a hallucination.
const (
	mobileNativePathPrefixMobile  = "mobile/"
	mobileNativePathPrefixBackend = "backend/"
	splitWebPathPrefixFrontend    = "frontend/"
	splitWebPathPrefixBackend     = "backend/"
)

// enforceProjectPathScope rejects writes outside the canonical subtrees
// for the project type. Currently only mobile_app_native is constrained;
// app/ecommerce projects can write anywhere.
//
// Returns "" if the path is allowed; an error message string (suitable for
// returning as the tool's first return value with isError=true) if not.
func enforceProjectPathScope(rc *RunContext, path string) string {
	if rc == nil || rc.Project == nil {
		return ""
	}
	if rc.Project.ProjectType == models.ProjectTypeApp &&
		(rc.Project.UsesSplitWebRuntime() || rc.Project.FrontendRuntime == models.FrontendRuntimeNone) {
		if path == designFilePath {
			return ""
		}
		if rc.Project.UsesSplitWebRuntime() && strings.HasPrefix(path, splitWebPathPrefixFrontend) {
			return ""
		}
		if strings.HasPrefix(path, splitWebPathPrefixBackend) {
			return ""
		}
		return fmt.Sprintf(
			"path %q is outside the allowed split-web subtrees. Write Vite UI under %q (e.g. frontend/src/App.tsx) and Next.js API routes under %q (e.g. backend/app/api/chat/route.ts).",
			path, splitWebPathPrefixFrontend, splitWebPathPrefixBackend,
		)
	}
	if rc.Project.ProjectType != models.ProjectTypeMobileAppNative {
		return ""
	}
	if strings.HasPrefix(path, mobileNativePathPrefixMobile) ||
		strings.HasPrefix(path, mobileNativePathPrefixBackend) {
		return ""
	}
	return fmt.Sprintf(
		"path %q is outside the allowed mobile_app_native subtrees. Write Expo source under %q (e.g. mobile/app/index.tsx) and Next.js API routes under %q (e.g. backend/app/api/users/route.ts).",
		path, mobileNativePathPrefixMobile, mobileNativePathPrefixBackend,
	)
}

// ---------- read_file ----------

type ReadFileTool struct{}

func (ReadFileTool) Name() string        { return "read_file" }
func (ReadFileTool) Description() string { return "Read a file from the project by path." }
func (ReadFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The project-relative path to the file.",
			},
		},
		"required": []string{"path"},
	}
}

func (ReadFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if isProtectedRead(path) {
		return fmt.Sprintf("reading %s is not allowed (contains secrets or system files)", path), true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	if len(content) > maxReadBytes {
		rc.MarkFileRead(path, content)
		return fmt.Sprintf("<truncated: file is %d bytes, max %d>\n\n%s",
			len(content), maxReadBytes, content[:maxReadBytes]), false, nil
	}
	rc.MarkFileRead(path, content)
	return content, false, nil
}

// ---------- read_files ----------

const readFilesMaxPaths = 20

type ReadFilesTool struct{}

func (ReadFilesTool) Name() string { return "read_files" }
func (ReadFilesTool) Description() string {
	return "Read one or more files in a single call. Pass an array of project-relative paths (even a single path). Returns each file's content delimited by '===== <path> =====' headers, or an error line for paths that fail. Always use this instead of individual file reads — same semantics, one round-trip."
}
func (ReadFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"description": "Project-relative file paths to read in one batch.",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"maxItems":    readFilesMaxPaths,
			},
		},
		"required": []string{"paths"},
	}
}

func (ReadFilesTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if len(args.Paths) == 0 {
		return "paths must contain at least one entry", true, nil
	}
	if len(args.Paths) > readFilesMaxPaths {
		return fmt.Sprintf("too many paths: %d (max %d) — split into multiple calls", len(args.Paths), readFilesMaxPaths), true, nil
	}

	var sb strings.Builder
	hadError := false
	for i, raw := range args.Paths {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		path, err := sanitizePath(raw)
		if err != nil {
			hadError = true
			fmt.Fprintf(&sb, "===== %s =====\nERROR: %s", raw, err.Error())
			continue
		}
		fmt.Fprintf(&sb, "===== %s =====\n", path)
		if isProtectedRead(path) {
			hadError = true
			fmt.Fprintf(&sb, "ERROR: reading %s is not allowed (contains secrets or system files)", path)
			continue
		}
		content, ok := rc.FileMap.Get(path)
		if !ok {
			hadError = true
			sb.WriteString("ERROR: file not found")
			continue
		}
		if len(content) > maxReadBytes {
			rc.MarkFileRead(path, content)
			fmt.Fprintf(&sb, "<truncated: file is %d bytes, max %d>\n\n%s", len(content), maxReadBytes, content[:maxReadBytes])
			continue
		}
		rc.MarkFileRead(path, content)
		sb.WriteString(content)
	}
	return sb.String(), hadError, nil
}

// ---------- read_file_range ----------

const readFileRangeMaxLines = 250

type ReadFileRangeTool struct{}

func (ReadFileRangeTool) Name() string { return "read_file_range" }
func (ReadFileRangeTool) Description() string {
	return "Read a bounded line range from one project file. Use this after grep and before exact-string edits to resync a specific region without loading a huge file."
}
func (ReadFileRangeTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":       map[string]any{"type": "string", "description": "The project-relative path to the file."},
			"start_line": map[string]any{"type": "integer", "minimum": 1, "description": "1-based first line to read."},
			"end_line":   map[string]any{"type": "integer", "minimum": 1, "description": fmt.Sprintf("1-based final line to read. Maximum range is %d lines.", readFileRangeMaxLines)},
		},
		"required": []string{"path", "start_line", "end_line"},
	}
}

func (ReadFileRangeTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if isProtectedRead(path) {
		return fmt.Sprintf("reading %s is not allowed (contains secrets or system files)", path), true, nil
	}
	if args.StartLine < 1 || args.EndLine < args.StartLine {
		return "start_line and end_line must be valid 1-based line numbers", true, nil
	}
	if args.EndLine-args.StartLine+1 > readFileRangeMaxLines {
		return fmt.Sprintf("line range too large: %d lines (max %d)", args.EndLine-args.StartLine+1, readFileRangeMaxLines), true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	rc.MarkFileRead(path, content)
	lines := strings.Split(content, "\n")
	if args.StartLine > len(lines) {
		return fmt.Sprintf("start_line %d is beyond end of file (%d lines)", args.StartLine, len(lines)), true, nil
	}
	end := args.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "===== %s lines %d-%d =====\n", path, args.StartLine, end)
	for i := args.StartLine; i <= end; i++ {
		fmt.Fprintf(&sb, "%4d: %s\n", i, lines[i-1])
	}
	return strings.TrimRight(sb.String(), "\n"), false, nil
}

// ---------- list_files ----------

type ListFilesTool struct{}

func (ListFilesTool) Name() string { return "list_files" }
func (ListFilesTool) Description() string {
	return "List files in the project, optionally filtered by glob."
}
func (ListFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional glob pattern, e.g. 'app/**/*.tsx'. Leave empty to list everything.",
			},
		},
	}
}

func (ListFilesTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Glob string `json:"glob"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	matches := rc.FileMap.Match(args.Glob)
	// Drop platform-managed files (.env.local, Dockerfile, next.config.mjs) so
	// the agent doesn't list or suggest editing them. They remain readable via
	// read_files if the agent explicitly requests one.
	var visible []string
	for _, p := range matches {
		if utils.IsHiddenEditorFile(p) {
			continue
		}
		visible = append(visible, p)
	}
	if len(visible) == 0 {
		return "no files match", false, nil
	}
	var sb strings.Builder
	for _, p := range visible {
		content, _ := rc.FileMap.Get(p)
		fmt.Fprintf(&sb, "%s (%d bytes)\n", p, len(content))
	}
	return sb.String(), false, nil
}

// ---------- delete_file ----------

type DeleteFileTool struct{}

func (DeleteFileTool) Name() string        { return "delete_file" }
func (DeleteFileTool) Description() string { return "Delete a file from the project." }
func (DeleteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":   map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		},
		"required": []string{"path"},
	}
}

func (DeleteFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if isProtected(path) {
		return fmt.Sprintf("protected file, cannot delete: %s", path), true, nil
	}
	if msg := enforceProjectPathScope(rc, path); msg != "" {
		return msg, true, nil
	}
	if !rc.FileMap.Delete(path) {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	if rc.BuildState != nil {
		rc.BuildState.RecordFileDelete(path)
	}
	rc.Emitter.Emit(EventFileDeleted, FileDeletedEvent{RunID: rc.RunID, Path: path})
	return fmt.Sprintf("deleted %s", path), false, nil
}

// ---------- shared full-file writes ----------

const (
	maxWriteBytes                 = 200 * 1024 // 200 KB
	largeExistingFileRewriteBytes = 30 * 1024
)

type fileWritePlan struct {
	Path       string
	Content    string
	Operation  string
	BatchID    string
	BatchIndex int
	BatchTotal int
}

func planFileWrite(rc *RunContext, rawPath, content string, overwrite bool, reason, batchID string, batchIndex, batchTotal int) (*fileWritePlan, string) {
	path, err := sanitizePath(rawPath)
	if err != nil {
		return nil, err.Error()
	}
	if isProtected(path) {
		return nil, fmt.Sprintf("protected file, cannot overwrite: %s", path)
	}
	if msg := enforceProjectPathScope(rc, path); msg != "" {
		return nil, msg
	}
	if len(content) > maxWriteBytes {
		return nil, fmt.Sprintf("content too large: %d bytes (max %d)", len(content), maxWriteBytes)
	}
	if isDesignFilePath(path) {
		if err := validateDesignBriefContent(content); err != nil {
			return nil, err.Error()
		}
	}

	op := "update"
	existing, exists := rc.FileMap.Get(path)
	if !exists {
		op = "create"
	} else if !overwrite {
		return nil, fmt.Sprintf("file already exists: %s — use edit_file/multi_edit for targeted changes, or call write_files again with overwrite=true and a reason if a full rewrite is intentional", path)
	} else if strings.TrimSpace(reason) == "" {
		return nil, fmt.Sprintf("overwrite reason is required for existing file: %s", path)
	} else if len(existing) > largeExistingFileRewriteBytes {
		return nil, fmt.Sprintf("existing file is too large for full rewrite: %s is %d bytes (limit %d). Use multi_edit for exact-string replacements, including multi-file batches", path, len(existing), largeExistingFileRewriteBytes)
	}

	return &fileWritePlan{
		Path:       path,
		Content:    content,
		Operation:  op,
		BatchID:    batchID,
		BatchIndex: batchIndex,
		BatchTotal: batchTotal,
	}, ""
}

func commitFileWrite(rc *RunContext, plan fileWritePlan) string {
	rc.FileMap.Set(plan.Path, plan.Content)
	if rc.BuildState != nil {
		rc.BuildState.RecordFileWrite(plan.Path, plan.Operation)
	}
	if plan.BatchTotal > 1 {
		rc.Emitter.Emit(EventToolProgress, ToolProgressEvent{
			RunID:      rc.RunID,
			ToolName:   "write_files",
			BatchID:    plan.BatchID,
			BatchIndex: plan.BatchIndex,
			BatchTotal: plan.BatchTotal,
			Path:       plan.Path,
			Operation:  plan.Operation,
			Bytes:      len(plan.Content),
			Message:    fmt.Sprintf("%s %s (%d/%d)", plan.Operation, plan.Path, plan.BatchIndex, plan.BatchTotal),
		})
	}
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:      rc.RunID,
		Path:       plan.Path,
		Operation:  plan.Operation,
		Bytes:      len(plan.Content),
		Content:    plan.Content,
		BatchID:    plan.BatchID,
		BatchIndex: plan.BatchIndex,
		BatchTotal: plan.BatchTotal,
	})
	return fmt.Sprintf("%sd %s (%d bytes)", plan.Operation, plan.Path, len(plan.Content))
}

func writeFileContent(rc *RunContext, rawPath, content string, overwrite bool, reason, batchID string, batchIndex, batchTotal int) (string, bool, error) {
	plan, msg := planFileWrite(rc, rawPath, content, overwrite, reason, batchID, batchIndex, batchTotal)
	if msg != "" {
		return msg, true, nil
	}
	return commitFileWrite(rc, *plan), false, nil
}

// ---------- write_files ----------

const writeFilesMaxFiles = 50

type WriteFilesTool struct{}

func (WriteFilesTool) Name() string { return "write_files" }
func (WriteFilesTool) Description() string {
	return "Create or fully overwrite one or more files in one call. Existing files require overwrite=true and a reason. Do not use for existing files larger than 30KB; use multi_edit for exact-string replacements, including multi-file batches."
}
func (WriteFilesTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"files": map[string]any{
				"type":        "array",
				"description": "Files to write in order.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":        map[string]any{"type": "string"},
						"content":     map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"overwrite":   map[string]any{"type": "boolean", "description": "Required to replace an existing small file. Large existing files cannot be overwritten with write_files."},
						"reason":      map[string]any{"type": "string", "description": "Required when overwrite=true. For existing files larger than 30KB, use multi_edit instead."},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		"required": []string{"files"},
	}
}

func (WriteFilesTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Files []struct {
			Path        string `json:"path"`
			Content     string `json:"content"`
			Description string `json:"description"`
			Overwrite   bool   `json:"overwrite"`
			Reason      string `json:"reason"`
		} `json:"files"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if len(args.Files) == 0 {
		return "files must contain at least one entry", true, nil
	}
	if len(args.Files) > writeFilesMaxFiles {
		return fmt.Sprintf("too many files: %d (max %d) — split into multiple calls", len(args.Files), writeFilesMaxFiles), true, nil
	}

	batchID := ""
	if len(args.Files) > 1 {
		batchID = uuid.NewString()
	}
	seenPaths := make(map[string]int, len(args.Files))
	plans := make([]fileWritePlan, 0, len(args.Files))
	var validationErrors []string
	for i, f := range args.Files {
		plan, msg := planFileWrite(rc, f.Path, f.Content, f.Overwrite, f.Reason, batchID, i+1, len(args.Files))
		if msg != "" {
			label := strings.TrimSpace(f.Path)
			if label == "" {
				label = fmt.Sprintf("files[%d]", i)
			}
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %s", label, msg))
			continue
		}
		if firstIndex, ok := seenPaths[plan.Path]; ok {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: duplicate path also appears at files[%d]", plan.Path, firstIndex))
			continue
		}
		seenPaths[plan.Path] = i
		plans = append(plans, *plan)
	}
	if len(validationErrors) > 0 {
		return "write_files failed before writing any files:\n" + strings.Join(validationErrors, "\n"), true, nil
	}

	created := 0
	updated := 0
	var lines []string
	for _, plan := range plans {
		if plan.Operation == "create" {
			created++
		} else {
			updated++
		}
		lines = append(lines, commitFileWrite(rc, plan))
	}
	summary := fmt.Sprintf("wrote %d %s: created %d, updated %d", len(plans), pluralize("file", len(plans)), created, updated)
	if len(lines) == 0 {
		return summary, false, nil
	}
	return summary + "\n" + strings.Join(lines, "\n"), false, nil
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}

// ---------- create_design ----------

type CreateDesignTool struct{}

func (CreateDesignTool) Name() string { return "create_design" }
func (CreateDesignTool) Description() string {
	return "Create DESIGN.md from structured design decisions. Use this instead of write_files for DESIGN.md on first generation."
}
func (CreateDesignTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"register": map[string]any{
				"type":        "string",
				"enum":        []string{"brand", "product"},
				"description": "Whether the design is the product impression (brand) or serves a workflow (product).",
			},
			"design_direction": map[string]any{"type": "string", "description": "Concrete visual point of view and rationale."},
			"color_system":     map[string]any{"type": "string", "description": "Named tokens, hex values, roles, and palette strategy."},
			"typography":       map[string]any{"type": "string", "description": "Font choices, scale, weight, and usage rules."},
			"layout":           map[string]any{"type": "string", "description": "Spacing, grid, page rhythm, and responsive rules."},
			"components":       map[string]any{"type": "string", "description": "Key reusable components and state expectations."},
			"imagery":          map[string]any{"type": "string", "description": "Image style, subject matter, crops, and what to avoid."},
			"accessibility":    map[string]any{"type": "string", "description": "Contrast, focus, motion, and text wrapping constraints."},
			"forbidden_patterns": map[string]any{
				"type":        "string",
				"description": "Specific generic or off-direction choices this app must avoid.",
			},
			"overwrite": map[string]any{"type": "boolean", "description": "Set true only when intentionally replacing an existing DESIGN.md."},
			"reason":    map[string]any{"type": "string", "description": "Required when overwrite=true."},
		},
		"required": []string{"register", "design_direction", "color_system", "typography", "layout", "components", "imagery", "accessibility"},
	}
}

func (CreateDesignTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Register          string `json:"register"`
		DesignDirection   string `json:"design_direction"`
		ColorSystem       string `json:"color_system"`
		Typography        string `json:"typography"`
		Layout            string `json:"layout"`
		Components        string `json:"components"`
		Imagery           string `json:"imagery"`
		Accessibility     string `json:"accessibility"`
		ForbiddenPatterns string `json:"forbidden_patterns"`
		Overwrite         bool   `json:"overwrite"`
		Reason            string `json:"reason"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	content := renderDesignBrief(args.Register, args.DesignDirection, args.ColorSystem, args.Typography, args.Layout, args.Components, args.Imagery, args.Accessibility, args.ForbiddenPatterns)
	return writeFileContent(rc, designFilePath, content, args.Overwrite, args.Reason, "", 0, 0)
}

func renderDesignBrief(register, direction, colors, typography, layout, components, imagery, accessibility, forbidden string) string {
	var sb strings.Builder
	sb.WriteString("# DESIGN.md\n\n")
	writeDesignSection(&sb, "Register", register)
	writeDesignSection(&sb, "Design Direction", direction)
	writeDesignSection(&sb, "Color System", colors)
	writeDesignSection(&sb, "Typography", typography)
	writeDesignSection(&sb, "Layout", layout)
	writeDesignSection(&sb, "Components", components)
	writeDesignSection(&sb, "Imagery", imagery)
	writeDesignSection(&sb, "Accessibility", accessibility)
	if strings.TrimSpace(forbidden) != "" {
		writeDesignSection(&sb, "Forbidden Patterns", forbidden)
	}
	return sb.String()
}

func writeDesignSection(sb *strings.Builder, title, body string) {
	sb.WriteString("## ")
	sb.WriteString(title)
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(body))
	sb.WriteString("\n\n")
}

// ---------- edit_file ----------

type EditFileTool struct{}

func (EditFileTool) Name() string { return "edit_file" }
func (EditFileTool) Description() string {
	return "Surgically replace exactly one occurrence of old_string with new_string in exactly one existing file. For 2+ edits or edits across files, use multi_edit. Fails if old_string does not appear or appears more than once — in that case, add surrounding context to old_string and retry."
}
func (EditFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"old_string":  map[string]any{"type": "string", "minLength": 1},
			"new_string":  map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (EditFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path        string `json:"path"`
		OldString   string `json:"old_string"`
		NewString   string `json:"new_string"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if isProtected(path) {
		return fmt.Sprintf("protected file, cannot edit: %s", path), true, nil
	}
	if msg := enforceProjectPathScope(rc, path); msg != "" {
		return msg, true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s — use write_files to create it", path), true, nil
	}
	if msg := rc.requireCurrentFileRead(path, content); msg != "" {
		return structuredEditError(path, "stale_read", msg, "read_file_range", ""), true, nil
	}
	if args.OldString == "" {
		return structuredEditError(path, "old_string_empty", "old_string must not be empty", "read_file_range", ""), true, nil
	}

	count := strings.Count(content, args.OldString)
	switch count {
	case 0:
		return structuredEditError(path, "old_string_not_found", fmt.Sprintf("old_string not found in %s — the file content may have changed since your last view", path), "read_file_range", args.OldString), true, nil
	case 1:
		// ok
	default:
		return structuredEditError(path, "old_string_ambiguous", fmt.Sprintf("old_string appears %d times in %s — add more surrounding context to disambiguate", count, path), "read_file_range", args.OldString), true, nil
	}
	newContent := strings.Replace(content, args.OldString, args.NewString, 1)
	if len(newContent) > maxWriteBytes {
		return structuredEditError(path, "result_too_large", fmt.Sprintf("resulting file too large: %d bytes (max %d)", len(newContent), maxWriteBytes), "read_file_range", ""), true, nil
	}
	rc.FileMap.Set(path, newContent)
	rc.MarkFileRead(path, newContent)
	if rc.BuildState != nil {
		rc.BuildState.RecordFileWrite(path, "update")
	}
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: "update",
		Bytes:     len(newContent),
		Content:   newContent,
	})
	summary := strings.TrimSpace(args.Description)
	if summary == "" {
		summary = fmt.Sprintf("edited %s (1 replacement, %d bytes)", path, len(newContent))
	}
	return structuredEditSuccess(path, summary, content, newContent, 1, changedLineNumbers(content, args.OldString)), false, nil
}

// ---------- grep ----------

const (
	grepDefaultContextLines = 25
	grepDefaultMaxMatches   = 200
	grepHardMaxMatches      = 200
	grepDefaultMaxBytes     = 16 * 1024
	grepHardMaxBytes        = 64 * 1024
	grepMaxLineLen          = 400
)

type GrepTool struct{}

func (GrepTool) Name() string { return "grep" }
func (GrepTool) Description() string {
	return "Search file contents across the project. Defaults to regex search with 25 surrounding context lines, but supports CLI-grep style controls: context_lines=0 for path:line matches, literal=true for fixed-string search, mode=files/count for compact discovery, and max_matches/max_bytes to bound noisy results. Use this BEFORE read_files / edit_file when you need to locate a string or symbol."
}
func (GrepTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression (RE2 syntax). Use \\Q...\\E or escape special chars for literal search.",
				"minLength":   1,
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional glob to limit files scanned, e.g. 'app/**/*.tsx'. Empty = all files.",
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": "If true, match regardless of case.",
			},
			"literal": map[string]any{
				"type":        "boolean",
				"description": "If true, treat pattern as a fixed string instead of a regex, similar to grep -F.",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "Output mode. 'context' returns matching lines with context, 'matches' returns path:line matches only, 'files' lists matching files, and 'count' returns match counts by file.",
				"enum":        []string{"context", "matches", "files", "count"},
				"default":     "context",
			},
			"context_lines": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Number of lines to show before and after each match. Default %d. Set to 0 for compact grep -n style output. Use higher values only after narrowing the pattern or glob.", grepDefaultContextLines),
				"default":     grepDefaultContextLines,
				"minimum":     0,
				"maximum":     100,
			},
			"max_matches": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum matching lines to return before truncating. Default %d, hard max %d.", grepDefaultMaxMatches, grepHardMaxMatches),
				"default":     grepDefaultMaxMatches,
				"minimum":     1,
				"maximum":     grepHardMaxMatches,
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Approximate maximum output bytes before truncating. Default %d, hard max %d.", grepDefaultMaxBytes, grepHardMaxBytes),
				"default":     grepDefaultMaxBytes,
				"minimum":     1024,
				"maximum":     grepHardMaxBytes,
			},
		},
		"required": []string{"pattern"},
	}
}

func (GrepTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Pattern         string `json:"pattern"`
		Glob            string `json:"glob"`
		CaseInsensitive bool   `json:"case_insensitive"`
		Literal         bool   `json:"literal"`
		Mode            string `json:"mode"`
		ContextLines    *int   `json:"context_lines"`
		MaxMatches      int    `json:"max_matches"`
		MaxBytes        int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if args.Pattern == "" {
		return "pattern is required", true, nil
	}
	contextLines := grepDefaultContextLines
	if args.ContextLines != nil {
		contextLines = *args.ContextLines
	}
	if contextLines < 0 || contextLines > 100 {
		return "context_lines must be between 0 and 100", true, nil
	}
	maxMatches := args.MaxMatches
	if maxMatches == 0 {
		maxMatches = grepDefaultMaxMatches
	}
	if maxMatches < 1 || maxMatches > grepHardMaxMatches {
		return fmt.Sprintf("max_matches must be between 1 and %d", grepHardMaxMatches), true, nil
	}
	maxBytes := args.MaxBytes
	if maxBytes == 0 {
		maxBytes = grepDefaultMaxBytes
	}
	if maxBytes < 1024 || maxBytes > grepHardMaxBytes {
		return fmt.Sprintf("max_bytes must be between 1024 and %d", grepHardMaxBytes), true, nil
	}
	mode := args.Mode
	if mode == "" {
		mode = "context"
	}
	if mode != "context" && mode != "matches" && mode != "files" && mode != "count" {
		return "mode must be one of: context, matches, files, count", true, nil
	}
	if mode == "matches" {
		contextLines = 0
	}

	pattern := args.Pattern
	if args.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if args.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("invalid regex: %v", err), true, nil
	}

	var paths []string
	if args.Glob != "" {
		paths = rc.FileMap.Match(args.Glob)
	} else {
		for p := range rc.FileMap.Snapshot() {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	type matchRange struct {
		start int
		end   int
		hits  map[int]bool
	}

	var sb strings.Builder
	matches := 0
	truncated := false
	filesHit := 0
	for _, p := range paths {
		if isProtectedRead(p) {
			continue
		}
		content, ok := rc.FileMap.Get(p)
		if !ok {
			continue
		}
		fileHit := false
		lines := strings.Split(content, "\n")
		var fileMatches []int
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			if matches >= maxMatches || sb.Len() >= maxBytes {
				truncated = true
				break
			}
			fileHit = true
			matches++
			fileMatches = append(fileMatches, i)
		}
		if fileHit {
			filesHit++
		}
		if len(fileMatches) > 0 {
			switch mode {
			case "files":
				fmt.Fprintf(&sb, "%s\n", p)
			case "count":
				fmt.Fprintf(&sb, "%s:%d\n", p, len(fileMatches))
			default:
				if contextLines == 0 {
					for _, i := range fileMatches {
						displayLine := lines[i]
						if len(displayLine) > grepMaxLineLen {
							displayLine = displayLine[:grepMaxLineLen] + "…"
						}
						fmt.Fprintf(&sb, "%s:%d:%s\n", p, i+1, displayLine)
						if sb.Len() >= maxBytes {
							truncated = true
							break
						}
					}
				} else {
					var ranges []matchRange
					for _, i := range fileMatches {
						start := i - contextLines
						if start < 0 {
							start = 0
						}
						end := i + contextLines + 1
						if end > len(lines) {
							end = len(lines)
						}
						if len(ranges) > 0 && start <= ranges[len(ranges)-1].end {
							last := &ranges[len(ranges)-1]
							if end > last.end {
								last.end = end
							}
							last.hits[i] = true
						} else {
							ranges = append(ranges, matchRange{start: start, end: end, hits: map[int]bool{i: true}})
						}
					}
					for _, r := range ranges {
						fmt.Fprintf(&sb, "--- %s (lines %d-%d) ---\n", p, r.start+1, r.end)
						for j := r.start; j < r.end; j++ {
							prefix := "  "
							if r.hits[j] {
								prefix = "> "
							}
							displayLine := lines[j]
							if len(displayLine) > grepMaxLineLen {
								displayLine = displayLine[:grepMaxLineLen] + "…"
							}
							fmt.Fprintf(&sb, "%s%4d: %s\n", prefix, j+1, displayLine)
							if sb.Len() >= maxBytes {
								truncated = true
								break
							}
						}
						fmt.Fprintln(&sb)
						if truncated {
							break
						}
					}
				}
			}
		}
		if sb.Len() >= maxBytes {
			truncated = true
		}
		if truncated {
			break
		}
	}

	if matches == 0 {
		return fmt.Sprintf("no matches for /%s/ in %d files", args.Pattern, len(paths)), false, nil
	}
	suffix := fmt.Sprintf("\n— %d match(es) across %d file(s)", matches, filesHit)
	if truncated {
		suffix += " (truncated — narrow the pattern or glob to see more)"
	}
	return sb.String() + suffix, false, nil
}

// ---------- multi_edit ----------

const (
	multiEditMaxEdits = 50
	multiEditMaxFiles = 20
)

type MultiEditTool struct{}

func (MultiEditTool) Name() string { return "multi_edit" }
func (MultiEditTool) Description() string {
	return "Apply surgical exact-string edits to one file or a batch of files. Each edit replaces one occurrence of old_string with new_string unless replace_all=true is set for that edit; edits run sequentially against the running content, so a later edit sees the result of earlier ones. Prefer this over multiple edit_file calls whenever you have 2+ changes or edits across files."
}

func editReplacementSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"old_string":  map[string]any{"type": "string", "minLength": 1},
			"new_string":  map[string]any{"type": "string"},
			"replace_all": map[string]any{"type": "boolean", "description": "When true, replace every occurrence of old_string. Use only for simple exact tokens you intentionally want to change globally in this file."},
		},
		"required": []string{"old_string", "new_string"},
	}
}

func editReplacementsSchema(description string) map[string]any {
	return map[string]any{
		"anyOf": []any{
			map[string]any{
				"type":        "array",
				"description": description,
				"items":       editReplacementSchema(),
			},
			map[string]any{
				"type":        "string",
				"description": "JSON-encoded " + description,
			},
		},
	}
}

func (MultiEditTool) Schema() map[string]any {
	fileEntrySchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"edits":       editReplacementsSchema("Ordered list of exact-string edits for this file."),
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "edits"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The project-relative path to the file.",
			},
			"edits":       editReplacementsSchema("Ordered list of edits. Each is applied to the file in turn; a later edit sees the content produced by earlier edits."),
			"files":       map[string]any{"type": "array", "description": "Optional multi-file batch. Each entry has path and edits.", "items": fileEntrySchema},
			"description": map[string]any{"type": "string"},
		},
		"anyOf": []any{
			map[string]any{"required": []string{"path", "edits"}},
			map[string]any{"required": []string{"files"}},
		},
	}
}

type editReplacement struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func decodeEditReplacements(raw json.RawMessage) ([]editReplacement, error) {
	var edits []editReplacement
	if err := json.Unmarshal(raw, &edits); err == nil {
		return edits, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(encoded), &edits); err != nil {
		return nil, err
	}
	return edits, nil
}

func (MultiEditTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	type multiEditFileInput struct {
		Path        string          `json:"path"`
		Edits       json.RawMessage `json:"edits"`
		Description string          `json:"description"`
	}
	var args struct {
		Path        string               `json:"path"`
		Edits       json.RawMessage      `json:"edits"`
		Files       []multiEditFileInput `json:"files"`
		Description string               `json:"description"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if len(args.Files) == 0 && args.Path != "" {
		args.Files = []multiEditFileInput{{
			Path:        args.Path,
			Edits:       args.Edits,
			Description: args.Description,
		}}
	}
	if len(args.Files) == 0 {
		return "path and edits, or files, must be provided", true, nil
	}
	if len(args.Files) > multiEditMaxFiles {
		return fmt.Sprintf("too many files: %d (max %d) — split into multiple calls", len(args.Files), multiEditMaxFiles), true, nil
	}

	seen := map[string]bool{}
	type preparedEdit struct {
		path         string
		edits        []editReplacement
		nextContent  string
		replacements int
	}
	prepared := make([]preparedEdit, 0, len(args.Files))
	for fileIdx, file := range args.Files {
		edits, err := decodeEditReplacements(file.Edits)
		if err != nil {
			return fmt.Sprintf("file %d: invalid edits: %v", fileIdx+1, err), true, nil
		}
		path, err := sanitizePath(file.Path)
		if err != nil {
			return err.Error(), true, nil
		}
		if seen[path] {
			return fmt.Sprintf("duplicate path in multi_edit: %s", path), true, nil
		}
		seen[path] = true
		if isProtected(path) {
			return fmt.Sprintf("protected file, cannot edit: %s", path), true, nil
		}
		if msg := enforceProjectPathScope(rc, path); msg != "" {
			return msg, true, nil
		}
		if len(edits) == 0 {
			return fmt.Sprintf("%s: edits must contain at least one entry", path), true, nil
		}
		if len(edits) > multiEditMaxEdits {
			return fmt.Sprintf("%s: too many edits: %d (max %d) — split into multiple calls", path, len(edits), multiEditMaxEdits), true, nil
		}
		content, ok := rc.FileMap.Get(path)
		if !ok {
			return fmt.Sprintf("file not found: %s — use write_files to create it", path), true, nil
		}
		if msg := rc.requireCurrentFileRead(path, content); msg != "" {
			return structuredEditError(path, "stale_read", msg, "read_file_range", ""), true, nil
		}
		nextContent, replacements, toolErr, errorCode, suggestedGrep := applyEditReplacements(content, edits)
		if toolErr != "" {
			return structuredEditError(path, errorCode, fmt.Sprintf("%s: %s", path, toolErr), "read_file_range", suggestedGrep), true, nil
		}
		if len(nextContent) > maxWriteBytes {
			return structuredEditError(path, "result_too_large", fmt.Sprintf("resulting file too large: %s is %d bytes (max %d)", path, len(nextContent), maxWriteBytes), "read_file_range", ""), true, nil
		}
		prepared = append(prepared, preparedEdit{
			path:         path,
			edits:        edits,
			nextContent:  nextContent,
			replacements: replacements,
		})
	}

	var totalReplacements int
	for _, p := range prepared {
		rc.FileMap.Set(p.path, p.nextContent)
		rc.MarkFileRead(p.path, p.nextContent)
		if rc.BuildState != nil {
			rc.BuildState.RecordFileWrite(p.path, "update")
		}
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      p.path,
			Operation: "update",
			Bytes:     len(p.nextContent),
			Content:   p.nextContent,
		})
		totalReplacements += p.replacements
	}
	if len(prepared) == 1 {
		summary := fmt.Sprintf("edited %s (%d replacements, %d bytes)", prepared[0].path, totalReplacements, len(prepared[0].nextContent))
		return structuredEditSuccess(prepared[0].path, summary, "", prepared[0].nextContent, totalReplacements, nil), false, nil
	}
	summary := fmt.Sprintf("edited %d files (%d replacements)", len(prepared), totalReplacements)
	return structuredEditSuccess("", summary, "", "", totalReplacements, nil), false, nil
}

func applyEditReplacements(content string, edits []editReplacement) (string, int, string, string, string) {
	working := content
	totalReplacements := 0
	for i, e := range edits {
		if e.OldString == "" {
			return "", 0, fmt.Sprintf("edit %d: old_string must not be empty", i+1), "old_string_empty", ""
		}
		count := strings.Count(working, e.OldString)
		switch count {
		case 0:
			return "", 0, fmt.Sprintf("edit %d: old_string not found — earlier edits may have mutated this region or the content differs from your last view. Use read_file_range to inspect the current state before retrying", i+1), "old_string_not_found", e.OldString
		case 1:
			// ok
		default:
			if !e.ReplaceAll {
				return "", 0, fmt.Sprintf("edit %d: old_string appears %d times — add more surrounding context to disambiguate, or set replace_all=true only when you intentionally want every exact match changed", i+1, count), "old_string_ambiguous", e.OldString
			}
		}
		limit := 1
		if e.ReplaceAll {
			limit = -1
		}
		working = strings.Replace(working, e.OldString, e.NewString, limit)
		totalReplacements += count
	}
	return working, totalReplacements, "", "", ""
}

type structuredEditResult struct {
	Changed       bool   `json:"changed"`
	Path          string `json:"path,omitempty"`
	Replacements  int    `json:"replacements,omitempty"`
	Summary       string `json:"summary,omitempty"`
	OldHash       string `json:"old_hash,omitempty"`
	NewHash       string `json:"new_hash,omitempty"`
	ChangedLines  []int  `json:"changed_lines,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	Error         string `json:"error,omitempty"`
	NextStep      string `json:"next_step,omitempty"`
	SuggestedGrep string `json:"suggested_grep,omitempty"`
}

func structuredEditSuccess(path, summary, oldContent, newContent string, replacements int, changedLines []int) string {
	result := structuredEditResult{
		Changed:      true,
		Path:         path,
		Replacements: replacements,
		Summary:      summary,
		ChangedLines: changedLines,
	}
	if oldContent != "" {
		result.OldHash = fileContentHash(oldContent)
	}
	if newContent != "" {
		result.NewHash = fileContentHash(newContent)
	}
	b, _ := json.Marshal(result)
	return string(b)
}

func structuredEditError(path, code, message, nextStep, suggestedGrep string) string {
	result := structuredEditResult{
		Changed:       false,
		Path:          path,
		ErrorCode:     code,
		Error:         message,
		NextStep:      nextStep,
		SuggestedGrep: suggestedGrep,
	}
	b, _ := json.Marshal(result)
	return string(b)
}

func changedLineNumbers(content, oldString string) []int {
	if oldString == "" {
		return nil
	}
	idx := strings.Index(content, oldString)
	if idx < 0 {
		return nil
	}
	start := strings.Count(content[:idx], "\n") + 1
	lines := strings.Count(oldString, "\n") + 1
	out := make([]int, 0, lines)
	for i := 0; i < lines; i++ {
		out = append(out, start+i)
	}
	return out
}

// ---------- generate_image ----------

type GenerateImageTool struct {
	Deps *RunDependencies
}

func (GenerateImageTool) Name() string { return "generate_image" }
func (GenerateImageTool) Description() string {
	return "Generate an image and return a public URL. Use for hero imagery, product photos, illustrations."
}
func (GenerateImageTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "Detailed visual description"},
			"width":  map[string]any{"type": "integer", "minimum": 256, "maximum": 2048},
			"height": map[string]any{"type": "integer", "minimum": 256, "maximum": 2048},
			"style":  map[string]any{"type": "string", "enum": []string{"photo", "illustration", "icon"}},
		},
		"required": []string{"prompt"},
	}
}

func (g GenerateImageTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Prompt string `json:"prompt"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Style  string `json:"style"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return "prompt is required", true, nil
	}
	if args.Width == 0 {
		args.Width = 1024
	}
	if args.Height == 0 {
		args.Height = 1024
	}
	if g.Deps == nil || g.Deps.GenerateImage == nil {
		return "image generation not available", true, nil
	}
	cacheKey := imageCacheKey(args.Prompt, args.Width, args.Height, args.Style)
	call, leader := beginImageGeneration(rc, cacheKey)
	if !leader {
		<-call.done
		if call.err != nil {
			return fmt.Sprintf("image generation failed: %v", call.err), true, nil
		}
		resp, _ := json.Marshal(map[string]any{
			"url":    call.url,
			"width":  args.Width,
			"height": args.Height,
			"cached": true,
		})
		return string(resp), false, nil
	}
	projectID := ""
	if rc != nil && rc.Project != nil {
		projectID = rc.Project.ID.String()
	}
	url, err := g.Deps.GenerateImage(ctx, args.Prompt, args.Width, args.Height, args.Style, projectID)
	if err != nil {
		finishImageGeneration(rc, cacheKey, call, "", err)
		return fmt.Sprintf("image generation failed: %v", err), true, nil
	}
	finishImageGeneration(rc, cacheKey, call, url, nil)
	resp, _ := json.Marshal(map[string]any{
		"url":    url,
		"width":  args.Width,
		"height": args.Height,
	})
	return string(resp), false, nil
}

func imageCacheKey(prompt string, width, height int, style string) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s", strings.Join(strings.Fields(strings.ToLower(prompt)), " "), width, height, strings.ToLower(strings.TrimSpace(style)))
}

func beginImageGeneration(rc *RunContext, key string) (*imageInflightCall, bool) {
	if rc == nil {
		return &imageInflightCall{done: make(chan struct{})}, true
	}
	rc.ImageCacheMu.Lock()
	defer rc.ImageCacheMu.Unlock()
	if rc.ImageCache != nil {
		if cachedURL, ok := rc.ImageCache[key]; ok {
			call := &imageInflightCall{done: make(chan struct{}), url: cachedURL}
			close(call.done)
			return call, false
		}
	}
	if rc.ImageInflight == nil {
		rc.ImageInflight = make(map[string]*imageInflightCall)
	}
	if call, ok := rc.ImageInflight[key]; ok {
		return call, false
	}
	call := &imageInflightCall{done: make(chan struct{})}
	rc.ImageInflight[key] = call
	return call, true
}

func finishImageGeneration(rc *RunContext, key string, call *imageInflightCall, url string, err error) {
	call.url = url
	call.err = err
	if rc == nil {
		close(call.done)
		return
	}
	rc.ImageCacheMu.Lock()
	if err == nil {
		if rc.ImageCache == nil {
			rc.ImageCache = make(map[string]string)
		}
		rc.ImageCache[key] = url
	}
	delete(rc.ImageInflight, key)
	rc.ImageCacheMu.Unlock()
	close(call.done)
}
