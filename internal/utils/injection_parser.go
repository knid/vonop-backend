package utils

import (
	"regexp"
	"strings"
)

// injectionOpenRegex matches <injection name="..." action="..."> opening tags.
var injectionOpenRegex = regexp.MustCompile(`<injection\s+name="([^"]+)"\s+action="([^"]+)"\s*>`)

// deleteInjectionRegex matches <delete-injection name="..."/> self-closing tags.
var deleteInjectionRegex = regexp.MustCompile(`<delete-injection\s+name="([^"]+)"\s*/>`)

// cssBlockRegex matches <css>...</css> blocks.
var cssBlockRegex = regexp.MustCompile(`(?s)<css>(.*?)</css>`)

// jsBlockRegex matches <js>...</js> blocks.
var jsBlockRegex = regexp.MustCompile(`(?s)<js>(.*?)</js>`)

// htmlBlockRegex matches <html>(.*?)</html> blocks inside injection.
var htmlBlockRegex = regexp.MustCompile(`(?s)<html>(.*?)</html>`)

// InjectionStreamParser parses `<injection name="..." action="...">` blocks
// with `<css>`, `<js>`, `<html>` sub-blocks and `<delete-injection name="..."/>`
// tags from a text stream incrementally.
type InjectionStreamParser struct {
	buffer           string
	inInjectionBlock bool
	currentName      string
	currentAction    string
	injectionContent strings.Builder

	// OnInjection is called when a complete </injection> closing tag is found.
	OnInjection func(name, action, css, js, html string)
	// OnDeleteInjection is called when a <delete-injection name="..."/> tag is found.
	OnDeleteInjection func(name string)
	// OnText is called for text outside of injection blocks.
	OnText func(text string)
}

// Feed processes an incremental chunk of streamed text.
func (p *InjectionStreamParser) Feed(chunk string) {
	p.buffer += chunk

	for {
		if p.inInjectionBlock {
			closeIdx := strings.Index(p.buffer, "</injection>")
			if closeIdx == -1 {
				// No closing tag yet — flush safe portion to content buffer.
				// "</injection" is 12 chars.
				if len(p.buffer) > 12 {
					safe := p.buffer[:len(p.buffer)-12]
					p.injectionContent.WriteString(safe)
					p.buffer = p.buffer[len(p.buffer)-12:]
				}
				return
			}

			// Found closing tag — extract sub-blocks and emit
			p.injectionContent.WriteString(p.buffer[:closeIdx])
			content := p.injectionContent.String()

			var css, js, html string
			if m := cssBlockRegex.FindStringSubmatch(content); len(m) >= 2 {
				css = strings.TrimSpace(m[1])
			}
			if m := jsBlockRegex.FindStringSubmatch(content); len(m) >= 2 {
				js = strings.TrimSpace(m[1])
			}
			if m := htmlBlockRegex.FindStringSubmatch(content); len(m) >= 2 {
				html = strings.TrimSpace(m[1])
			}

			if p.OnInjection != nil {
				p.OnInjection(p.currentName, p.currentAction, css, js, html)
			}

			p.injectionContent.Reset()
			p.inInjectionBlock = false
			p.currentName = ""
			p.currentAction = ""
			p.buffer = p.buffer[closeIdx+len("</injection>"):]
			// Continue loop to process remaining buffer
		} else {
			// Check for <delete-injection name="..."/> tags first
			if delLoc := deleteInjectionRegex.FindStringIndex(p.buffer); delLoc != nil {
				if delLoc[0] > 0 {
					before := p.buffer[:delLoc[0]]
					if p.OnText != nil && before != "" {
						p.OnText(before)
					}
				}
				match := deleteInjectionRegex.FindStringSubmatch(p.buffer[delLoc[0]:delLoc[1]])
				if len(match) >= 2 && p.OnDeleteInjection != nil {
					p.OnDeleteInjection(match[1])
				}
				p.buffer = p.buffer[delLoc[1]:]
				continue
			}

			// Look for opening <injection name="..." action="..."> tag
			loc := injectionOpenRegex.FindStringIndex(p.buffer)
			if loc == nil {
				// No opening tag found — emit safe text, keeping potential partial tag.
				lastLT := strings.LastIndex(p.buffer, "<")
				if lastLT >= 0 {
					tail := p.buffer[lastLT:]
					isPartialTag := strings.HasPrefix(tail, "<i") ||
						strings.HasPrefix(tail, "<I") ||
						strings.HasPrefix(tail, "<d") ||
						strings.HasPrefix(tail, "<D") ||
						tail == "<"
					if !isPartialTag {
						if p.OnText != nil {
							p.OnText(p.buffer)
						}
						p.buffer = ""
						return
					}
					if lastLT > 0 {
						if p.OnText != nil {
							p.OnText(p.buffer[:lastLT])
						}
						p.buffer = p.buffer[lastLT:]
					}
				} else {
					if p.OnText != nil && p.buffer != "" {
						p.OnText(p.buffer)
					}
					p.buffer = ""
				}
				return
			}

			// Emit text before the opening tag
			if loc[0] > 0 {
				before := p.buffer[:loc[0]]
				if p.OnText != nil && before != "" {
					p.OnText(before)
				}
			}

			// Extract name and action from the tag
			match := injectionOpenRegex.FindStringSubmatch(p.buffer[loc[0]:loc[1]])
			if len(match) < 3 {
				return
			}
			p.currentName = match[1]
			p.currentAction = match[2]
			p.inInjectionBlock = true
			p.buffer = p.buffer[loc[1]:]
		}
	}
}

// Flush emits any remaining buffered content when the stream ends.
func (p *InjectionStreamParser) Flush() {
	if p.inInjectionBlock {
		// Stream ended mid-injection — discard incomplete block
		p.injectionContent.Reset()
		p.inInjectionBlock = false
		p.currentName = ""
		p.currentAction = ""
	} else if p.buffer != "" {
		if p.OnText != nil {
			p.OnText(p.buffer)
		}
	}
	p.buffer = ""
}
