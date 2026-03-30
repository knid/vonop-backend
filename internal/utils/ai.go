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

// maxResponseBufferSize is the maximum bytes accumulated in a response text
// buffer before further content is dropped. This prevents unbounded memory
// growth from pathologically large LLM responses.
const maxResponseBufferSize = 2 * 1024 * 1024 // 2 MB

// ErrResponseTruncated is returned when the LLM response was cut off due to
// reaching the output token limit (finish_reason=length). The response text
// is still delivered via the onChunk callback — callers should check for this
// error to decide whether recovery (e.g. continuation) is needed.
var ErrResponseTruncated = errors.New("LLM response truncated (output token limit reached)")

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
// The returned string is used as the tool result so the LLM knows the call failed.
func sanitizeToolError(toolName string, err error) string {
	msg := err.Error()
	// Strip common internal prefixes/patterns
	for _, sensitive := range []string{"sql:", "pq:", "s3:", "dial tcp", "connect:", "tls:", "x509:"} {
		if strings.Contains(strings.ToLower(msg), sensitive) {
			slog.Error("Tool execution error (sanitized)", "tool", toolName, "error", msg)
			return fmt.Sprintf("Tool execution failed for %s: internal error, please try again", toolName)
		}
	}
	return fmt.Sprintf("Tool execution failed for %s: %v", toolName, err)
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

// --- LLM error types ---

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

// ErrPermanentLLM is returned when the LLM API returns an error that will not
// succeed on retry (e.g. invalid_request_error, authentication_error).
type ErrPermanentLLM struct {
	ErrorType string
	Message   string
}

func (e *ErrPermanentLLM) Error() string {
	return fmt.Sprintf("permanent LLM error (%s): %s", e.ErrorType, e.Message)
}

// IsPermanentError checks if an error is a permanent (non-retryable) LLM error.
func IsPermanentError(err error) bool {
	var pe *ErrPermanentLLM
	return errors.As(err, &pe)
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
	SpanName                 string    // LLM function name (e.g. "get_json", "stream_text") for PostHog $ai_span_name
	RecordedAt               time.Time // wall-clock time when this entry was recorded
	CostUSD                  float64   // estimated cost in USD (computed from model pricing)
}

// modelCosts maps normalized model names to per-million-token pricing {input, output}.
// Kept minimal — only models we actually use. Unknown models get zero cost.
var modelCosts = map[string][2]float64{
	"claude-sonnet-4-6":         {3.0, 15.0},
	"claude-sonnet-4-5":         {3.0, 15.0},
	"claude-sonnet-4":           {3.0, 15.0},
	"claude-sonnet-4-20250514":  {3.0, 15.0},
	"claude-haiku-4-5":          {0.8, 4.0},
	"claude-haiku-4-5-20251001": {0.8, 4.0},
	"claude-opus-4-6":           {15.0, 75.0},
	"claude-opus-4-5":           {15.0, 75.0},
}

// EstimateInputOutputCostUSD returns separate input and output costs for a single LLM call.
// Used for PostHog $ai_input_cost / $ai_output_cost standard properties.
func EstimateInputOutputCostUSD(model string, inputTokens, outputTokens, cacheCreation, cacheRead int) (inputCost, outputCost float64) {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	prices, ok := modelCosts[model]
	if !ok {
		return 0, 0
	}
	inputPrice, outputPrice := prices[0], prices[1]
	regularInput := inputTokens - cacheCreation - cacheRead
	if regularInput < 0 {
		regularInput = 0
	}
	inputCost = float64(regularInput)*inputPrice/1_000_000 +
		float64(cacheCreation)*inputPrice*1.25/1_000_000 +
		float64(cacheRead)*inputPrice*0.1/1_000_000
	outputCost = float64(outputTokens) * outputPrice / 1_000_000
	return
}

// estimateCostUSD computes the estimated cost for a single LLM call.
func estimateCostUSD(model string, inputTokens, outputTokens, cacheCreation, cacheRead int) float64 {
	// Normalize: strip provider prefix
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	prices, ok := modelCosts[model]
	if !ok {
		return 0
	}
	inputPrice, outputPrice := prices[0], prices[1]
	// Cache pricing: creation = 1.25x input, read = 0.1x input, regular input = 1x
	regularInput := inputTokens - cacheCreation - cacheRead
	if regularInput < 0 {
		regularInput = 0
	}
	cost := float64(regularInput)*inputPrice/1_000_000 +
		float64(cacheCreation)*inputPrice*1.25/1_000_000 +
		float64(cacheRead)*inputPrice*0.1/1_000_000 +
		float64(outputTokens)*outputPrice/1_000_000
	return cost
}

// TokenTracker is a thread-safe accumulator that collects token usage across multiple LLM calls.
type TokenTracker struct {
	mu           sync.Mutex
	entries      []TokenEntry
	TrackContent bool      // opt-in: when true, StreamWithTools records message content per round
	TraceInput   string    // user's original message (for $ai_trace)
	TraceOutput  string    // final assistant response (for $ai_trace)
	UserID       string    // for slog attribution
	ProjectID    string    // for slog attribution
	StartedAt    time.Time // wall-clock start of the generation (for accurate $ai_latency)
}

func NewTokenTracker() *TokenTracker {
	return &TokenTracker{StartedAt: time.Now()}
}

// trackerAttrs returns slog attributes for user_id and project_id from the
// TokenTracker stored in ctx. If no tracker is present, empty strings are used.
func trackerAttrs(ctx context.Context) []any {
	if t := TokenTrackerFromContext(ctx); t != nil {
		return []any{"user_id", t.UserID, "project_id", t.ProjectID}
	}
	return []any{"user_id", "", "project_id", ""}
}

// Record adds a usage entry to the tracker. An optional spanName identifies
// the LLM function (e.g. "get_json", "stream_text") for PostHog analytics.
func (t *TokenTracker) Record(model string, usage UsageInfo, latencyMs int64, isStreaming bool, spanName ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	name := ""
	if len(spanName) > 0 {
		name = spanName[0]
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
		SpanName:                 name,
		RecordedAt:               time.Now(),
		CostUSD:                  estimateCostUSD(model, usage.PromptTokens, usage.CompletionTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens),
	})
}

// RecordWithContent adds a usage entry including the messages and output text.
// An optional spanName identifies the LLM function for PostHog analytics.
func (t *TokenTracker) RecordWithContent(model string, usage UsageInfo, latencyMs int64, isStreaming bool, inputMessages []Message, outputText string, spanName ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	name := ""
	if len(spanName) > 0 {
		name = spanName[0]
	}
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
		SpanName:                 name,
		RecordedAt:               time.Now(),
		CostUSD:                  estimateCostUSD(model, usage.PromptTokens, usage.CompletionTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens),
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

// llmHTTPClient is used for non-streaming LLM calls (GetJSONFromLLM, GetTextFromLLM, etc.)
// with a hard 10-minute timeout.
var llmHTTPClient = &http.Client{
	Timeout: 10 * time.Minute,
}

// llmStreamingClient is used for streaming LLM calls (StreamTextFromLLMWithMessages,
// StreamWithTools) where responses can legitimately take 10+ minutes for large code
// generation. A generous ResponseHeaderTimeout prevents hanging indefinitely when
// the server accepts the connection but never starts sending data. Once headers arrive,
// the body read is governed by the request context (typically 25 minutes).
var llmStreamingClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: 3 * time.Minute,
	},
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
	DisablePromptCache  bool                  `json:"-"` // skip Anthropic cache_control on system blocks (for one-shot calls with no follow-up)
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
	// Ensure image has a proper data URI prefix with correct media type
	mediaType, rawData := parseDataURI(base64Image)
	imageURI := fmt.Sprintf("data:%s;base64,%s", mediaType, rawData)

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
							URL: imageURI,
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
		DisablePromptCache: true, // one-shot call — no follow-up will read the cache
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
		slog.Error("Error making request to LLM:", "error", err, "from", "llm_utils", "model", model)
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
		if rlErr := parseRateLimitError(resp, errBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Response received", "from", "llm_utils")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error creating items.", "error", err, "from", "llm_utils", "model", model)
		return "", fmt.Errorf("error creating items. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "from", "llm_utils", "model", model)
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: message}}
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, parsed.Choices[0].Message.Content, "get_json")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_json")
			}
		}
	}

	return parsed.Choices[0].Message.Content, nil
}

// GetJSONFromLLMWithMaxTokens is like GetJSONFromLLM but allows specifying a
// custom max_tokens limit. Use this when the expected JSON output is large
// (e.g. file structure planning for complex apps).
func GetJSONFromLLMWithMaxTokens(ctx context.Context, model, system, message string, provider *LLMProvider, maxTokens int) (string, error) {
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
		MaxTokens:          maxTokens,
		DisablePromptCache: true,
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
		slog.Error("Error making request to LLM:", "error", err, "from", "llm_utils", "model", model)
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
		if rlErr := parseRateLimitError(resp, errBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Response received", "from", "llm_utils")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error creating items.", "error", err, "from", "llm_utils", "model", model)
		return "", fmt.Errorf("error creating items. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "from", "llm_utils", "model", model)
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: message}}
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, parsed.Choices[0].Message.Content, "get_json")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_json")
			}
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
		DisablePromptCache: true, // one-shot call — no follow-up will read the cache
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
		slog.Error("Error making request to LLM:", "error", err, "from", "llm_utils")
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
		if rlErr := parseRateLimitError(resp, errBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Response received", "from", "llm_utils_text")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error parsing LLM response", "error", err, "from", "llm_utils_text")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "from", "llm_utils_text")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils_text", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: message}}
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, parsed.Choices[0].Message.Content, "get_text")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_text")
			}
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
	OnRetry   func(attempt int, reason string) // optional callback when a retry is about to happen
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
	var onRetry func(int, string)
	if len(opts) > 0 && opts[0].OnRetry != nil {
		onRetry = opts[0].OnRetry
	}

	// Retry state — mirrors StreamWithTools retry logic so that Phase 2 chat
	// calls are as resilient as Phase 1 tool-calling calls.
	const maxTransientRetries = 3
	const maxRateLimitRetries = 5
	rateLimitRetries := 0
	transientRetries := 0
	chunksDelivered := false // tracks whether any content was streamed to the caller

	notifyRetry := func(attempt int, reason string) {
		if onRetry != nil {
			onRetry(attempt, reason)
		}
	}

	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
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
		resp, err := llmStreamingClient.Do(req)
		if err != nil {
			// Retry transient connection errors (timeout, DNS, TLS) if no chunks delivered
			if !chunksDelivered && transientRetries < maxTransientRetries {
				transientRetries++
				wait := time.Duration(transientRetries) * 5 * time.Second
				slog.Warn("LLM stream_text connection error, retrying",
					"error", err, "attempt", transientRetries, "model", model)
				notifyRetry(transientRetries, fmt.Sprintf("connection error: %v", err))
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return "", ctx.Err()
				}
				continue
			}
			slog.Error("Error making streaming request to LLM:", "error", err)
			return "", fmt.Errorf("connection error after %d retries: %w", transientRetries, err)
		}

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			// Retry rate-limited responses (429/529)
			if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
				if !chunksDelivered && rateLimitRetries < maxRateLimitRetries {
					rateLimitRetries++
					wait := rlErr.RetryAfter + time.Duration(rateLimitRetries)*5*time.Second
					if wait > 120*time.Second {
						wait = 120 * time.Second
					}
					slog.Warn("LLM stream_text rate limited, retrying",
						"retry_after", wait, "attempt", rateLimitRetries, "model", model)
					notifyRetry(rateLimitRetries, "rate limited")
					select {
					case <-time.After(wait):
					case <-ctx.Done():
						return "", ctx.Err()
					}
					continue
				}
				return "", rlErr
			}

			// Retry transient server errors (500, 502, 503)
			if resp.StatusCode >= 500 && resp.StatusCode != 529 && !chunksDelivered && transientRetries < maxTransientRetries {
				transientRetries++
				wait := time.Duration(transientRetries) * 5 * time.Second
				slog.Warn("LLM stream_text server error, retrying",
					"status", resp.StatusCode, "attempt", transientRetries, "model", model)
				notifyRetry(transientRetries, fmt.Sprintf("server error %d", resp.StatusCode))
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return "", ctx.Err()
				}
				continue
			}

			slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
			return "", fmt.Errorf("LLM returned HTTP %d: %s", resp.StatusCode, truncateBody(string(respBody), 200))
		}

		// Wrap onChunk to track whether any content was delivered to the caller.
		// Once chunks are delivered, retrying would corrupt the stream.
		wrappedOnChunk := func(chunk string) {
			chunksDelivered = true
			if onChunk != nil {
				onChunk(chunk)
			}
		}

		// Anthropic SSE format — use dedicated parser
		if provider.IsAnthropic() {
			result, parseErr := parseAnthropicSSEStream(resp.Body, wrappedOnChunk, onReasoning, nil)
			resp.Body.Close()

			if parseErr != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				// Permanent errors — never retry (invalid request, auth, etc.)
				if IsPermanentError(parseErr) {
					slog.Error("LLM stream_text permanent error, not retrying",
						"error", parseErr, "model", model)
					return "", parseErr
				}
				// Retry rate limit errors from SSE stream (only if no chunks delivered)
				if rlErr, ok := IsRateLimited(parseErr); ok {
					if !chunksDelivered && rateLimitRetries < maxRateLimitRetries {
						rateLimitRetries++
						wait := rlErr.RetryAfter + time.Duration(rateLimitRetries)*5*time.Second
						if wait > 120*time.Second {
							wait = 120 * time.Second
						}
						slog.Warn("LLM stream_text rate limited (SSE), retrying",
							"retry_after", wait, "attempt", rateLimitRetries, "model", model)
						notifyRetry(rateLimitRetries, "rate limited")
						select {
						case <-time.After(wait):
						case <-ctx.Done():
							return "", ctx.Err()
						}
						continue
					}
					return "", rlErr
				}
				// Retry transient stream errors (only if no chunks delivered)
				if !chunksDelivered && transientRetries < maxTransientRetries {
					transientRetries++
					wait := time.Duration(transientRetries) * 5 * time.Second
					slog.Warn("LLM stream_text SSE error, retrying",
						"error", parseErr, "attempt", transientRetries, "model", model)
					notifyRetry(transientRetries, fmt.Sprintf("stream error: %v", parseErr))
					select {
					case <-time.After(wait):
					case <-ctx.Done():
						return "", ctx.Err()
					}
					continue
				}
				return "", fmt.Errorf("stream error after %d retries: %w", transientRetries, parseErr)
			}
			if result.Usage.TotalTokens > 0 {
				latencyMs := time.Since(start).Milliseconds()
				slog.Info("LLM usage", append([]any{"from", "llm_stream_text", "model", model, "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
				if tracker := TokenTrackerFromContext(ctx); tracker != nil {
					if tracker.TrackContent {
						tracker.RecordWithContent(model, result.Usage, latencyMs, true, messages, result.Text, "stream_text")
					} else {
						tracker.Record(model, result.Usage, latencyMs, true, "stream_text")
					}
				}
			}
			if result.FinishReason == "length" {
				slog.Warn("LLM streaming response truncated (finish_reason=length)", "model", model, "response_length", len(result.Text))
				return result.Text, ErrResponseTruncated
			}
			return result.Text, nil
		}

		// OpenRouter / OpenAI-compatible SSE format
		var fullText strings.Builder
		var lastUsage UsageInfo
		var lastFinishReason string
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
					chunksDelivered = true
					if fullText.Len() < maxResponseBufferSize {
						fullText.WriteString(content)
					}
					if onChunk != nil {
						onChunk(content)
					}
				}
				if chunk.Choices[0].FinishReason != nil {
					lastFinishReason = *chunk.Choices[0].FinishReason
				}
			}
		}

		scanErr := scanner.Err()
		resp.Body.Close()

		if scanErr != nil {
			if ctx.Err() != nil {
				return fullText.String(), ctx.Err()
			}
			// Retry transient stream errors (only if no chunks delivered)
			if !chunksDelivered && transientRetries < maxTransientRetries {
				transientRetries++
				wait := time.Duration(transientRetries) * 5 * time.Second
				slog.Warn("LLM stream_text scanner error, retrying",
					"error", scanErr, "attempt", transientRetries, "model", model)
				notifyRetry(transientRetries, fmt.Sprintf("stream error: %v", scanErr))
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return "", ctx.Err()
				}
				continue
			}
			return "", fmt.Errorf("stream error after %d retries: %w", transientRetries, scanErr)
		}

		if fullText.Len() >= maxResponseBufferSize {
			slog.Warn("LLM streaming response exceeded max buffer size", "max_bytes", maxResponseBufferSize, "from", "llm_stream_text")
		}

		if lastUsage.TotalTokens > 0 {
			latencyMs := time.Since(start).Milliseconds()
			slog.Info("LLM usage", append([]any{"from", "llm_stream_text", "model", model, "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				if tracker.TrackContent {
					tracker.RecordWithContent(model, lastUsage, latencyMs, true, messages, fullText.String(), "stream_text")
				} else {
					tracker.Record(model, lastUsage, latencyMs, true, "stream_text")
				}
			}
		}

		if lastFinishReason == "length" {
			slog.Warn("LLM streaming response truncated (finish_reason=length)", "model", model, "response_length", fullText.Len())
			return fullText.String(), ErrResponseTruncated
		}

		return fullText.String(), nil
	}
}

// GetJSONFromLLMWithMessages sends a multi-turn conversation to the LLM and expects JSON output.
// Used for chat-based interactions where conversation history matters.
func GetJSONFromLLMWithMessages(ctx context.Context, model string, messages []Message, provider *LLMProvider) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model:              model,
		Messages:           messages,
		MaxTokens:          16384,
		ResponseFormat:     &ResponseFormat{Type: "json_object"},
		DisablePromptCache: true, // one-shot call — no follow-up will read the cache
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
		slog.Error("Error making request to LLM:", "error", err, "from", "llm_utils_chat")
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
		if rlErr := parseRateLimitError(resp, errBody); rlErr != nil {
			return "", rlErr
		}
		return "", fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	latencyMs := time.Since(start).Milliseconds()
	slog.Info("LLM Chat Response received", "from", "llm_utils_chat")

	parsed, err := parseLLMResponseBody(respBody, provider)
	if err != nil {
		slog.Error("error parsing LLM response", "error", err, "from", "llm_utils_chat")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "from", "llm_utils_chat")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils_chat", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, messages, parsed.Choices[0].Message.Content, "get_json_chat")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_json_chat")
			}
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
	resp, err := llmStreamingClient.Do(req)
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
			latencyMs := time.Since(start).Milliseconds()
			slog.Info("LLM usage", append([]any{"from", "llm_stream_chat", "model", model, "prompt_tokens", result.Usage.PromptTokens, "completion_tokens", result.Usage.CompletionTokens, "total_tokens", result.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				tracker.Record(model, result.Usage, latencyMs, true, "stream_json")
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
				if fullText.Len() < maxResponseBufferSize {
					fullText.WriteString(content)
				}
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
		latencyMs := time.Since(start).Milliseconds()
		slog.Info("LLM usage", append([]any{"from", "llm_stream_chat", "model", model, "prompt_tokens", lastUsage.PromptTokens, "completion_tokens", lastUsage.CompletionTokens, "total_tokens", lastUsage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			tracker.Record(model, lastUsage, latencyMs, true, "stream_json")
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
	slog.Info("LLM Vision Response received", "from", "llm_utils_vision")

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
		slog.Info("LLM usage", append([]any{"from", "llm_utils_vision", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: userText + " [image attached]"}}
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, parsed.Choices[0].Message.Content, "get_json_vision")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_json_vision")
			}
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
	OnRoundStart      func(round int)                                       // Called at the start of each round (useful for resetting per-round state like text buffers)
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
	// toolCallHistory caches tool results for deduplication across rounds.
	// No mutex is needed because tools execute sequentially (each tool call
	// blocks via select until completion before the next one starts).
	toolCallHistory := make(map[string]string) // "tool:args" → result (dedup cache)
	consecutiveToolRounds := 0
	consecutiveEmptyResponses := 0
	nudged := false
	rateLimitRetries := 0
	transientRetries := 0
	prevToolResultRange := [2]int{0, 0} // [start, end) of previous round's tool result messages

	for round := 0; round < maxRounds; round++ {
		// Notify caller that a new round is starting (e.g. to reset per-round state).
		if len(opts) > 0 && opts[0].OnRoundStart != nil {
			opts[0].OnRoundStart(round)
		}
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
			return "", fmt.Errorf("round %d: error building request: %w", round, err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("round %d: error creating LLM request: %w", round, err)
		}
		setLLMHeaders(req, provider)

		roundStart := time.Now()
		resp, err := llmStreamingClient.Do(req)
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
			return "", fmt.Errorf("round %d: error generating content: %w", round, err)
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
				if deadline, ok := ctx.Deadline(); ok {
					if time.Until(deadline) < wait {
						return "", fmt.Errorf("context deadline too short for rate-limit backoff (%s remaining, need %s)", time.Until(deadline), wait)
					}
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
			return "", fmt.Errorf("round %d: LLM returned status %d", round, resp.StatusCode)
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
				// Permanent errors — never retry
				if IsPermanentError(parseErr) {
					slog.Error("LLM permanent error in tool round", "error", parseErr, "round", round)
					return "", parseErr
				}
				// Retry on overloaded/rate-limit errors from SSE stream
				if rlErr, ok := IsRateLimited(parseErr); ok && rateLimitRetries < 5 {
					rateLimitRetries++
					wait := rlErr.RetryAfter + time.Duration(rateLimitRetries)*5*time.Second
					if wait > 120*time.Second {
						wait = 120 * time.Second
					}
					if deadline, ok := ctx.Deadline(); ok {
						if time.Until(deadline) < wait {
							return "", fmt.Errorf("context deadline too short for rate-limit backoff (%s remaining, need %s)", time.Until(deadline), wait)
						}
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
				return "", fmt.Errorf("round %d: error reading streaming response: %w", round, parseErr)
			}

			if len(result.Text) > maxResponseBufferSize {
				slog.Warn("Anthropic SSE result text exceeded max buffer size, truncating", "max_bytes", maxResponseBufferSize, "actual_bytes", len(result.Text), "round", round)
				fullText.WriteString(result.Text[:maxResponseBufferSize])
			} else {
				fullText.WriteString(result.Text)
			}
			fullReasoning.WriteString(result.Reasoning)
			lastFinishReason = result.FinishReason
			roundUsage = result.Usage
			if result.ToolCalls != nil {
				for i, tc := range result.ToolCalls {
					toolCallsMap[i] = &ToolCall{
						ID:       tc.ID,
						Type:     tc.Type,
						Function: tc.Function,
					}
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
						if fullText.Len() < maxResponseBufferSize {
							fullText.WriteString(choice.Delta.Content)
						} else if fullText.Len()-len(choice.Delta.Content) < maxResponseBufferSize {
							// Log once when we first exceed the limit
							slog.Warn("LLM response buffer exceeded max size, no longer accumulating", "max_bytes", maxResponseBufferSize, "round", round)
						}
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
				return "", fmt.Errorf("round %d: error reading streaming response: %w", round, scanErr)
			}
		}

		// Record usage from this round
		roundLatency := time.Since(roundStart).Milliseconds()
		if roundUsage.TotalTokens > 0 {
			slog.Info("LLM usage", append([]any{"from", "llm_stream_tools", "model", model, "round", round,
				"prompt_tokens", roundUsage.PromptTokens,
				"completion_tokens", roundUsage.CompletionTokens,
				"total_tokens", roundUsage.TotalTokens,
				"latency_ms", roundLatency,
				"tool_calls", len(toolCallsMap),
				"history_msgs", len(currentMessages),
			}, trackerAttrs(ctx)...)...)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				// Only record full message content for the first 10 rounds to cap memory usage.
				// After that, record usage stats only.
				if tracker.TrackContent && round < 10 {
					tracker.RecordWithContent(model, roundUsage, time.Since(roundStart).Milliseconds(), true, currentMessages, fullText.String(), "stream_with_tools")
				} else {
					tracker.Record(model, roundUsage, time.Since(roundStart).Milliseconds(), true, "stream_with_tools")
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
				consecutiveEmptyResponses++
				if consecutiveEmptyResponses >= 3 {
					return "", fmt.Errorf("LLM returned empty content %d consecutive times", consecutiveEmptyResponses)
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
							defer func() {
								if r := recover(); r != nil {
									slog.Error("tool callback panicked", "tool", toolName, "panic", r)
									ch <- toolResult{"", fmt.Errorf("tool %s panicked: %v", toolName, r)}
								}
							}()
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
								slog.Error("tool execution aborted: parent context cancelled", "tool", toolName, "round", round, "error", ctx.Err())
								return "", ctx.Err()
							}
							slog.Warn("tool execution timed out", "tool", toolName, "round", round, "timeout", toolExecTimeout)
							result = fmt.Sprintf("Error: tool %s timed out after %s", toolName, toolExecTimeout)
						}
						toolCallHistory[cacheKey] = result
					}
				}
			toolResultDone:

				// Ensure tool result is non-empty; LLM providers reject empty tool results.
				if result == "" {
					slog.Warn("tool returned empty result", "tool", toolName, "round", round)
					result = "(no output)"
				}
				currentMessages = append(currentMessages, Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       toolName,
				})
			}

			// Capture the range of tool result messages now, before any system
			// messages are appended below. The tool results are the last
			// len(toolCalls) messages in currentMessages.
			toolResultEndIdx := len(currentMessages)
			toolResultStartIdx := toolResultEndIdx - len(toolCalls)
			if toolResultStartIdx < 0 {
				toolResultStartIdx = 0
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
			// Save the range using toolResultStartIdx captured before system messages
			// were appended, since dedup/intercept messages shift indices.
			if len(opts) > 0 && opts[0].TrimResults != nil {
				prevToolResultRange = [2]int{toolResultStartIdx, toolResultEndIdx}
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
			consecutiveEmptyResponses = 0
		}

		// Chunks were already streamed in real-time via onChunk above.
		// No tool calls — return final response
		if lastFinishReason == "length" && round < maxRounds-1 {
			if fullText.Len() > maxResponseBufferSize {
				slog.Warn("response exceeded max size during continuation, returning partial", "size", fullText.Len())
				return fullText.String(), nil
			}
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
