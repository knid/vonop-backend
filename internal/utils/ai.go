package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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

	"github.com/knid/wemob/internal/observability"
	"go.opentelemetry.io/otel/trace"
)

// toolExecTimeout is the maximum time a single tool execution may take.
const toolExecTimeout = 2 * time.Minute

// maxResponseBufferSize is the maximum bytes accumulated in a response text
// buffer before further content is dropped. This prevents unbounded memory
// growth from pathologically large LLM responses.
const maxResponseBufferSize = 2 * 1024 * 1024 // 2 MB

// incompleteToolCallRecoveryRetries bounds in-run recovery when a provider
// ends a stream after starting, but not completing, a tool call.
const incompleteToolCallRecoveryRetries = 1

// maxBase64ImageBytes is the maximum raw image size we will convert to base64
// before sending to LLM providers (OpenRouter, Moonshot, etc.). Larger images
// are left as URLs to avoid excessive prompt bloat and request size limits.
const maxBase64ImageBytes = 8 * 1024 * 1024 // 8 MiB raw (base64 will be ~33% larger)

// ErrResponseTruncated is returned when the LLM response was cut off due to
// reaching the output token limit (finish_reason=length). The response text
// is still delivered via the onChunk callback — callers should check for this
// error to decide whether recovery (e.g. continuation) is needed.
var ErrResponseTruncated = errors.New("LLM response truncated (output token limit reached)")

// ErrIncompleteToolCall is returned when a streaming response starts emitting
// tool calls but ends before the provider marks them as complete.
var ErrIncompleteToolCall = errors.New("LLM response ended with incomplete tool call")

type ToolRoundsExceededError struct {
	MaxRounds int
}

func (e *ToolRoundsExceededError) Error() string {
	return fmt.Sprintf("tool calling exceeded maximum rounds (%d)", e.MaxRounds)
}

// isValidJSON returns true if s is valid JSON.
func isValidJSON(s string) bool {
	return json.Valid([]byte(s))
}

func isPinnedCurrentRunMessage(m Message) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, "CURRENT RUN CONTEXT\n")
}

// truncateBody truncates a string to maxLen characters for safe logging.
func truncateBody(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// imageDownloadClient is a shared HTTP client for fetching image URLs to
// base64. Reuses connections and avoids creating a new client per call.
var imageDownloadClient = &http.Client{Timeout: 15 * time.Second}

// downloadImageToBase64 fetches an image URL and returns it as a base64 data URI.
// It rejects images larger than maxBase64ImageBytes to prevent prompt bloat.
func downloadImageToBase64(url string) (string, error) {
	resp, err := imageDownloadClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read image body: %w", err)
	}
	if len(data) > maxBase64ImageBytes {
		return "", fmt.Errorf("image too large for base64 conversion (%d bytes > %d)", len(data), maxBase64ImageBytes)
	}
	mediaType := resp.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = "image/png"
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mediaType, b64), nil
}

// convertImageURLsToBase64 converts any HTTP/HTTPS image URLs in the
// Messages' Images field into base64 data URIs.
//
// This is required for some providers (Moonshot/Kimi) and strongly recommended
// for OpenRouter when sending images hosted behind our own asset URLs (which
// 302 to short-lived presigned S3 objects). Converting to base64 removes any
// dependency on the LLM provider successfully fetching the image later.
//
// The function shallow-copies the slice and only deep-copies image slices
// that actually contain HTTP URLs.
func convertImageURLsToBase64(msgs []Message) ([]Message, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if len(out[i].Images) == 0 {
			continue
		}
		hasHTTP := false
		for _, img := range out[i].Images {
			if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
				hasHTTP = true
				break
			}
		}
		if !hasHTTP {
			continue
		}
		imgs := make([]string, len(out[i].Images))
		for j, img := range out[i].Images {
			if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
				b64, err := downloadImageToBase64(img)
				if err != nil {
					return nil, fmt.Errorf("convert image %s: %w", img, err)
				}
				imgs[j] = b64
			} else {
				imgs[j] = img
			}
		}
		out[i].Images = imgs
	}
	return out, nil
}

// --- LLM error types ---

// ErrRateLimited is returned when the LLM API responds with HTTP 429 or 529.
type ErrRateLimited struct {
	RetryAfter time.Duration
	StatusCode int
	Body       string
	// Overloaded is true for Anthropic-specific overload signals (HTTP 529 or
	// SSE overloaded_error). Callers can use this to trigger a one-shot
	// fallback to a secondary provider before normal retry/backoff.
	Overloaded bool
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
// OpenRouter returns 402 when the account has insufficient credits — treated as
// a permanent error rather than a rate limit.
func parseRateLimitError(resp *http.Response, body []byte) *ErrRateLimited {
	if resp.StatusCode == 402 {
		return nil // handled as permanent error by caller
	}
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
		Overloaded: resp.StatusCode == 529, // Anthropic uses 529 for overloaded
	}
}

// tryFallbackProvider returns the fallback provider and model when an
// overloaded error occurs and a fallback is configured. The second return
// value is true when a swap should happen. Callers should pass `attempted` as
// true after the first swap so the fallback is only used once per call.
func tryFallbackProvider(provider *LLMProvider, model string, err error, attempted bool) (*LLMProvider, string, bool) {
	if attempted || provider == nil || provider.Fallback == nil {
		return provider, model, false
	}
	rlErr, ok := IsRateLimited(err)
	if !ok || !rlErr.Overloaded {
		return provider, model, false
	}
	fb := provider.Fallback
	fbModel := provider.FallbackModel
	if fbModel == "" {
		fbModel = fb.DefaultModel()
	}
	slog.Warn("LLM overloaded, falling back to secondary provider",
		"from_provider", provider.Name, "to_provider", fb.Name,
		"from_model", model, "to_model", fbModel)
	return fb, fbModel, true
}

// withFallback wraps a one-shot LLM call. If the call fails with an overloaded
// error and the provider has a fallback configured, it is retried once with
// the fallback provider and its FallbackModel.
func withFallback[T any](provider *LLMProvider, model string, fn func(*LLMProvider, string) (T, error)) (T, error) {
	result, err := fn(provider, model)
	if err == nil {
		return result, nil
	}
	newProvider, newModel, swapped := tryFallbackProvider(provider, model, err, false)
	if !swapped {
		return result, err
	}
	return fn(newProvider, newModel)
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
	// CacheReadInclusive is true when the provider (e.g. DeepSeek) includes
	// cache-read tokens inside PromptTokens. Anthropic reports them separately.
	CacheReadInclusive bool
	LatencyMs          int64
	IsStreaming        bool
	InputMessages      []Message  // optional: messages sent to LLM (only when TrackContent is set)
	OutputText         string     // optional: LLM text completion (only when TrackContent is set)
	ToolCalls          []ToolCall // optional: tool calls emitted by the LLM in this round
	SpanName           string     // LLM function name (e.g. "get_json", "stream_text") for PostHog $ai_span_name
	RecordedAt         time.Time  // wall-clock time when this entry was recorded
	CostUSD            float64    // upstream or estimated cost in USD
	CostSource         string     // "upstream" when usage.cost was provided, otherwise "catalog_estimate"
}

// modelCosts maps normalized model names to per-million-token pricing {input, output}.
// Kept minimal — only models we actually use. Unknown models get zero cost.
var modelCosts = map[string][2]float64{
	"claude-sonnet-4.6":         {3.0, 15.0},
	"claude-sonnet-4-6":         {3.0, 15.0},
	"claude-sonnet-4-5":         {3.0, 15.0},
	"claude-sonnet-4":           {3.0, 15.0},
	"claude-sonnet-4-20250514":  {3.0, 15.0},
	"claude-haiku-4-5":          {0.8, 4.0},
	"claude-haiku-4-5-20251001": {0.8, 4.0},
	"claude-opus-4.7":           {5.0, 25.0},
	"claude-opus-4-6":           {15.0, 75.0},
	"claude-opus-4-5":           {15.0, 75.0},
	"kimi-k2.6":                 {1.0, 4.0},
	"kimi-k2.5":                 {2.0, 8.0},
	// OpenRouter models
	"deepseek-v4-pro":   {0.435, 0.87},
	"deepseek-v4-flash": {0.1, 0.2},
	"deepseek-chat":     {0.5, 2.0},
	"deepseek-reasoner": {0.5, 2.0},
	"deepseek-r1":       {0.5, 2.0},
	"grok-3-beta":       {3.0, 15.0},
	"grok-3":            {3.0, 15.0},
	"grok-3-fast-beta":  {5.0, 25.0},
	"gemini-2.5-flash":  {0.15, 0.6},
	"gemini-2.5-pro":    {1.25, 10.0},
	// Step models (via OpenRouter)
	"step-3.5-flash": {0.09, 0.30},
	"step-3.7-flash": {0.20, 1.15},
	// Xiaomi models (via OpenRouter)
	"mimo-v2.5": {0.14, 0.28},
}

// cacheReadIsInclusive returns true for providers where cache-read tokens are
// counted inside prompt_tokens (DeepSeek via OpenRouter). For Anthropic,
// cache_read_input_tokens are reported separately and are NOT included in
// prompt_tokens.
func cacheReadIsInclusive(model string) bool {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	return strings.Contains(model, "deepseek")
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
	regularInput := inputTokens - cacheCreation
	if cacheReadIsInclusive(model) {
		regularInput -= cacheRead
	}
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
	// Cache pricing: creation = 1.25x input, read = 0.1x input, regular input = 1x.
	// Anthropic: prompt_tokens = regular input + cache creation (cache reads are
	//   reported separately in cache_read_input_tokens).
	// DeepSeek:  prompt_tokens = regular input + cache reads (cache reads are
	//   reported inside prompt_tokens_details.cached_tokens and INCLUDED in
	//   prompt_tokens). No separate cache creation tokens.
	regularInput := inputTokens - cacheCreation
	if cacheReadIsInclusive(model) {
		regularInput -= cacheRead
	}
	if regularInput < 0 {
		regularInput = 0
	}
	cost := float64(regularInput)*inputPrice/1_000_000 +
		float64(cacheCreation)*inputPrice*1.25/1_000_000 +
		float64(cacheRead)*inputPrice*0.1/1_000_000 +
		float64(outputTokens)*outputPrice/1_000_000
	return cost
}

// resolveCostUSD picks the most accurate cost for a single LLM call: the
// upstream-billed `usage.cost` (OpenRouter) when present, otherwise the
// catalog-based estimate. Without this fallback, every PostHog $ai_generation
// for a non-Claude model (modelCosts only carries Claude SKUs) reported
// $ai_total_cost_usd = 0 even though OpenRouter knew the real number.
func resolveCostUSD(model string, usage UsageInfo) (float64, string) {
	if usage.Cost > 0 {
		return usage.Cost, "upstream"
	}
	return estimateCostUSD(model, usage.PromptTokens, usage.CompletionTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens), "catalog_estimate"
}

func recordLangfuseGeneration(ctx context.Context, model string, usage UsageInfo, latencyMs int64, streaming bool, spanName string, round int, startedAt time.Time, inputMessages []Message, outputText string, toolNames []string) {
	cost, costSource := resolveCostUSD(model, usage)
	_, span := observability.StartGeneration(ctx, spanName, observability.GenerationAttributeInput{
		Model:                    model,
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		LatencyMs:                latencyMs,
		IsStreaming:              streaming,
		SpanName:                 spanName,
		CostUSD:                  cost,
		CostSource:               costSource,
		InputMessages:            messageSnapshots(inputMessages),
		OutputText:               outputText,
		ToolNames:                toolNames,
	}, round, startedAt)
	if tracker := TokenTrackerFromContext(ctx); tracker != nil {
		required, loaded := tracker.AgentSkills()
		span.SetAttributes(observability.TraceAttributes(observability.TraceAttributeInput{
			UserID:         tracker.UserID,
			OrgID:          tracker.OrganizationID,
			ProjectID:      tracker.ProjectID,
			TraceID:        tracker.TraceID,
			RunID:          tracker.RunID,
			Input:          tracker.TraceInput,
			Output:         tracker.TraceOutput,
			RequiredSkills: required,
			LoadedSkills:   loaded,
			Tags:           []string{"wemob", "llm"},
		})...)
	}
	endOpts := []trace.SpanEndOption{}
	if !startedAt.IsZero() {
		endOpts = append(endOpts, trace.WithTimestamp(startedAt.Add(time.Duration(latencyMs)*time.Millisecond)))
	}
	span.End(endOpts...)
}

func messageSnapshots(messages []Message) []observability.MessageSnapshot {
	if len(messages) == 0 {
		return nil
	}
	out := make([]observability.MessageSnapshot, 0, len(messages))
	for _, m := range messages {
		out = append(out, observability.MessageSnapshot{
			Role:    m.Role,
			Content: m.Content,
			Images:  append([]string(nil), m.Images...),
		})
	}
	return out
}

func toolCallNamesFromMap(toolCallsMap map[int]*ToolCall) []string {
	if len(toolCallsMap) == 0 {
		return nil
	}
	indices := make([]int, 0, len(toolCallsMap))
	for idx := range toolCallsMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	names := make([]string, 0, len(indices))
	for _, idx := range indices {
		tc := toolCallsMap[idx]
		if tc != nil && tc.Function != nil && tc.Function.Name != "" {
			names = append(names, tc.Function.Name)
		}
	}
	return names
}

// TokenTracker is a thread-safe accumulator that collects token usage across multiple LLM calls.
type TokenTracker struct {
	mu             sync.Mutex
	entries        []TokenEntry
	toolsUsed      []string // tool names called during this trace (for PostHog $ai_trace)
	TrackContent   bool     // opt-in: when true, StreamWithTools records message content per round
	TraceInput     string   // user's original message (for $ai_trace)
	TraceOutput    string   // final assistant response (for $ai_trace)
	UserID         string   // for slog attribution
	ProjectID      string   // for slog attribution
	OrganizationID string   // for PostHog org_id attribution on $ai_generation / $ai_trace
	// TraceID, when non-empty, overrides the auto-generated trace id in
	// RecordTokenUsage so callers (agent.Run) can bind the PostHog trace to
	// an external identifier such as AppAgentRun.ID. Error events emitted
	// separately via RecordLLMError also use this id to stay linked to the
	// trace's $ai_generation events.
	TraceID string
	RunID   string // optional: written to events as a "run_id" property for cross-system correlation
	// TraceErrorCode / TraceErrorMessage, when non-empty, mark the $ai_trace
	// event as errored so PostHog's trace list surfaces the failure without
	// relying on scanning child generations. Populated by agent.Run's failure
	// path before RecordLLMUsage is invoked.
	TraceErrorCode    string
	TraceErrorMessage string
	StartedAt         time.Time // wall-clock start of the generation (for accurate $ai_latency)
	RequiredSkillIDs  []string
	LoadedSkillIDs    []string
}

// RecordToolUsed appends a tool name to the list of tools called in this trace.
func (t *TokenTracker) RecordToolUsed(toolName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolsUsed = append(t.toolsUsed, toolName)
}

// ToolsUsed returns a deduplicated list of tool names called in this trace.
func (t *TokenTracker) ToolsUsed() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.toolsUsed) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(t.toolsUsed))
	deduped := make([]string, 0, len(t.toolsUsed))
	for _, name := range t.toolsUsed {
		if !seen[name] {
			seen[name] = true
			deduped = append(deduped, name)
		}
	}
	return deduped
}

func (t *TokenTracker) SetAgentSkills(required, loaded []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.RequiredSkillIDs = dedupeStrings(required)
	t.LoadedSkillIDs = dedupeStrings(loaded)
}

func (t *TokenTracker) AgentSkills() (required, loaded []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	required = append([]string(nil), t.RequiredSkillIDs...)
	loaded = append([]string(nil), t.LoadedSkillIDs...)
	return required, loaded
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
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
	cost, costSource := resolveCostUSD(model, usage)
	t.entries = append(t.entries, TokenEntry{
		Model:                    model,
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheReadInclusive:       cacheReadIsInclusive(model),
		LatencyMs:                latencyMs,
		IsStreaming:              isStreaming,
		SpanName:                 name,
		RecordedAt:               time.Now(),
		CostUSD:                  cost,
		CostSource:               costSource,
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
	cost, costSource := resolveCostUSD(model, usage)
	t.entries = append(t.entries, TokenEntry{
		Model:                    model,
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheReadInclusive:       cacheReadIsInclusive(model),
		LatencyMs:                latencyMs,
		IsStreaming:              isStreaming,
		InputMessages:            msgs,
		OutputText:               outputText,
		SpanName:                 name,
		RecordedAt:               time.Now(),
		CostUSD:                  cost,
		CostSource:               costSource,
	})
}

// SetLastEntryToolCalls attaches the tool calls emitted by the LLM in the
// most recent round to the last TokenEntry. This feeds PostHog's
// $ai_tools_called property on the per-generation event.
func (t *TokenTracker) SetLastEntryToolCalls(toolCalls []ToolCall) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) == 0 {
		return
	}
	t.entries[len(t.entries)-1].ToolCalls = make([]ToolCall, len(toolCalls))
	copy(t.entries[len(t.entries)-1].ToolCalls, toolCalls)
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

// CacheTotals returns aggregated prompt-cache creation/read token counts.
func (t *TokenTracker) CacheTotals() (cacheRead, cacheWrite int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.entries {
		cacheRead += e.CacheReadInputTokens
		cacheWrite += e.CacheCreationInputTokens
	}
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
	Type       string       `json:"type"` // "function", "builtin_function", "server_tool"
	Function   ToolFunction `json:"function,omitempty"`
	ServerTool *ServerTool  `json:"-"` // Anthropic server-side tools; not serialized to OpenAI format
}

// ServerTool represents an Anthropic server-side tool (e.g. web_search).
type ServerTool struct {
	Type    string `json:"type"` // e.g. "web_search_20250305"
	Name    string `json:"name"` // e.g. "web_search"
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
	Effort  string `json:"effort,omitempty"`  // "none", "low", "medium", "high", "xhigh"
	Exclude bool   `json:"exclude,omitempty"` // true = model thinks but doesn't return reasoning tokens
	Enabled *bool  `json:"enabled,omitempty"` // explicitly enable/disable
}

// ThinkingConfig controls thinking mode on Moonshot (Kimi).
// See https://docs.z.ai/guides/capabilities/thinking-mode
type ThinkingConfig struct {
	Type          string `json:"type"`                     // "enabled" or "disabled"
	ClearThinking bool   `json:"clear_thinking,omitempty"` // false = preserve for cache
	Keep          string `json:"keep,omitempty"`           // "all" = preserve reasoning_content across multi-turn (Moonshot K2.6)
}

type StreamOptionsConfig struct {
	IncludeUsage bool `json:"include_usage"`
}

type ChatRequest struct {
	Model               string               `json:"model"`
	Messages            []Message            `json:"messages"`
	Stream              bool                 `json:"stream,omitempty"`
	StreamOptions       *StreamOptionsConfig `json:"stream_options,omitempty"`
	MaxTokens           int                  `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                  `json:"max_completion_tokens,omitempty"`
	Temperature         *float64             `json:"temperature,omitempty"`
	TopP                *float64             `json:"top_p,omitempty"`
	ResponseFormat      *ResponseFormat      `json:"response_format,omitempty"`
	Tools               []Tool               `json:"tools,omitempty"`
	ToolChoice          any                  `json:"tool_choice,omitempty"`
	Reasoning           *ReasoningConfig     `json:"reasoning,omitempty"`
	Thinking            *ThinkingConfig      `json:"thinking,omitempty"`
	DisablePromptCache  bool                 `json:"-"`                          // skip Anthropic cache_control on system blocks (for one-shot calls with no follow-up)
	PromptCacheKey      string               `json:"prompt_cache_key,omitempty"` // Moonshot K2.6 session-level prompt cache key
}

type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // Moonshot: preserve thinking across tool rounds
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
	// Cost is the upstream-billed USD for this call. OpenRouter returns
	// it in usage.cost when the request sets `usage.include = true`; it
	// already accounts for prompt-cache discounts, provider routing
	// surcharges, and thinking-token pricing — everything the hardcoded
	// catalog can't see. Gateway chat reads this in preference to
	// catalog math, falling back to per-M prices only if the upstream
	// didn't return a cost.
	Cost float64 `json:"cost,omitempty"`
}

// UnmarshalJSON parses UsageInfo from the provider response. It handles both
// the flat Anthropic-style fields (cache_creation_input_tokens,
// cache_read_input_tokens) and the nested DeepSeek/OpenRouter field
// prompt_tokens_details.cached_tokens.
func (u *UsageInfo) UnmarshalJSON(data []byte) error {
	type Alias UsageInfo
	var aux struct {
		*Alias
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	aux.Alias = (*Alias)(u)
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	// DeepSeek via OpenRouter reports cached tokens inside
	// prompt_tokens_details.cached_tokens. These tokens are INCLUDED in
	// prompt_tokens (unlike Anthropic where cache_read_input_tokens are
	// EXCLUDED). Only populate CacheReadInputTokens from the nested field
	// when the flat Anthropic field is absent.
	if aux.PromptTokensDetails.CachedTokens > 0 && u.CacheReadInputTokens == 0 {
		u.CacheReadInputTokens = aux.PromptTokensDetails.CachedTokens
	}
	return nil
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
	prepared, err := prepareChatRequest(req, provider)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(prepared)
	if err != nil {
		return nil, err
	}
	// OpenRouter: inject usage.include=true so the response carries the
	// upstream-billed cost in usage.cost. Without this, streaming responses
	// omit cost and we fall back to catalog math that misses provider
	// discounts (prompt-cache, routing surcharges, thinking tokens).
	if provider != nil && provider.IsOpenRouter() {
		var m map[string]any
		if err := json.Unmarshal(body, &m); err == nil {
			m["usage"] = map[string]any{"include": true}
			body, _ = json.Marshal(m)
		}
	}
	return body, nil
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
// "thinking" format (Moonshot/Kimi), it converts the OpenRouter-style
// "reasoning" config into the thinking config and sets required defaults.
func prepareChatRequest(req ChatRequest, provider *LLMProvider) (ChatRequest, error) {
	if provider == nil {
		return req, nil
	}

	// Anthropic uses a different request format entirely (handled by buildLLMRequestBody)
	if provider.IsAnthropic() {
		return req, nil
	}

	// --- Provider-specific image + thinking transformations ---
	if provider.IsMoonshot() || provider.IsOpenRouter() {
		// Moonshot (Kimi) requires base64 images (rejects HTTPS URLs).
		// OpenRouter accepts both, but we convert to base64 for reliability
		// when the images are our own Wemob asset URLs (302 → short-lived
		// presigned S3). This avoids transient "provider failed to fetch image"
		// errors during long agent runs.
		if len(req.Messages) > 0 {
			converted, err := convertImageURLsToBase64(req.Messages)
			if err != nil {
				return req, err
			}
			req.Messages = converted
		}
	}

	if provider.IsMoonshot() {
		// Kimi K2.6 defaults thinking to "enabled" when omitted. Explicitly
		// default to disabled for predictable agent behavior unless reasoning
		// was explicitly requested.
		if req.Reasoning == nil && req.Thinking == nil && provider.DefaultReasoning == nil {
			req.Thinking = &ThinkingConfig{Type: "disabled"}
		}
	}

	// Apply provider's default reasoning when no explicit config was supplied.
	// All providers inherit it; thinking-format providers translate it below.
	if req.Reasoning == nil && req.Thinking == nil && provider.DefaultReasoning != nil {
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
			// K2.5/K2.6 thinking: temperature MUST be 1.0, top_p MUST be 0.95
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
			// Moonshot K2.6: preserve reasoning_content across multi-turn tool
			// calling so the model doesn't re-reason from scratch each round.
			if provider.IsMoonshot() {
				req.Thinking.Keep = "all"
			}
		} else if provider.IsMoonshot() {
			// K2.5/K2.6 non-thinking: temperature MUST be 0.6, top_p MUST be 0.95
			t := 0.6
			req.Temperature = &t
			tp := 0.95
			req.TopP = &tp
		}
	}

	// Moonshot/K2.5/K2.6: always prefer max_completion_tokens over max_tokens
	if provider.IsMoonshot() && req.MaxTokens > 0 && req.MaxCompletionTokens == 0 {
		req.MaxCompletionTokens = req.MaxTokens
		req.MaxTokens = 0
	}

	return req, nil
}

// GetJSONFromLLM sends a system+user message to the LLM and returns the raw response text.
func GetJSONFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider) (string, error) {
	return withFallback(provider, model, func(p *LLMProvider, m string) (string, error) {
		return getJSONFromLLMOnce(ctx, m, system, message, p)
	})
}

func getJSONFromLLMOnce(ctx context.Context, model, system, message string, provider *LLMProvider) (string, error) {
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
		ResponseFormat:     &ResponseFormat{Type: "json_object"},
		DisablePromptCache: false, // enable cache — identical system prompts (classification, naming, validation) benefit from repeated calls
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
	SetLLMHeaders(req, provider)

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
		if resp.StatusCode == 402 {
			return "", &ErrPermanentLLM{ErrorType: "payment_required", Message: "OpenRouter account has insufficient credits"}
		}
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

	content := stripCodeFences(parsed.Choices[0].Message.Content)

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs}, trackerAttrs(ctx)...)...)
		msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: message}}
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, content, "get_json")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_json")
			}
		}
		recordLangfuseGeneration(ctx, model, parsed.Usage, latencyMs, false, "get_json", 1, start, msgs, content, nil)
	}

	return content, nil
}

// stripCodeFences removes markdown code fences (```json ... ```) that LLMs
// sometimes wrap around JSON output despite being told not to.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

func GetTextFromLLM(ctx context.Context, model, system, message string, provider *LLMProvider, maxTokens ...int) (string, error) {
	return withFallback(provider, model, func(p *LLMProvider, m string) (string, error) {
		return getTextFromLLMOnce(ctx, m, system, message, p, maxTokens...)
	})
}

func getTextFromLLMOnce(ctx context.Context, model, system, message string, provider *LLMProvider, maxTokens ...int) (string, error) {
	if provider == nil || provider.APIKey == "" {
		return "", fmt.Errorf("missing LLM provider configuration")
	}

	// Default to 4096 for one-shot text calls. Callers that need more (e.g.
	// seed SQL generation) pass an explicit limit. The previous default of
	// 16384 caused runaway generations when the model ignored short-output
	// instructions (e.g. summarization prompts producing 16K tokens of garbage).
	tokenLimit := 4096
	if len(maxTokens) > 0 && maxTokens[0] > 0 {
		tokenLimit = maxTokens[0]
	}

	body, err := buildLLMRequestBody(ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
		MaxTokens:          tokenLimit,
		DisablePromptCache: false, // enable cache — identical system prompts (classification, naming, validation) benefit from repeated calls
	}, provider)
	if err != nil {
		return "", fmt.Errorf("error building request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("error creating LLM request: %w", err)
	}
	SetLLMHeaders(req, provider)

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
		if resp.StatusCode == 402 {
			return "", &ErrPermanentLLM{ErrorType: "payment_required", Message: "OpenRouter account has insufficient credits"}
		}
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

	content := parsed.Choices[0].Message.Content
	finishReason := parsed.Choices[0].FinishReason

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", append([]any{"from", "llm_utils_text", "model", model, "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens, "latency_ms", latencyMs, "finish_reason", finishReason}, trackerAttrs(ctx)...)...)
		msgs := []Message{{Role: "system", Content: system}, {Role: "user", Content: message}}
		if tracker := TokenTrackerFromContext(ctx); tracker != nil {
			if tracker.TrackContent {
				tracker.RecordWithContent(model, parsed.Usage, latencyMs, false, msgs, content, "get_text")
			} else {
				tracker.Record(model, parsed.Usage, latencyMs, false, "get_text")
			}
		}
		recordLangfuseGeneration(ctx, model, parsed.Usage, latencyMs, false, "get_text", 1, start, msgs, content, nil)
	}

	// Detect truncated responses — the model hit max_tokens before finishing.
	// Return the content alongside the error so callers can decide whether to
	// use the partial result or discard it.
	if finishReason == "length" {
		slog.Warn("GetTextFromLLM: response truncated (hit max_tokens)",
			"model", model, "max_tokens", tokenLimit,
			"output_tokens", parsed.Usage.CompletionTokens)
		return content, ErrResponseTruncated
	}

	return content, nil
}

type StreamDelta struct {
	Content          string     `json:"content,omitempty"`
	Reasoning        string     `json:"reasoning,omitempty"`         // OpenRouter
	ReasoningContent string     `json:"reasoning_content,omitempty"` // Moonshot
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

// MultiContentPart represents a content part in a multimodal message.
type MultiContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *ImageURLContent `json:"image_url,omitempty"`
}

type ImageURLContent struct {
	URL string `json:"url"`
}

// ToolCallConfig holds optional configuration for StreamWithTools.
type ToolCallConfig struct {
	MaxRounds            int                                                        // Maximum tool-calling rounds (default 10)
	MaxTokens            int                                                        // Max completion tokens per round (default 16384)
	TrimArgs             func(toolName, args string) string                         // Trim tool call arguments in history to reduce token usage
	TrimResults          func(toolName, result string) string                       // Trim tool result messages in history to reduce token usage
	InterceptToolCall    func(toolName, toolArgs string) (result string, skip bool) // Intercept tool calls before execution; if skip=true, use result and skip executor
	FilterTools          func(tools []Tool) []Tool                                  // Called each round to filter/modify tools; return nil to strip all tools
	OnToolCallStart      func(toolName string)                                      // Called when a tool call name is first detected during SSE streaming (before args are complete)
	OnToolResult         func(toolName string, result string, isError bool)         // Called after each tool executes (success, failure, dedup, or intercept). Useful for emitting tool_result SSE events.
	OnRoundStart         func(round int)                                            // Called at the start of each round (useful for resetting per-round state like text buffers)
	MaxContinuations     int                                                        // Max finish_reason=length continuation rounds (default 3). -1 = disable continuations entirely.
	ShouldContinue       func() bool                                                // Called before each round/continuation. Return false to abort. nil = always continue.
	PromptCacheKey       string                                                     // Moonshot K2.6 session-level prompt cache key (sent on every round)
	MaxMessages          int                                                        // Maximum messages to send to the LLM; drops oldest non-system messages when exceeded. 0 = unlimited.
	RefreshPrefix        func(msg *Message)                                         // Called before each round (except round 0) to refresh the user-turn prefix (e.g. file tree). Modifies the message in-place.
	CompactHistory       func(messages []Message) ([]Message, bool)                 // Called before each round to replace old history with a compact LLM view. bool=true when changed.
	KeepRecentToolRounds int                                                        // Number of latest completed tool rounds to leave untrimmed. Older rounds may have args/results trimmed. Default 1.
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
	maxTokens := 16384
	maxContinuations := 3
	if len(opts) > 0 {
		if opts[0].MaxRounds > 0 {
			maxRounds = opts[0].MaxRounds
		}
		if opts[0].MaxTokens > 0 {
			maxTokens = opts[0].MaxTokens
		}
		if opts[0].MaxContinuations != 0 { // 0 = use default; -1 = disable; positive = explicit limit
			maxContinuations = opts[0].MaxContinuations
		}
	}
	promptCacheKey := ""
	if len(opts) > 0 {
		promptCacheKey = opts[0].PromptCacheKey
	}
	keepRecentToolRounds := 1
	if len(opts) > 0 && opts[0].KeepRecentToolRounds > 0 {
		keepRecentToolRounds = opts[0].KeepRecentToolRounds
	}
	currentMessages := make([]Message, len(messages))
	copy(currentMessages, messages)

	// appendToLastUserMsg appends text to the last message if it's a user message,
	// or creates a new user message at the end. This keeps nudges AFTER tool results
	// in conversation flow and preserves cache for earlier messages.
	appendToLastUserMsg := func(text string) {
		last := len(currentMessages) - 1
		if last >= 0 && currentMessages[last].Role == "user" {
			currentMessages[last].Content += "\n\n[System] " + text
			return
		}
		// Append a new user message at the end — convertToAnthropicRequest
		// will handle alternation. This keeps nudges AFTER tool results
		// in conversation flow and preserves cache for earlier messages.
		currentMessages = append(currentMessages, Message{Role: "user", Content: "[System] " + text})
	}

	// toolCallHistory caches tool results for deduplication across rounds.
	// No mutex is needed because tools execute sequentially (each tool call
	// blocks via select until completion before the next one starts).
	toolCallHistory := make(map[string]string) // "tool:args" → result (dedup cache)
	consecutiveEmptyResponses := 0
	rateLimitRetries := 0
	transientRetries := 0
	fallbackAttempted := false
	var toolRoundHistory []toolRoundTrimState
	continuationCount := 0
	incompleteToolCallRetries := 0
	var recoveryToolDenylist map[string]bool
	var fullText strings.Builder // declared outside loop so ShouldContinue abort can return partial text
	recoverIncompleteToolCall := func(round int, finishReason string, toolCallsMap map[int]*ToolCall) bool {
		if len(toolCallsMap) == 0 || incompleteToolCallRetries >= incompleteToolCallRecoveryRetries {
			return false
		}
		incompleteToolCallRetries++
		if incompleteToolCallsInclude(toolCallsMap, "write_files") {
			recoveryToolDenylist = map[string]bool{"write_files": true}
		}
		if text := strings.TrimSpace(fullText.String()); text != "" {
			currentMessages = append(currentMessages, Message{
				Role:    "assistant",
				Content: text,
			})
		}
		currentMessages = append(currentMessages, Message{
			Role: "user",
			Content: incompleteToolCallRecoveryMessage(
				finishReason,
				toolCallsMap,
				incompleteToolCallRetries,
				incompleteToolCallRecoveryRetries,
			),
		})
		slog.Warn("LLM response ended during tool call; retrying with recovery prompt",
			"round", round,
			"finish_reason", finishReason,
			"tool_calls", len(toolCallsMap),
			"retry", incompleteToolCallRetries,
			"max_retries", incompleteToolCallRecoveryRetries)
		return true
	}

	for round := 0; round < maxRounds; round++ {
		// Check caller abort signal before starting any round
		if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
			slog.Info("Caller aborted StreamWithTools via ShouldContinue", "round", round)
			return fullText.String(), nil
		}
		// Notify caller that a new round is starting (e.g. to reset per-round state).
		if len(opts) > 0 && opts[0].OnRoundStart != nil {
			opts[0].OnRoundStart(round)
		}
		if len(opts) > 0 {
			toolRoundHistory = trimToolRoundsOlderThan(
				currentMessages,
				toolRoundHistory,
				keepRecentToolRounds,
				opts[0].TrimArgs,
				opts[0].TrimResults,
			)
		}
		roundTools := tools
		if len(opts) > 0 && opts[0].FilterTools != nil {
			roundTools = opts[0].FilterTools(roundTools)
		}
		if len(recoveryToolDenylist) > 0 {
			roundTools = filterToolsByDeniedNames(roundTools, recoveryToolDenylist)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// Refresh the user-turn prefix before each round so the model sees
		// current file contents (e.g. file tree) before old history is compacted
		// or trimmed.
		if len(opts) > 0 && opts[0].RefreshPrefix != nil && round > 0 {
			for i := len(currentMessages) - 1; i >= 0; i-- {
				if currentMessages[i].Role == "user" && !strings.HasPrefix(currentMessages[i].Content, "[System]") {
					opts[0].RefreshPrefix(&currentMessages[i])
					break
				}
			}
		}

		if len(opts) > 0 && opts[0].CompactHistory != nil {
			if compacted, changed := opts[0].CompactHistory(currentMessages); changed {
				currentMessages = compacted
				slog.Warn("StreamWithTools compacted message history", "round", round, "remaining", len(currentMessages))
			}
		}

		// Trim message history if it exceeds the max message cap.
		// We must not split a tool round — a tool message must always follow its
		// assistant message, and all tool messages for an assistant must be contiguous.
		// Anthropic rejects requests where a tool_result lacks a preceding tool_use.
		// Keep the current run context user message pinned, because it carries the
		// live file tree/build state/original request across long tool loops.
		if len(opts) > 0 && opts[0].MaxMessages > 0 && len(currentMessages) > opts[0].MaxMessages {
			// Keep all leading system messages; drop oldest non-system messages.
			systemCount := 0
			for i := range currentMessages {
				if currentMessages[i].Role == "system" {
					systemCount++
				} else {
					break
				}
			}
			pinnedIdx := -1
			for i := systemCount; i < len(currentMessages); i++ {
				if isPinnedCurrentRunMessage(currentMessages[i]) {
					pinnedIdx = i
					break
				}
			}
			// How many non-system messages we can keep.
			nonSystemKeep := opts[0].MaxMessages - systemCount
			if nonSystemKeep < 0 {
				nonSystemKeep = 0
			}
			suffixKeep := nonSystemKeep
			if pinnedIdx >= systemCount {
				suffixKeep--
			}
			if suffixKeep < 0 {
				suffixKeep = 0
			}
			// Ideal start index for non-system messages.
			keepFrom := len(currentMessages) - suffixKeep
			if keepFrom < systemCount {
				keepFrom = systemCount
			}
			// Never start the kept suffix on a tool message — that would orphan it
			// from its assistant (Anthropic: "tool_result must have a corresponding
			// tool_use in the previous message").
			for keepFrom < len(currentMessages) && currentMessages[keepFrom].Role == "tool" {
				keepFrom++
			}
			if keepFrom > systemCount {
				dropped := keepFrom - systemCount
				trimmed := make([]Message, 0, opts[0].MaxMessages+1)
				trimmed = append(trimmed, currentMessages[:systemCount]...)
				if pinnedIdx >= systemCount && pinnedIdx < keepFrom {
					trimmed = append(trimmed, currentMessages[pinnedIdx])
					dropped--
				}
				trimmed = append(trimmed, currentMessages[keepFrom:]...)
				currentMessages = trimmed
				slog.Warn("StreamWithTools trimmed message history", "dropped", dropped, "remaining", len(currentMessages), "max", opts[0].MaxMessages)
			}
		}

		// Moonshot (and occasionally other providers) omit tool_call.id in
		// streaming deltas. Without a matching id on the assistant message's
		// tool_calls, the provider rejects the next round with
		// "tool_call_id is not found". Backfill empty ids in the entire
		// history so in-flight runs survive the round transition.
		for i := 0; i < len(currentMessages); i++ {
			if currentMessages[i].Role != "assistant" || len(currentMessages[i].ToolCalls) == 0 {
				continue
			}
			for j := range currentMessages[i].ToolCalls {
				if currentMessages[i].ToolCalls[j].ID == "" {
					currentMessages[i].ToolCalls[j].ID = fmt.Sprintf("call_synth_%d_%d", i, j)
				}
			}
			// The tool results for this assistant message follow immediately.
			for j := 0; j < len(currentMessages[i].ToolCalls) && i+1+j < len(currentMessages) && currentMessages[i+1+j].Role == "tool"; j++ {
				if currentMessages[i+1+j].ToolCallID == "" {
					currentMessages[i+1+j].ToolCallID = currentMessages[i].ToolCalls[j].ID
				}
			}
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
			PromptCacheKey: promptCacheKey,
		}, provider)
		if err != nil {
			return "", fmt.Errorf("round %d: error building request: %w", round, err)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint(), bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("round %d: error creating LLM request: %w", round, err)
		}
		SetLLMHeaders(req, provider)

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
			// One-shot fallback to secondary provider on overload (before backoff)
			if rlErr := parseRateLimitError(resp, respBody); rlErr != nil {
				if newProvider, newModel, swapped := tryFallbackProvider(provider, model, rlErr, fallbackAttempted); swapped {
					provider = newProvider
					model = newModel
					fallbackAttempted = true
					round-- // retry same round with fallback provider
					continue
				}
			}
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
			// OpenRouter 402 = insufficient credits — permanent error, do not retry
			if resp.StatusCode == 402 {
				return "", &ErrPermanentLLM{ErrorType: "payment_required", Message: "OpenRouter account has insufficient credits"}
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
		rateLimitRetries = 0 // reset after successful request
		transientRetries = 0
		recoveryToolDenylist = nil

		fullText.Reset()
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
				// One-shot fallback to secondary provider on overload (before backoff)
				if rlErr, ok := IsRateLimited(parseErr); ok {
					if newProvider, newModel, swapped := tryFallbackProvider(provider, model, rlErr, fallbackAttempted); swapped {
						provider = newProvider
						model = newModel
						fallbackAttempted = true
						round-- // retry same round with fallback provider
						continue
					}
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

					// Accumulate reasoning (Moonshot: reasoning_content, OpenRouter: reasoning)
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
				"cache_read_tokens", roundUsage.CacheReadInputTokens,
				"latency_ms", roundLatency,
				"tool_calls", len(toolCallsMap),
				"history_msgs", len(currentMessages),
			}, trackerAttrs(ctx)...)...)
			outputForTracking := fullText.String()
			if outputForTracking == "" && len(toolCallsMap) > 0 {
				var toolSummary []string
				for _, tc := range toolCallsMap {
					name := ""
					if tc.Function != nil {
						name = tc.Function.Name
					}
					toolSummary = append(toolSummary, fmt.Sprintf("[tool_call: %s]", name))
				}
				outputForTracking = strings.Join(toolSummary, " ")
			}
			toolNames := toolCallNamesFromMap(toolCallsMap)
			if tracker := TokenTrackerFromContext(ctx); tracker != nil {
				if tracker.TrackContent {
					// When the round produced tool calls instead of text, record a
					// summary of the tool calls so PostHog $ai_output_choices is not null.
					tracker.RecordWithContent(model, roundUsage, time.Since(roundStart).Milliseconds(), true, currentMessages, outputForTracking, "stream_with_tools")
					if len(toolCallsMap) > 0 {
						indices := make([]int, 0, len(toolCallsMap))
						for idx := range toolCallsMap {
							indices = append(indices, idx)
						}
						sort.Ints(indices)
						tcs := make([]ToolCall, 0, len(indices))
						for _, idx := range indices {
							tcs = append(tcs, *toolCallsMap[idx])
						}
						tracker.SetLastEntryToolCalls(tcs)
					}
				} else {
					tracker.Record(model, roundUsage, time.Since(roundStart).Milliseconds(), true, "stream_with_tools")
					if len(toolCallsMap) > 0 {
						indices := make([]int, 0, len(toolCallsMap))
						for idx := range toolCallsMap {
							indices = append(indices, idx)
						}
						sort.Ints(indices)
						tcs := make([]ToolCall, 0, len(indices))
						for _, idx := range indices {
							tcs = append(tcs, *toolCallsMap[idx])
						}
						tracker.SetLastEntryToolCalls(tcs)
					}
				}
			}
			recordLangfuseGeneration(ctx, model, roundUsage, roundLatency, true, "stream_with_tools", round, roundStart, currentMessages, outputForTracking, toolNames)
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
				appendToLastUserMsg("Your previous response was empty. Please generate your final response as plain text now.")
				continue
			}
			roundChunks = nil // discard intermediate reasoning chunks

			// Build sorted tool calls list
			indices := make([]int, 0, len(toolCallsMap))
			for idx := range toolCallsMap {
				indices = append(indices, idx)
			}
			sort.Ints(indices)

			toolCalls := make([]ToolCall, 0, len(indices))
			hasInvalidToolArgs := false
			for _, idx := range indices {
				if toolCallsMap[idx].ID == "" {
					toolCallsMap[idx].ID = fmt.Sprintf("call_synth_%d_%d", round, idx)
					slog.Warn("LLM streaming omitted tool_call id; using synthetic id", "round", round, "index", idx, "synthetic_id", toolCallsMap[idx].ID, "provider", provider.Name, "model", model)
				}
				if tc := toolCallsMap[idx]; tc != nil && tc.Function != nil {
					args := tc.Function.Arguments
					if args != "" && args != "{}" && !isValidJSON(args) {
						hasInvalidToolArgs = true
						slog.Warn("Tool call has invalid JSON arguments; recovering before execution",
							"tool", tc.Function.Name,
							"round", round,
							"args_len", len(args))
					}
				}
				toolCalls = append(toolCalls, *toolCallsMap[idx])
			}
			if hasInvalidToolArgs {
				if recoverIncompleteToolCall(round, lastFinishReason, toolCallsMap) {
					round-- // retry the same logical round with recovery context
					continue
				}
				return fullText.String(), fmt.Errorf("%w: finish_reason=%q", ErrIncompleteToolCall, lastFinishReason)
			}

			// Append assistant message with tool calls. Older rounds are later
			// trimmed by tool_call IDs so recent rounds keep their full shape.
			assistantMsg := Message{
				Role:      "assistant",
				Content:   fullText.String(),
				ToolCalls: toolCalls,
			}
			if fullReasoning.Len() > 0 {
				assistantMsg.ReasoningContent = fullReasoning.String()
			}
			currentMessages = append(currentMessages, assistantMsg)

			type preparedToolCall struct {
				tc       ToolCall
				name     string
				args     string
				cacheKey string
				result   string
				cached   bool
				skipExec bool
			}
			type executedToolCall struct {
				result string
				err    error
			}

			runTool := func(toolName, toolArgs string) executedToolCall {
				slog.Info("Executing tool call", "tool", toolName, "round", round)

				if onToolCall != nil {
					onToolCall(toolName, toolArgs)
				}

				// Execute with per-tool timeout to prevent a single slow tool from blocking the loop
				toolCtx, toolCancel := context.WithTimeout(ctx, toolExecTimeout)
				ch := make(chan executedToolCall, 1)
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("tool callback panicked", "tool", toolName, "panic", r)
							ch <- executedToolCall{"", fmt.Errorf("tool %s panicked: %v", toolName, r)}
						}
					}()
					r, e := executor(toolName, toolArgs)
					ch <- executedToolCall{r, e}
				}()
				select {
				case tr := <-ch:
					toolCancel()
					if tr.err != nil {
						tr.result = sanitizeToolError(toolName, tr.err)
					}
					return tr
				case <-toolCtx.Done():
					toolCancel()
					if ctx.Err() != nil {
						slog.Error("tool execution aborted: parent context cancelled", "tool", toolName, "round", round, "error", ctx.Err())
						return executedToolCall{"", ctx.Err()}
					}
					slog.Warn("tool execution timed out", "tool", toolName, "round", round, "timeout", toolExecTimeout)
					return executedToolCall{fmt.Sprintf("Error: tool %s timed out after %s", toolName, toolExecTimeout), nil}
				}
			}

			finishToolResult := func(toolName string, result string, tc ToolCall) {
				// Ensure tool result is non-empty; LLM providers reject empty tool results.
				if result == "" {
					slog.Warn("tool returned empty result", "tool", toolName, "round", round)
					result = "(no output)"
				}
				// Notify the caller so they can emit a tool_result SSE event.
				// "Error: ..." prefix matches the convention used by executors for
				// failure states and keeps the classifier simple on the frontend.
				if len(opts) > 0 && opts[0].OnToolResult != nil {
					isErr := strings.HasPrefix(result, "Error:") ||
						strings.Contains(result, " timed out after ") ||
						strings.Contains(result, " panicked: ")
					opts[0].OnToolResult(toolName, result, isErr)
				}
				currentMessages = append(currentMessages, Message{
					Role:       "tool",
					Content:    result,
					ToolCallID: tc.ID,
					Name:       toolName,
				})
			}

			// Prepare each tool call sequentially so interception, deduplication,
			// and JSON validation keep their existing deterministic semantics.
			dedupCount := 0
			interceptCount := 0
			prepared := make([]preparedToolCall, 0, len(toolCalls))
			allParallelSafe := len(opts) == 0 || opts[0].InterceptToolCall == nil
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
				if !isParallelSafeTool(toolName) {
					allParallelSafe = false
				}

				call := preparedToolCall{tc: tc, name: toolName, args: toolArgs}

				// Intercept tool calls before cache check and execution.
				if len(opts) > 0 && opts[0].InterceptToolCall != nil {
					interceptResult, skip := opts[0].InterceptToolCall(toolName, toolArgs)
					if skip {
						call.result = interceptResult
						call.skipExec = true
						interceptCount++
						call.cacheKey = normalizeToolCacheKey(toolName, toolArgs)
						toolCallHistory[call.cacheKey] = call.result
						slog.Info("Tool call intercepted", "tool", toolName, "round", round)
						prepared = append(prepared, call)
						continue
					}
					// Non-empty result with skip=false means modified args
					// (e.g. write_files batch with already-written files stripped out)
					if interceptResult != "" {
						toolArgs = interceptResult
						call.args = interceptResult
						if tc.Function != nil {
							tc.Function.Arguments = interceptResult
						}
					}
				}

				call.cacheKey = normalizeToolCacheKey(toolName, call.args)
				if cached, ok := toolCallHistory[call.cacheKey]; ok {
					call.result = cached
					call.cached = true
					call.skipExec = true
					dedupCount++
					slog.Info("Tool call deduped (cached)", "tool", toolName, "round", round)
					prepared = append(prepared, call)
					continue
				}

				// Validate JSON arguments before execution to avoid wasting a tool round.
				if call.args != "" && call.args != "{}" && !isValidJSON(call.args) {
					slog.Warn("Tool call has invalid JSON arguments", "tool", toolName, "round", round, "args_len", len(call.args))
					call.result = fmt.Sprintf("Error: invalid JSON arguments for tool %s. Please provide valid JSON.", toolName)
					call.skipExec = true
					toolCallHistory[call.cacheKey] = call.result
					prepared = append(prepared, call)
					continue
				}

				prepared = append(prepared, call)
			}

			if allParallelSafe {
				var wg sync.WaitGroup
				results := make([]executedToolCall, len(prepared))
				for i, call := range prepared {
					if call.skipExec {
						results[i] = executedToolCall{result: call.result}
						continue
					}
					wg.Add(1)
					go func(i int, call preparedToolCall) {
						defer wg.Done()
						results[i] = runTool(call.name, call.args)
					}(i, call)
				}
				wg.Wait()
				for i, call := range prepared {
					if ctx.Err() != nil {
						return "", ctx.Err()
					}
					result := results[i].result
					if results[i].err != nil {
						return result, results[i].err
					}
					if !call.skipExec {
						toolCallHistory[call.cacheKey] = result
					}
					finishToolResult(call.name, result, call.tc)
				}
			} else {
				for _, call := range prepared {
					if ctx.Err() != nil {
						return "", ctx.Err()
					}
					result := call.result
					if !call.skipExec {
						tr := runTool(call.name, call.args)
						result = tr.result
						if tr.err != nil {
							return result, tr.err
						}
						toolCallHistory[call.cacheKey] = result
					}
					finishToolResult(call.name, result, call.tc)
				}
			}

			if dedupCount > 0 && dedupCount+interceptCount == len(toolCalls) {
				appendToLastUserMsg("All your tool calls in this round returned cached results — you already have this information. Stop making redundant tool calls and produce your final response.")
				slog.Warn("All tool calls deduped; injected redundancy warning", "round", round, "dedup_count", dedupCount, "intercept_count", interceptCount)
			}

			if len(opts) > 0 && (opts[0].TrimArgs != nil || opts[0].TrimResults != nil) {
				ids := make([]string, 0, len(toolCalls))
				for _, tc := range toolCalls {
					if tc.ID != "" {
						ids = append(ids, tc.ID)
					}
				}
				toolRoundHistory = append(toolRoundHistory, toolRoundTrimState{toolCallIDs: ids})
			}

			// Continue the loop — re-call LLM with tool results
			continue
		} else {
			consecutiveEmptyResponses = 0
		}

		if len(toolCallsMap) > 0 {
			if recoverIncompleteToolCall(round, lastFinishReason, toolCallsMap) {
				round-- // retry the same logical round with recovery context
				continue
			}
			return fullText.String(), fmt.Errorf("%w: finish_reason=%q", ErrIncompleteToolCall, lastFinishReason)
		}

		// Chunks were already streamed in real-time via onChunk above.
		// No tool calls — return final response
		if lastFinishReason == "length" && round < maxRounds-1 {
			// Check caller abort signal before continuing
			if len(opts) > 0 && opts[0].ShouldContinue != nil && !opts[0].ShouldContinue() {
				slog.Info("Caller aborted continuation via ShouldContinue", "round", round, "response_length", fullText.Len())
				return fullText.String(), nil
			}
			// Check continuation limit
			if maxContinuations < 0 || continuationCount >= maxContinuations {
				slog.Warn("LLM response truncated, continuation limit reached",
					"continuations", continuationCount, "max", maxContinuations, "response_length", fullText.Len())
				return fullText.String(), ErrResponseTruncated
			}
			if fullText.Len() > maxResponseBufferSize {
				slog.Warn("response exceeded max size during continuation, returning partial", "size", fullText.Len())
				return fullText.String(), nil
			}
			// Response was truncated — request continuation by appending the partial
			// response as assistant message and asking the LLM to continue.
			continuationCount++
			slog.Warn("LLM response truncated (finish_reason=length), requesting continuation",
				"response_length", fullText.Len(), "round", round, "continuation", continuationCount, "max", maxContinuations)
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

	return "", &ToolRoundsExceededError{MaxRounds: maxRounds}
}
