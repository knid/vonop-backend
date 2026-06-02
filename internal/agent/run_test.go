package agent

import (
	"encoding/json"
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertHistory_UserMessage(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "user", Content: "hello world"},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "user", out[0].Role)
	assert.Contains(t, out[0].Content, "hello world")
	assert.Contains(t, out[0].Content, "<user_message>")
}

func TestConvertHistory_AssistantErrorKind(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "oops", ErrorKind: "timeout"},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "user", out[0].Role)
	assert.Contains(t, out[0].Content, "oops")
}

func TestFailureDisplayMessageIncludesWrappedDetail(t *testing.T) {
	got := failureDisplayMessage("LLM call failed", assert.AnError)
	assert.Contains(t, got, "LLM call failed")
	assert.Contains(t, got, assert.AnError.Error())
}

func TestConvertHistory_AssistantLegacyNoParts(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "legacy text", Parts: []byte("[]"), ToolCalls: []byte("[]")},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "assistant", out[0].Role)
	assert.Equal(t, "legacy text", out[0].Content)
	assert.Empty(t, out[0].ToolCalls)
}

func TestConvertHistory_AssistantTextOnly(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: "just text"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "just text", Parts: parts},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "assistant", out[0].Role)
	assert.Equal(t, "just text", out[0].Content)
	assert.Empty(t, out[0].ToolCalls)
}

func TestConvertHistory_AssistantSingleToolRound(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: "Let me read."},
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{"paths":["a.txt"]}`), Result: "content of a"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "Let me read.", Parts: parts},
	}
	out := convertHistory(history)
	require.Len(t, out, 2)

	// Assistant message with tool_calls
	assert.Equal(t, "assistant", out[0].Role)
	assert.Equal(t, "Let me read.", out[0].Content)
	require.Len(t, out[0].ToolCalls, 1)
	assert.Equal(t, "tc1", out[0].ToolCalls[0].ID)
	assert.Equal(t, "function", out[0].ToolCalls[0].Type)
	require.NotNil(t, out[0].ToolCalls[0].Function)
	assert.Equal(t, "read_files", out[0].ToolCalls[0].Function.Name)
	assert.Equal(t, `{"paths":["a.txt"]}`, out[0].ToolCalls[0].Function.Arguments)

	// With basic history, read results are preserved (no drawer truncation).
	assert.Equal(t, "tool", out[1].Role)
	assert.Equal(t, "tc1", out[1].ToolCallID)
	assert.Equal(t, "read_files", out[1].Name)
	assert.Equal(t, "content of a", out[1].Content) // full result kept for this last-turn case
}

func TestConvertHistory_AssistantMultiRound(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: "Round one."},
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "r1"},
		{Type: "text", Text: "Round two."},
		{Type: "tool_call", ToolCallID: "tc2", Name: "edit_file", Input: []byte(`{}`), Result: "r2"},
		{Type: "text", Text: "Final summary."},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "Round one.Round two.Final summary.", Parts: parts},
	}
	out := convertHistory(history)
	require.Len(t, out, 5)

	// Round 1 assistant
	assert.Equal(t, "assistant", out[0].Role)
	assert.Equal(t, "Round one.", out[0].Content)
	require.Len(t, out[0].ToolCalls, 1)
	assert.Equal(t, "tc1", out[0].ToolCalls[0].ID)

	// Round 1 tool result — read_files ALWAYS truncated
	assert.Equal(t, "tool", out[1].Role)
	assert.Equal(t, "tc1", out[1].ToolCallID)
	// basic history: reads are no longer special-cased to drawer message
	assert.NotContains(t, out[1].Content, "CURRENT FILES drawer")

	// Round 2 assistant
	assert.Equal(t, "assistant", out[2].Role)
	assert.Equal(t, "Round two.", out[2].Content)
	require.Len(t, out[2].ToolCalls, 1)
	assert.Equal(t, "tc2", out[2].ToolCalls[0].ID)

	// Round 2 tool result — edit_file on last turn keeps full result
	assert.Equal(t, "tool", out[3].Role)
	assert.Equal(t, "tc2", out[3].ToolCallID)
	assert.Equal(t, "r2", out[3].Content)

	// Final assistant (no tool calls)
	assert.Equal(t, "assistant", out[4].Role)
	assert.Equal(t, "Final summary.", out[4].Content)
	assert.Empty(t, out[4].ToolCalls)
}

func TestConvertHistory_MixedConversation(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: "Done."},
		{Type: "tool_call", ToolCallID: "tc1", Name: "write_file", Input: []byte(`{}`), Result: "ok"},
	})
	history := []models.AppChatMessage{
		{Role: "user", Content: "make it blue"},
		{Role: "assistant", Content: "Done.", Parts: parts},
		{Role: "user", Content: "now red"},
	}
	out := convertHistory(history)
	require.Len(t, out, 4)

	assert.Equal(t, "user", out[0].Role)
	assert.Contains(t, out[0].Content, "make it blue")

	assert.Equal(t, "assistant", out[1].Role)
	assert.Equal(t, "Done.", out[1].Content)
	require.Len(t, out[1].ToolCalls, 1)

	assert.Equal(t, "tool", out[2].Role)
	assert.Equal(t, "tc1", out[2].ToolCallID)

	assert.Equal(t, "user", out[3].Role)
	assert.Contains(t, out[3].Content, "now red")
}

func TestConvertHistory_AssistantMultipleToolsInOneRound(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: "I'll read both."},
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "a"},
		{Type: "tool_call", ToolCallID: "tc2", Name: "read_files", Input: []byte(`{}`), Result: "b"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "I'll read both.", Parts: parts},
	}
	out := convertHistory(history)
	require.Len(t, out, 3)

	assert.Equal(t, "assistant", out[0].Role)
	assert.Equal(t, "I'll read both.", out[0].Content)
	require.Len(t, out[0].ToolCalls, 2)
	assert.Equal(t, "tc1", out[0].ToolCalls[0].ID)
	assert.Equal(t, "tc2", out[0].ToolCalls[1].ID)

	assert.Equal(t, "tool", out[1].Role)
	assert.Equal(t, "tc1", out[1].ToolCallID)
	assert.Equal(t, "tool", out[2].Role)
	assert.Equal(t, "tc2", out[2].ToolCallID)
}

func TestConvertHistory_AssistantOnlyToolsNoText(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "a"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts},
	}
	out := convertHistory(history)
	require.Len(t, out, 2)

	// Assistant message with empty content but tool calls
	assert.Equal(t, "assistant", out[0].Role)
	assert.Empty(t, out[0].Content)
	require.Len(t, out[0].ToolCalls, 1)
	assert.Equal(t, "tc1", out[0].ToolCalls[0].ID)

	// Tool result
	assert.Equal(t, "tool", out[1].Role)
	assert.Equal(t, "tc1", out[1].ToolCallID)
}

func TestConvertHistory_EmptyPartsDefaultsToContent(t *testing.T) {
	// Empty Parts array (not nil, but []) should fall back to Content
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "fallback", Parts: []byte("[]")},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "fallback", out[0].Content)
}

func TestConvertHistory_UserSlashCommandExpansion(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "user", Content: "/plan a blog"},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "user", out[0].Role)
	// Should NOT contain the raw /plan command; should be expanded
	assert.NotContains(t, out[0].Content, "/plan")
	assert.Contains(t, out[0].Content, "<user_message>")
}

func TestConvertHistory_SkipsSystemRolePassthrough(t *testing.T) {
	// If a system message somehow ends up in history, it should pass through
	history := []models.AppChatMessage{
		{Role: "system", Content: "be helpful"},
	}
	out := convertHistory(history)
	require.Len(t, out, 1)
	assert.Equal(t, "system", out[0].Role)
	assert.Equal(t, "be helpful", out[0].Content)
}

func TestConvertHistory_ReadFilesAlwaysTruncated(t *testing.T) {
	parts1, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "===== a.txt =====\nold content a\n===== b.txt =====\nold content b"},
	})
	parts2, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc2", Name: "read_files", Input: []byte(`{}`), Result: "===== c.txt =====\nnew content c"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts1},
		{Role: "assistant", Content: "", Parts: parts2},
	}
	out := convertHistory(history)

	// Both assistant turns produce 1 assistant + 1 tool result = 4 messages total
	require.Len(t, out, 4)

	// Basic history: first (non-last) turn is truncated only if long; here it's short so full
	assert.Equal(t, "tool", out[1].Role)
	assert.Contains(t, out[1].Content, "old content a")

	// Last turn keeps full read result
	assert.Equal(t, "tool", out[3].Role)
	assert.Contains(t, out[3].Content, "new content c")
}

func TestConvertHistory_OnlyLastAssistantTurnKeepsFullNonReadResults(t *testing.T) {
	parts1, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "edit_file", Input: []byte(`{}`), Result: "first edit result"},
	})
	parts2, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc2", Name: "edit_file", Input: []byte(`{}`), Result: "second edit result"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts1},
		{Role: "assistant", Content: "", Parts: parts2},
	}
	out := convertHistory(history)

	require.Len(t, out, 4)

	// First turn — non-read tool result should be truncated
	assert.Equal(t, "tool", out[1].Role)
	assert.Contains(t, out[1].Content, "first edit result")
	// It should be truncated since it's not the last assistant turn
	assert.True(t, len(out[1].Content) < len("first edit result")+10, "expected truncated result for first turn")

	// Second (last) turn — non-read tool result should keep full content
	assert.Equal(t, "tool", out[3].Role)
	assert.Equal(t, "second edit result", out[3].Content)
}

// Drawer extraction & mutation path helpers were removed as part of the
// basic history simplification. These tests no longer apply.

func TestLastAssistantTurn(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
		{Role: "assistant", Content: "goodbye"},
	}
	turn := lastAssistantTurn(history)
	require.NotNil(t, turn)
	assert.Equal(t, "goodbye", turn.Content)
}

func TestLastAssistantTurn_SkipsErrors(t *testing.T) {
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "ok", ErrorKind: ""},
		{Role: "assistant", Content: "oops", ErrorKind: "internal"},
	}
	turn := lastAssistantTurn(history)
	require.NotNil(t, turn)
	assert.Equal(t, "ok", turn.Content)
}

func TestLastAssistantTurn_EmptyHistory(t *testing.T) {
	assert.Nil(t, lastAssistantTurn(nil))
	assert.Nil(t, lastAssistantTurn([]models.AppChatMessage{}))
}
