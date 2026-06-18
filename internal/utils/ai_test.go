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

func TestLangfuseGenerationNameUsesProductStepNames(t *testing.T) {
	tests := map[string]string{
		"stream_with_tools": "generate-agent-response",
		"get_json":          "generate-json",
		"get_text":          "generate-text",
		"custom_step":       "custom_step",
	}

	for input, want := range tests {
		if got := langfuseGenerationName(input); got != want {
			t.Fatalf("langfuseGenerationName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildLLMRequestBodyStripsRootCombinatorsForOpenRouterTools(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:    "anthropic/claude-haiku-4.5:online",
		Messages: []Message{{Role: "user", Content: "edit"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name: "multi_edit",
				Parameters: map[string]any{
					"type": "object",
					"anyOf": []any{
						map[string]any{"required": []any{"path", "edits"}},
						map[string]any{"required": []any{"files"}},
					},
					"properties": map[string]any{
						"edits": map[string]any{
							"anyOf": []any{
								map[string]any{"type": "array"},
								map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		}},
	}, &LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	if err != nil {
		t.Fatalf("buildLLMRequestBody: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	tools := payload["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	if _, ok := params["anyOf"]; ok {
		t.Fatalf("root anyOf should be stripped: %#v", params)
	}
	props := params["properties"].(map[string]any)
	edits := props["edits"].(map[string]any)
	if _, ok := edits["anyOf"]; !ok {
		t.Fatalf("nested property anyOf should be preserved: %#v", edits)
	}
}

func TestBuildLLMRequestBodyStripsRootCombinatorsForAnthropicTools(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:    "anthropic/claude-haiku-4.5",
		Messages: []Message{{Role: "user", Content: "edit"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name: "multi_edit",
				Parameters: map[string]any{
					"type":  "object",
					"oneOf": []any{map[string]any{"required": []any{"path"}}},
					"properties": map[string]any{
						"edits": map[string]any{
							"anyOf": []any{
								map[string]any{"type": "array"},
								map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		}},
	}, &LLMProvider{Name: "anthropic", BaseURL: "https://api.anthropic.com/v1"})
	if err != nil {
		t.Fatalf("buildLLMRequestBody: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	tools := payload["tools"].([]any)
	inputSchema := tools[0].(map[string]any)["input_schema"].(map[string]any)
	if _, ok := inputSchema["oneOf"]; ok {
		t.Fatalf("root oneOf should be stripped: %#v", inputSchema)
	}
	props := inputSchema["properties"].(map[string]any)
	edits := props["edits"].(map[string]any)
	if _, ok := edits["anyOf"]; !ok {
		t.Fatalf("nested property anyOf should be preserved: %#v", edits)
	}
}

func TestBuildLLMRequestBodyMergesRootOnlyUnionToolSchema(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:    "anthropic/claude-haiku-4.5:online",
		Messages: []Message{{Role: "user", Content: "search"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name: "mcp_search",
				Parameters: map[string]any{
					"anyOf": []any{
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query": map[string]any{"type": "string"},
								"limit": map[string]any{"type": "integer"},
							},
							"required": []any{"query"},
						},
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"url":   map[string]any{"type": "string"},
								"limit": map[string]any{"type": "integer"},
							},
							"required": []any{"url"},
						},
					},
				},
			},
		}},
	}, &LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	if err != nil {
		t.Fatalf("buildLLMRequestBody: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	tools := payload["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	if _, ok := params["anyOf"]; ok {
		t.Fatalf("root anyOf should be stripped: %#v", params)
	}
	if got := params["type"]; got != "object" {
		t.Fatalf("merged schema type = %#v, want object", got)
	}
	props := params["properties"].(map[string]any)
	for _, name := range []string{"query", "url", "limit"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("merged schema missing property %q: %#v", name, props)
		}
	}
	if _, ok := params["required"]; ok {
		t.Fatalf("anyOf-only alternatives should not force variant-specific required fields: %#v", params["required"])
	}
}

func TestBuildLLMRequestBodyMergesRootAllOfRequiredFields(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:    "anthropic/claude-haiku-4.5:online",
		Messages: []Message{{Role: "user", Content: "search"}},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name: "mcp_search",
				Parameters: map[string]any{
					"allOf": []any{
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"query": map[string]any{"type": "string"},
							},
							"required": []any{"query"},
						},
						map[string]any{
							"type": "object",
							"properties": map[string]any{
								"limit": map[string]any{"type": "integer"},
							},
							"required": []any{"limit"},
						},
					},
				},
			},
		}},
	}, &LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	if err != nil {
		t.Fatalf("buildLLMRequestBody: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	tools := payload["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	params := function["parameters"].(map[string]any)
	if _, ok := params["allOf"]; ok {
		t.Fatalf("root allOf should be stripped: %#v", params)
	}
	props := params["properties"].(map[string]any)
	for _, name := range []string{"query", "limit"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("merged schema missing property %q: %#v", name, props)
		}
	}
	required := map[string]bool{}
	for _, raw := range params["required"].([]any) {
		required[raw.(string)] = true
	}
	for _, name := range []string{"query", "limit"} {
		if !required[name] {
			t.Fatalf("merged allOf required missing %q: %#v", name, params["required"])
		}
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

func TestMessageSnapshotsIncludeReasoningContent(t *testing.T) {
	snapshots := messageSnapshots([]Message{
		{
			Role:             "assistant",
			Content:          "visible text",
			ReasoningContent: "hidden plan",
			Images:           []string{"img://one"},
		},
		{Role: "user", Content: "next"},
	})

	if got := snapshots[0].ReasoningContent; got != "hidden plan" {
		t.Fatalf("reasoning_content = %q, want hidden plan", got)
	}
	if snapshots[0].Role != "assistant" {
		t.Fatalf("role = %q, want assistant", snapshots[0].Role)
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

func TestBuildLLMRequestBodyStripsReasoningContentFromMessages(t *testing.T) {
	body, err := buildLLMRequestBody(ChatRequest{
		Model:  "test-model",
		Stream: true,
		Messages: []Message{
			{Role: "assistant", Content: "ok", ReasoningContent: "should not be sent"},
			{Role: "user", Content: "next", ReasoningContent: "also hidden"},
		},
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
	rawMessages, ok := got["messages"].([]any)
	if !ok {
		t.Fatalf("request body missing messages array: %s", body)
	}
	for _, raw := range rawMessages {
		msg, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("message is not object: %#v", raw)
		}
		if _, has := msg["reasoning_content"]; has {
			t.Fatalf("request message leaked reasoning_content: %#v", msg)
		}
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
	var incomplete *IncompleteToolCallError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error = %T, want IncompleteToolCallError", err)
	}
	if incomplete.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", incomplete.FinishReason)
	}
	if len(incomplete.Details) != 1 {
		t.Fatalf("details len = %d, want 1", len(incomplete.Details))
	}
	if incomplete.Details[0].Name != "write_files" {
		t.Fatalf("tool name = %q, want write_files", incomplete.Details[0].Name)
	}
	if incomplete.Details[0].ArgumentsSoFar != `{"files": ` {
		t.Fatalf("arguments so far = %q", incomplete.Details[0].ArgumentsSoFar)
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
	if !strings.Contains(recoveryRequestBody, "streamed tool-call JSON ended incomplete") {
		t.Fatalf("recovery request did not diagnose incomplete JSON: %s", recoveryRequestBody)
	}
	if !strings.Contains(recoveryRequestBody, "does not mean write_files cannot create the requested file") {
		t.Fatalf("recovery request implied write_files cannot create files: %s", recoveryRequestBody)
	}
	if !strings.Contains(recoveryRequestBody, "multi_edit") {
		t.Fatalf("recovery request did not steer toward multi_edit: %s", recoveryRequestBody)
	}
	if !json.Valid([]byte(executedArgs)) {
		t.Fatalf("executor received invalid JSON args: %s", executedArgs)
	}
	if !strings.Contains(executedArgs, "app/page.tsx") {
		t.Fatalf("executor args = %s, want recovered edit payload", executedArgs)
	}
}

func TestStreamWithToolsRetriesSecondIncompleteToolCallRecovery(t *testing.T) {
	var requests int
	var recoveryPromptCount int
	var secondRecoveryPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "previous response started a tool call") {
			recoveryPromptCount++
		}
		if secondRecoveryPrompt == "" && strings.Contains(string(body), "Recovery attempt 2 of 2") {
			secondRecoveryPrompt = string(body)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		switch requests {
		case 1:
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_files","arguments":"{\"files\": "}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		case 2:
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"multi_edit","arguments":"{\"files\": "}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`)
		case 3:
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_3","type":"function","function":{"name":"multi_edit","arguments":"{\"path\":\"frontend/src/App.tsx\",\"edits\":[{\"old_string\":\"old\",\"new_string\":\"ok\"}]}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}}`)
		default:
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Recovered."}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}`)
		}
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
		[]Message{{Role: "user", Content: "make the changes"}},
		[]Tool{
			{Type: "function", Function: ToolFunction{Name: "write_files"}},
			{Type: "function", Function: ToolFunction{Name: "multi_edit"}},
		},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			executed = append(executed, name)
			if !json.Valid([]byte(arguments)) {
				t.Fatalf("executor received invalid JSON args for %s: %s", name, arguments)
			}
			return "edited frontend/src/App.tsx", nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{MaxRounds: 3},
	)

	if err != nil {
		t.Fatalf("StreamWithTools returned error after second recovery: %v", err)
	}
	if got != "Recovered." {
		t.Fatalf("response = %q, want Recovered.", got)
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want 4", requests)
	}
	if recoveryPromptCount < 2 {
		t.Fatalf("recovery prompts = %d, want at least 2", recoveryPromptCount)
	}
	if secondRecoveryPrompt == "" {
		t.Fatal("second recovery prompt missing attempt count")
	}
	if !strings.Contains(secondRecoveryPrompt, "previous recovery attempt also produced incomplete tool-call JSON") {
		t.Fatalf("second recovery prompt missing stronger warning: %s", secondRecoveryPrompt)
	}
	if strings.Contains(secondRecoveryPrompt, "smallest possible valid JSON payload") {
		t.Fatalf("second recovery prompt used old size-oriented warning: %s", secondRecoveryPrompt)
	}
	if len(executed) != 1 || executed[0] != "multi_edit" {
		t.Fatalf("executed tools = %#v, want only multi_edit", executed)
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
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"multi_edit","arguments":"{\"path\":\"frontend/src/App.tsx\",\"edits\":[{\"old_string\":\"old\",\"new_string\":\"ok\"}]}"}}]}}]}`)
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
			{Type: "function", Function: ToolFunction{Name: "multi_edit"}},
		},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			executed = append(executed, name)
			if !json.Valid([]byte(arguments)) {
				t.Fatalf("executor received invalid JSON args for %s: %s", name, arguments)
			}
			return "edited frontend/src/App.tsx", nil
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
	if len(executed) != 1 || executed[0] != "multi_edit" {
		t.Fatalf("executed tools = %#v, want only recovered multi_edit", executed)
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
		{Role: "user", Content: "finish"},
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

func TestStreamWithToolsSecondRoundPreservesCurrentUserMessage(t *testing.T) {
	const userRequest = "i want to be able to manage and add main tasks like sleep, work etc"
	var secondRoundBody string
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"I'll inspect first."}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_files","arguments":"{\"glob\":\"frontend/src/**/*\"}"}}]}}]}`)
			fmt.Fprintln(w, `data: {"choices":[{"finish_reason":"tool_calls","delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
			fmt.Fprintln(w, "data: [DONE]")
			return
		}
		secondRoundBody = string(body)
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

	_, err := StreamWithTools(
		context.Background(),
		"test-model",
		[]Message{{Role: "user", Content: userRequest}},
		[]Tool{{Type: "function", Function: ToolFunction{Name: "list_files"}}},
		&LLMProvider{Name: "openrouter", BaseURL: server.URL, APIKey: "test-key"},
		nil,
		nil,
		func(name, arguments string) (string, error) {
			return "frontend/src/App.tsx", nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{MaxRounds: 2},
	)

	if err != nil {
		t.Fatalf("StreamWithTools returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if !strings.Contains(secondRoundBody, userRequest) {
		t.Fatalf("second round request lost user message: %s", secondRoundBody)
	}
	if strings.Contains(secondRoundBody, "CURRENT RUN CONTEXT") || strings.Contains(secondRoundBody, "USER REQUEST") {
		t.Fatalf("second round request contains synthetic context markers: %s", secondRoundBody)
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

func TestTrimToolCallsForWindowKeepsFullTruncatedAndDropsOlderCalls(t *testing.T) {
	messages := []Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "tc1", Function: &FunctionCall{Name: "read_files", Arguments: `{"paths":["a.tsx"]}`}},
				{ID: "tc2", Function: &FunctionCall{Name: "grep", Arguments: `{"pattern":"needle"}`}},
			},
		},
		{Role: "tool", ToolCallID: "tc1", Name: "read_files", Content: "result 1"},
		{Role: "tool", ToolCallID: "tc2", Name: "grep", Content: "result 2"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "tc3", Function: &FunctionCall{Name: "read_files", Arguments: `{"paths":["b.tsx"]}`}},
				{ID: "tc4", Function: &FunctionCall{Name: "edit_file", Arguments: `{"path":"c.tsx","old_string":"a","new_string":"b"}`}},
			},
		},
		{Role: "tool", ToolCallID: "tc3", Name: "read_files", Content: "result 3"},
		{Role: "tool", ToolCallID: "tc4", Name: "edit_file", Content: "result 4"},
	}

	out := trimToolCallsForWindow(
		messages,
		1,
		2,
		func(toolName, args string) string { return "trimmed args " + toolName },
		func(toolName, result string) string { return "trimmed result " + toolName },
	)

	var body strings.Builder
	for _, msg := range out {
		body.WriteString(msg.Content)
		body.WriteString("\n")
		for _, tc := range msg.ToolCalls {
			body.WriteString(tc.ID)
			body.WriteString(":")
			body.WriteString(tc.Function.Arguments)
			body.WriteString("\n")
		}
	}
	got := body.String()
	if strings.Contains(got, "tc1") || strings.Contains(got, "result 1") {
		t.Fatalf("oldest tool call was not dropped:\n%s", got)
	}
	if !strings.Contains(got, "tc2:trimmed args grep") || !strings.Contains(got, "trimmed result grep") {
		t.Fatalf("first retained older call was not truncated:\n%s", got)
	}
	if !strings.Contains(got, "tc3:trimmed args read_files") || !strings.Contains(got, "trimmed result read_files") {
		t.Fatalf("second retained older call was not truncated:\n%s", got)
	}
	if !strings.Contains(got, `tc4:{"path":"c.tsx","old_string":"a","new_string":"b"}`) || !strings.Contains(got, "result 4") {
		t.Fatalf("latest call was not kept full:\n%s", got)
	}
}

func TestEnsureToolCallIDsSynthesizesBeforeHistoryTracking(t *testing.T) {
	toolCallsMap := map[int]*ToolCall{
		0: {Function: &FunctionCall{Name: "read_files", Arguments: `{"paths":["a.tsx"]}`}},
		1: {ID: "provider_id", Function: &FunctionCall{Name: "grep", Arguments: `{"pattern":"needle"}`}},
	}

	toolCalls := sortedToolCallsWithIDs(toolCallsMap, 3, "openrouter", "qwen")

	if len(toolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(toolCalls))
	}
	if toolCalls[0].ID != "call_synth_3_0" {
		t.Fatalf("synthetic id = %q, want call_synth_3_0", toolCalls[0].ID)
	}
	if toolCallsMap[0].ID != "call_synth_3_0" {
		t.Fatalf("toolCallsMap id was not updated: %q", toolCallsMap[0].ID)
	}
	if toolCalls[1].ID != "provider_id" {
		t.Fatalf("provider id changed: %q", toolCalls[1].ID)
	}
}
