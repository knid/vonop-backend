package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
	"package-lock.json":   true,
	"yarn.lock":           true,
	"pnpm-lock.yaml":      true,
	"bun.lockb":           true,
	// Config files at project root
	"tsconfig.json":       true,
	"next.config.ts":      true,
	"next.config.js":      true,
	"next.config.mjs":     true,
	"next.config.cjs":     true,
	"tailwind.config.ts":  true,
	"tailwind.config.js":  true,
	"tailwind.config.mjs": true,
	"postcss.config.ts":   true,
	"postcss.config.js":   true,
	"postcss.config.mjs":  true,
	"drizzle.config.ts":   true,
	"drizzle.config.js":   true,
	"middleware.ts":       true,
	"middleware.js":       true,
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
	if len(matches) == 0 {
		return "no files match", false, nil
	}
	var sb strings.Builder
	for _, p := range matches {
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
	if !rc.FileMap.Delete(path) {
		return fmt.Sprintf("file not found: %s", path), true, nil
	}
	rc.Emitter.Emit(EventFileDeleted, FileDeletedEvent{RunID: rc.RunID, Path: path})
	return fmt.Sprintf("deleted %s", path), false, nil
}

// ---------- write_file ----------

const maxWriteBytes = 200 * 1024 // 200 KB

type WriteFileTool struct{}

func (WriteFileTool) Name() string { return "write_file" }
func (WriteFileTool) Description() string {
	return "Create a new file or fully overwrite an existing file. Prefer edit_file for targeted changes to existing files."
}
func (WriteFileTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        map[string]any{"type": "string"},
			"content":     map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}
}

func (WriteFileTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Path        string `json:"path"`
		Content     string `json:"content"`
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
		return fmt.Sprintf("protected file, cannot overwrite: %s", path), true, nil
	}
	if len(args.Content) > maxWriteBytes {
		return fmt.Sprintf("content too large: %d bytes (max %d)", len(args.Content), maxWriteBytes), true, nil
	}

	op := "update"
	if _, exists := rc.FileMap.Get(path); !exists {
		op = "create"
	}
	rc.FileMap.Set(path, args.Content)
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
		return fmt.Sprintf("old_string not found in %s — the file content may have drifted. call read_file to resync and retry", path), true, nil
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
	rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
		RunID:     rc.RunID,
		Path:      path,
		Operation: "update",
		Bytes:     len(newContent),
		Content:   newContent,
	})
	return fmt.Sprintf("edited %s (1 replacement, %d bytes)", path, len(newContent)), false, nil
}

// ---------- generate_image ----------

type GenerateImageTool struct {
	Deps *RunDependencies
}

func (GenerateImageTool) Name() string { return "generate_image" }
func (GenerateImageTool) Description() string {
	return "Generate an image and return a public URL. Use for hero imagery, product photos, illustrations. Never use stock URLs (picsum, unsplash, etc.)."
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
