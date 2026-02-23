package utils

import (
	"regexp"
	"strings"
)

// ansiRegex strips ANSI escape sequences from container log output.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// FilterErrorLogs extracts only user-code error blocks from Next.js container output.
// It skips internal Next.js / node_modules noise and startup messages.
func FilterErrorLogs(raw string) string {
	lines := strings.Split(raw, "\n")

	// Lines containing any of these are always ignored (internal noise, not user errors).
	ignorePatterns := []string{
		"node_modules/",
		"patch-incorrect-lockfile",
		"at async Promise.all",
		"at processTicksAndRejections",
		"at async patchIncorrectLockfile",
		"at fetchPkgInfo",
		"Ready in",
		"ready started server",
		"Compiling /",
		"Compiled successfully",
		"Compiled /",
		"▲ Next.js",
		"- Local:",
		"- Network:",
		"GET /",
		"POST /",
		"200 in",
		"304 in",
	}

	// Error markers that indicate the start of a real user-code error block.
	errorMarkers := []string{
		"Failed to compile",
		"Module not found",
		"Module build failed",
		"Unhandled Runtime Error",
		"is not defined",
		"Cannot find module",
		"./app/",
		"./components/",
		"./lib/",
		"./src/",
		"Import trace",
	}

	var result []string
	inBlock := false
	contextRemaining := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if inBlock {
				result = append(result, line)
			}
			continue
		}

		// Skip ignored lines entirely — they break any active error block too.
		ignored := false
		for _, pat := range ignorePatterns {
			if strings.Contains(line, pat) {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}

		isError := false
		for _, marker := range errorMarkers {
			if strings.Contains(line, marker) {
				isError = true
				break
			}
		}

		if isError {
			inBlock = true
			contextRemaining = 5
			result = append(result, line)
		} else if inBlock && contextRemaining > 0 {
			result = append(result, line)
			contextRemaining--
			if contextRemaining == 0 {
				inBlock = false
			}
		}
	}

	return ansiRegex.ReplaceAllString(strings.Join(result, "\n"), "")
}
