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

// toolExecTimeout is the maximum time a single tool execution may take.
const toolExecTimeout = 2 * time.Minute

// normalizeToolCacheKey produces a canonical cache key for tool dedup.
// It normalizes JSON arguments so semantically identical calls (different key order) match.
func normalizeToolCacheKey(name, args string) string {
	var obj any
	if json.Unmarshal([]byte(args), &obj) == nil {
		if canonical, err := json.Marshal(obj); err == nil {
			return name + ":" + string(canonical)
		}
	}
	return name + ":" + args
}

// sanitizeToolError strips internal details from tool execution errors
// so they don't leak into LLM context or user-visible responses.
func sanitizeToolError(toolName string, err error) string {
	msg := err.Error()
	// Strip common internal prefixes/patterns
	for _, sensitive := range []string{"sql:", "pq:", "s3:", "dial tcp", "connect:", "tls:", "x509:"} {
		if strings.Contains(strings.ToLower(msg), sensitive) {
			slog.Error("Tool execution error (sanitized)", "tool", toolName, "error", msg)
			return fmt.Sprintf("Error executing tool %s: internal error, please try again", toolName)
		}
	}
	return fmt.Sprintf("Error executing tool %s: %s", toolName, msg)
}

// isValidJSON returns true if s is valid JSON.
func isValidJSON(s string) bool {
	return json.Valid([]byte(s))
}

// truncateBody truncates a string to maxLen characters for safe logging.
func truncateBody(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

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
		Body:       truncateBody(string(body), 2000),
	}
}

// --- Token usage tracking ---

// TokenEntry records usage from a single LLM call.
type TokenEntry struct {
	Model                    string
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	LatencyMs                int64
	IsStreaming              bool
	InputMessages            []Message // optional: messages sent to LLM (only when TrackContent is set)
	OutputText               string    // optional: LLM text completion (only when TrackContent is set)
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
		Model:                    model,
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		LatencyMs:                latencyMs,
		IsStreaming:              isStreaming,
	})
}

// RecordWithContent adds a usage entry including the messages and output text.
func (t *TokenTracker) RecordWithContent(model string, usage UsageInfo, latencyMs int64, isStreaming bool, inputMessages []Message, outputText string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Deep-snapshot messages so caller mutations don't affect stored data.
	// copy() is shallow — slices like ToolCalls and Images are shared with the
	// original. We deep-copy those fields to fully decouple the snapshot.
	msgs := make([]Message, len(inputMessages))
	for i, m := range inputMessages {
		msgs[i] = m
		if len(m.ToolCalls) > 0 {
			msgs[i].ToolCalls = make([]ToolCall, len(m.ToolCalls))
			copy(msgs[i].ToolCalls, m.ToolCalls)
		}
		if len(m.Images) > 0 {
			msgs[i].Images = make([]string, len(m.Images))
			copy(msgs[i].Images, m.Images)
		}
	}
	t.entries = append(t.entries, TokenEntry{
		Model:                    model,
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		LatencyMs:                latencyMs,
		IsStreaming:              isStreaming,
		InputMessages:            msgs,
		OutputText:               outputText,
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
	Type       string       `json:"type"`                   // "function", "builtin_function", "server_tool"
	Function   ToolFunction `json:"function,omitempty"`
	ServerTool *ServerTool  `json:"-"` // Anthropic server-side tools; not serialized to OpenAI format
}

// ServerTool represents an Anthropic server-side tool (e.g. web_search).
type ServerTool struct {
	Type    string `json:"type"`              // e.g. "web_search_20250305"
	Name    string `json:"name"`              // e.g. "web_search"
	MaxUses int    `json:"max_uses,omitempty"`
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
	Images           []string   `json:"-"` // base64 data URIs; excluded from default serialization
}

// MarshalJSON customises JSON output for Message: when Images is non-empty and
// role is "user", content is emitted as a multimodal array (OpenRouter format)
// instead of a plain string.
func (m Message) MarshalJSON() ([]byte, error) {
	type Alias Message // avoid infinite recursion
	if m.Role == "user" && len(m.Images) > 0 {
		parts := []MultiContentPart{{Type: "text", Text: m.Content}}
		for _, img := range m.Images {
			parts = append(parts, MultiContentPart{
				Type:     "image_url",
				ImageURL: &ImageURLContent{URL: img},
			})
		}
		// Marshal with content as multimodal array
		type multiMsg struct {
			Role             string             `json:"role"`
			Content          []MultiContentPart `json:"content"`
			ReasoningContent string             `json:"reasoning_content,omitempty"`
			ToolCalls        []ToolCall         `json:"tool_calls,omitempty"`
			ToolCallID       string             `json:"tool_call_id,omitempty"`
			Name             string             `json:"name,omitempty"`
		}
		return json.Marshal(multiMsg{
			Role:             m.Role,
			Content:          parts,
			ReasoningContent: m.ReasoningContent,
			ToolCalls:        m.ToolCalls,
			ToolCallID:       m.ToolCallID,
			Name:             m.Name,
		})
	}
	return json.Marshal(Alias(m))
}

// Response structure for parsing the LLM output
type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type UsageInfo struct {
	PromptTokens             int `json:"prompt_tokens"`
	CompletionTokens         int `json:"completion_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
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

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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

// StreamTextConfig provides optional configuration for StreamTextFromLLMWithMessages.
type StreamTextConfig struct {
	MaxTokens int              // 0 means default (16384)
	Reasoning *ReasoningConfig // nil means use provider default
}

// StreamTextFromLLMWithMessages streams LLM response tokens for a pre-built message array.
// Useful for multi-block system prompts (caching) without tool calling overhead.
func StreamTextFromLLMWithMessages(ctx context.Context, model string, messages []Message, provider *LLMProvider, onChunk func(chunk string), onReasoning func(chunk string), opts ...StreamTextConfig) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	maxTokens := 16384
	if len(opts) > 0 && opts[0].MaxTokens > 0 {
		maxTokens = opts[0].MaxTokens
	}

	var reasoning *ReasoningConfig
	if len(opts) > 0 && opts[0].Reasoning != nil {
		reasoning = opts[0].Reasoning
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:         model,
		Stream:        true,
		StreamOptions: &StreamOptionsConfig{IncludeUsage: true},
		MaxTokens:     maxTokens,
		Messages:      messages,
		Reasoning:     reasoning,
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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
				if tracker.TrackContent {
					tracker.RecordWithContent(model, result.Usage, time.Since(start).Milliseconds(), true, messages, result.Text)
				} else {
					tracker.Record(model, result.Usage, time.Since(start).Milliseconds(), true)
				}
			}
		}
		return result.Text, nil
	}

	var fullText strings.Builder
	var lastUsage UsageInfo
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

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
			if tracker.TrackContent {
				tracker.RecordWithContent(model, lastUsage, time.Since(start).Milliseconds(), true, messages, fullText.String())
			} else {
				tracker.Record(model, lastUsage, time.Since(start).Milliseconds(), true)
			}
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

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

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

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
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

	if resp.StatusCode != http.StatusOK {
		slog.Error("LLM vision returned non-200", "status", resp.StatusCode,
			"body", truncateBody(string(respBody), 2000))
		if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("error generating content. try again please")
	}

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

// ToolCallConfig holds optional configuration for StreamWithTools.
type ToolCallConfig struct {
	MaxRounds         int                                                    // Maximum tool-calling rounds (default 10)
	NudgeAfter        int                                                    // Inject nudge after this many consecutive tool rounds (default 3)
	StripAfter        int                                                    // Strip tools entirely after this many consecutive rounds (default 5)
	MaxTokens         int                                                    // Max completion tokens per round (default 16384)
	TrimArgs          func(toolName, args string) string                     // Trim tool call arguments in history to reduce token usage
	TrimResults       func(toolName, result string) string                   // Trim tool result messages in history to reduce token usage
	InterceptToolCall func(toolName, toolArgs string) (result string, skip bool) // Intercept tool calls before execution; if skip=true, use result and skip executor
	NudgeMessage      string                                                 // Custom nudge message (replaces default "stop researching" text)
	AfterToolRound    func(allIntercepted bool) string                       // Called after each tool round; allIntercepted=true when every call was intercepted; non-empty return is injected as a system message
	FilterTools       func(tools []Tool) []Tool                              // Called each round to filter/modify tools; return nil to strip all tools
	OnToolCallStart   func(toolName string)                                  // Called when a tool call name is first detected during SSE streaming (before args are complete)
}

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
	transientRetries := 0
	prevToolResultRange := [2]int{0, 0} // [start, end) of previous round's tool result messages

	for round := 0; round < maxRounds; round++ {
		// Trim the PREVIOUS round's tool results now (deferred from last iteration).
		// This ensures the model sees full results for one round before they're compressed.
		if len(opts) > 0 && opts[0].TrimResults != nil && prevToolResultRange[1] > prevToolResultRange[0] {
			for i := prevToolResultRange[0]; i < prevToolResultRange[1] && i < len(currentMessages); i++ {
				if currentMessages[i].Role == "tool" {
					currentMessages[i].Content = opts[0].TrimResults(currentMessages[i].Name, currentMessages[i].Content)
				}
			}
			prevToolResultRange = [2]int{0, 0}
		}
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

		req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("error creating LLM request: %w", err)
		}
		setLLMHeaders(req, provider)

		roundStart := time.Now()
		resp, err := llmHTTPClient.Do(req)
		if err != nil {
			// Retry transient connection errors (network blips, DNS, TLS) up to 3 times
			if transientRetries < 3 {
				transientRetries++
				wait := time.Duration(transientRetries) * 5 * time.Second
				slog.Warn("LLM connection error, retrying", "error", err, "attempt", transientRetries, "round", round)
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return "", ctx.Err()
				}
				round-- // retry same round
				continue
			}
			slog.Error("Error making streaming request to LLM:", "error", err, "round", round)
			return "", fmt.Errorf("error generating content. try again please")
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			// Handle 429 rate limit with retry + backoff
			if rlErr := parseRateLimitError(resp, respBody); rlErr != nil && rateLimitRetries < 5 {
				rateLimitRetries++
				wait := rlErr.RetryAfter + time.Duration(rateLimitRetries)*5*time.Second
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
			// Retry transient server errors (500, 502, 503) up to 3 times
			if resp.StatusCode >= 500 && resp.StatusCode != 529 && transientRetries < 3 {
				transientRetries++
				wait := time.Duration(transientRetries) * 5 * time.Second
				slog.Warn("LLM server error, retrying", "status", resp.StatusCode, "attempt", transientRetries, "round", round)
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
		rateLimitRetries = 0   // reset after successful request
		transientRetries = 0

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
					wait := rlErr.RetryAfter + time.Duration(rateLimitRetries)*5*time.Second
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
			scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

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

			// Append assistant message with tool calls — save the index for later trimming
			assistantMsg := Message{
				Role:      "assistant",
				Content:   fullText.String(),
				ToolCalls: toolCalls,
			}
			if provider.UsesThinkingFormat() && fullReasoning.Len() > 0 {
				assistantMsg.ReasoningContent = fullReasoning.String()
			}
			assistantMsgIdx := len(currentMessages)
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
						// Cache intercepted results so they can be deduped in later rounds
						cacheKey := normalizeToolCacheKey(toolName, toolArgs)
						toolCallHistory[cacheKey] = result
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
					cacheKey := normalizeToolCacheKey(toolName, toolArgs)
					if cached, ok := toolCallHistory[cacheKey]; ok {
						// Return cached result instead of re-executing
						result = cached
						dedupCount++
						slog.Info("Tool call deduped (cached)", "tool", toolName, "round", round)
					} else {
						// Validate JSON arguments before execution to avoid wasting a tool round
						if toolArgs != "" && toolArgs != "{}" && !isValidJSON(toolArgs) {
							slog.Warn("Tool call has invalid JSON arguments", "tool", toolName, "round", round, "args_len", len(toolArgs))
							result = fmt.Sprintf("Error: invalid JSON arguments for tool %s. Please provide valid JSON.", toolName)
							toolCallHistory[cacheKey] = result
							goto toolResultDone
						}

						slog.Info("Executing tool call", "tool", toolName, "round", round)

						if onToolCall != nil {
							onToolCall(toolName, toolArgs)
						}

						// Execute with per-tool timeout to prevent a single slow tool from blocking the loop
						toolCtx, toolCancel := context.WithTimeout(ctx, toolExecTimeout)
						type toolResult struct {
							result string
							err    error
						}
						ch := make(chan toolResult, 1)
						go func() {
							r, e := executor(toolName, toolArgs)
							ch <- toolResult{r, e}
						}()
						select {
						case tr := <-ch:
							toolCancel()
							result = tr.result
							if tr.err != nil {
								result = sanitizeToolError(toolName, tr.err)
							}
						case <-toolCtx.Done():
							toolCancel()
							if ctx.Err() != nil {
								return "", ctx.Err()
							}
							slog.Warn("Tool execution timed out", "tool", toolName, "round", round)
							result = fmt.Sprintf("Error: tool %s timed out after %s", toolName, toolExecTimeout)
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

			// Trim assistant content, tool call arguments, and tool results to reduce history tokens.
			// Uses saved assistantMsgIdx instead of computing from end, which could be wrong
			// if system messages were appended between the assistant msg and tool results.
			if len(opts) > 0 && opts[0].TrimArgs != nil && assistantMsgIdx < len(currentMessages) {
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

			// Record the range of this round's tool result messages for deferred trimming.
			// We defer trimming to the next round so the model sees full results once.
			if len(opts) > 0 && opts[0].TrimResults != nil {
				start := len(currentMessages) - len(toolCalls)
				if start < 0 {
					start = 0
				}
				prevToolResultRange = [2]int{start, len(currentMessages)}
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
		if lastFinishReason == "length" && round < maxRounds-1 {
			// Response was truncated — request continuation by appending the partial
			// response as assistant message and asking the LLM to continue.
			slog.Warn("LLM response truncated (finish_reason=length), requesting continuation", "response_length", fullText.Len(), "round", round)
			currentMessages = append(currentMessages, Message{
				Role:    "assistant",
				Content: fullText.String(),
			})
			currentMessages = append(currentMessages, Message{
				Role:    "user",
				Content: "Your response was cut off. Continue EXACTLY where you left off. Do not repeat any content.",
			})
			continue
		}

		if lastFinishReason == "length" {
			slog.Warn("LLM response truncated (finish_reason=length), no rounds left", "response_length", fullText.Len(), "round", round)
		}

		return fullText.String(), nil
	}

	return "", fmt.Errorf("tool calling exceeded maximum rounds (%d)", maxRounds)
}
