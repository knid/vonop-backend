package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)

const (
	KeepLastMessages               = 15
	KeepLastToolResults            = 5
	KeepLastTruncatedToolResults   = 10
	KeepLastSummarizedToolResults  = 10
	TrimmedToolResultChars         = 4000
	TrimmedToolArgChars            = 1200
	SummarizedToolResultPreviewLen = 240
)

type HistoryWindowConfig struct {
	KeepLastMessages               int
	KeepLastToolResults            int
	KeepLastTruncatedToolResults   int
	KeepLastSummarizedToolResults  int
	TrimmedToolResultChars         int
	TrimmedToolArgChars            int
	SummarizedToolResultPreviewLen int
}

func defaultHistoryWindowConfig() HistoryWindowConfig {
	return HistoryWindowConfig{
		KeepLastMessages:               KeepLastMessages,
		KeepLastToolResults:            KeepLastToolResults,
		KeepLastTruncatedToolResults:   KeepLastTruncatedToolResults,
		KeepLastSummarizedToolResults:  KeepLastSummarizedToolResults,
		TrimmedToolResultChars:         TrimmedToolResultChars,
		TrimmedToolArgChars:            TrimmedToolArgChars,
		SummarizedToolResultPreviewLen: SummarizedToolResultPreviewLen,
	}
}

func historyWindowConfigForRun(isFirstGen bool, rc *remoteconfig.Service) HistoryWindowConfig {
	cfg := defaultHistoryWindowConfig()
	if rc == nil {
		return cfg
	}
	messageKey, fullToolKey, truncatedToolKey, summarizedToolKey := historyWindowKeysForRun(isFirstGen)
	cfg.KeepLastMessages = positiveOrDefault(rc.GetInt(messageKey, remoteconfig.Global), cfg.KeepLastMessages)
	cfg.KeepLastToolResults = positiveOrDefault(rc.GetInt(fullToolKey, remoteconfig.Global), cfg.KeepLastToolResults)
	cfg.KeepLastTruncatedToolResults = positiveOrDefault(rc.GetInt(truncatedToolKey, remoteconfig.Global), cfg.KeepLastTruncatedToolResults)
	cfg.KeepLastSummarizedToolResults = positiveOrDefault(rc.GetInt(summarizedToolKey, remoteconfig.Global), cfg.KeepLastSummarizedToolResults)
	cfg.TrimmedToolResultChars = positiveOrDefault(rc.GetInt(remoteconfig.AITrimToolResultChars, remoteconfig.Global), cfg.TrimmedToolResultChars)
	cfg.TrimmedToolArgChars = positiveOrDefault(rc.GetInt(remoteconfig.AITrimToolArgChars, remoteconfig.Global), cfg.TrimmedToolArgChars)
	return cfg
}

func historyWindowKeysForRun(isFirstGen bool) (remoteconfig.Key, remoteconfig.Key, remoteconfig.Key, remoteconfig.Key) {
	return remoteconfig.AIKeepLastMessages,
		remoteconfig.AIKeepLastToolResults,
		remoteconfig.AIKeepLastTruncatedToolResults,
		remoteconfig.AIKeepLastSummarizedToolResults
}

func liveToolWindowKeysForRun(isFirstGen bool) (remoteconfig.Key, remoteconfig.Key, remoteconfig.Key) {
	return remoteconfig.AIProcessKeepLastToolResults,
		remoteconfig.AIProcessKeepLastTruncatedToolResults,
		remoteconfig.AIProcessKeepLastSummarizedToolResults
}

func liveToolWindowConfigForRun(isFirstGen bool, historyCfg HistoryWindowConfig, rc *remoteconfig.Service) HistoryWindowConfig {
	cfg := historyCfg
	if rc == nil {
		return cfg
	}
	fullToolKey, truncatedToolKey, summarizedToolKey := liveToolWindowKeysForRun(isFirstGen)
	cfg.KeepLastToolResults = positiveOrDefault(rc.GetInt(fullToolKey, remoteconfig.Global), cfg.KeepLastToolResults)
	cfg.KeepLastTruncatedToolResults = positiveOrDefault(rc.GetInt(truncatedToolKey, remoteconfig.Global), cfg.KeepLastTruncatedToolResults)
	cfg.KeepLastSummarizedToolResults = positiveOrDefault(rc.GetInt(summarizedToolKey, remoteconfig.Global), cfg.KeepLastSummarizedToolResults)
	return cfg
}

func positiveOrDefault(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func historyToolMessageLimitForRun(cfg HistoryWindowConfig) int {
	limit := cfg.KeepLastToolResults + cfg.KeepLastTruncatedToolResults + cfg.KeepLastSummarizedToolResults
	if limit < 0 {
		return 0
	}
	return limit
}

// partsToContent flattens an ordered parts slice into the legacy plain-text
// content string by concatenating every text part in order.
func partsToContent(parts []models.ChatMessagePart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// partsToToolCalls projects the tool_call parts into the legacy
// ChatMessageToolCall slice readers still expect.
func partsToToolCalls(parts []models.ChatMessagePart) []models.ChatMessageToolCall {
	out := make([]models.ChatMessageToolCall, 0, len(parts))
	for _, p := range parts {
		if p.Type != "tool_call" {
			continue
		}
		out = append(out, models.ChatMessageToolCall{
			ID:         p.ToolCallID,
			Name:       p.Name,
			Input:      p.Input,
			Result:     p.Result,
			IsError:    p.IsError,
			DurationMs: p.DurationMs,
		})
	}
	return out
}

func convertHistoryForRun(history []models.AppChatMessage, cfg HistoryWindowConfig) []utils.Message {
	return convertHistoryWithWindow(history, cfg)
}

func convertHistory(history []models.AppChatMessage) []utils.Message {
	cfg := HistoryWindowConfig{
		KeepLastMessages:               0,
		KeepLastToolResults:            int(^uint(0) >> 1),
		KeepLastTruncatedToolResults:   0,
		KeepLastSummarizedToolResults:  0,
		TrimmedToolResultChars:         TrimmedToolResultChars,
		TrimmedToolArgChars:            TrimmedToolArgChars,
		SummarizedToolResultPreviewLen: SummarizedToolResultPreviewLen,
	}
	return convertHistoryWithWindow(history, cfg)
}

func convertHistoryWithToolLimit(history []models.AppChatMessage, maxToolResults int) []utils.Message {
	cfg := HistoryWindowConfig{
		KeepLastMessages:               0,
		KeepLastToolResults:            maxToolResults,
		KeepLastTruncatedToolResults:   0,
		KeepLastSummarizedToolResults:  0,
		TrimmedToolResultChars:         TrimmedToolResultChars,
		TrimmedToolArgChars:            TrimmedToolArgChars,
		SummarizedToolResultPreviewLen: SummarizedToolResultPreviewLen,
	}
	if maxToolResults < 0 {
		cfg.KeepLastToolResults = int(^uint(0) >> 1)
	}
	return convertHistoryWithWindow(history, cfg)
}

func convertHistoryWithWindow(history []models.AppChatMessage, cfg HistoryWindowConfig) []utils.Message {
	var out []utils.Message
	toolModes := toolHistoryModes(history, cfg)
	toolIndex := 0
	keepTranscript := transcriptHistoryKeepMask(history, cfg.KeepLastMessages)
	for i, m := range history {
		if m.ErrorKind != "" {
			reason := strings.TrimSpace(m.Content)
			if reason == "" {
				reason = humanizeErrorKind(m.ErrorKind)
			}
			out = append(out, utils.Message{
				Role:    "user",
				Content: fmt.Sprintf("The previous assistant generation failed with: %s. Please continue from where it left off.", reason),
			})
			continue
		}

		content := m.Content
		if m.Role == "user" {
			if !keepTranscript[i] {
				continue
			}
			content, _, _ = ExpandSlashCommand(content)
			content = "<user_message>" + escapeUntrustedTag(content, "user_message") + "</user_message>"
			out = append(out, utils.Message{Role: m.Role, Content: content})
			continue
		}

		if m.Role == "assistant" {
			var parts []models.ChatMessagePart
			if len(m.Parts) > 0 && string(m.Parts) != "[]" {
				_ = json.Unmarshal(m.Parts, &parts)
			}
			if len(parts) == 0 {
				if keepTranscript[i] {
					out = append(out, utils.Message{Role: "assistant", Content: content})
				}
				continue
			}
			msgs := convertAssistantPartsWithToolWindow(parts, toolModes, &toolIndex, keepTranscript[i], cfg)
			out = append(out, msgs...)
			continue
		}

		if keepTranscript[i] {
			out = append(out, utils.Message{Role: m.Role, Content: content})
		}
	}
	return out
}

func transcriptHistoryKeepMask(history []models.AppChatMessage, limit int) map[int]bool {
	keep := map[int]bool{}
	if limit == 0 {
		for i := range history {
			keep[i] = true
		}
		return keep
	}
	if limit < 0 {
		return keep
	}
	remaining := limit
	for i := len(history) - 1; i >= 0 && remaining > 0; i-- {
		switch history[i].Role {
		case "user", "assistant":
			keep[i] = true
			remaining--
		}
	}
	return keep
}

func countAssistantToolParts(history []models.AppChatMessage) int {
	count := 0
	for _, m := range history {
		if m.Role != "assistant" || len(m.Parts) == 0 || string(m.Parts) == "[]" {
			continue
		}
		var parts []models.ChatMessagePart
		if err := json.Unmarshal(m.Parts, &parts); err != nil {
			continue
		}
		for _, p := range parts {
			if p.Type == "tool_call" {
				count++
			}
		}
	}
	return count
}

func convertAssistantParts(parts []models.ChatMessagePart) []utils.Message {
	return convertAssistantPartsWithToolWindow(parts, nil, nil, true, defaultHistoryWindowConfig())
}

func convertAssistantPartsWithToolLimit(parts []models.ChatMessagePart, skipToolResults *int) ([]utils.Message, int) {
	if skipToolResults == nil {
		return convertAssistantParts(parts), 0
	}
	total := 0
	for _, p := range parts {
		if p.Type == "tool_call" {
			total++
		}
	}
	modes := make([]toolHistoryMode, total)
	for i := range modes {
		if *skipToolResults > 0 {
			modes[i] = toolHistoryDrop
			*skipToolResults--
			continue
		}
		modes[i] = toolHistoryFull
	}
	idx := 0
	return convertAssistantPartsWithToolWindow(parts, modes, &idx, true, defaultHistoryWindowConfig()), countToolMode(modes, toolHistoryDrop)
}

type toolHistoryMode int

const (
	toolHistoryDrop toolHistoryMode = iota
	toolHistorySummarized
	toolHistoryTruncated
	toolHistoryFull
)

func toolHistoryModes(history []models.AppChatMessage, cfg HistoryWindowConfig) []toolHistoryMode {
	total := countAssistantToolParts(history)
	modes := make([]toolHistoryMode, total)
	full := cfg.KeepLastToolResults
	if full < 0 {
		full = 0
	}
	truncated := cfg.KeepLastTruncatedToolResults
	if truncated < 0 {
		truncated = 0
	}
	summarized := cfg.KeepLastSummarizedToolResults
	if summarized < 0 {
		summarized = 0
	}
	fullStart := total - full
	if fullStart < 0 {
		fullStart = 0
	}
	truncatedStart := fullStart - truncated
	if truncatedStart < 0 {
		truncatedStart = 0
	}
	summarizedStart := truncatedStart - summarized
	if summarizedStart < 0 {
		summarizedStart = 0
	}
	for i := range modes {
		switch {
		case i >= fullStart:
			modes[i] = toolHistoryFull
		case i >= truncatedStart:
			modes[i] = toolHistoryTruncated
		case i >= summarizedStart:
			modes[i] = toolHistorySummarized
		default:
			modes[i] = toolHistoryDrop
		}
	}
	return modes
}

func countToolMode(modes []toolHistoryMode, want toolHistoryMode) int {
	n := 0
	for _, mode := range modes {
		if mode == want {
			n++
		}
	}
	return n
}

func convertAssistantPartsWithToolWindow(parts []models.ChatMessagePart, modes []toolHistoryMode, toolIndex *int, includeText bool, cfg HistoryWindowConfig) []utils.Message {
	var out []utils.Message
	var pendingText string
	var pendingToolCalls []utils.ToolCall
	var pendingToolResults []utils.Message

	flush := func() {
		if pendingText == "" && len(pendingToolCalls) == 0 {
			return
		}
		out = append(out, utils.Message{
			Role:      "assistant",
			Content:   pendingText,
			ToolCalls: pendingToolCalls,
		})
		out = append(out, pendingToolResults...)
		pendingText = ""
		pendingToolCalls = nil
		pendingToolResults = nil
	}

	for _, p := range parts {
		switch p.Type {
		case "reasoning":
			continue
		case "text":
			if !includeText {
				continue
			}
			if pendingText != "" || len(pendingToolCalls) > 0 {
				flush()
			}
			pendingText = p.Text
		case "tool_call":
			mode := toolHistoryFull
			if toolIndex != nil {
				if *toolIndex < len(modes) {
					mode = modes[*toolIndex]
				}
				*toolIndex++
			}
			if mode == toolHistoryDrop {
				continue
			}
			args := string(p.Input)
			result := p.Result
			if mode == toolHistoryTruncated {
				args = trimmedToolArgs(p, cfg)
				result = trimmedToolResult(p, cfg)
			}
			if mode == toolHistorySummarized {
				args = summarizedToolArgs(p)
				result = summarizedToolResult(p, cfg)
			}
			pendingToolCalls = append(pendingToolCalls, utils.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: &utils.FunctionCall{
					Name:      p.Name,
					Arguments: args,
				},
			})
			pendingToolResults = append(pendingToolResults, utils.Message{
				Role:       "tool",
				ToolCallID: p.ToolCallID,
				Name:       p.Name,
				Content:    result,
			})
		}
	}
	flush()
	return out
}

func trimmedToolArgs(p models.ChatMessagePart, cfg HistoryWindowConfig) string {
	return trimToolArgsForLLM(p.Name, string(p.Input), cfg.TrimmedToolArgChars)
}

func trimmedToolResult(p models.ChatMessagePart, cfg HistoryWindowConfig) string {
	result := trimToolResultForLLM(p.Name, p.Result, cfg.TrimmedToolResultChars)
	if result == strings.TrimSpace(p.Result) {
		return result
	}
	return strings.Join([]string{
		fmt.Sprintf("Tool result truncated: %s", p.Name),
		fmt.Sprintf("error: %t", p.IsError),
		fmt.Sprintf("result_bytes: %d", len(p.Result)),
		"",
		result,
	}, "\n")
}

func summarizedToolArgs(p models.ChatMessagePart) string {
	payload := map[string]any{
		"summary":            "metadata only for older tool call args",
		"original_arg_bytes": len(p.Input),
	}
	if paths := extractToolPaths(string(p.Input)); len(paths) > 0 {
		payload["paths"] = paths
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return `{"summary":"metadata only for older tool call args"}`
	}
	return string(raw)
}

func summarizedToolResult(p models.ChatMessagePart, cfg HistoryWindowConfig) string {
	paths := extractToolPaths(string(p.Input))
	parts := []string{
		fmt.Sprintf("Tool result summary: %s", p.Name),
		fmt.Sprintf("error: %t", p.IsError),
		fmt.Sprintf("args_bytes: %d", len(p.Input)),
		fmt.Sprintf("result_bytes: %d", len(p.Result)),
	}
	if len(paths) > 0 {
		parts = append(parts, "paths: "+strings.Join(paths, ", "))
	}
	previewLen := cfg.SummarizedToolResultPreviewLen
	if previewLen <= 0 {
		previewLen = SummarizedToolResultPreviewLen
	}
	if preview := firstLine(p.Result, previewLen); preview != "" {
		parts = append(parts, "preview: "+preview)
	}
	return strings.Join(parts, "\n")
}

// lastAssistantTurn returns the most recent non-error assistant message in
// history, or nil if there isn't one. Kept for test compatibility.
func lastAssistantTurn(history []models.AppChatMessage) *models.AppChatMessage {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role == "assistant" && m.ErrorKind == "" {
			return &m
		}
	}
	return nil
}

func escapeUntrustedTag(content, tag string) string {
	closing := "</" + tag + ">"
	escaped := "&lt;/" + tag + "&gt;"
	return strings.ReplaceAll(content, closing, escaped)
}
