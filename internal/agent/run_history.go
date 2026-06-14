package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

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

func convertHistory(history []models.AppChatMessage) []utils.Message {
	var out []utils.Message
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
			out = append(out, convertAssistantParts(parts)...)
			continue
		}

		out = append(out, utils.Message{Role: m.Role, Content: content})
	}
	return out
}

func convertAssistantParts(parts []models.ChatMessagePart) []utils.Message {
	var out []utils.Message
	var pendingText string
	var pendingReasoning string
	var pendingToolCalls []utils.ToolCall
	var pendingToolResults []utils.Message

	flush := func() {
		if pendingText == "" && pendingReasoning == "" && len(pendingToolCalls) == 0 {
			return
		}
		out = append(out, utils.Message{
			Role:             "assistant",
			Content:          pendingText,
			ReasoningContent: pendingReasoning,
			ToolCalls:        pendingToolCalls,
		})
		out = append(out, pendingToolResults...)
		pendingText = ""
		pendingReasoning = ""
		pendingToolCalls = nil
		pendingToolResults = nil
	}

	for _, p := range parts {
		switch p.Type {
		case "reasoning":
			if len(pendingToolCalls) > 0 {
				flush()
			}
			pendingReasoning += p.Text
		case "text":
			if pendingText != "" || len(pendingToolCalls) > 0 {
				flush()
			}
			pendingText = p.Text
		case "tool_call":
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
	return out
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
