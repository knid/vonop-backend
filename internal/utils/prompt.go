package utils

import (
	"path"
	"strings"
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

	// Clean the path (resolves ".." and "." components)
	cleaned := path.Clean(filePath)

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
