package utils

import (
	"fmt"
	"strings"
	"testing"
)

func TestTrimToolRoundsOlderThanKeepsRecentRoundsUntrimmed(t *testing.T) {
	messages := make([]Message, 0, 12)
	history := make([]toolRoundTrimState, 0, 6)
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("tc%d", i)
		messages = append(messages,
			Message{
				Role:             "assistant",
				ReasoningContent: "reasoning",
				ToolCalls: []ToolCall{{
					ID: id,
					Function: &FunctionCall{
						Name:      "write_file",
						Arguments: fmt.Sprintf(`{"path":"app/%d.tsx","content":"full content %d"}`, i, i),
					},
				}},
			},
			Message{
				Role:       "tool",
				ToolCallID: id,
				Name:       "write_file",
				Content:    fmt.Sprintf("wrote app/%d.tsx with full result", i),
			},
		)
		history = append(history, toolRoundTrimState{toolCallIDs: []string{id}})
	}

	remaining := trimToolRoundsOlderThan(
		messages,
		history,
		5,
		func(toolName, args string) string {
			return strings.ReplaceAll(args, "full content", "trimmed content")
		},
		func(toolName, result string) string {
			return strings.ReplaceAll(result, "full result", "trimmed result")
		},
	)

	if len(remaining) != 5 {
		t.Fatalf("remaining history = %d, want 5", len(remaining))
	}
	if got := messages[0].ToolCalls[0].Function.Arguments; !strings.Contains(got, "trimmed content 1") {
		t.Fatalf("oldest args were not trimmed: %s", got)
	}
	if got := messages[1].Content; !strings.Contains(got, "trimmed result") {
		t.Fatalf("oldest result was not trimmed: %s", got)
	}
	if messages[0].ReasoningContent != "" {
		t.Fatal("oldest assistant reasoning was not trimmed")
	}
	for round := 2; round <= 6; round++ {
		msgIdx := (round - 1) * 2
		if got := messages[msgIdx].ToolCalls[0].Function.Arguments; !strings.Contains(got, fmt.Sprintf("full content %d", round)) {
			t.Fatalf("recent round %d args were trimmed: %s", round, got)
		}
		if got := messages[msgIdx+1].Content; !strings.Contains(got, "full result") {
			t.Fatalf("recent round %d result was trimmed: %s", round, got)
		}
	}
}

func TestTrimToolRoundsOlderThanTrimsAllToolCallsInOldRound(t *testing.T) {
	messages := []Message{
		{
			Role:             "assistant",
			ReasoningContent: "reasoning",
			ToolCalls: []ToolCall{
				{ID: "tc1", Function: &FunctionCall{Name: "read_files", Arguments: `{"paths":["a.tsx"]}`}},
				{ID: "tc2", Function: &FunctionCall{Name: "grep", Arguments: `{"pattern":"needle"}`}},
			},
		},
		{Role: "tool", ToolCallID: "tc1", Name: "read_files", Content: "full read result"},
		{Role: "tool", ToolCallID: "tc2", Name: "grep", Content: "full grep result"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "tc3", Function: &FunctionCall{Name: "write_file", Arguments: `{"path":"b.tsx","content":"full content"}`}},
			},
		},
		{Role: "tool", ToolCallID: "tc3", Name: "write_file", Content: "full write result"},
	}
	history := []toolRoundTrimState{
		{toolCallIDs: []string{"tc1", "tc2"}},
		{toolCallIDs: []string{"tc3"}},
	}

	remaining := trimToolRoundsOlderThan(
		messages,
		history,
		1,
		func(toolName, args string) string { return "trimmed " + toolName },
		func(toolName, result string) string { return "trimmed " + toolName },
	)

	if len(remaining) != 1 || remaining[0].toolCallIDs[0] != "tc3" {
		t.Fatalf("remaining history = %+v, want only tc3 round", remaining)
	}
	for _, tc := range messages[0].ToolCalls {
		if !strings.HasPrefix(tc.Function.Arguments, "trimmed ") {
			t.Fatalf("old multi-tool round args not trimmed: %+v", tc)
		}
	}
	if messages[1].Content != "trimmed read_files" || messages[2].Content != "trimmed grep" {
		t.Fatalf("old multi-tool results not trimmed: %q / %q", messages[1].Content, messages[2].Content)
	}
	if messages[3].ToolCalls[0].Function.Arguments != `{"path":"b.tsx","content":"full content"}` {
		t.Fatalf("recent args were trimmed: %s", messages[3].ToolCalls[0].Function.Arguments)
	}
	if messages[4].Content != "full write result" {
		t.Fatalf("recent result was trimmed: %s", messages[4].Content)
	}
}
