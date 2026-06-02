package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestStreamWithToolsTrimsToolResultsOnlyAfterOneModelRound(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
		calls    int
	)
	oldClient := llmStreamingClient
	defer func() { llmStreamingClient = oldClient }()
	llmStreamingClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		calls++
		call := calls
		mu.Unlock()

		var sb strings.Builder
		switch call {
		case 1:
			writeSSE(t, &sb, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_files","arguments":"{\"paths\":[\"a.txt\"]}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)
		case 2:
			writeSSE(t, &sb, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"read_files","arguments":"{\"paths\":[\"b.txt\"]}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":1,"total_tokens":21}}`)
		default:
			writeSSE(t, &sb, `{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":30,"completion_tokens":1,"total_tokens":31}}`)
		}
		sb.WriteString("data: [DONE]\n\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(sb.String())),
		}, nil
	})}

	execCount := 0
	got, err := StreamWithTools(
		context.Background(),
		"test-model",
		[]Message{{Role: "user", Content: "start"}},
		[]Tool{{Type: "function", Function: ToolFunction{
			Name:        "read_files",
			Description: "read files",
			Parameters:  map[string]any{"type": "object"},
		}}},
		&LLMProvider{Name: "test", BaseURL: "http://llm.test", APIKey: "test-key"},
		nil,
		nil,
		func(name string, arguments string) (string, error) {
			execCount++
			return fmt.Sprintf("FULL_RESULT_%d", execCount), nil
		},
		nil,
		nil,
		nil,
		ToolCallConfig{
			MaxRounds: 3,
			TrimResults: func(toolName, result string) string {
				return "TRIMMED_" + toolName
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamWithTools returned error: %v", err)
	}
	if got != "done" {
		t.Fatalf("got final response %q, want done", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("got %d requests, want 3", len(requests))
	}
	if !strings.Contains(requests[1], "FULL_RESULT_1") {
		t.Fatalf("second request did not contain full first tool result: %s", requests[1])
	}
	if strings.Contains(requests[1], "TRIMMED_read_files") {
		t.Fatalf("second request trimmed the previous tool result before the model could see it: %s", requests[1])
	}
	if !strings.Contains(requests[2], "TRIMMED_read_files") {
		t.Fatalf("third request did not contain trimmed older tool result: %s", requests[2])
	}
	if !strings.Contains(requests[2], "FULL_RESULT_2") {
		t.Fatalf("third request should still contain the immediately previous full tool result: %s", requests[2])
	}
}

func writeSSE(t *testing.T, w io.Writer, payload string) {
	t.Helper()
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		t.Fatalf("write SSE: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
