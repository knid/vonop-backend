package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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
		return fmt.Sprintf("<truncated: file is %d bytes, max %d>\n\n%s",
			len(content), maxReadBytes, content[:maxReadBytes]), false, nil
	}
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
	for i, raw := range args.Paths {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		path, err := sanitizePath(raw)
		if err != nil {
			fmt.Fprintf(&sb, "===== %s =====\nERROR: %s", raw, err.Error())
			continue
		}
		fmt.Fprintf(&sb, "===== %s =====\n", path)
		if isProtectedRead(path) {
			fmt.Fprintf(&sb, "ERROR: reading %s is not allowed (contains secrets or system files)", path)
			continue
		}
		content, ok := rc.FileMap.Get(path)
		if !ok {
			sb.WriteString("ERROR: file not found")
			continue
		}
		if len(content) > maxReadBytes {
			fmt.Fprintf(&sb, "<truncated: file is %d bytes, max %d>\n\n%s", len(content), maxReadBytes, content[:maxReadBytes])
			continue
		}
		sb.WriteString(content)
	}
	return sb.String(), false, nil
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

// ---------- write_file ----------

const (
	maxWriteBytes = 200 * 1024 // 200 KB

)

type WriteFileTool struct{}

func (WriteFileTool) Name() string { return "write_file" }
func (WriteFileTool) Description() string {
	return "Create a new file. To fully overwrite an existing file, pass overwrite=true with a short reason; prefer edit_file or multi_edit for targeted changes."
}
func (WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"content":     map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"overwrite":   map[string]any{"type": "boolean", "description": "Set true only when intentionally replacing the entire existing file."},
			"reason":      map[string]any{"type": "string", "description": "Required when overwrite=true. Explain why a full rewrite is safer than edit_file or multi_edit."},
		},
		"required": []string{"path", "content"},
	}
}

func (WriteFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
		Description string `json:"description"`
		Overwrite   bool   `json:"overwrite"`
		Reason      string `json:"reason"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	path, err := sanitizePath(args.Path)
	if err != nil {
		return err.Error(), true, nil
	}
	if isProtected(path) {
		return fmt.Sprintf("protected file, cannot overwrite: %s", path), true, nil
	}
	if msg := enforceProjectPathScope(rc, path); msg != "" {
		return msg, true, nil
	}
	if len(args.Content) > maxWriteBytes {
		return fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxWriteBytes), true, nil
	}

	op := "update"
	_, exists := rc.FileMap.Get(path)
	if !exists {
		op = "create"
	} else if !args.Overwrite {
		return fmt.Sprintf("file already exists: %s — use edit_file/multi_edit for targeted changes, or call write_file again with overwrite=true and a reason if a full rewrite is intentional", path), true, nil
	} else if strings.TrimSpace(args.Reason) == "" {
		return fmt.Sprintf("overwrite reason is required for existing file: %s", path), true, nil
	}
	rc.FileMap.Set(path, args.Content)
	if rc.BuildState != nil {
		rc.BuildState.RecordFileWrite(path, op)
	}
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: op,
		Bytes:     len(args.Content),
		Content:   args.Content,
	})
	return fmt.Sprintf("%sd %s (%d bytes)", op, path, len(args.Content)), false, nil
}

// ---------- edit_file ----------

type EditFileTool struct{}

func (EditFileTool) Name() string { return "edit_file" }
func (EditFileTool) Description() string {
	return "Surgically replace exactly one occurrence of old_string with new_string in an existing file. Fails if old_string does not appear or appears more than once — in that case, add surrounding context to old_string and retry."
}
func (EditFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"old_string":  map[string]any{"type": "string"},
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
		return fmt.Sprintf("file not found: %s — use write_file to create it", path), true, nil
	}
	if args.OldString == "" {
		return "old_string must not be empty", true, nil
	}

	count := strings.Count(content, args.OldString)
	switch count {
	case 0:
		return fmt.Sprintf("old_string not found in %s — the file content may have changed since your last view. Consider using grep to find the current text before retrying", path), true, nil
	case 1:
		// ok
	default:
		return fmt.Sprintf("old_string appears %d times in %s — add more surrounding context to disambiguate", count, path), true, nil
	}
	newContent := strings.Replace(content, args.OldString, args.NewString, 1)
	if len(newContent) > maxWriteBytes {
		return fmt.Sprintf("resulting file too large: %d bytes (max %d)", len(newContent), maxWriteBytes), true, nil
	}
	rc.FileMap.Set(path, newContent)
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
	return fmt.Sprintf("edited %s (1 replacement, %d bytes)", path, len(newContent)), false, nil
}

// ---------- grep ----------

const (
	grepMaxMatches = 200
	grepMaxBytes   = 64 * 1024
	grepMaxLineLen = 400
)

type GrepTool struct{}

func (GrepTool) Name() string { return "grep" }
func (GrepTool) Description() string {
	return "Search file contents for a regex pattern across the project. Returns file:line:content for each match. Use this BEFORE read_file / edit_file when you need to find where a string or symbol lives — it is ~50× cheaper than guessing file paths and reading them one by one."
}
func (GrepTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Regular expression (RE2 syntax). Use \\Q...\\E or escape special chars for literal search.",
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "Optional glob to limit files scanned, e.g. 'app/**/*.tsx'. Empty = all files.",
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": "If true, match regardless of case.",
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
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if args.Pattern == "" {
		return "pattern is required", true, nil
	}

	pattern := args.Pattern
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

	var (
		sb        strings.Builder
		matches   int
		truncated bool
		filesHit  int
	)
	for _, p := range paths {
		if isProtectedRead(p) {
			continue
		}
		content, ok := rc.FileMap.Get(p)
		if !ok {
			continue
		}
		fileHit := false
		for i, line := range strings.Split(content, "\n") {
			if !re.MatchString(line) {
				continue
			}
			if matches >= grepMaxMatches || sb.Len() >= grepMaxBytes {
				truncated = true
				break
			}
			if len(line) > grepMaxLineLen {
				line = line[:grepMaxLineLen] + "…"
			}
			fmt.Fprintf(&sb, "%s:%d:%s\n", p, i+1, line)
			matches++
			fileHit = true
		}
		if fileHit {
			filesHit++
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

const multiEditMaxEdits = 50

type MultiEditTool struct{}

func (MultiEditTool) Name() string { return "multi_edit" }
func (MultiEditTool) Description() string {
	return "Apply multiple surgical edits to a single file in one call. Each edit replaces one occurrence of old_string with new_string; edits run sequentially against the running content, so a later edit sees the result of earlier ones. Prefer this over multiple edit_file calls whenever you have ≥2 changes to the same file — same semantics, one round-trip."
}
func (MultiEditTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The project-relative path to the file.",
			},
			"edits": map[string]any{
				"type":        "array",
				"description": "Ordered list of edits. Each is applied to the file in turn; a later edit sees the content produced by earlier edits.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"old_string": map[string]any{"type": "string"},
						"new_string": map[string]any{"type": "string"},
					},
					"required": []string{"old_string", "new_string"},
				},
			},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "edits"},
	}
}

func (MultiEditTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path  string `json:"path"`
		Edits []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
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
	if len(args.Edits) == 0 {
		return "edits must contain at least one entry", true, nil
	}
	if len(args.Edits) > multiEditMaxEdits {
		return fmt.Sprintf("too many edits: %d (max %d) — split into multiple calls", len(args.Edits), multiEditMaxEdits), true, nil
	}
	content, ok := rc.FileMap.Get(path)
	if !ok {
		return fmt.Sprintf("file not found: %s — use write_file to create it", path), true, nil
	}

	working := content
	for i, e := range args.Edits {
		if e.OldString == "" {
			return fmt.Sprintf("edit %d: old_string must not be empty", i+1), true, nil
		}
		count := strings.Count(working, e.OldString)
		switch count {
		case 0:
			return fmt.Sprintf("edit %d: old_string not found — earlier edits may have mutated this region or the content differs from your last view. Use grep first to inspect the current state if needed", i+1), true, nil
		case 1:
			// ok
		default:
			return fmt.Sprintf("edit %d: old_string appears %d times — add more surrounding context to disambiguate", i+1, count), true, nil
		}
		working = strings.Replace(working, e.OldString, e.NewString, 1)
	}
	if len(working) > maxWriteBytes {
		return fmt.Sprintf("resulting file too large: %d bytes (max %d)", len(working), maxWriteBytes), true, nil
	}

	rc.FileMap.Set(path, working)
	if rc.BuildState != nil {
		rc.BuildState.RecordFileWrite(path, "update")
	}
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: "update",
		Bytes:     len(working),
		Content:   working,
	})
	return fmt.Sprintf("edited %s (%d replacements, %d bytes)", path, len(args.Edits), len(working)), false, nil
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
	url, err := g.Deps.GenerateImage(ctx, args.Prompt, args.Width, args.Height, args.Style, rc.Project.ID.String())
	if err != nil {
		return fmt.Sprintf("image generation failed: %v", err), true, nil
	}
	resp, _ := json.Marshal(map[string]any{
		"url":    url,
		"width":  args.Width,
		"height": args.Height,
	})
	return string(resp), false, nil
}
