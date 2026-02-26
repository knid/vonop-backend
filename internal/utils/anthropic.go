package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// --- Anthropic Request Types ---

// AnthropicCacheControl marks a block as cacheable (5-min sliding TTL).
type AnthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// AnthropicContentBlock is a polymorphic content block in Anthropic messages.
type AnthropicContentBlock struct {
	Type string `json:"type"` // "text", "image", "tool_use", "tool_result", "thinking"

	// text block
	Text string `json:"text,omitempty"`

	// image block
	Source *AnthropicImageSource `json:"source,omitempty"`

	// tool_use block
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result block
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`

	// thinking block
	Thinking string `json:"thinking,omitempty"`

	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicImageSource holds base64 image data for the Anthropic API.
type AnthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // e.g. "image/png"
	Data      string `json:"data"`       // raw base64 data
}

// AnthropicMessage is an Anthropic API message with role and content blocks.
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []AnthropicContentBlock
}

// AnthropicTool is a tool definition in Anthropic format.
type AnthropicTool struct {
	Name         string                `json:"name"`
	Description  string                `json:"description,omitempty"`
	InputSchema  map[string]any        `json:"input_schema"`                 // NOT "parameters"
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicThinkingConfig controls extended thinking.
type AnthropicThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled" or "adaptive"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // required when type="enabled"
}

// AnthropicRequest is the top-level request to the Anthropic Messages API.
type AnthropicRequest struct {
	Model     string                  `json:"model"`
	MaxTokens int                     `json:"max_tokens"`
	System    []AnthropicContentBlock `json:"system,omitempty"`
	Messages  []AnthropicMessage      `json:"messages"`
	Tools     []AnthropicTool         `json:"tools,omitempty"`
	Stream    bool                    `json:"stream,omitempty"`
	Thinking  *AnthropicThinkingConfig `json:"thinking,omitempty"`
}

// --- Anthropic Response Types ---

// AnthropicResponseBlock is a content block in an Anthropic response.
type AnthropicResponseBlock struct {
	Type     string          `json:"type"` // "text", "tool_use", "thinking"
	Text     string          `json:"text,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Thinking string          `json:"thinking,omitempty"`
}

// AnthropicResponse is the top-level non-streaming response.
type AnthropicResponse struct {
	ID         string                   `json:"id"`
	Content    []AnthropicResponseBlock `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      AnthropicUsage           `json:"usage"`
}

// AnthropicUsage includes Anthropic-specific caching fields.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ToUsageInfo converts Anthropic usage to internal UsageInfo.
func (u AnthropicUsage) ToUsageInfo() UsageInfo {
	return UsageInfo{
		PromptTokens:     u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
	}
}

// --- Request Translation ---

// convertToAnthropicRequest translates an internal ChatRequest into an AnthropicRequest.
func convertToAnthropicRequest(req ChatRequest, provider *LLMProvider) AnthropicRequest {
	// Strip "anthropic/" prefix from model name (OpenRouter format → native)
	model := req.Model
	model = strings.TrimPrefix(model, "anthropic/")

	// Extract system messages → top-level system field
	var systemBlocks []AnthropicContentBlock
	var nonSystemMessages []Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemBlocks = append(systemBlocks, AnthropicContentBlock{
				Type: "text",
				Text: m.Content,
			})
		} else {
			nonSystemMessages = append(nonSystemMessages, m)
		}
	}

	// Inject JSON instruction into system prompt when response_format is json_object
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		systemBlocks = append(systemBlocks, AnthropicContentBlock{
			Type: "text",
			Text: "You MUST respond with valid JSON only. No markdown fences, no explanation outside JSON.",
		})
	}

	// Apply automatic prompt caching to the last few substantial system blocks.
	// Anthropic supports up to 4 cache breakpoints total; reserve 1 for tools,
	// so mark at most 3 system blocks (scanning backwards to prioritize stable ones).
	maxSystemCache := 3
	cached := 0
	for i := len(systemBlocks) - 1; i >= 0 && cached < maxSystemCache; i-- {
		if len(systemBlocks[i].Text) > 500 {
			systemBlocks[i].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
			cached++
		}
	}

	// Convert messages
	anthropicMessages := convertMessagesToAnthropic(nonSystemMessages)

	// Convert tools
	var anthropicTools []AnthropicTool
	for _, t := range req.Tools {
		if t.Type == "function" {
			anthropicTools = append(anthropicTools, AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
	}
	// Mark last tool cacheable so Anthropic caches the entire tools block
	if len(anthropicTools) > 0 {
		anthropicTools[len(anthropicTools)-1].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
	}

	// Determine max_tokens
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = req.MaxCompletionTokens
	}
	if maxTokens == 0 {
		maxTokens = 16384
	}

	result := AnthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    systemBlocks,
		Messages:  anthropicMessages,
		Tools:     anthropicTools,
		Stream:    req.Stream,
	}

	// Map extended thinking
	result.Thinking = mapThinkingConfig(req, model)

	return result
}

// mapThinkingConfig determines the Anthropic thinking config from the request.
func mapThinkingConfig(req ChatRequest, model string) *AnthropicThinkingConfig {
	// Check if reasoning is configured
	var reasoning *ReasoningConfig
	if req.Reasoning != nil {
		reasoning = req.Reasoning
	}
	if reasoning == nil || reasoning.Effort == "" || reasoning.Effort == "none" {
		return nil
	}

	// Opus 4.6: use adaptive thinking (model decides when/how much to think)
	if strings.Contains(model, "opus") {
		return &AnthropicThinkingConfig{Type: "adaptive"}
	}

	// Other models: map effort to budget_tokens
	budgetMap := map[string]int{
		"low":    2048,
		"medium": 5000,
		"high":   10000,
		"xhigh":  20000,
	}
	budget, ok := budgetMap[reasoning.Effort]
	if !ok {
		budget = 5000 // default to medium
	}
	return &AnthropicThinkingConfig{
		Type:         "enabled",
		BudgetTokens: budget,
	}
}

// convertMessagesToAnthropic converts internal messages to Anthropic format,
// enforcing strict user/assistant alternation.
func convertMessagesToAnthropic(messages []Message) []AnthropicMessage {
	var result []AnthropicMessage

	for i := 0; i < len(messages); i++ {
		m := messages[i]

		switch m.Role {
		case "assistant":
			var blocks []AnthropicContentBlock

			// Include thinking content if present
			if m.ReasoningContent != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type:     "thinking",
					Thinking: m.ReasoningContent,
				})
			}

			// Include text content
			if m.Content != "" {
				blocks = append(blocks, AnthropicContentBlock{
					Type: "text",
					Text: m.Content,
				})
			}

			// Convert tool calls to tool_use blocks
			for _, tc := range m.ToolCalls {
				block := AnthropicContentBlock{
					Type: "tool_use",
					ID:   tc.ID,
				}
				if tc.Function != nil {
					block.Name = tc.Function.Name
					args := tc.Function.Arguments
					if args == "" || args == "null" {
						args = "{}"
					}
					block.Input = json.RawMessage(args)
				} else {
					block.Input = json.RawMessage("{}")
				}
				blocks = append(blocks, block)
			}

			if len(blocks) > 0 {
				result = append(result, AnthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			}

		case "tool":
			// Group consecutive tool messages into a single user message with tool_result blocks
			var toolBlocks []AnthropicContentBlock
			for ; i < len(messages) && messages[i].Role == "tool"; i++ {
				toolBlocks = append(toolBlocks, AnthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: messages[i].ToolCallID,
					Content:   messages[i].Content,
				})
			}
			i-- // back up since the for loop will increment
			result = append(result, AnthropicMessage{
				Role:    "user",
				Content: toolBlocks,
			})

		case "user":
			result = append(result, AnthropicMessage{
				Role:    "user",
				Content: m.Content,
			})

		default:
			// Skip unknown roles
			slog.Warn("Skipping unknown message role in Anthropic conversion", "role", m.Role)
		}
	}

	// Enforce strict user/assistant alternation by merging consecutive same-role messages
	result = enforceAlternation(result)

	return result
}

// enforceAlternation merges consecutive messages with the same role to satisfy
// Anthropic's strict user/assistant alternation requirement.
func enforceAlternation(messages []AnthropicMessage) []AnthropicMessage {
	if len(messages) == 0 {
		return messages
	}

	var merged []AnthropicMessage
	for _, m := range messages {
		if len(merged) > 0 && merged[len(merged)-1].Role == m.Role {
			// Merge into the previous message
			prev := &merged[len(merged)-1]
			prevBlocks := toContentBlocks(prev.Content)
			newBlocks := toContentBlocks(m.Content)
			prev.Content = append(prevBlocks, newBlocks...)
		} else {
			merged = append(merged, m)
		}
	}

	// Ensure conversation starts with a user message
	if len(merged) > 0 && merged[0].Role != "user" {
		merged = append([]AnthropicMessage{{
			Role:    "user",
			Content: "Continue.",
		}}, merged...)
	}

	return merged
}

// toContentBlocks normalizes message content to []AnthropicContentBlock.
func toContentBlocks(content any) []AnthropicContentBlock {
	switch v := content.(type) {
	case string:
		return []AnthropicContentBlock{{Type: "text", Text: v}}
	case []AnthropicContentBlock:
		return v
	default:
		return []AnthropicContentBlock{{Type: "text", Text: fmt.Sprintf("%v", v)}}
	}
}

// --- Vision Translation ---

// convertVisionToAnthropic creates an Anthropic request for a vision (image) call.
func convertVisionToAnthropic(model, system, userText, base64Image string, provider *LLMProvider) AnthropicRequest {
	model = strings.TrimPrefix(model, "anthropic/")

	// Parse media type from the base64 data URL prefix if present
	mediaType := "image/png" // default

	systemBlocks := []AnthropicContentBlock{
		{
			Type:         "text",
			Text:         system,
			CacheControl: &AnthropicCacheControl{Type: "ephemeral"},
		},
	}

	userBlocks := []AnthropicContentBlock{
		{Type: "text", Text: userText},
		{
			Type: "image",
			Source: &AnthropicImageSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      base64Image,
			},
		},
	}

	return AnthropicRequest{
		Model:     model,
		MaxTokens: 16384,
		System:    systemBlocks,
		Messages: []AnthropicMessage{
			{Role: "user", Content: userBlocks},
		},
		Stream: false,
	}
}

// --- Non-Streaming Response Parser ---

// parseAnthropicResponse converts an Anthropic API response to internal ChatResponse.
func parseAnthropicResponse(body []byte) (ChatResponse, error) {
	var raw AnthropicResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return ChatResponse{}, fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	var content string
	var reasoning string
	var toolCalls []ToolCall

	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "thinking":
			reasoning += block.Thinking
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: &FunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	// Map stop_reason
	finishReason := mapStopReason(raw.StopReason)

	msg := Message{
		Content:          content,
		ReasoningContent: reasoning,
		ToolCalls:        toolCalls,
	}

	return ChatResponse{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: raw.Usage.ToUsageInfo(),
	}, nil
}

// mapStopReason converts Anthropic stop_reason to OpenAI finish_reason.
func mapStopReason(stopReason string) string {
	switch stopReason {
	case "tool_use":
		return "tool_calls"
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return stopReason
	}
}

// --- Streaming SSE Parser ---

// AnthropicStreamResult holds the accumulated result of parsing an Anthropic SSE stream.
type AnthropicStreamResult struct {
	Text         string
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        UsageInfo
}

// parseAnthropicSSEStream reads and parses the Anthropic SSE stream format.
// It returns accumulated text, tool calls, reasoning, and usage info.
func parseAnthropicSSEStream(
	body io.Reader,
	onChunk func(chunk string),
	onReasoning func(chunk string),
	onToolStart func(toolName string),
) (*AnthropicStreamResult, error) {
	var fullText strings.Builder
	var fullReasoning strings.Builder
	var finishReason string
	var usage AnthropicUsage

	// Track content blocks by index for tool call accumulation
	type blockInfo struct {
		blockType string
		id        string
		name      string
		input     strings.Builder
	}
	blocks := make(map[int]*blockInfo)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	var currentEvent string

	for scanner.Scan() {
		line := scanner.Text()

		// Track event type
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Only process data lines
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		switch currentEvent {
		case "message_start":
			// Extract initial usage from message_start
			var envelope struct {
				Message struct {
					Usage AnthropicUsage `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &envelope); err == nil {
				usage = envelope.Message.Usage
			}

		case "content_block_start":
			var block struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &block); err == nil {
				blocks[block.Index] = &blockInfo{
					blockType: block.ContentBlock.Type,
					id:        block.ContentBlock.ID,
					name:      block.ContentBlock.Name,
				}
				if block.ContentBlock.Type == "tool_use" && block.ContentBlock.Name != "" && onToolStart != nil {
					onToolStart(block.ContentBlock.Name)
				}
			}

		case "content_block_delta":
			var delta struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text,omitempty"`
					PartialJSON string `json:"partial_json,omitempty"`
					Thinking    string `json:"thinking,omitempty"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err != nil {
				continue
			}

			switch delta.Delta.Type {
			case "text_delta":
				fullText.WriteString(delta.Delta.Text)
				if onChunk != nil {
					onChunk(delta.Delta.Text)
				}
			case "input_json_delta":
				if b, ok := blocks[delta.Index]; ok {
					b.input.WriteString(delta.Delta.PartialJSON)
				}
			case "thinking_delta":
				fullReasoning.WriteString(delta.Delta.Thinking)
				if onReasoning != nil {
					onReasoning(delta.Delta.Thinking)
				}
			}

		case "message_delta":
			var msgDelta struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage AnthropicUsage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &msgDelta); err == nil {
				finishReason = mapStopReason(msgDelta.Delta.StopReason)
				// Merge output_tokens from message_delta
				usage.OutputTokens = msgDelta.Usage.OutputTokens
			}

		case "message_stop":
			// End of message — nothing to do

		case "ping":
			// Keep-alive — ignore

		case "error":
			slog.Error("Anthropic SSE error event", "data", data)
			// Parse error type to return retryable errors
			var errEvt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &errEvt) == nil {
				if errEvt.Error.Type == "overloaded_error" || errEvt.Error.Type == "rate_limit_error" {
					return nil, &ErrRateLimited{
						RetryAfter: 30 * time.Second,
						StatusCode: 529,
						Body:       data,
					}
				}
			}
			return nil, fmt.Errorf("anthropic stream error: %s", data)
		}

		currentEvent = "" // reset after processing
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Build tool calls from accumulated blocks
	var toolCalls []ToolCall
	indices := make([]int, 0, len(blocks))
	for idx := range blocks {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		b := blocks[idx]
		if b.blockType == "tool_use" {
			toolCalls = append(toolCalls, ToolCall{
				ID:   b.id,
				Type: "function",
				Function: &FunctionCall{
					Name:      b.name,
					Arguments: b.input.String(),
				},
			})
		}
	}

	return &AnthropicStreamResult{
		Text:         fullText.String(),
		Reasoning:    fullReasoning.String(),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage:        usage.ToUsageInfo(),
	}, nil
}
