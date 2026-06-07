package utils

import (
	"strings"
	"testing"
)

func TestParseAnthropicSSEStreamRoutesThinkingSeparately(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"thinking_delta","thinking":"hidden"}}`,
		`event: content_block_delta`,
		`data: {"index":1,"delta":{"type":"text_delta","text":"visible"}}`,
		`event: message_delta`,
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`event: message_stop`,
		`data: {}`,
		``,
	}, "\n"))

	var chunks []string
	var reasoning []string
	result, err := parseAnthropicSSEStream(
		body,
		func(chunk string) { chunks = append(chunks, chunk) },
		func(chunk string) { reasoning = append(reasoning, chunk) },
		nil,
	)
	if err != nil {
		t.Fatalf("parseAnthropicSSEStream error = %v", err)
	}
	if got := strings.Join(chunks, ""); got != "visible" {
		t.Fatalf("text chunks = %q, want visible", got)
	}
	if got := strings.Join(reasoning, ""); got != "hidden" {
		t.Fatalf("reasoning chunks = %q, want hidden", got)
	}
	if result.Text != "visible" || result.Reasoning != "hidden" {
		t.Fatalf("result = %#v, want separated text/reasoning", result)
	}
}
