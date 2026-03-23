package utils

import (
	"log/slog"
	"regexp"
	"strings"
)

const maxFileContentSize = 10 * 1024 * 1024 // 10MB

// fileTagRegex matches <file path="..."> opening tags.
var fileTagRegex = regexp.MustCompile(`<file\s+path="([^"]+)"\s*>`)

// deleteTagRegex matches <delete path="..."/> self-closing tags.
var deleteTagRegex = regexp.MustCompile(`<delete\s+path="([^"]+)"\s*/>`)

// FileStreamParser parses `<file path="...">content</file>` blocks from a
// text stream incrementally. Non-file text is emitted via OnText; completed
// file blocks are emitted via OnFile.
//
// NOTE (B70): If the actual file content contains a literal "</file>" string
// (e.g. inside a code comment or string), the parser may incorrectly treat it
// as the closing tag and split the file. Mitigation: the parser checks whether
// "«/file»" appears inside a quoted string context (same line) and skips
// false positives. The escaped form "<\/file>" is also unescaped on emit.
// A full fix would require a different framing format (e.g. length-prefixed).
type FileStreamParser struct {
	buffer      string
	inFileBlock bool
	currentPath string
	fileContent strings.Builder

	// DiscardIncomplete, when true, causes Flush() to discard any file
	// block that was still open when the stream ended (e.g. LLM timeout).
	DiscardIncomplete bool

	// TruncatedPath records the path of the file that was being generated
	// when the stream ended with DiscardIncomplete. Empty if no file was truncated.
	TruncatedPath string

	// OnFile is called when a complete </file> closing tag is found.
	OnFile func(path, content string)
	// OnDelete is called when a <delete path="..."/> tag is found.
	OnDelete func(path string)
	// OnText is called for text outside of file blocks.
	OnText func(text string)
}

// Feed processes an incremental chunk of streamed text.
func (p *FileStreamParser) Feed(chunk string) {
	p.buffer += chunk

	for {
		if p.inFileBlock {
			// Look for closing </file> tag, skipping false positives inside string literals
			closeIdx := p.findRealCloseTag()
			if closeIdx == -1 {
				// No closing tag yet — flush everything except last 7 chars
				// ("</file" is 7 chars) to file content buffer as a safe boundary.
				if len(p.buffer) > 7 {
					safe := p.buffer[:len(p.buffer)-7]
					p.fileContent.WriteString(safe)
					p.buffer = p.buffer[len(p.buffer)-7:]
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
				content = strings.ReplaceAll(content, `<\/file>`, `</file>`)
				p.OnFile(p.currentPath, content)
			}
			p.fileContent.Reset()
			p.inFileBlock = false
			p.currentPath = ""
			p.buffer = p.buffer[closeIdx+len("</file>"):]
			// Continue loop to process remaining buffer
		} else {
			// Check for <delete path="..."/> tags first
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

			// Look for opening <file path="..."> tag
			loc := fileTagRegex.FindStringIndex(p.buffer)
			if loc == nil {
				// No opening tag found — emit safe text.
				// Keep everything from the last '<' in the buffer since it could
				// be the start of a partial <file ...> tag forming across chunks.
				// The tag can be 50+ chars (e.g. <file path="components/ecommerce/checkout-form.tsx">),
				// so a fixed buffer size is not safe.
				lastLT := strings.LastIndex(p.buffer, "<")
				if lastLT >= 0 {
					tail := p.buffer[lastLT:]
					// If the tail starts with "<" but is long enough that we can tell
					// it's NOT going to be "<file ...", flush it as text to avoid
					// indefinite buffering (e.g. "a < b" in explanatory text).
					isPartialFileTag := strings.HasPrefix(tail, "<f") ||
						strings.HasPrefix(tail, "<F") ||
						tail == "<"
					// B68: If the tail looks like a partial file tag but is
					// longer than any valid <file path="..."> or
					// <delete path="..."/> opening could be, it is not a
					// partial tag — flush it to avoid stalling the parser.
					if isPartialFileTag && len(tail) > 100 {
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
				content = strings.ReplaceAll(content, `<\/file>`, `</file>`)
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

// findRealCloseTag returns the index of the real </file> closing tag in p.buffer,
// skipping false positives that appear inside string literals. Returns -1 if no
// real closing tag is found.
//
// Heuristic: a </file> that appears on the same line as an unclosed quote
// character (", ', `) is likely inside a string literal and is skipped.
func (p *FileStreamParser) findRealCloseTag() int {
	searchFrom := 0
	for {
		idx := strings.Index(p.buffer[searchFrom:], "</file>")
		if idx == -1 {
			return -1
		}
		idx += searchFrom // absolute index

		// Check if this </file> is inside a string literal by examining the
		// content from the last newline to the tag position. If there's an
		// odd number of quote characters, the tag is likely inside a string.
		lineStart := strings.LastIndex(p.buffer[:idx], "\n")
		if lineStart == -1 {
			lineStart = 0
		} else {
			lineStart++ // skip the newline itself
		}
		linePrefix := p.buffer[lineStart:idx]

		// Also skip the escaped form <\/file>
		if idx > 0 && p.buffer[idx-1] == '\\' {
			searchFrom = idx + len("</file>")
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
			slog.Debug("FileStreamParser: skipping </file> inside string literal",
				"path", p.currentPath, "line_prefix_len", len(linePrefix))
			searchFrom = idx + len("</file>")
			continue
		}

		return idx
	}
}
