package utils

import (
	"path"
	"strings"
	"unicode"
)

// SanitizePromptContent escapes characters that could be interpreted as prompt
// formatting or injection vectors when user-controlled content is interpolated
// into LLM prompts. It wraps the content in XML-like delimiters so the LLM
// treats it as data rather than instructions.
func SanitizePromptContent(s string) string {
	if s == "" {
		return s
	}
	// Strip triple backticks that could escape code blocks in the prompt.
	s = strings.ReplaceAll(s, "```", "'''")
	// Escape nested user_data tags to prevent tag nesting attacks.
	s = strings.ReplaceAll(s, "<user_data>", "&lt;user_data&gt;")
	s = strings.ReplaceAll(s, "</user_data>", "&lt;/user_data&gt;")
	return "<user_data>" + s + "</user_data>"
}

// SanitizeFilePath validates and cleans a file path from LLM output to prevent
// path traversal attacks. Returns the cleaned path and true if valid, or empty
// string and false if the path is suspicious.
func SanitizeFilePath(filePath string) (string, bool) {
	// Reject empty paths
	if filePath == "" {
		return "", false
	}
	if strings.TrimSpace(filePath) != filePath {
		return "", false
	}
	for _, r := range filePath {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	if strings.ContainsAny(filePath, "\"'`\\") {
		return "", false
	}

	// Clean the path (resolves ".." and "." components)
	cleaned := path.Clean(filePath)
	if cleaned == "." {
		return "", false
	}

	// Reject absolute paths
	if strings.HasPrefix(cleaned, "/") {
		return "", false
	}

	// Reject paths that traverse above the project root
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", false
	}

	// Reject paths with suspicious components
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", false
		}
	}

	return cleaned, true
}
