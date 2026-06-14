package agent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	rawFileBlockStart = "BEGIN_WEMOB_FILE"
	rawFileBlockEnd   = "END_WEMOB_FILE"
)

var rawFileBlockHeaderRE = regexp.MustCompile(`^BEGIN_WEMOB_FILE\s+path=([^\s]+)\s*$`)

type rawFileBlock struct {
	Path    string
	Content string
}

func applyRawFileBlocks(rc *RunContext, text string) (int, error) {
	blocks, err := parseRawFileBlocks(text)
	if err != nil || len(blocks) == 0 {
		return 0, err
	}
	if rc == nil || rc.FileMap == nil {
		return 0, fmt.Errorf("raw file blocks cannot be applied without run file map")
	}

	plans := make([]fileWritePlan, 0, len(blocks))
	seen := make(map[string]int, len(blocks))
	batchID := ""
	if len(blocks) > 1 {
		batchID = uuid.NewString()
	}
	for i, block := range blocks {
		plan, err := planRawFileBlockWrite(rc, block, batchID, i+1, len(blocks))
		if err != nil {
			return 0, err
		}
		if firstIndex, ok := seen[plan.Path]; ok {
			return 0, fmt.Errorf("%s: duplicate raw file block also appears at block %d", plan.Path, firstIndex+1)
		}
		seen[plan.Path] = i
		plans = append(plans, *plan)
	}

	for _, plan := range plans {
		commitFileWrite(rc, plan)
	}
	return len(plans), nil
}

func planRawFileBlockWrite(rc *RunContext, block rawFileBlock, batchID string, batchIndex, batchTotal int) (*fileWritePlan, error) {
	path, err := sanitizePath(block.Path)
	if err != nil {
		return nil, err
	}
	if _, exists := rc.FileMap.Get(path); exists {
		return nil, fmt.Errorf("raw file block target already exists: %s — use multi_edit or patch_files for existing files", path)
	}
	plan, msg := planFileWrite(rc, path, block.Content, false, "", batchID, batchIndex, batchTotal)
	if msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}
	return plan, nil
}

func parseRawFileBlocks(text string) ([]rawFileBlock, error) {
	var blocks []rawFileBlock
	searchFrom := 0
	for {
		start := strings.Index(text[searchFrom:], rawFileBlockStart)
		if start < 0 {
			return blocks, nil
		}
		start += searchFrom
		headerEndRel := strings.IndexByte(text[start:], '\n')
		if headerEndRel < 0 {
			return nil, fmt.Errorf("raw file block missing newline after header")
		}
		headerEnd := start + headerEndRel
		header := strings.TrimSpace(text[start:headerEnd])
		matches := rawFileBlockHeaderRE.FindStringSubmatch(header)
		if matches == nil {
			return nil, fmt.Errorf("invalid raw file block header: %s", header)
		}

		contentStart := headerEnd + 1
		endRel := strings.Index(text[contentStart:], rawFileBlockEnd)
		if endRel < 0 {
			return nil, fmt.Errorf("raw file block for %s missing %s", matches[1], rawFileBlockEnd)
		}
		contentEnd := contentStart + endRel
		content := text[contentStart:contentEnd]
		blocks = append(blocks, rawFileBlock{Path: matches[1], Content: content})

		searchFrom = contentEnd + len(rawFileBlockEnd)
	}
}

func stripRawFileBlocksFromText(text string, written int) string {
	if written <= 0 || !strings.Contains(text, rawFileBlockStart) {
		return text
	}
	blocks, err := parseRawFileBlocks(text)
	if err != nil || len(blocks) == 0 {
		return text
	}
	stripped := text
	for _, block := range blocks {
		header := fmt.Sprintf("%s path=%s", rawFileBlockStart, block.Path)
		start := strings.Index(stripped, header)
		if start < 0 {
			continue
		}
		endRel := strings.Index(stripped[start:], rawFileBlockEnd)
		if endRel < 0 {
			continue
		}
		end := start + endRel + len(rawFileBlockEnd)
		replacement := fmt.Sprintf("Created %s (%d bytes) via raw file block.", block.Path, len(block.Content))
		stripped = stripped[:start] + replacement + stripped[end:]
	}
	return strings.TrimSpace(stripped)
}
