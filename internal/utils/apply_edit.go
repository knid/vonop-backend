package utils

import (
	"strings"
)

// ApplyEdit finds the search text in original and replaces it with replace.
// Returns the modified string and true on success, or the original and false
// if the search text was not found.
//
// Matching strategy (in order):
//  1. Exact substring match
//  2. Whitespace-normalized match (collapses runs of spaces/tabs)
//  3. Trimmed-lines match (trims leading/trailing whitespace per line)
func ApplyEdit(original, search, replace string) (string, bool) {
	// Guard: empty search is a no-op, not a valid edit.
	// This can happen when the parser fails to extract search content
	// (e.g. model used an unrecognised XML format).
	if strings.TrimSpace(search) == "" {
		return original, false
	}

	// Strategy 1: exact match
	idx := strings.Index(original, search)
	if idx != -1 {
		return original[:idx] + replace + original[idx+len(search):], true
	}

	// Strategy 2: whitespace-normalized match
	if result, ok := applyEditNormalized(original, search, replace); ok {
		return result, true
	}

	// Strategy 3: trimmed-lines match
	if result, ok := applyEditTrimmedLines(original, search, replace); ok {
		return result, true
	}

	return original, false
}

// applyEditNormalized collapses runs of spaces/tabs (not newlines) in both
// original and search, finds the match in the normalized original, maps the
// match offsets back to the real original, and performs the replacement there.
func applyEditNormalized(original, search, replace string) (string, bool) {
	type mapping struct {
		normIdx int
		origIdx int
	}

	// Build normalized version of original with index mapping
	var normBuf strings.Builder
	var mappings []mapping
	i := 0
	for i < len(original) {
		if original[i] == ' ' || original[i] == '\t' {
			start := i
			for i < len(original) && (original[i] == ' ' || original[i] == '\t') {
				i++
			}
			mappings = append(mappings, mapping{normBuf.Len(), start})
			normBuf.WriteByte(' ')
		} else {
			mappings = append(mappings, mapping{normBuf.Len(), i})
			normBuf.WriteByte(original[i])
			i++
		}
	}
	// Sentinel mapping for end-of-string
	mappings = append(mappings, mapping{normBuf.Len(), len(original)})

	// Normalize search the same way
	var normSearch strings.Builder
	for j := 0; j < len(search); {
		if search[j] == ' ' || search[j] == '\t' {
			for j < len(search) && (search[j] == ' ' || search[j] == '\t') {
				j++
			}
			normSearch.WriteByte(' ')
		} else {
			normSearch.WriteByte(search[j])
			j++
		}
	}

	normOrig := normBuf.String()
	normSearchStr := normSearch.String()

	normIdx := strings.Index(normOrig, normSearchStr)
	if normIdx == -1 {
		return "", false
	}
	normEnd := normIdx + len(normSearchStr)

	// Map normalized offsets back to original offsets
	origStart := -1
	origEnd := -1
	for _, m := range mappings {
		if m.normIdx == normIdx && origStart == -1 {
			origStart = m.origIdx
		}
		if m.normIdx >= normEnd && origEnd == -1 {
			origEnd = m.origIdx
			break
		}
	}
	if origStart == -1 || origEnd == -1 {
		// Fallback: if mapping fails, try the last mapping entry
		if origEnd == -1 {
			origEnd = len(original)
		}
		if origStart == -1 {
			return "", false
		}
	}

	return original[:origStart] + replace + original[origEnd:], true
}

// applyEditTrimmedLines splits both original and search into lines, trims each
// line, and looks for a contiguous sequence of trimmed search lines within the
// trimmed original lines. If found, replaces the corresponding original lines.
func applyEditTrimmedLines(original, search, replace string) (string, bool) {
	origLines := strings.Split(original, "\n")
	searchLines := strings.Split(search, "\n")

	// Remove empty trailing lines from search (LLM often adds trailing newline)
	for len(searchLines) > 0 && strings.TrimSpace(searchLines[len(searchLines)-1]) == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	if len(searchLines) == 0 {
		return "", false
	}

	// Build trimmed versions for comparison
	trimmedSearch := make([]string, len(searchLines))
	for i, l := range searchLines {
		trimmedSearch[i] = strings.TrimSpace(l)
	}

	// Slide a window over original lines looking for the match
	for i := 0; i <= len(origLines)-len(searchLines); i++ {
		match := true
		for j := 0; j < len(searchLines); j++ {
			if strings.TrimSpace(origLines[i+j]) != trimmedSearch[j] {
				match = false
				break
			}
		}
		if match {
			// Found the match — replace original lines [i..i+len(searchLines)) with replace
			var result strings.Builder
			// Lines before match
			if i > 0 {
				result.WriteString(strings.Join(origLines[:i], "\n"))
				result.WriteString("\n")
			}
			// Replacement content
			result.WriteString(replace)
			// Lines after match
			afterIdx := i + len(searchLines)
			if afterIdx < len(origLines) {
				if replace != "" {
					result.WriteString("\n")
				}
				result.WriteString(strings.Join(origLines[afterIdx:], "\n"))
			}
			return result.String(), true
		}
	}

	return "", false
}
