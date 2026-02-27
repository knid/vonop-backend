package utils

import (
	"regexp"
	"strings"
)

// fileTagRegex matches <file path="..."> opening tags.
var fileTagRegex = regexp.MustCompile(`<file\s+path="([^"]+)"\s*>`)

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
				p.OnFile(p.currentPath, p.fileContent.String())
			}
			p.fileContent.Reset()
			p.inFileBlock = false
			p.currentPath = ""
			p.buffer = p.buffer[closeIdx+len("</file>"):]
			// Continue loop to process remaining buffer
		} else {
			// Look for opening <file path="..."> tag
			loc := fileTagRegex.FindStringIndex(p.buffer)
			if loc == nil {
				// No opening tag found — emit text up to a safe boundary.
				// Keep last 20 chars in buffer in case a partial tag is forming.
				if len(p.buffer) > 20 {
					safe := p.buffer[:len(p.buffer)-20]
					if p.OnText != nil && safe != "" {
						p.OnText(safe)
					}
					p.buffer = p.buffer[len(p.buffer)-20:]
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
			p.OnFile(p.currentPath, p.fileContent.String())
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
