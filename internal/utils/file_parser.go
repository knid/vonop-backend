package utils

import (
	"regexp"
	"strings"
)

// fileTagRegex matches <file path="..."> opening tags.
var fileTagRegex = regexp.MustCompile(`<file\s+path="([^"]+)"\s*>`)

// deleteTagRegex matches <delete path="..."/> self-closing tags.
var deleteTagRegex = regexp.MustCompile(`<delete\s+path="([^"]+)"\s*/>`)

// FileStreamParser parses `<file path="...">content</file>` blocks from a
// text stream incrementally. Non-file text is emitted via OnText; completed
// file blocks are emitted via OnFile.
type FileStreamParser struct {
	buffer      string
	inFileBlock bool
	currentPath string
	fileContent strings.Builder

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
			// Look for closing </file> tag
			closeIdx := strings.Index(p.buffer, "</file>")
			if closeIdx == -1 {
				// No closing tag yet — flush everything except last 7 chars
				// ("</file" is 7 chars) to file content buffer as a safe boundary.
				if len(p.buffer) > 7 {
					safe := p.buffer[:len(p.buffer)-7]
					p.fileContent.WriteString(safe)
					p.buffer = p.buffer[len(p.buffer)-7:]
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
		// Stream ended mid-file — emit what we have
		p.fileContent.WriteString(p.buffer)
		if p.OnFile != nil {
			content := p.fileContent.String()
			content = strings.TrimPrefix(content, "\n")
			content = strings.TrimSuffix(content, "\n")
			p.OnFile(p.currentPath, content)
		}
		p.fileContent.Reset()
		p.inFileBlock = false
		p.currentPath = ""
	} else if p.buffer != "" {
		if p.OnText != nil {
			p.OnText(p.buffer)
		}
	}
	p.buffer = ""
}
