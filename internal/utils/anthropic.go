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

// minCacheableBlockSize is the minimum character count for a system block
// to be eligible for Anthropic prompt caching.
const minCacheableBlockSize = 500

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
	Tools     []any                   `json:"tools,omitempty"`
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
// PromptTokens includes all input tokens (regular + cache) for internal billing.
// Cache tokens are preserved separately so PostHog can apply correct per-category pricing.
func (u AnthropicUsage) ToUsageInfo() UsageInfo {
	return UsageInfo{
		PromptTokens:             u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CompletionTokens:         u.OutputTokens,
		TotalTokens:              u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
	}
}

// --- Request Translation ---

// convertToAnthropicRequest translates an internal ChatRequest into an AnthropicRequest.
func convertToAnthropicRequest(req ChatRequest, provider *LLMProvider) AnthropicRequest {
	// Strip "anthropic/" prefix from model name (OpenRouter format → native)
	model := req.Model
	model = strings.TrimPrefix(model, "anthropic/")

	// Extract only leading contiguous system messages → top-level system field.
	// Mid-conversation system messages (e.g. nudges injected by StreamWithTools)
	// are converted to user-role so they don't destabilize the system block array
	// and break Anthropic's prefix-based prompt caching across tool rounds.
	var systemBlocks []AnthropicContentBlock
	var nonSystemMessages []Message
	seenNonSystem := false
	for _, m := range req.Messages {
		if m.Role == "system" && !seenNonSystem {
			if strings.TrimSpace(m.Content) == "" {
				continue // skip empty system blocks — Anthropic rejects empty text
			}
			systemBlocks = append(systemBlocks, AnthropicContentBlock{
				Type: "text",
				Text: m.Content,
			})
		} else {
			seenNonSystem = true
			if m.Role == "system" {
				// Mid-conversation system message → convert to user role
				// enforceAlternation will merge with adjacent user messages
				nonSystemMessages = append(nonSystemMessages, Message{
					Role:    "user",
					Content: "[System] " + m.Content,
				})
			} else {
				nonSystemMessages = append(nonSystemMessages, m)
			}
		}
	}

	// Inject JSON instruction into system prompt when response_format is json_object
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		systemBlocks = append(systemBlocks, AnthropicContentBlock{
			Type: "text",
			Text: "You MUST respond with valid JSON only. No markdown fences, no explanation outside JSON.",
		})
	}

	// Apply automatic prompt caching to system blocks.
	// Anthropic supports up to 4 cache breakpoints total; we use up to 2 for system
	// blocks, reserving 1 for tools and 1 for the last user message.
	// Strategy: always cache the FIRST system block (stable prompt shared across
	// users/projects) and the LAST system block (dynamic project context). This
	// ensures the static prompt prefix gets cache hits even when the dynamic
	// context changes between calls.
	// Skip caching for one-shot calls (DisablePromptCache) where no follow-up
	// call will read the cache — the 1.25x creation surcharge is wasted.
	if !req.DisablePromptCache && len(systemBlocks) > 0 {
		// Always cache the first system block (stable prefix — enables cross-user sharing)
		if len(systemBlocks[0].Text) > minCacheableBlockSize {
			systemBlocks[0].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
		}
		// Cache the last system block if different from the first (dynamic project context)
		if last := len(systemBlocks) - 1; last > 0 && len(systemBlocks[last].Text) > minCacheableBlockSize {
			systemBlocks[last].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
		}
	}

	// Convert messages
	anthropicMessages := convertMessagesToAnthropic(nonSystemMessages)

	// Mark last user message cacheable for conversation prefix caching.
	// This lets Anthropic cache system blocks + all messages up to the last user turn,
	// so each tool round only pays for the new assistant+tool_result tokens.
	// Skip for one-shot calls where no follow-up will read the cache.
	if !req.DisablePromptCache {
		for i := len(anthropicMessages) - 1; i >= 0; i-- {
			if anthropicMessages[i].Role == "user" {
				blocks := toContentBlocks(anthropicMessages[i].Content)
				if len(blocks) > 0 {
					blocks[len(blocks)-1].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
					anthropicMessages[i].Content = blocks
				}
				break
			}
		}
	}

	// Convert tools — supports both function tools and server-side tools
	var anthropicTools []any
	for _, t := range req.Tools {
		if t.Type == "function" {
			anthropicTools = append(anthropicTools, AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		} else if t.ServerTool != nil {
			st := map[string]any{
				"type": t.ServerTool.Type,
				"name": t.ServerTool.Name,
			}
			if t.ServerTool.MaxUses > 0 {
				st["max_uses"] = t.ServerTool.MaxUses
			}
			anthropicTools = append(anthropicTools, st)
		}
	}
	// Mark last function tool cacheable (skip server tools for caching)
	if !req.DisablePromptCache {
		for i := len(anthropicTools) - 1; i >= 0; i-- {
			if at, ok := anthropicTools[i].(AnthropicTool); ok {
				at.CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
				anthropicTools[i] = at
				break
			}
		}
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
	// Clone messages to avoid mutating caller's slice
	msgs := make([]Message, len(messages))
	copy(msgs, messages)

	var result []AnthropicMessage

	for i := 0; i < len(msgs); i++ {
		m := msgs[i]

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
			for ; i < len(msgs) && msgs[i].Role == "tool"; i++ {
				toolBlocks = append(toolBlocks, AnthropicContentBlock{
					Type:      "tool_result",
					ToolUseID: msgs[i].ToolCallID,
					Content:   msgs[i].Content,
				})
			}
			i-- // back up since the for loop will increment
			result = append(result, AnthropicMessage{
				Role:    "user",
				Content: toolBlocks,
			})

		case "user":
			if len(m.Images) > 0 {
				blocks := []AnthropicContentBlock{{Type: "text", Text: m.Content}}
				for _, img := range m.Images {
					mediaType, data := parseDataURI(img)
					blocks = append(blocks, AnthropicContentBlock{
						Type: "image",
						Source: &AnthropicImageSource{
							Type:      "base64",
							MediaType: mediaType,
							Data:      data,
						},
					})
				}
				result = append(result, AnthropicMessage{
					Role:    "user",
					Content: blocks,
				})
			} else {
				result = append(result, AnthropicMessage{
					Role:    "user",
					Content: m.Content,
				})
			}

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

// parseDataURI splits a "data:<mediaType>;base64,<data>" URI into its media
// type and raw base64 payload. Falls back to "image/png" if the format is
// unexpected.
func parseDataURI(dataURI string) (mediaType, data string) {
	// Expected format: data:image/png;base64,iVBOR...
	if after, ok := strings.CutPrefix(dataURI, "data:"); ok {
		if semi := strings.Index(after, ";base64,"); semi >= 0 {
			return after[:semi], after[semi+8:]
		}
	}
	// Fallback: treat the whole string as raw base64
	return "image/png", dataURI
}

// --- Vision Translation ---

// convertVisionToAnthropic creates an Anthropic request for a vision (image) call.
func convertVisionToAnthropic(model, system, userText, base64Image string, provider *LLMProvider) AnthropicRequest {
	model = strings.TrimPrefix(model, "anthropic/")

	// Parse media type from the base64 data URL prefix if present
	mediaType, imageData := parseDataURI(base64Image)

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
				Data:      imageData,
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
		case "server_tool_use":
			// Server tools (e.g. web_search) are executed by Anthropic's servers
			// within the same API call — do not return them as client tool calls.
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
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

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
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &block); err == nil && block.ContentBlock != nil && block.ContentBlock.Type != "" {
				blocks[block.Index] = &blockInfo{
					blockType: block.ContentBlock.Type,
					id:        block.ContentBlock.ID,
					name:      block.ContentBlock.Name,
				}
				if (block.ContentBlock.Type == "tool_use" || block.ContentBlock.Type == "server_tool_use") && block.ContentBlock.Name != "" && onToolStart != nil {
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
			var errEvt struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(data), &errEvt) == nil {
				// Retryable: overloaded or rate limited
				if errEvt.Error.Type == "overloaded_error" || errEvt.Error.Type == "rate_limit_error" {
					return nil, &ErrRateLimited{
						RetryAfter: 30 * time.Second,
						StatusCode: 529,
						Body:       data,
					}
				}
				// Permanent: invalid request, auth errors, etc — do not retry
				if errEvt.Error.Type == "invalid_request_error" ||
					errEvt.Error.Type == "authentication_error" ||
					errEvt.Error.Type == "permission_error" ||
					errEvt.Error.Type == "not_found_error" {
					return nil, &ErrPermanentLLM{
						ErrorType: errEvt.Error.Type,
						Message:   errEvt.Error.Message,
					}
				}
			}
			return nil, fmt.Errorf("anthropic stream error: %s", data)

		default:
			// Unknown event type — log and skip so API evolution doesn't cause silent data loss.
			if currentEvent != "" {
				slog.Debug("Anthropic SSE: unknown event type, skipping", "event", currentEvent)
			}
		}

		currentEvent = "" // reset after processing
	}

	if err := scanner.Err(); err != nil {
		slog.Error("SSE scanner error in Anthropic stream parser", "error", err)
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
		// Only return client tool calls — server tools (e.g. web_search) are
		// executed by Anthropic's servers within the same stream.
		if b.blockType == "tool_use" {
			args := b.input.String()
			// Validate tool call arguments are valid JSON; if the stream was
			// truncated mid-tool, the partial JSON would cause downstream errors.
			if args != "" && !json.Valid([]byte(args)) {
				slog.Warn("Anthropic SSE: discarding tool call with invalid JSON arguments",
					"tool", b.name, "args_length", len(args))
				continue
			}
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   b.id,
				Type: "function",
				Function: &FunctionCall{
					Name:      b.name,
					Arguments: args,
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
