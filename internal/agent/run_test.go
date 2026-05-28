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
	assert.Contains(t, out[0].Content, "run timed out")
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

	// Tool result message — read_files is ALWAYS truncated (drawer provides contents)
	assert.Equal(t, "tool", out[1].Role)
	assert.Equal(t, "tc1", out[1].ToolCallID)
	assert.Equal(t, "read_files", out[1].Name)
	assert.Contains(t, out[1].Content, "file content omitted")
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
	assert.Contains(t, out[1].Content, "file content omitted")

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

	// First turn tool result — read_files should ALWAYS be truncated
	assert.Equal(t, "tool", out[1].Role)
	assert.Contains(t, out[1].Content, "file content omitted")

	// Second turn tool result — read_files should ALSO be truncated (even though it's the last turn)
	assert.Equal(t, "tool", out[3].Role)
	assert.Contains(t, out[3].Content, "file content omitted")
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

func TestExtractFileDrawerFromHistory_ReadFiles(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "===== a.txt =====\ncontent of a\n===== b.txt =====\ncontent of b"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts},
	}
	drawer := extractFileDrawerFromHistory(history)

	require.Len(t, drawer, 2)
	assert.Equal(t, "content of a", drawer["a.txt"])
	assert.Equal(t, "content of b", drawer["b.txt"])
}

func TestExtractFileDrawerFromHistory_ReadFile(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_file", Input: []byte(`{}`), Result: "===== foo.go =====\npackage main\n\nfunc main() {}"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts},
	}
	drawer := extractFileDrawerFromHistory(history)

	require.Len(t, drawer, 1)
	assert.Equal(t, "package main\n\nfunc main() {}", drawer["foo.go"])
}

func TestExtractFileDrawerFromHistory_OnlyLastTwoTurns(t *testing.T) {
	parts1, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "===== old.txt =====\nold content"},
	})
	parts2, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc2", Name: "read_files", Input: []byte(`{}`), Result: "===== mid.txt =====\nmid content"},
	})
	parts3, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc3", Name: "read_files", Input: []byte(`{}`), Result: "===== new.txt =====\nnew content"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts1},
		{Role: "assistant", Content: "", Parts: parts2},
		{Role: "assistant", Content: "", Parts: parts3},
	}
	drawer := extractFileDrawerFromHistory(history)

	// Only last 2 turns scanned, so old.txt should be absent
	require.Len(t, drawer, 2)
	assert.NotContains(t, drawer, "old.txt")
	assert.Equal(t, "mid content", drawer["mid.txt"])
	assert.Equal(t, "new content", drawer["new.txt"])
}

func TestExtractFileDrawerFromHistory_SkipsNonReadTools(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "edit_file", Input: []byte(`{}`), Result: "edit done"},
		{Type: "tool_call", ToolCallID: "tc2", Name: "read_file", Input: []byte(`{}`), Result: "===== x.txt =====\nhello"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts},
	}
	drawer := extractFileDrawerFromHistory(history)

	require.Len(t, drawer, 1)
	assert.Equal(t, "hello", drawer["x.txt"])
}

func TestExtractFileDrawerFromHistory_MultiplePartsInOneTurn(t *testing.T) {
	parts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "tool_call", ToolCallID: "tc1", Name: "read_files", Input: []byte(`{}`), Result: "===== a.txt =====\nalpha"},
		{Type: "tool_call", ToolCallID: "tc2", Name: "read_files", Input: []byte(`{}`), Result: "===== b.txt =====\nbeta"},
	})
	history := []models.AppChatMessage{
		{Role: "assistant", Content: "", Parts: parts},
	}
	drawer := extractFileDrawerFromHistory(history)

	require.Len(t, drawer, 2)
	assert.Equal(t, "alpha", drawer["a.txt"])
	assert.Equal(t, "beta", drawer["b.txt"])
}
