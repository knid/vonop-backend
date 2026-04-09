package utils

import (
	"log/slog"
	"regexp"
	"strings"
)

const maxFileContentSize = 10 * 1024 * 1024 // 10MB

// fileTagRegex matches <wemob_file path="..."> opening tags.
var fileTagRegex = regexp.MustCompile(`<wemob_file\s+path="([^"]+)"\s*>`)

// deleteTagRegex matches <wemob_delete path="..."/> self-closing tags.
var deleteTagRegex = regexp.MustCompile(`<wemob_delete\s+path="([^"]+)"\s*/>`)

// editTagRegex matches <wemob_edit path="..."> opening tags.
var editTagRegex = regexp.MustCompile(`<wemob_edit\s+path="([^"]+)"\s*>`)

// FileStreamParser parses `<wemob_file path="...">content</wemob_file>` blocks from a
// text stream incrementally. Non-file text is emitted via OnText; completed
// file blocks are emitted via OnFile.
//
// Tags use a "wemob_" prefix to avoid collisions with Anthropic's internal
// tool-calling XML (e.g. <invoke>, <parameter>) and with HTML/JSX tags that
// may appear in generated code.
//
// NOTE (B70): If the actual file content contains a literal "</wemob_file>" string
// (e.g. inside a code comment or string), the parser may incorrectly treat it
// as the closing tag and split the file. Mitigation: the parser checks whether
// the tag appears inside a quoted string context (same line) and skips
// false positives. The escaped form "<\/wemob_file>" is also unescaped on emit.
// A full fix would require a different framing format (e.g. length-prefixed).
type FileStreamParser struct {
	buffer      string
	inFileBlock bool
	currentPath string
	fileContent strings.Builder

	// Edit block state
	inEditBlock    bool
	editPath       string
	inSearch       bool
	inReplace      bool
	searchContent  strings.Builder
	replaceContent strings.Builder

	// DiscardIncomplete, when true, causes Flush() to discard any file
	// block that was still open when the stream ended (e.g. LLM timeout).
	DiscardIncomplete bool

	// TruncatedPath records the path of the file that was being generated
	// when the stream ended with DiscardIncomplete. Empty if no file was truncated.
	TruncatedPath string

	// OnFile is called when a complete </wemob_file> closing tag is found.
	OnFile func(path, content string)
	// OnDelete is called when a <wemob_delete path="..."/> tag is found.
	OnDelete func(path string)
	// OnText is called for text outside of file blocks.
	OnText func(text string)
	// OnEdit is called when a complete </wemob_edit> closing tag is found.
	// path is the file to edit, search is the text to find, replace is the replacement.
	OnEdit func(path, search, replace string)
}

// Feed processes an incremental chunk of streamed text.
func (p *FileStreamParser) Feed(chunk string) {
	p.buffer += chunk

	for {
		if p.inFileBlock {
			// Look for closing </wemob_file> tag, skipping false positives inside string literals
			closeIdx := p.findRealCloseTag()
			if closeIdx == -1 {
				// No closing tag yet — flush everything except last 13 chars
				// ("</wemob_file" is 13 chars) to file content buffer as a safe boundary.
				if len(p.buffer) > 13 {
					safe := p.buffer[:len(p.buffer)-13]
					p.fileContent.WriteString(safe)
					p.buffer = p.buffer[len(p.buffer)-13:]
				}
				// Check if accumulated content exceeds the size limit.
				if p.fileContent.Len() > maxFileContentSize {
					slog.Warn("file content exceeded max size, discarding",
						"path", p.currentPath,
						"size", p.fileContent.Len(),
						"max", maxFileContentSize,
					)
					p.fileContent.Reset()
					p.inFileBlock = false
					p.currentPath = ""
					p.buffer = ""
					return
				}
				return
			}

			// Found closing tag — emit the file
			p.fileContent.WriteString(p.buffer[:closeIdx])
			if p.OnFile != nil {
				content := p.fileContent.String()
				// Trim leading/trailing newline that typically surrounds file content
				content = strings.TrimPrefix(content, "\n")
				content = strings.TrimSuffix(content, "\n")
				// Unescape escaped close tags that the LLM may have used
				content = strings.ReplaceAll(content, `<\/wemob_file>`, `</wemob_file>`)
				p.OnFile(p.currentPath, content)
			}
			p.fileContent.Reset()
			p.inFileBlock = false
			p.currentPath = ""
			p.buffer = p.buffer[closeIdx+len("</wemob_file>"):]
			// Continue loop to process remaining buffer
		} else if p.inEditBlock {
			p.processEditBlock()
			return
		} else {
			// Check for <wemob_delete path="..."/> tags first
			if delLoc := deleteTagRegex.FindStringIndex(p.buffer); delLoc != nil {
				// Emit text before the delete tag
				if delLoc[0] > 0 {
					before := p.buffer[:delLoc[0]]
					if p.OnText != nil && before != "" {
						p.OnText(before)
					}
				}
				match := deleteTagRegex.FindStringSubmatch(p.buffer[delLoc[0]:delLoc[1]])
				if len(match) >= 2 && p.OnDelete != nil {
					p.OnDelete(match[1])
				}
				p.buffer = p.buffer[delLoc[1]:]
				continue
			}

			// Check for <wemob_edit path="..."> tags
			if editLoc := editTagRegex.FindStringIndex(p.buffer); editLoc != nil {
				// Check if an <wemob_edit> tag comes before any <wemob_file> tag
				fileLoc := fileTagRegex.FindStringIndex(p.buffer)
				if fileLoc == nil || editLoc[0] < fileLoc[0] {
					// Emit text before the edit tag
					if editLoc[0] > 0 {
						before := p.buffer[:editLoc[0]]
						if p.OnText != nil && before != "" {
							p.OnText(before)
						}
					}
					match := editTagRegex.FindStringSubmatch(p.buffer[editLoc[0]:editLoc[1]])
					if len(match) >= 2 {
						p.editPath = match[1]
						p.inEditBlock = true
						p.searchContent.Reset()
						p.replaceContent.Reset()
						p.inSearch = false
						p.inReplace = false
						p.buffer = p.buffer[editLoc[1]:]
						continue
					}
				}
			}

			// Look for opening <wemob_file path="..."> tag
			loc := fileTagRegex.FindStringIndex(p.buffer)
			if loc == nil {
				// No opening tag found — emit safe text.
				// Keep everything from the last '<' in the buffer since it could
				// be the start of a partial <wemob_file ...> tag forming across chunks.
				// The tag can be 60+ chars (e.g. <wemob_file path="components/ecommerce/checkout-form.tsx">),
				// so a fixed buffer size is not safe.
				lastLT := strings.LastIndex(p.buffer, "<")
				if lastLT >= 0 {
					tail := p.buffer[lastLT:]
					// If the tail starts with "<" but is long enough that we can tell
					// it's NOT going to be "<wemob_...", flush it as text to avoid
					// indefinite buffering (e.g. "a < b" in explanatory text).
					// Only buffer tails that could be the start of a wemob_ tag.
					isPartialFileTag := strings.HasPrefix(tail, "<w") ||
						strings.HasPrefix(tail, "<W") ||
						tail == "<"
					// B68: If the tail looks like a partial wemob_ tag but is
					// longer than any valid <wemob_file path="..."> or
					// <wemob_delete path="..."/> opening could be, it is not a
					// partial tag — flush it to avoid stalling the parser.
					if isPartialFileTag && len(tail) > 120 {
						isPartialFileTag = false
					}
					if !isPartialFileTag {
						// Not a file tag — emit everything
						if p.OnText != nil {
							p.OnText(p.buffer)
						}
						p.buffer = ""
						return
					}
					// Emit everything before the '<' as text
					if lastLT > 0 {
						if p.OnText != nil {
							p.OnText(p.buffer[:lastLT])
						}
						p.buffer = p.buffer[lastLT:]
					}
					// Buffer holds from '<' onward — wait for more data
				} else {
					// No '<' at all — safe to emit everything
					if p.OnText != nil && p.buffer != "" {
						p.OnText(p.buffer)
					}
					p.buffer = ""
				}
				return
			}

			// Emit any text before the opening tag
			if loc[0] > 0 {
				before := p.buffer[:loc[0]]
				if p.OnText != nil && before != "" {
					p.OnText(before)
				}
			}

			// Extract path from the tag
			match := fileTagRegex.FindStringSubmatch(p.buffer[loc[0]:loc[1]])
			if len(match) < 2 {
				// Shouldn't happen given FindStringIndex succeeded, but be safe
				return
			}
			p.currentPath = match[1]
			p.inFileBlock = true
			p.buffer = p.buffer[loc[1]:]
			// Continue loop to look for closing tag
		}
	}
}

// Flush emits any remaining buffered content when the stream ends.
func (p *FileStreamParser) Flush() {
	if p.inEditBlock {
		slog.Warn("FileStreamParser: discarding incomplete edit block (stream truncated)",
			"path", p.editPath)
		p.searchContent.Reset()
		p.replaceContent.Reset()
		p.inEditBlock = false
		p.inSearch = false
		p.inReplace = false
		p.editPath = ""
	}
	if p.inFileBlock {
		if p.DiscardIncomplete {
			// Stream ended mid-file — discard incomplete content
			slog.Warn("FileStreamParser: discarding incomplete file (stream truncated)", "path", p.currentPath, "partial_bytes", p.fileContent.Len())
			p.TruncatedPath = p.currentPath
			p.fileContent.Reset()
			p.inFileBlock = false
			p.currentPath = ""
		} else {
			// Legacy: emit partial file
			p.fileContent.WriteString(p.buffer)
			if p.OnFile != nil {
				content := p.fileContent.String()
				content = strings.TrimPrefix(content, "\n")
				content = strings.TrimSuffix(content, "\n")
				content = strings.ReplaceAll(content, `<\/wemob_file>`, `</wemob_file>`)
				p.OnFile(p.currentPath, content)
			}
			p.fileContent.Reset()
			p.inFileBlock = false
			p.currentPath = ""
		}
	} else if p.buffer != "" {
		if p.OnText != nil {
			p.OnText(p.buffer)
		}
	}
	p.buffer = ""
}

// findRealCloseTag returns the index of the real </wemob_file> closing tag in p.buffer,
// skipping false positives that appear inside string literals. Returns -1 if no
// real closing tag is found.
//
// Heuristic: a </wemob_file> that appears on the same line as an unclosed quote
// character (", ', `) is likely inside a string literal and is skipped.
func (p *FileStreamParser) findRealCloseTag() int {
	searchFrom := 0
	for {
		idx := strings.Index(p.buffer[searchFrom:], "</wemob_file>")
		if idx == -1 {
			return -1
		}
		idx += searchFrom // absolute index

		// Check if this </wemob_file> is inside a string literal by examining the
		// content from the last newline to the tag position. If there's an
		// odd number of quote characters, the tag is likely inside a string.
		lineStart := strings.LastIndex(p.buffer[:idx], "\n")
		if lineStart == -1 {
			lineStart = 0
		} else {
			lineStart++ // skip the newline itself
		}
		linePrefix := p.buffer[lineStart:idx]

		// Also skip the escaped form <\/wemob_file>
		if idx > 0 && p.buffer[idx-1] == '\\' {
			searchFrom = idx + len("</wemob_file>")
			continue
		}

		// Count unescaped quotes — if any type has an odd count, we're inside a string
		insideString := false
		for _, q := range []byte{'"', '\'', '`'} {
			count := 0
			for i := 0; i < len(linePrefix); i++ {
				if linePrefix[i] == q && (i == 0 || linePrefix[i-1] != '\\') {
					count++
				}
			}
			if count%2 != 0 {
				insideString = true
				break
			}
		}

		if insideString {
			// False positive — skip and keep looking
			slog.Debug("FileStreamParser: skipping </wemob_file> inside string literal",
				"path", p.currentPath, "line_prefix_len", len(linePrefix))
			searchFrom = idx + len("</wemob_file>")
			continue
		}

		return idx
	}
}

// normalizeEditTags rewrites Anthropic-style <parameter name="wemob_search">
// tags to the canonical <wemob_search> form. Claude sometimes falls back to its
// trained tool-calling XML format, producing <parameter name="wemob_search">
// instead of <wemob_search>. This normalisation lets the parser handle both.
func normalizeEditTags(s string) string {
	s = strings.ReplaceAll(s, `<parameter name="wemob_search">`, "<wemob_search>")
	s = strings.ReplaceAll(s, `<parameter name="wemob_replace">`, "<wemob_replace>")
	// The closing </parameter> is ambiguous — only replace when we're inside
	// an edit block (caller guarantees this). Map to the correct closing tag
	// based on parse state by replacing all occurrences; the parser's
	// inSearch/inReplace state ensures they're consumed in the right order.
	// We replace with both closing tags concatenated... no, that's wrong.
	// Instead, we handle </parameter> as an alias for the currently-open
	// sub-block's closing tag directly in processEditBlock.
	return s
}

// processEditBlock processes buffer content while inside an <wemob_edit> block.
// Handles the <wemob_search>...</wemob_search> and <wemob_replace>...</wemob_replace> sub-blocks,
// and emits OnEdit when </wemob_edit> is found.
func (p *FileStreamParser) processEditBlock() {
	// Normalize Anthropic-style <parameter name="wemob_*"> opening tags.
	p.buffer = normalizeEditTags(p.buffer)

	for {
		if p.inSearch {
			// Accept both </wemob_search> and </parameter> as closing tag.
			closeTag, closeLen := p.findEditCloseTag("</wemob_search>", "</parameter>")
			closeIdx := strings.Index(p.buffer, closeTag)
			if closeIdx == -1 {
				if len(p.buffer) > 15 {
					p.searchContent.WriteString(p.buffer[:len(p.buffer)-15])
					p.buffer = p.buffer[len(p.buffer)-15:]
				}
				return
			}
			p.searchContent.WriteString(p.buffer[:closeIdx])
			p.inSearch = false
			p.buffer = p.buffer[closeIdx+closeLen:]
			continue
		}

		if p.inReplace {
			// Accept both </wemob_replace> and </parameter> as closing tag.
			closeTag, closeLen := p.findEditCloseTag("</wemob_replace>", "</parameter>")
			closeIdx := strings.Index(p.buffer, closeTag)
			if closeIdx == -1 {
				if len(p.buffer) > 16 {
					p.replaceContent.WriteString(p.buffer[:len(p.buffer)-16])
					p.buffer = p.buffer[len(p.buffer)-16:]
				}
				return
			}
			p.replaceContent.WriteString(p.buffer[:closeIdx])
			p.inReplace = false
			p.buffer = p.buffer[closeIdx+closeLen:]
			continue
		}

		if idx := strings.Index(p.buffer, "</wemob_edit>"); idx != -1 {
			if p.OnEdit != nil {
				search := p.searchContent.String()
				search = strings.TrimPrefix(search, "\n")
				search = strings.TrimSuffix(search, "\n")
				replace := p.replaceContent.String()
				replace = strings.TrimPrefix(replace, "\n")
				replace = strings.TrimSuffix(replace, "\n")
				p.OnEdit(p.editPath, search, replace)
			}
			p.searchContent.Reset()
			p.replaceContent.Reset()
			p.inEditBlock = false
			p.editPath = ""
			p.buffer = p.buffer[idx+len("</wemob_edit>"):]
			return
		}

		if idx := strings.Index(p.buffer, "<wemob_search>"); idx != -1 {
			p.inSearch = true
			p.buffer = p.buffer[idx+len("<wemob_search>"):]
			continue
		}

		if idx := strings.Index(p.buffer, "<wemob_replace>"); idx != -1 {
			p.inReplace = true
			p.buffer = p.buffer[idx+len("<wemob_replace>"):]
			continue
		}

		return
	}
}

// findEditCloseTag returns whichever closing tag appears first in the buffer:
// the canonical tag (e.g. </wemob_search>) or the Anthropic-style </parameter>.
// Returns the matched tag string and its length.
func (p *FileStreamParser) findEditCloseTag(canonical, alt string) (string, int) {
	ci := strings.Index(p.buffer, canonical)
	ai := strings.Index(p.buffer, alt)
	if ci == -1 && ai == -1 {
		return canonical, len(canonical) // neither found — caller will see -1
	}
	if ai == -1 || (ci != -1 && ci <= ai) {
		return canonical, len(canonical)
	}
	return alt, len(alt)
}
