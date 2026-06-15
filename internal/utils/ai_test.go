package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/observability"
)

func TestTokenTrackerLangfuseContextSnapshotsSlices(t *testing.T) {
	tracker := NewTokenTracker()
	base := observability.TraceContext{
		RequiredSkills: []string{"web/frontend-patterns"},
		LoadedSkills:   []string{"shared/api-design"},
		Tags:           []string{"wemob", "agent"},
	}

	tracker.SetLangfuseContext(base)
	base.RequiredSkills[0] = "mutated"
	base.LoadedSkills[0] = "mutated"
	base.Tags[0] = "mutated"

	first := tracker.LangfuseContext()
	if first.RequiredSkills[0] != "web/frontend-patterns" {
		t.Fatalf("required skill leaked from caller mutation: %q", first.RequiredSkills[0])
	}
	if first.LoadedSkills[0] != "shared/api-design" {
		t.Fatalf("loaded skill leaked from caller mutation: %q", first.LoadedSkills[0])
	}
	if first.Tags[0] != "wemob" {
		t.Fatalf("tag leaked from caller mutation: %q", first.Tags[0])
	}

	first.RequiredSkills[0] = "child-mutated"
	first.LoadedSkills[0] = "child-mutated"
	first.Tags[0] = "child-mutated"

	second := tracker.LangfuseContext()
	if second.RequiredSkills[0] != "web/frontend-patterns" {
		t.Fatalf("required skill leaked from snapshot mutation: %q", second.RequiredSkills[0])
	}
	if second.LoadedSkills[0] != "shared/api-design" {
		t.Fatalf("loaded skill leaked from snapshot mutation: %q", second.LoadedSkills[0])
	}
	if second.Tags[0] != "wemob" {
		t.Fatalf("tag leaked from snapshot mutation: %q", second.Tags[0])
	}
}

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

func TestStreamDeltaGetReasoningPrefersReasoningContent(t *testing.T) {
	d := StreamDelta{Reasoning: "openrouter", ReasoningContent: "moonshot"}
	if got := d.GetReasoning(); got != "moonshot" {
		t.Fatalf("GetReasoning() = %q, want moonshot", got)
	}
}

func TestStreamDeltaGetReasoningFallsBackToReasoning(t *testing.T) {
	d := StreamDelta{Reasoning: "openrouter"}
	if got := d.GetReasoning(); got != "openrouter" {
		t.Fatalf("GetReasoning() = %q, want openrouter", got)
	}
}

func TestBuildLLMRequestBodyAppliesOpenRouterDefaultReasoning(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:    "z-ai/glm-5.1-20260406",
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, &LLMProvider{
		Name:             "openrouter",
		BaseURL:          "https://openrouter.ai/api/v1",
		APIKey:           "test-key",
		DefaultReasoning: &ReasoningConfig{Effort: "none"},
	})
	if err != nil {
		t.Fatalf("buildLLMRequestBody returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing reasoning object: %s", body)
	}
	if reasoning["effort"] != "none" {
		t.Fatalf("reasoning.effort = %#v, want none; body=%s", reasoning["effort"], body)
	}
	usage, ok := got["usage"].(map[string]any)
	if !ok || usage["include"] != true {
		t.Fatalf("request body missing usage.include=true: %s", body)
	}
}

func TestStreamWithToolsErrorsOnIncompleteStreamedToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Now I'll rewrite the page."}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_files","arguments":"{\"files\": "}}]}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	oldClient := llmStreamingClient
	llmStreamingClient = server.Client()
	t.Cleanup(func() {
		llmStreamingClient = oldClient
	})

	executed := false
	got, err := StreamWithTools(
		context.Background(),
		"test-model",
		[]Message{{Role: "user", Content: "rewrite the page"}},
		[]Tool{{Type: "function", Function: ToolFunction{Name: "write_files"}}},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			executed = true
			return "unexpected", nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{MaxRounds: 1},
	)

	if err == nil {
		t.Fatalf("StreamWithTools returned nil error with incomplete tool call; got response %q", got)
	}
	if !errors.Is(err, ErrIncompleteToolCall) {
		t.Fatalf("error = %v, want ErrIncompleteToolCall", err)
	}
	if executed {
		t.Fatal("executor should not run for incomplete tool call arguments")
	}
}

func TestStreamWithToolsRecoversIncompleteStreamedToolCall(t *testing.T) {
	var requests int
	var sawRecoveryPrompt bool
	var recoveryRequestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "previous response started a tool call") {
			sawRecoveryPrompt = true
			if recoveryRequestBody == "" {
				recoveryRequestBody = string(body)
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Now I'll rewrite the page."}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_files","arguments":"{\"files\": "}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"error","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		if requests == 2 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"multi_edit","arguments":"{\"path\":\"app/page.tsx\",\"edits\":[{\"old_string\":\"old\",\"new_string\":\"ok\"}]}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Done."}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	oldClient := llmStreamingClient
	llmStreamingClient = server.Client()
	t.Cleanup(func() {
		llmStreamingClient = oldClient
	})

	var executedArgs string
	got, err := StreamWithTools(
		context.Background(),
		"test-model",
		[]Message{{Role: "user", Content: "rewrite the page"}},
		[]Tool{
			{Type: "function", Function: ToolFunction{Name: "write_files"}},
			{Type: "function", Function: ToolFunction{Name: "multi_edit"}},
			{Type: "function", Function: ToolFunction{Name: "patch_files"}},
		},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			if name != "multi_edit" {
				t.Fatalf("executor tool = %s, want multi_edit after write_files recovery", name)
			}
			executedArgs = arguments
			return "edited app/page.tsx", nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{MaxRounds: 2},
	)

	if err != nil {
		t.Fatalf("StreamWithTools returned error after recoverable incomplete tool call: %v", err)
	}
	if got != "Done." {
		t.Fatalf("response = %q, want Done.", got)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if !sawRecoveryPrompt {
		t.Fatal("second request did not include recovery prompt")
	}
	if strings.Contains(recoveryRequestBody, `"name":"write_files"`) {
		t.Fatalf("recovery request still exposed write_files tool: %s", recoveryRequestBody)
	}
	if strings.Contains(recoveryRequestBody, "BEGIN_WEMOB_FILE") || strings.Contains(recoveryRequestBody, "END_WEMOB_FILE") {
		t.Fatalf("recovery request referenced removed raw file block protocol: %s", recoveryRequestBody)
	}
	if !strings.Contains(recoveryRequestBody, "multi_edit") || !strings.Contains(recoveryRequestBody, "patch_files") {
		t.Fatalf("recovery request did not steer toward edit tools: %s", recoveryRequestBody)
	}
	if !json.Valid([]byte(executedArgs)) {
		t.Fatalf("executor received invalid JSON args: %s", executedArgs)
	}
	if !strings.Contains(executedArgs, "app/page.tsx") {
		t.Fatalf("executor args = %s, want recovered edit payload", executedArgs)
	}
}

func TestStreamWithToolsRecoversInvalidJSONToolCallRound(t *testing.T) {
	var requests int
	var recoveryRequestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if recoveryRequestBody == "" && strings.Contains(string(body), "previous response started a tool call") {
			recoveryRequestBody = string(body)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Starting with package config and CSS:"}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_files","arguments":"{\"files\": "}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":16274,"completion_tokens":2270,"total_tokens":18544}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		if requests == 2 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"patch_files","arguments":"{\"patches\":[{\"path\":\"frontend/src/App.tsx\",\"old_string\":\"old\",\"new_string\":\"ok\"}]}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Updated the portfolio surface."}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":50,"completion_tokens":8,"total_tokens":58}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	oldClient := llmStreamingClient
	llmStreamingClient = server.Client()
	t.Cleanup(func() {
		llmStreamingClient = oldClient
	})

	var executed []string
	got, err := StreamWithTools(
		context.Background(),
		"test-model",
		[]Message{{Role: "user", Content: "build a portfolio"}},
		[]Tool{
			{Type: "function", Function: ToolFunction{Name: "write_files"}},
			{Type: "function", Function: ToolFunction{Name: "patch_files"}},
		},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			executed = append(executed, name)
			if !json.Valid([]byte(arguments)) {
				t.Fatalf("executor received invalid JSON args for %s: %s", name, arguments)
			}
			return "patched frontend/src/App.tsx", nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{MaxRounds: 3},
	)

	if err != nil {
		t.Fatalf("StreamWithTools returned error after recovery: %v", err)
	}
	if got != "Updated the portfolio surface." {
		t.Fatalf("response = %q, want recovered final text", got)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if recoveryRequestBody == "" {
		t.Fatal("missing recovery prompt after invalid JSON tool call")
	}
	if strings.Contains(recoveryRequestBody, `"name":"write_files"`) {
		t.Fatalf("recovery request still exposed write_files tool: %s", recoveryRequestBody)
	}
	if len(executed) != 1 || executed[0] != "patch_files" {
		t.Fatalf("executed tools = %#v, want only recovered patch_files", executed)
	}
}

func TestStreamWithToolsCompactsBeforeMaxMessageTrim(t *testing.T) {
	var compactSawMessages int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Done."}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer server.Close()

	oldClient := llmStreamingClient
	llmStreamingClient = server.Client()
	t.Cleanup(func() {
		llmStreamingClient = oldClient
	})

	messages := []Message{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "old user 1"},
		{Role: "assistant", Content: "old assistant 1"},
		{Role: "user", Content: "old user 2"},
		{Role: "assistant", Content: "old assistant 2"},
		{Role: "user", Content: "CURRENT RUN CONTEXT\n===================\n\nfiles\n\nUSER REQUEST\n============\n\nfinish"},
	}

	_, err := StreamWithTools(
		context.Background(),
		"test-model",
		messages,
		nil,
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		ToolCallConfig{
			MaxRounds:   1,
			MaxMessages: 3,
			CompactHistory: func(messages []Message) ([]Message, bool) {
				compactSawMessages = len(messages)
				return []Message{
					messages[0],
					{Role: "user", Content: "COMPACT MEMORY"},
					messages[len(messages)-1],
				}, true
			},
		},
	)

	if err != nil {
		t.Fatalf("StreamWithTools returned error: %v", err)
	}
	if compactSawMessages != len(messages) {
		t.Fatalf("CompactHistory saw %d messages, want untrimmed %d", compactSawMessages, len(messages))
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
