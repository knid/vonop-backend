package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	trimmedToolResultMaxChars = 2000
)

var readFilesHeaderRE = regexp.MustCompile(`===== (.+?) =====`)

// trimToolArgsForHistory removes bulky file/edit payloads from prior assistant
// tool_call messages. The tool has already executed by the time this runs, so
// future rounds only need the operation metadata; exact file contents remain
// available from SafeFileMap via read_files.
func trimToolArgsForHistory(toolName, args string) string {
	switch toolName {
	case "write_file":
		return trimWriteFileArgs(args)
	case "write_files":
		return trimWriteFilesArgs(args)
	case "edit_file":
		return trimEditFileArgs(args)
	case "multi_edit":
		return trimMultiEditArgs(args)
	case "patch_files":
		return trimPatchFilesArgs(args)
	default:
		return args
	}
}

// trimToolResultForHistory compresses previous-round tool results before they
// are sent to the LLM again. The model sees full tool output for one round;
// after that, exact current file contents should be re-fetched from SafeFileMap.
func trimToolResultForHistory(toolName, result string) string {
	switch toolName {
	case "read_files":
		return trimReadFilesResult(result)
	default:
		return result
	}
}

func trimWriteFileArgs(args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return invalidTrimmedArgs(toolNameWriteFile, args)
	}
	if content, ok := m["content"].(string); ok {
		m["content"] = trimmedPayload(fmt.Sprintf("omitted after execution; %d bytes written", len(content)))
		m["content_bytes"] = len(content)
	}
	return marshalTrimmedArgs(m, args)
}

func trimWriteFilesArgs(args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return invalidTrimmedArgs(toolNameWriteFiles, args)
	}
	files, ok := m["files"].([]any)
	if !ok {
		return args
	}
	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if content, ok := file["content"].(string); ok {
			file["content"] = trimmedPayload(fmt.Sprintf("omitted after execution; %d bytes written", len(content)))
			file["content_bytes"] = len(content)
		}
	}
	return marshalTrimmedArgs(m, args)
}

func trimEditFileArgs(args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return invalidTrimmedArgs(toolNameEditFile, args)
	}
	trimEditMap(m)
	return marshalTrimmedArgs(m, args)
}

func trimMultiEditArgs(args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return invalidTrimmedArgs(toolNameMultiEdit, args)
	}
	if edits, ok := m["edits"].([]any); ok {
		trimEditList(edits)
		m["edit_count"] = len(edits)
	}
	return marshalTrimmedArgs(m, args)
}

func trimPatchFilesArgs(args string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return invalidTrimmedArgs(toolNamePatchFiles, args)
	}
	files, ok := m["files"].([]any)
	if !ok {
		return args
	}
	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if edits, ok := file["edits"].([]any); ok {
			trimEditList(edits)
			file["edit_count"] = len(edits)
		}
	}
	return marshalTrimmedArgs(m, args)
}

const (
	toolNameWriteFile  = "write_file"
	toolNameWriteFiles = "write_files"
	toolNameEditFile   = "edit_file"
	toolNameMultiEdit  = "multi_edit"
	toolNamePatchFiles = "patch_files"
)

func invalidTrimmedArgs(toolName, args string) string {
	out, err := json.Marshal(map[string]any{
		"tool":          toolName,
		"trimmed":       true,
		"invalid_json":  true,
		"original_size": len(args),
		"note":          "invalid tool arguments omitted from continuing context after execution",
	})
	if err != nil {
		return `{"trimmed":true,"invalid_json":true}`
	}
	return string(out)
}

func trimEditList(edits []any) {
	for _, raw := range edits {
		edit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		trimEditMap(edit)
	}
}

func trimEditMap(m map[string]any) {
	if oldString, ok := m["old_string"].(string); ok {
		m["old_string"] = trimmedPayload(fmt.Sprintf("omitted after execution; old_string was %d bytes", len(oldString)))
		m["old_string_bytes"] = len(oldString)
	}
	if newString, ok := m["new_string"].(string); ok {
		m["new_string"] = trimmedPayload(fmt.Sprintf("omitted after execution; new_string was %d bytes", len(newString)))
		m["new_string_bytes"] = len(newString)
	}
}

func trimmedPayload(note string) string {
	return "[trimmed] " + note
}

func marshalTrimmedArgs(v any, fallback string) string {
	out, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	return string(out)
}

func trimReadFilesResult(result string) string {
	paths := readFilesPaths(result)
	var sb strings.Builder
	sb.WriteString(`<tool_output tool="read_files">`)
	sb.WriteString("Read result omitted from continuing context after one round to control token usage.\n")
	if len(paths) > 0 {
		sb.WriteString("Files previously read:\n")
		for _, p := range paths {
			sb.WriteString("- ")
			sb.WriteString(p)
			sb.WriteByte('\n')
		}
	}
	if details := readFilesErrorDetails(result); details != "" {
		sb.WriteString("\nErrors/details preserved:\n")
		sb.WriteString(details)
		sb.WriteByte('\n')
	}
	sb.WriteString("\nExact current file contents are still available from project state; call read_files again for any file you need to inspect before editing.")
	sb.WriteString(`</tool_output>`)
	return sb.String()
}

func readFilesPaths(result string) []string {
	matches := readFilesHeaderRE.FindAllStringSubmatch(result, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		p := strings.TrimSpace(m[1])
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
}

func readFilesErrorDetails(result string) string {
	var lines []string
	for _, line := range strings.Split(result, "\n") {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "ERROR:") || strings.Contains(upper, "TRUNCATED") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	details := strings.Join(lines, "\n")
	if len(details) > trimmedToolResultMaxChars {
		return details[:trimmedToolResultMaxChars] + "...(trimmed)"
	}
	return details
}
