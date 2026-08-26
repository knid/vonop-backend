package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/stretchr/testify/require"
)

func TestSanitizeChatMessagesForDisplayHidesRawToolPayloads(t *testing.T) {
	parts, err := json.Marshal([]models.ChatMessagePart{
		{
			Type:       "tool_call",
			ToolCallID: "tc_1",
			Name:       "multi_edit",
			Input:      json.RawMessage(`{"path":"app/page.tsx","edits":[{"old_string":"secret old","new_string":"secret new"}]}`),
			Result:     strings.Repeat("file content\n", 40),
		},
	})
	require.NoError(t, err)
	toolCalls, err := json.Marshal([]models.ChatMessageToolCall{
		{
			ID:     "tc_1",
			Name:   "multi_edit",
			Input:  json.RawMessage(`{"path":"app/page.tsx","edits":[{"old_string":"secret old","new_string":"secret new"}]}`),
			Result: strings.Repeat("file content\n", 40),
		},
	})
	require.NoError(t, err)

	msgs := SanitizeChatMessagesForDisplay([]models.AppChatMessage{{
		ID:        uuid.New(),
		Role:      "assistant",
		Parts:     parts,
		ToolCalls: toolCalls,
	}})

	require.Len(t, msgs, 1)
	body, err := json.Marshal(msgs[0])
	require.NoError(t, err)
	require.NotContains(t, string(body), "secret old")
	require.NotContains(t, string(body), "secret new")
	require.Contains(t, string(body), "app/page.tsx")
}
