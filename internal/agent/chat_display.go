package agent

import (
	"encoding/json"

	"github.com/knid/wemob/internal/models"
)

// SanitizeChatMessagesForDisplay returns copies of chat messages with tool
// inputs/results reduced to UI-safe previews. The database keeps full tool
// payloads so convertHistory can reconstruct LLM context; API responses should
// not expose those raw payloads to chat clients.
func SanitizeChatMessagesForDisplay(msgs []models.AppChatMessage) []models.AppChatMessage {
	out := make([]models.AppChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		out[i].Parts = sanitizeChatPartsForDisplay(out[i].Parts)
		out[i].ToolCalls = sanitizeLegacyToolCallsForDisplay(out[i].ToolCalls)
	}
	return out
}

func sanitizeChatPartsForDisplay(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "[]" {
		return raw
	}
	var parts []models.ChatMessagePart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return raw
	}
	for i := range parts {
		if parts[i].Type != "tool_call" {
			continue
		}
		if len(parts[i].InputPreview) == 0 && len(parts[i].Input) > 0 {
			parts[i].InputPreview = stripToolInputForPreview(parts[i].Name, string(parts[i].Input))
		}
		if parts[i].ResultPreview == "" {
			parts[i].ResultPreview = firstLine(parts[i].Result, 140)
		}
		parts[i].Input = parts[i].InputPreview
		parts[i].Result = parts[i].ResultPreview
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return raw
	}
	return encoded
}

func sanitizeLegacyToolCallsForDisplay(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "[]" {
		return raw
	}
	var calls []models.ChatMessageToolCall
	if err := json.Unmarshal(raw, &calls); err != nil {
		return raw
	}
	for i := range calls {
		calls[i].Input = stripToolInputForPreview(calls[i].Name, string(calls[i].Input))
		calls[i].Result = firstLine(calls[i].Result, 140)
	}
	encoded, err := json.Marshal(calls)
	if err != nil {
		return raw
	}
	return encoded
}
