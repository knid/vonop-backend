package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

const (
	editHistoryMessageLimit        = 6
	firstGenHistoryMessageLimit    = 10
	editHistoryToolResultLimit     = 4
	firstGenHistoryToolResultLimit = 8
)

func historyLimitForRun(isFirstGen bool) int {
	if isFirstGen {
		return firstGenHistoryMessageLimit
	}
	return editHistoryMessageLimit
}

func historicalToolResultLimitForRun(isFirstGen bool) int {
	if isFirstGen {
		return firstGenHistoryToolResultLimit
	}
	return editHistoryToolResultLimit
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

func convertHistoryForRun(history []models.AppChatMessage, isFirstGen bool) []utils.Message {
	limit := historyLimitForRun(isFirstGen)
	if limit > 0 && len(history) > limit {
		history = history[len(history)-limit:]
	}
	return convertHistoryWithToolLimit(history, historicalToolResultLimitForRun(isFirstGen))
}

func convertHistory(history []models.AppChatMessage) []utils.Message {
	return convertHistoryWithToolLimit(history, -1)
}

func convertHistoryWithToolLimit(history []models.AppChatMessage, maxToolResults int) []utils.Message {
	var out []utils.Message
	skipToolResults := 0
	if maxToolResults >= 0 {
		totalToolResults := countAssistantToolParts(history)
		if totalToolResults > maxToolResults {
			skipToolResults = totalToolResults - maxToolResults
		}
	}
	for _, m := range history {
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
				out = append(out, utils.Message{Role: "assistant", Content: content})
				continue
			}
			msgs, _ := convertAssistantPartsWithToolLimit(parts, &skipToolResults)
			out = append(out, msgs...)
			continue
		}

		out = append(out, utils.Message{Role: m.Role, Content: content})
	}
	return out
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
	msgs, _ := convertAssistantPartsWithToolLimit(parts, nil)
	return msgs
}

func convertAssistantPartsWithToolLimit(parts []models.ChatMessagePart, skipToolResults *int) ([]utils.Message, int) {
	var out []utils.Message
	var pendingText string
	var pendingToolCalls []utils.ToolCall
	var pendingToolResults []utils.Message
	skippedToolResults := 0

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
			if pendingText != "" || len(pendingToolCalls) > 0 {
				flush()
			}
			pendingText = p.Text
		case "tool_call":
			if skipToolResults != nil && *skipToolResults > 0 {
				*skipToolResults--
				skippedToolResults++
				continue
			}
			pendingToolCalls = append(pendingToolCalls, utils.ToolCall{
				ID:   p.ToolCallID,
				Type: "function",
				Function: &utils.FunctionCall{
					Name:      p.Name,
					Arguments: string(p.Input),
				},
			})
			pendingToolResults = append(pendingToolResults, utils.Message{
				Role:       "tool",
				ToolCallID: p.ToolCallID,
				Name:       p.Name,
				Content:    p.Result,
			})
		}
	}
	flush()
	return out, skippedToolResults
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
