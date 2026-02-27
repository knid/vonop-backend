package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Rate limit error ---

// ErrRateLimited is returned when the LLM API responds with HTTP 429.
type ErrRateLimited struct {
	RetryAfter time.Duration
	StatusCode int
	Body       string
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("rate limited (HTTP %d), retry after %s", e.StatusCode, e.RetryAfter)
}

// IsRateLimited checks if an error is a rate limit error and returns it.
func IsRateLimited(err error) (*ErrRateLimited, bool) {
	var rl *ErrRateLimited
	if errors.As(err, &rl) {
		return rl, true
	}
	return nil, false
}

// parseRateLimitError returns an ErrRateLimited for 429/529 responses, nil otherwise.
func parseRateLimitError(resp *http.Response, body []byte) *ErrRateLimited {
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != 529 {
		return nil
	}
	retryAfter := 30 * time.Second // default
	if raHeader := resp.Header.Get("Retry-After"); raHeader != "" {
		if secs, err := strconv.Atoi(raHeader); err == nil && secs > 0 {
			retryAfter = time.Duration(secs) * time.Second
		}
	}
	if retryAfter > 120*time.Second {
		retryAfter = 120 * time.Second
	}
	return &ErrRateLimited{
		RetryAfter: retryAfter,
		StatusCode: resp.StatusCode,
		Body:       string(body),
	}
}

// --- Token usage tracking ---

// TokenEntry records usage from a single LLM call.
type TokenEntry struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMs        int64
	IsStreaming      bool
	InputMessages    []Message // optional: messages sent to LLM (only when TrackContent is set)
	OutputText       string    // optional: LLM text completion (only when TrackContent is set)
}

// TokenTracker is a thread-safe accumulator that collects token usage across multiple LLM calls.
type TokenTracker struct {
	mu           sync.Mutex
	entries      []TokenEntry
	TrackContent bool   // opt-in: when true, StreamWithTools records message content per round
	TraceInput   string // user's original message (for $ai_trace)
	TraceOutput  string // final assistant response (for $ai_trace)
}

func NewTokenTracker() *TokenTracker {
	return &TokenTracker{}
}

// Record adds a usage entry to the tracker.
func (t *TokenTracker) Record(model string, usage UsageInfo, latencyMs int64, isStreaming bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, TokenEntry{
		Model:            model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		LatencyMs:        latencyMs,
		IsStreaming:      isStreaming,
	})
}

// RecordWithContent adds a usage entry including the messages and output text.
func (t *TokenTracker) RecordWithContent(model string, usage UsageInfo, latencyMs int64, isStreaming bool, inputMessages []Message, outputText string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Snapshot messages so caller mutations don't affect stored data
	msgs := make([]Message, len(inputMessages))
	copy(msgs, inputMessages)
	t.entries = append(t.entries, TokenEntry{
		Model:            model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		LatencyMs:        latencyMs,
		IsStreaming:      isStreaming,
		InputMessages:    msgs,
		OutputText:       outputText,
	})
}

// Entries returns a copy of all recorded token entries.
func (t *TokenTracker) Entries() []TokenEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]TokenEntry, len(t.entries))
	copy(entries, t.entries)
	return entries
}

// Totals returns aggregated token counts and the number of LLM calls recorded.
func (t *TokenTracker) Totals() (prompt, completion, total, callCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.entries {
		prompt += e.PromptTokens
		completion += e.CompletionTokens
		total += e.TotalTokens
	}
	callCount = len(t.entries)
	return
}

// Context helpers for passing a TokenTracker through context.
type tokenTrackerKey struct{}

func WithTokenTracker(ctx context.Context, t *TokenTracker) context.Context {
	return context.WithValue(ctx, tokenTrackerKey{}, t)
}

func TokenTrackerFromContext(ctx context.Context) *TokenTracker {
	t, _ := ctx.Value(tokenTrackerKey{}).(*TokenTracker)
	return t
}

// llmHTTPClient is a shared HTTP client for all LLM calls with a 10-minute timeout.
var llmHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
}

type ResponseFormat struct {
	Type string `json:"type"`
}

// --- Tool calling types ---

type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema
}

type ToolCall struct {
	Index    *int          `json:"index,omitempty"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function *FunctionCall `json:"function,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ToolExecutor is called when the LLM wants to use a tool.
// Returns the tool result as a string.
type ToolExecutor func(name string, arguments string) (string, error)

// ReasoningConfig controls thinking/reasoning mode on OpenRouter.
// See https://openrouter.ai/docs/guides/best-practices/reasoning-tokens
type ReasoningConfig struct {
	Effort  string `json:"effort,omitempty"`     // "none", "low", "medium", "high", "xhigh"
	Exclude bool   `json:"exclude,omitempty"`    // true = model thinks but doesn't return reasoning tokens
	Enabled *bool  `json:"enabled,omitempty"`    // explicitly enable/disable
}

// ThinkingConfig controls thinking mode on ZAI (GLM-4.7).
// See https://docs.z.ai/guides/capabilities/thinking-mode
type ThinkingConfig struct {
	Type          string `json:"type"`                     // "enabled" or "disabled"
	ClearThinking bool   `json:"clear_thinking,omitempty"` // false = preserve for cache
}

type StreamOptionsConfig struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatRequest struct {
	Model               string               `json:"model"`
	Messages            []Message             `json:"messages"`
	Stream              bool                  `json:"stream,omitempty"`
	StreamOptions       *StreamOptionsConfig  `json:"stream_options,omitempty"`
	MaxTokens           int                   `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"`
	Temperature         *float64              `json:"temperature,omitempty"`
	TopP                *float64              `json:"top_p,omitempty"`
	ResponseFormat      *ResponseFormat       `json:"response_format,omitempty"`
	Tools               []Tool                `json:"tools,omitempty"`
	ToolChoice          any                   `json:"tool_choice,omitempty"`
	Reasoning           *ReasoningConfig      `json:"reasoning,omitempty"`
	Thinking            *ThinkingConfig       `json:"thinking,omitempty"`
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // ZAI: preserve thinking across tool rounds
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
}

// Response structure for parsing the LLM output
type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	Choices []Choice  `json:"choices"`
	Usage   UsageInfo `json:"usage"`
}

// buildLLMRequestBody marshals a ChatRequest into the correct wire format
// for the given provider. For Anthropic, it translates to the Messages API format.
func buildLLMRequestBody(req ChatRequest, provider *LLMProvider) ([]byte, error) {
	if provider != nil && provider.IsAnthropic() {
		anthReq := convertToAnthropicRequest(req, provider)
		return json.Marshal(anthReq)
	}
	prepared := prepareChatRequest(req, provider)
	return json.Marshal(prepared)
}

// buildVisionRequestBody marshals a vision request into the correct wire format.
func buildVisionRequestBody(model, system, userText, base64Image string, provider *LLMProvider) ([]byte, error) {
	if provider != nil && provider.IsAnthropic() {
		anthReq := convertVisionToAnthropic(model, system, userText, base64Image, provider)
		return json.Marshal(anthReq)
	}
	reqBody := MultiChatRequest{
		Model: model,
		Messages: []MultiMessage{
			{Role: "system", Content: system},
			{
				Role: "user",
				Content: []MultiContentPart{
					{Type: "text", Text: userText},
					{
						Type: "image_url",
						ImageURL: &ImageURLContent{
							URL: "data:image/png;base64," + base64Image,
						},
					},
				},
			},
		},
	}
	return json.Marshal(reqBody)
}

// buildStreamVisionRequestBody marshals a streaming vision request.
func buildStreamVisionRequestBody(model, system, userText, base64Image string, provider *LLMProvider) ([]byte, error) {
	if provider != nil && provider.IsAnthropic() {
		anthReq := convertVisionToAnthropic(model, system, userText, base64Image, provider)
		anthReq.Stream = true
		return json.Marshal(anthReq)
	}
	type streamMultiChatRequest struct {
		Model         string               `json:"model"`
		Stream        bool                 `json:"stream"`
		StreamOptions *StreamOptionsConfig `json:"stream_options,omitempty"`
		Messages      []MultiMessage       `json:"messages"`
	}
	reqBody := streamMultiChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &StreamOptionsConfig{IncludeUsage: true},
		Messages: []MultiMessage{
			{Role: "system", Content: system},
			{
				Role: "user",
				Content: []MultiContentPart{
					{Type: "text", Text: userText},
					{
						Type: "image_url",
						ImageURL: &ImageURLContent{
							URL: "data:image/png;base64," + base64Image,
						},
					},
				},
			},
		},
	}
	return json.Marshal(reqBody)
}

// parseLLMResponseBody unmarshals a non-streaming LLM response body.
func parseLLMResponseBody(body []byte, provider *LLMProvider) (ChatResponse, error) {
	if provider != nil && provider.IsAnthropic() {
		return parseAnthropicResponse(body)
	}
	var parsed ChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ChatResponse{}, err
	}
	return parsed, nil
}

// prepareChatRequest translates provider-agnostic ChatRequest fields into the
// format expected by the target LLM provider. For providers that use the
// "thinking" format (ZAI, Moonshot/Kimi), it converts the OpenRouter-style
// "reasoning" config into the thinking config and sets required defaults.
func prepareChatRequest(req ChatRequest, provider *LLMProvider) ChatRequest {
	if provider == nil {
		return req
	}

	// Anthropic uses a different request format entirely (handled by buildLLMRequestBody)
	if provider.IsAnthropic() {
		return req
	}

	// Apply provider's default reasoning when no explicit config was supplied
	if provider.UsesThinkingFormat() && req.Reasoning == nil && req.Thinking == nil && provider.DefaultReasoning != nil {
		req.Reasoning = provider.DefaultReasoning
	}

	// Translate reasoning -> thinking for providers that use the thinking format
	if provider.UsesThinkingFormat() && req.Reasoning != nil {
		thinkingType := "disabled"
		if req.Reasoning.Effort != "" && req.Reasoning.Effort != "none" {
			thinkingType = "enabled"
		}

		req.Thinking = &ThinkingConfig{
			Type:          thinkingType,
			ClearThinking: false, // preserve for cache
		}
		req.Reasoning = nil // don't send OpenRouter format

		if thinkingType == "enabled" {
			// K2.5 thinking: temperature MUST be 1.0, top_p MUST be 0.95
			t := 1.0
			req.Temperature = &t
			tp := 0.95
			req.TopP = &tp
			// Swap max_tokens -> max_completion_tokens so reasoning tokens
			// don't eat into the output budget
			if req.MaxTokens > 0 && req.MaxCompletionTokens == 0 {
				req.MaxCompletionTokens = req.MaxTokens
				req.MaxTokens = 0
			}
		} else if provider.IsMoonshot() {
			// K2.5 non-thinking: temperature MUST be 0.6, top_p MUST be 0.95
			t := 0.6
			req.Temperature = &t
			tp := 0.95
			req.TopP = &tp
		}
	}

	// Moonshot/K2.5: always prefer max_completion_tokens over max_tokens
	if provider.IsMoonshot() && req.MaxTokens > 0 && req.MaxCompletionTokens == 0 {
		req.MaxCompletionTokens = req.MaxTokens
		req.MaxTokens = 0
	}

	return req
}

// GetJSONFromLLM sends a system+user message to the LLM and returns the raw response text.
func GetJSONFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider) (string, error) {
	if provider == nil || provider.APIKey == "" {
		slog.Error("LLM provider is not configured")
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("error generating items. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		bodyStr := string(errBody)
		if len(bodyStr) > 2000 {
			bodyStr = bodyStr[:2000] + "...(truncated)"
		}
		slog.Error("LLM returned non-200", "status", resp.StatusCode, "body", bodyStr, "model", model, "provider", provider.Name)
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Response received", "FROM", "LLM_UTILS")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error creating items.", "error", err, "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("error creating items. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, parsed.Usage, latencyMs, false)
		}
	}

	return parsed.Choices[0].Message.Content, nil
}

func GetTextFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS")
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		bodyStr := string(errBody)
		if len(bodyStr) > 2000 {
			bodyStr = bodyStr[:2000] + "...(truncated)"
		}
		slog.Error("LLM returned non-200", "status", resp.StatusCode, "body", bodyStr, "model", model, "provider", provider.Name)
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Response received", "FROM", "LLM_UTILS_TEXT")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error parsing LLM response", "error", err, "FROM", "LLM_UTILS_TEXT")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS_TEXT")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_TEXT", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, parsed.Usage, latencyMs, false)
		}
	}

	return parsed.Choices[0].Message.Content, nil
}

type StreamDelta struct {
	Content          string     `json:"content,omitempty"`
	Reasoning        string     `json:"reasoning,omitempty"`          // OpenRouter
	ReasoningContent string     `json:"reasoning_content,omitempty"`  // ZAI
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// GetReasoning returns the reasoning text from whichever field the provider uses.
func (d *StreamDelta) GetReasoning() string {
	if d.ReasoningContent != "" {
		return d.ReasoningContent
	}
	return d.Reasoning
}

type StreamChoice struct {
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
	Usage   *UsageInfo     `json:"usage,omitempty"`
}

// StreamTextFromLLM streams LLM response tokens and calls onChunk for each text chunk.
// Returns the full accumulated text.
func StreamTextFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider, reasoning *ReasoningConfig, onChunk func(chunk string), onReasoning func(chunk string)) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &StreamOptionsConfig{IncludeUsage: true},
		MaxTokens:     16384,
		Reasoning:     reasoning,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

	// Anthropic SSE format is different — use dedicated parser
	if provider.IsAnthropic() {
		result, err := parseAnthropicSSEStream(resp.Body, onChunk, onReasoning, nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if rlErr, ok := IsRateLimited(err); ok {
				return "", rlErr
			}
			return "", fmt.Errorf("error reading streaming response. try again please")
		}
		if result.Usage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_TEXT", "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			prefix := data
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			slog.Debug("Skipped malformed SSE chunk", "error", err, "data_prefix", prefix)
			continue
		}

		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			if r := chunk.Choices[0].Delta.GetReasoning(); r != "" && onReasoning != nil {
				onReasoning(r)
			}
			if chunk.Choices[0].Delta.Content != "" {
				content := chunk.Choices[0].Delta.Content
				fullText.WriteString(content)
				if onChunk != nil {
					onChunk(content)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			slog.Warn("LLM streaming interrupted", "reason", ctx.Err())
			return fullText.String(), ctx.Err()
		}
		slog.Error("scanner error during LLM streaming", "error", err)
		return "", fmt.Errorf("error reading streaming response. try again please")
	}

	if lastUsage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_STREAM_TEXT", "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
		}
	}

	return fullText.String(), nil
}

// StreamTextFromLLMWithMessages streams LLM response tokens for a pre-built message array.
// Useful for multi-block system prompts (caching) without tool calling overhead.
func StreamTextFromLLMWithMessages(ctx context.Context, model string, messages []Message, provider *LLMProvider, onChunk func(chunk string), onReasoning func(chunk string)) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &StreamOptionsConfig{IncludeUsage: true},
		MaxTokens:     16384,
		Messages:      messages,
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

	// Anthropic SSE format — use dedicated parser
	if provider.IsAnthropic() {
		result, err := parseAnthropicSSEStream(resp.Body, onChunk, onReasoning, nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if rlErr, ok := IsRateLimited(err); ok {
				return "", rlErr
			}
			return "", fmt.Errorf("error reading streaming response. try again please")
		}
		if result.Usage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_TEXT_MSGS", "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			if r := chunk.Choices[0].Delta.GetReasoning(); r != "" && onReasoning != nil {
				onReasoning(r)
			}
			if chunk.Choices[0].Delta.Content != "" {
				content := chunk.Choices[0].Delta.Content
				fullText.WriteString(content)
				if onChunk != nil {
					onChunk(content)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}
		return "", fmt.Errorf("error reading streaming response. try again please")
	}

	if lastUsage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_STREAM_TEXT_MSGS", "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
		}
	}

	return fullText.String(), nil
}

// GetJSONFromLLMWithMessages sends a multi-turn conversation to the LLM and expects JSON output.
// Used for chat-based interactions where conversation history matters.
func GetJSONFromLLMWithMessages(ctx context.Context, model string, messages []Message, provider *LLMProvider) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:          model,
		Messages:       messages,
		MaxTokens:      16384,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		bodyStr := string(errBody)
		if len(bodyStr) > 2000 {
			bodyStr = bodyStr[:2000] + "...(truncated)"
		}
		slog.Error("LLM returned non-200", "status", resp.StatusCode, "body", bodyStr, "model", model, "provider", provider.Name)
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Chat Response received", "FROM", "LLM_UTILS_CHAT")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error parsing LLM response", "error", err, "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_CHAT", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, parsed.Usage, latencyMs, false)
		}
	}

	return parsed.Choices[0].Message.Content, nil
}

// StreamJSONFromLLMWithMessages streams an LLM response from a multi-turn conversation.
// Sends chunks via onChunk and returns the cleaned JSON.
func StreamJSONFromLLMWithMessages(ctx context.Context, model string, messages []Message, provider *LLMProvider, onChunk func(chunk string), onReasoning func(chunk string)) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:          model,
		Stream:         true,
		StreamOptions:  &StreamOptionsConfig{IncludeUsage: true},
		Messages:       messages,
		MaxTokens:      16384,
		ResponseFormat: &ResponseFormat{Type: "json_object"},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

	// Anthropic SSE format — use dedicated parser
	if provider.IsAnthropic() {
		result, err := parseAnthropicSSEStream(resp.Body, onChunk, onReasoning, nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if rlErr, ok := IsRateLimited(err); ok {
				return "", rlErr
			}
			return "", fmt.Errorf("error reading streaming response. try again please")
		}
		if result.FinishReason == "length" {
			slog.Warn("LLM chat response truncated (finish_reason=length)")
		}
		if result.Usage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_CHAT", "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastFinishReason string
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			prefix := data
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			slog.Debug("Skipped malformed SSE chunk", "error", err, "data_prefix", prefix)
			continue
		}

		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			if r := chunk.Choices[0].Delta.GetReasoning(); r != "" && onReasoning != nil {
				onReasoning(r)
			}
			if chunk.Choices[0].Delta.Content != "" {
				content := chunk.Choices[0].Delta.Content
				fullText.WriteString(content)
				if onChunk != nil {
					onChunk(content)
				}
			}
			if chunk.Choices[0].FinishReason != nil {
				lastFinishReason = *chunk.Choices[0].FinishReason
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			slog.Warn("LLM streaming interrupted", "reason", ctx.Err())
			return fullText.String(), ctx.Err()
		}
		slog.Error("scanner error during LLM streaming", "error", err)
		return "", fmt.Errorf("error reading streaming response. try again please")
	}

	if lastFinishReason == "length" {
		slog.Warn("LLM chat response truncated (finish_reason=length)", "response_length", fullText.Len())
	}

	if lastUsage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_STREAM_CHAT", "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
		}
	}

	return fullText.String(), nil
}

// MultiContentPart represents a content part in a multimodal message.
type MultiContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *ImageURLContent  `json:"image_url,omitempty"`
}

type ImageURLContent struct {
	URL string `json:"url"`
}

type MultiMessage struct {
	Role    string        `json:"role"`
	Content any           `json:"content"`
}

type MultiChatRequest struct {
	Model    string         `json:"model"`
	Messages []MultiMessage `json:"messages"`
}

// GetJSONFromLLMWithVision sends a multimodal request with an image to the LLM.
func GetJSONFromLLMWithVision(ctx context.Context, model, system, userText, base64Image string, provider *LLMProvider) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}
	if !provider.SupportsVision() {
		return "", fmt.Errorf("vision is not supported by %s provider", provider.Name)
	}

	body, err := buildVisionRequestBody(model, system, userText, base64Image, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making vision request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Vision Response received", "FROM", "LLM_UTILS_VISION")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error parsing LLM vision response", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_VISION", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, parsed.Usage, latencyMs, false)
		}
	}

	return parsed.Choices[0].Message.Content, nil
}

// StreamJSONFromLLM streams LLM response tokens for a JSON-expecting call.
// Sends chunks via onChunk and returns the cleaned JSON output.
func StreamJSONFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider, onChunk func(chunk string), onReasoning func(chunk string)) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &StreamOptionsConfig{IncludeUsage: true},
		MaxTokens:     16384,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

	// Anthropic SSE format — use dedicated parser
	if provider.IsAnthropic() {
		result, err := parseAnthropicSSEStream(resp.Body, onChunk, onReasoning, nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if rlErr, ok := IsRateLimited(err); ok {
				return "", rlErr
			}
			return "", fmt.Errorf("error reading streaming response. try again please")
		}
		if result.Usage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_JSON", "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			prefix := data
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			slog.Debug("Skipped malformed SSE chunk", "error", err, "data_prefix", prefix)
			continue
		}

		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			if r := chunk.Choices[0].Delta.GetReasoning(); r != "" && onReasoning != nil {
				onReasoning(r)
			}
			if chunk.Choices[0].Delta.Content != "" {
				content := chunk.Choices[0].Delta.Content
				fullText.WriteString(content)
				if onChunk != nil {
					onChunk(content)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			slog.Warn("LLM streaming interrupted", "reason", ctx.Err())
			return fullText.String(), ctx.Err()
		}
		slog.Error("scanner error during LLM streaming", "error", err)
		return "", fmt.Errorf("error reading streaming response. try again please")
	}

	if lastUsage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_STREAM_JSON", "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
		}
	}

	return fullText.String(), nil
}

// StreamJSONFromLLMWithVision streams a multimodal (vision) LLM response.
// Sends chunks via onChunk and returns the cleaned JSON output.
func StreamJSONFromLLMWithVision(ctx context.Context, model, system, userText, base64Image string, provider *LLMProvider, onChunk func(chunk string), onReasoning func(chunk string)) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}
	if !provider.SupportsVision() {
		return "", fmt.Errorf("vision is not supported by %s provider", provider.Name)
	}

	body, err := buildStreamVisionRequestBody(model, system, userText, base64Image, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	setLLMHeaders(req, provider)

	start := time.Now()
	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making vision streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM vision streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

	// Anthropic SSE format — use dedicated parser
	if provider.IsAnthropic() {
		result, err := parseAnthropicSSEStream(resp.Body, onChunk, onReasoning, nil)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if rlErr, ok := IsRateLimited(err); ok {
				return "", rlErr
			}
			return "", fmt.Errorf("error reading streaming response. try again please")
		}
		if result.Usage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_VISION", "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return fullText.String(), ctx.Err()
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			prefix := data
			if len(prefix) > 200 {
				prefix = prefix[:200]
			}
			slog.Debug("Skipped malformed SSE chunk", "error", err, "data_prefix", prefix)
			continue
		}

		if chunk.Usage != nil {
			lastUsage = *chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			if r := chunk.Choices[0].Delta.GetReasoning(); r != "" && onReasoning != nil {
				onReasoning(r)
			}
			if chunk.Choices[0].Delta.Content != "" {
				content := chunk.Choices[0].Delta.Content
				fullText.WriteString(content)
				if onChunk != nil {
					onChunk(content)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			slog.Warn("LLM vision streaming interrupted", "reason", ctx.Err())
			return fullText.String(), ctx.Err()
		}
		slog.Error("scanner error during vision LLM streaming", "error", err)
		return "", fmt.Errorf("error reading streaming response. try again please")
	}

	if lastUsage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_STREAM_VISION", "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
		}
	}

	return fullText.String(), nil
}

// ToolCallConfig holds optional configuration for StreamWithTools.
type ToolCallConfig struct {
	MaxRounds        int                                                   // Maximum tool-calling rounds (default 10)
	NudgeAfter       int                                                   // Inject nudge after this many consecutive tool rounds (default 3)
	StripAfter       int                                                   // Strip tools entirely after this many consecutive rounds (default 5)
	MaxTokens        int                                                   // Max completion tokens per round (default 16384)
	TrimArgs         func(toolName, args string) string                    // Trim tool call arguments in history to reduce token usage
	TrimResults      func(toolName, result string) string                  // Trim tool result messages in history to reduce token usage
	InterceptToolCall func(toolName, toolArgs string) (result string, skip bool) // Intercept tool calls before execution; if skip=true, use result and skip executor
	NudgeMessage     string                                                // Custom nudge message (replaces default "stop researching" text)
	AfterToolRound   func(allIntercepted bool) string                      // Called after each tool round; allIntercepted=true when every call was intercepted; non-empty return is injected as a system message
	FilterTools      func(tools []Tool) []Tool                             // Called each round to filter/modify tools; return nil to strip all tools
	OnToolCallStart  func(toolName string)                                  // Called when a tool call name is first detected during SSE streaming (before args are complete)
}

// StreamWithTools sends messages to LLM with tools, handles the tool call loop,
// and returns the final JSON response. Max 10 tool call rounds to prevent infinite loops.
// onToolCall is called for each tool invocation (for SSE progress).
// onChunk is called for content streaming.
// An optional ToolCallConfig can be passed to override defaults.
func StreamWithTools(
	ctx context.Context,
	model string,
	messages []Message,
	tools []Tool,
	provider *LLMProvider,
	responseFormat *ResponseFormat,
	reasoning *ReasoningConfig,
	executor ToolExecutor,
	onToolCall func(toolName string, args string),
	onChunk func(chunk string),
	onReasoning func(chunk string),
	opts ...ToolCallConfig,
) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	maxRounds := 10
	nudgeAfter := 3
	stripAfter := 5
	maxTokens := 16384
	if len(opts) > 0 {
		if opts[0].MaxRounds > 0 {
			maxRounds = opts[0].MaxRounds
		}
		if opts[0].NudgeAfter > 0 {
			nudgeAfter = opts[0].NudgeAfter
		}
		if opts[0].StripAfter > 0 {
			stripAfter = opts[0].StripAfter
		}
		if opts[0].MaxTokens > 0 {
			maxTokens = opts[0].MaxTokens
		}
	}
	currentMessages := make([]Message, len(messages))
	copy(currentMessages, messages)
	toolCallHistory := make(map[string]string) // "tool:args" → result (dedup cache)
	consecutiveToolRounds := 0
	nudged := false
	rateLimitRetries := 0

	for round := 0; round < maxRounds; round++ {
		// Progressively nudge the AI to stop researching and produce a final response.
		// Phase 1 (nudgeAfter): inject a nudge message but keep tools available.
		// Phase 2 (stripAfter): strip tools entirely to force text generation.
		roundTools := tools
		if len(opts) > 0 && opts[0].FilterTools != nil {
			roundTools = opts[0].FilterTools(roundTools)
		}
		if consecutiveToolRounds >= stripAfter {
			currentMessages = append(currentMessages, Message{
				Role:    "system",
				Content: "FINAL WARNING: You MUST generate your complete response NOW. All tools have been removed. Use the information you have already gathered.",
			})
			roundTools = nil
			slog.Warn("Stripping tools to force final response", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
		} else if consecutiveToolRounds >= nudgeAfter && !nudged {
			nudgeContent := "You have already gathered sufficient context from tool calls. Please wrap up your research and generate your final complete response. You may make one or two more targeted tool calls if absolutely necessary, but prioritize producing your response."
			if len(opts) > 0 && opts[0].NudgeMessage != "" {
				nudgeContent = opts[0].NudgeMessage
			}
			currentMessages = append(currentMessages, Message{
				Role:    "system",
				Content: nudgeContent,
			})
			nudged = true
			slog.Warn("Injecting stop-researching nudge (tools still available)", "round", round, "consecutive_tool_rounds", consecutiveToolRounds)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		body, err := buildLLMRequestBody(ChatRequest{
			Model:          model,
			Stream:         true,
			StreamOptions:  &StreamOptionsConfig{IncludeUsage: true},
			MaxTokens:      maxTokens,
			Reasoning:      reasoning,
			Messages:       currentMessages,
			Tools:          roundTools,
			ResponseFormat: responseFormat,
		}, provider)
		if err != nil {
			return "", fmt.Errorf("error building request: %w", err)
		}

		req, _ := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
		setLLMHeaders(req, provider)

		roundStart := time.Now()
		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			slog.Error("Error making streaming request to LLM:", "error", err, "round", round)
			return "", fmt.Errorf("error generating content. try again please")
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Handle 429 rate limit with retry + backoff
			if rlErr := parseRateLimitError(resp, respBody); rlErr != nil && rateLimitRetries < 5 {
				rateLimitRetries++
				wait := rlErr.RetryAfter * time.Duration(rateLimitRetries)
				if wait > 120*time.Second {
					wait = 120 * time.Second
				}
				slog.Warn("LLM rate limited, retrying", "retry_after", wait, "attempt", rateLimitRetries, "round", round)
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return "", ctx.Err()
				}
				round-- // retry same round
				continue
			}
			slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody), "round", round)
			if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
				return "", rlErr
			}
			return "", fmt.Errorf("error generating content. try again please")
		}
		rateLimitRetries = 0 // reset after successful request

		var fullText strings.Builder
		var fullReasoning strings.Builder // accumulate reasoning_content for cache continuity
		var roundChunks []string          // buffer chunks per round to suppress intermediate reasoning
		var lastFinishReason string
		var roundUsage UsageInfo
		toolCallsMap := make(map[int]*ToolCall)

		// Anthropic SSE format — use dedicated parser then map to local vars
		if provider.IsAnthropic() {
			var toolStartCb func(string)
			if len(opts) > 0 && opts[0].OnToolCallStart != nil {
				toolStartCb = opts[0].OnToolCallStart
			}
			result, parseErr := parseAnthropicSSEStream(resp.Body, func(chunk string) {
				roundChunks = append(roundChunks, chunk)
				if onChunk != nil {
					onChunk(chunk)
				}
			}, func(chunk string) {
				if onReasoning != nil {
					onReasoning(chunk)
				}
			}, toolStartCb)
			resp.Body.Close()

			if parseErr != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				// Retry on overloaded/rate-limit errors from SSE stream
				if rlErr, ok := IsRateLimited(parseErr); ok && rateLimitRetries < 5 {
					rateLimitRetries++
					wait := rlErr.RetryAfter * time.Duration(rateLimitRetries)
					if wait > 120*time.Second {
						wait = 120 * time.Second
					}
					slog.Warn("LLM rate limited (SSE), retrying", "retry_after", wait, "attempt", rateLimitRetries, "round", round)
					select {
					case <-time.After(wait):
					case <-ctx.Done():
						return "", ctx.Err()
					}
					round-- // retry same round
					continue
				}
				slog.Error("error parsing Anthropic SSE stream", "error", parseErr, "round", round)
				if rlErr, ok := IsRateLimited(parseErr); ok {
					return "", rlErr
				}
				return "", fmt.Errorf("error reading streaming response. try again please")
			}

			fullText.WriteString(result.Text)
			fullReasoning.WriteString(result.Reasoning)
			lastFinishReason = result.FinishReason
			roundUsage = result.Usage
			for i, tc := range result.ToolCalls {
				toolCallsMap[i] = &ToolCall{
					ID:       tc.ID,
					Type:     tc.Type,
					Function: tc.Function,
				}
			}
		} else {
			// OpenAI-compatible SSE format
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

			for scanner.Scan() {
				if ctx.Err() != nil {
					resp.Body.Close()
					return fullText.String(), ctx.Err()
				}

				line := scanner.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}

				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					break
				}

				var chunk StreamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					prefix := data
					if len(prefix) > 200 {
						prefix = prefix[:200]
					}
					slog.Debug("Skipped malformed SSE chunk", "error", err, "data_prefix", prefix)
					continue
				}

				if chunk.Usage != nil {
					roundUsage = *chunk.Usage
				}

				if len(chunk.Choices) > 0 {
					choice := chunk.Choices[0]

					// Buffer content — only flush to onChunk after confirming this isn't
					// an intermediate tool-calling round (which contains reasoning, not code)
					if choice.Delta.Content != "" {
						fullText.WriteString(choice.Delta.Content)
						roundChunks = append(roundChunks, choice.Delta.Content)
						// Stream in real-time for UX; if this round is a tool-call
						// round, frontend replaces streaming text on chat_complete
						if onChunk != nil {
							onChunk(choice.Delta.Content)
						}
					}

					// Accumulate reasoning (ZAI: reasoning_content, OpenRouter: reasoning)
					if r := choice.Delta.GetReasoning(); r != "" {
						fullReasoning.WriteString(r)
						if onReasoning != nil {
							onReasoning(r)
						}
					}

					// Accumulate tool calls
					for _, tc := range choice.Delta.ToolCalls {
						idx := 0
						if tc.Index != nil {
							idx = *tc.Index
						}

						if _, exists := toolCallsMap[idx]; !exists {
							name := ""
							if tc.Function != nil {
								name = tc.Function.Name
							}
							toolCallsMap[idx] = &ToolCall{
								ID:   tc.ID,
								Type: tc.Type,
								Function: &FunctionCall{
									Name: name,
								},
							}
							if name != "" && len(opts) > 0 && opts[0].OnToolCallStart != nil {
								opts[0].OnToolCallStart(name)
							}
						} else {
							// Update ID/Type if provided in later chunks
							if tc.ID != "" {
								toolCallsMap[idx].ID = tc.ID
							}
							if tc.Type != "" {
								toolCallsMap[idx].Type = tc.Type
							}
							if tc.Function != nil && tc.Function.Name != "" {
								toolCallsMap[idx].Function.Name = tc.Function.Name
							}
						}

						// Append arguments incrementally
						if tc.Function != nil && tc.Function.Arguments != "" {
							toolCallsMap[idx].Function.Arguments += tc.Function.Arguments
						}
					}

					if choice.FinishReason != nil {
						lastFinishReason = *choice.FinishReason
					}
				}
			}

			scanErr := scanner.Err()
			resp.Body.Close()

			if scanErr != nil {
				if ctx.Err() != nil {
					return fullText.String(), ctx.Err()
				}
				slog.Error("scanner error during LLM streaming with tools", "error", scanErr, "round", round)
				return "", fmt.Errorf("error reading streaming response. try again please")
			}
		}

		// Record usage from this round
		roundLatency := time.Since(roundStart).Milliseconds()
		if roundUsage.TotalTokens > 0 {
			slog.Info("LLM usage", "FROM", "LLM_STREAM_TOOLS", "round", round,
				"prompt_tokens", roundUsage.PromptTokens,
				"completion_tokens", roundUsage.CompletionTokens,
				"total_tokens", roundUsage.TotalTokens,
				"latency_ms", roundLatency,
				"tool_calls", len(toolCallsMap),
				"history_msgs", len(currentMessages),
			)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				// Only record full message content for the first 10 rounds to cap memory usage.
				// After that, record usage stats only.
				if tracker.TrackContent && round < 10 {
					tracker.RecordWithContent(model, roundUsage, time.Since(roundStart).Milliseconds(), true, currentMessages, fullText.String())
				} else {
					tracker.Record(model, roundUsage, time.Since(roundStart).Milliseconds(), true)
				}
			}
		}

		// If LLM wants to call tools — discard buffered chunks (they're reasoning, not code)
		if lastFinishReason == "tool_calls" && len(toolCallsMap) > 0 {
			// If tools were stripped but LLM still returned tool_calls, treat accumulated
			// text as the final response. Some providers ignore tool removal.
			if roundTools == nil {
				slog.Warn("LLM returned tool_calls after tools were stripped, using accumulated text as response",
					"round", round, "text_length", fullText.Len())
				if fullText.Len() > 0 {
					return fullText.String(), nil
				}
				// No text content either — retry one more round with an explicit instruction
				currentMessages = append(currentMessages, Message{
					Role:    "system",
					Content: "Your previous response was empty. Please generate your final response as plain text now.",
				})
				continue
			}
			consecutiveToolRounds++
			roundChunks = nil // discard intermediate reasoning chunks

			// Build sorted tool calls list
			indices := make([]int, 0, len(toolCallsMap))
			for idx := range toolCallsMap {
				indices = append(indices, idx)
			}
			sort.Ints(indices)

			toolCalls := make([]ToolCall, 0, len(indices))
			for _, idx := range indices {
				toolCalls = append(toolCalls, *toolCallsMap[idx])
			}

			// Append assistant message with tool calls
			assistantMsg := Message{
				Role:      "assistant",
				Content:   fullText.String(),
				ToolCalls: toolCalls,
			}
			if provider.UsesThinkingFormat() && fullReasoning.Len() > 0 {
				assistantMsg.ReasoningContent = fullReasoning.String()
			}
			currentMessages = append(currentMessages, assistantMsg)

			// Execute each tool (with dedup) and append results
			dedupCount := 0
			interceptCount := 0
			for _, tc := range toolCalls {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}

				toolName := ""
				toolArgs := ""
				if tc.Function != nil {
					toolName = tc.Function.Name
					toolArgs = tc.Function.Arguments
				}

				var result string

				// Intercept tool calls before cache check and execution
				if len(opts) > 0 && opts[0].InterceptToolCall != nil {
					interceptResult, skip := opts[0].InterceptToolCall(toolName, toolArgs)
					if skip {
						result = interceptResult
						interceptCount++
						slog.Info("Tool call intercepted", "tool", toolName, "round", round)
						goto toolResultDone
					}
					// Non-empty result with skip=false means modified args
					// (e.g. write_files batch with already-written files stripped out)
					if interceptResult != "" {
						toolArgs = interceptResult
						if tc.Function != nil {
							tc.Function.Arguments = interceptResult
						}
					}
				}

				{
					cacheKey := toolName + ":" + toolArgs
					if cached, ok := toolCallHistory[cacheKey]; ok {
						// Return cached result instead of re-executing
						result = cached
						dedupCount++
						slog.Info("Tool call deduped (cached)", "tool", toolName, "round", round)
					} else {
						slog.Info("Executing tool call", "tool", toolName, "round", round)

						if onToolCall != nil {
							onToolCall(toolName, toolArgs)
						}

						var err error
						result, err = executor(toolName, toolArgs)
						if err != nil {
							result = fmt.Sprintf("Error executing tool %s: %v", toolName, err)
						}
						toolCallHistory[cacheKey] = result
					}
				}
			toolResultDone:

				currentMessages = append(currentMessages, Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       toolName,
				})
			}

			// If all tool calls were deduped or intercepted, accelerate toward nudge/strip.
			// Use an additive bump (~20% of nudgeAfter, min 2) instead of jumping directly
			// to threshold — a single wasted round shouldn't trigger premature nudging.
			wastedCount := dedupCount + interceptCount
			if wastedCount > 0 && wastedCount == len(toolCalls) {
				bump := nudgeAfter / 5
				if bump < 2 {
					bump = 2
				}
				consecutiveToolRounds += bump
				if consecutiveToolRounds > stripAfter {
					consecutiveToolRounds = stripAfter
				}
				if interceptCount == len(toolCalls) {
					currentMessages = append(currentMessages, Message{
						Role:    "system",
						Content: "All your tool calls in this round were served from memory. Move on to files that have NOT been created yet, or provide a summary to the user if all files are done.",
					})
					slog.Warn("All tool calls intercepted, accelerating nudge", "round", round, "intercept_count", interceptCount, "consecutive_tool_rounds", consecutiveToolRounds)
				} else {
					currentMessages = append(currentMessages, Message{
						Role:    "system",
						Content: "All your tool calls in this round returned cached results — you already have this information. Stop making redundant tool calls and produce your final response.",
					})
					slog.Warn("All tool calls deduped, accelerating nudge", "round", round, "dedup_count", dedupCount, "consecutive_tool_rounds", consecutiveToolRounds)
				}
			}

			// Trim assistant content, tool call arguments, and tool results to reduce history tokens
			if len(opts) > 0 && opts[0].TrimArgs != nil {
				assistantMsgIdx := len(currentMessages) - 1 - len(toolCalls)
				if assistantMsgIdx >= 0 {
					// Clear extended reasoning to save tokens, but keep the assistant's
					// short text content so the LLM retains context about what it just did.
					currentMessages[assistantMsgIdx].ReasoningContent = ""
					// Trim tool call arguments (e.g. strip file content from write_file)
					for i := range currentMessages[assistantMsgIdx].ToolCalls {
						tc := &currentMessages[assistantMsgIdx].ToolCalls[i]
						if tc.Function != nil {
							tc.Function.Arguments = opts[0].TrimArgs(tc.Function.Name, tc.Function.Arguments)
						}
					}
				}
			}

			// Trim tool result messages (e.g. collapse read_file content to a summary)
			if len(opts) > 0 && opts[0].TrimResults != nil {
				for i := len(currentMessages) - len(toolCalls); i < len(currentMessages); i++ {
					if i >= 0 && currentMessages[i].Role == "tool" {
						currentMessages[i].Content = opts[0].TrimResults(currentMessages[i].Name, currentMessages[i].Content)
					}
				}
			}

			// Inject post-round system message (e.g. progress summary).
			// Skip when tools have been stripped — avoid contradicting the "generate now" message.
			allIntercepted := interceptCount > 0 && interceptCount == len(toolCalls)
			if roundTools != nil && len(opts) > 0 && opts[0].AfterToolRound != nil {
				if msg := opts[0].AfterToolRound(allIntercepted); msg != "" {
					currentMessages = append(currentMessages, Message{
						Role:    "system",
						Content: msg,
					})
				}
			}

			// Continue the loop — re-call LLM with tool results
			continue
		} else {
			consecutiveToolRounds = 0
		}

		// Chunks were already streamed in real-time via onChunk above.
		// No tool calls — return final response
		if lastFinishReason == "length" {
			slog.Warn("LLM response truncated (finish_reason=length)", "response_length", fullText.Len(), "round", round)
		}

		return fullText.String(), nil
	}

	return "", fmt.Errorf("tool calling exceeded maximum rounds (%d)", maxRounds)
}

// StreamTextWithTools is like StreamTextFromLLM but with tool calling support.
// Uses system+user messages format. Returns the final text content after all tool rounds.
func StreamTextWithTools(
	ctx context.Context,
	model, system, message string,
	provider *LLMProvider,
	tools []Tool,
	reasoning *ReasoningConfig,
	executor ToolExecutor,
	onToolCall func(toolName string, args string),
	onChunk func(chunk string),
	onReasoning func(chunk string),
) (string, error) {
	messages := []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: message},
	}
	return StreamWithTools(ctx, model, messages, tools, provider, nil, reasoning, executor, onToolCall, onChunk, onReasoning)
}

// ReplaceIntegrationPlaceholders replaces {{PLACEHOLDER}} tokens in content with real values from integrations.
func ReplaceIntegrationPlaceholders(content string, integrations map[string]map[string]any) string {
	replacements := map[string]struct {
		provider string
		key      string
	}{
		"{{SUPABASE_URL}}":         {provider: "supabase", key: "url"},
		"{{SUPABASE_ANON_KEY}}":    {provider: "supabase", key: "anon_key"},
		"{{STRIPE_PK}}":            {provider: "stripe", key: "publishable_key"},
		"{{FIREBASE_CONFIG}}":      {provider: "firebase", key: "config"},
		"{{IYZICO_API_KEY}}":       {provider: "iyzico", key: "api_key"},
		"{{RESEND_API_KEY}}":       {provider: "resend", key: "api_key"},
		"{{CLOUDINARY_CLOUD_NAME}}": {provider: "cloudinary", key: "cloud_name"},
		"{{GOOGLE_SHEETS_API_KEY}}": {provider: "google_sheets", key: "api_key"},
	}

	for placeholder, info := range replacements {
		if creds, ok := integrations[info.provider]; ok {
			if val, ok := creds[info.key]; ok {
				if strVal, ok := val.(string); ok {
					content = strings.ReplaceAll(content, placeholder, strVal)
				}
			}
		}
	}

	return content
}
