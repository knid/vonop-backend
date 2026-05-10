package utils

import "strings"

// TruncatePromptPreview returns at most n runes of s with an ellipsis when
// truncation occurred. Empty input yields empty output. Rune-based so
// UTF-8 (Turkish, emoji, …) is not split mid-codepoint.
func TruncatePromptPreview(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" || n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
