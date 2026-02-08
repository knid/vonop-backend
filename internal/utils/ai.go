package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// llmHTTPClient is a shared HTTP client for all LLM calls with a 5-minute timeout.
var llmHTTPClient = &http.Client{
	Timeout: 5 * time.Minute,
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response structure for parsing the LLM output
type Choice struct {
	Message Message `json:"message"`
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

// create name for this func
func GetJSONFromLLM(model, system, message, apiKey string) (string, error) {
	if apiKey == "" {
		slog.Error("OPENROUTER_API_KEY is not configured")
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model: model, // or "claude-3.5-sonnet", "mistral-large-latest", etc.
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io") // Optional branding
	req.Header.Set("X-Title", "wemob-nav-analyzer")    // Optional project name

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("error generating items. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	slog.Info("LLM Response received", "FROM", "LLM_UTILS")

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		slog.Error("error creating items.", "error", err, "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("error creating items. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("no choices returned from LLM")
	}

	outputJson := CleanLLMOutput(parsed.Choices[0].Message.Content)

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
	}

	return outputJson, nil
}

func GetTextFromLLM(model, system, message, apiKey string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS")
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	slog.Info("LLM Response received", "FROM", "LLM_UTILS_TEXT")

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		slog.Error("error parsing LLM response", "error", err, "FROM", "LLM_UTILS_TEXT")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS_TEXT")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_TEXT", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
	}

	return parsed.Choices[0].Message.Content, nil
}

type StreamDelta struct {
	Content string `json:"content"`
}

type StreamChoice struct {
	Delta StreamDelta `json:"delta"`
}

type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
}

// StreamTextFromLLM streams LLM response tokens and calls onChunk for each text chunk.
// Returns the full accumulated text.
func StreamTextFromLLM(ctx context.Context, model, system, message, apiKey string, onChunk func(chunk string)) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model:  model,
		Stream: true,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		return "", fmt.Errorf("error generating content. try again please")
	}

	var fullText strings.Builder
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

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			fullText.WriteString(content)
			if onChunk != nil {
				onChunk(content)
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

	return fullText.String(), nil
}

// GetJSONFromLLMWithMessages sends a multi-turn conversation to the LLM and expects JSON output.
// Used for chat-based interactions where conversation history matters.
func GetJSONFromLLMWithMessages(model string, messages []Message, apiKey string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model:    model,
		Messages: messages,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	slog.Info("LLM Chat Response received", "FROM", "LLM_UTILS_CHAT")

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		slog.Error("error parsing LLM response", "error", err, "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		slog.Error("no choices returned from LLM", "FROM", "LLM_UTILS_CHAT")
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_CHAT", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
	}

	return CleanLLMOutput(parsed.Choices[0].Message.Content), nil
}

// StreamJSONFromLLMWithMessages streams an LLM response from a multi-turn conversation.
// Sends chunks via onChunk and returns the cleaned JSON.
func StreamJSONFromLLMWithMessages(ctx context.Context, model string, messages []Message, apiKey string, onChunk func(chunk string)) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model:    model,
		Stream:   true,
		Messages: messages,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		return "", fmt.Errorf("error generating content. try again please")
	}

	var fullText strings.Builder
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

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			fullText.WriteString(content)
			if onChunk != nil {
				onChunk(content)
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

	return CleanLLMOutput(fullText.String()), nil
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
func GetJSONFromLLMWithVision(model, system, userText, base64Image, apiKey string) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	messages := []MultiMessage{
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
	}

	reqBody := MultiChatRequest{
		Model:    model,
		Messages: messages,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making vision request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	slog.Info("LLM Vision Response received", "FROM", "LLM_UTILS_VISION")

	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		slog.Error("error parsing LLM vision response", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from LLM")
	}

	if parsed.Usage.TotalTokens > 0 {
		slog.Info("LLM usage", "FROM", "LLM_UTILS_VISION", "prompt_tokens", parsed.Usage.PromptTokens, "completion_tokens", parsed.Usage.CompletionTokens, "total_tokens", parsed.Usage.TotalTokens)
	}

	return CleanLLMOutput(parsed.Choices[0].Message.Content), nil
}

// StreamJSONFromLLM streams LLM response tokens for a JSON-expecting call.
// Sends chunks via onChunk and returns the cleaned JSON output.
func StreamJSONFromLLM(ctx context.Context, model, system, message, apiKey string, onChunk func(chunk string)) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	reqBody := ChatRequest{
		Model:  model,
		Stream: true,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: message},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		return "", fmt.Errorf("error generating content. try again please")
	}

	var fullText strings.Builder
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

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			fullText.WriteString(content)
			if onChunk != nil {
				onChunk(content)
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

	return CleanLLMOutput(fullText.String()), nil
}

// StreamJSONFromLLMWithVision streams a multimodal (vision) LLM response.
// Sends chunks via onChunk and returns the cleaned JSON output.
func StreamJSONFromLLMWithVision(ctx context.Context, model, system, userText, base64Image, apiKey string, onChunk func(chunk string)) (string, error) {
	if apiKey == "" {
		return "", fmt.Errorf("missing OPENROUTER_API_KEY")
	}

	type streamMultiChatRequest struct {
		Model    string         `json:"model"`
		Stream   bool           `json:"stream"`
		Messages []MultiMessage `json:"messages"`
	}

	messages := []MultiMessage{
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
	}

	reqBody := streamMultiChatRequest{
		Model:    model,
		Stream:   true,
		Messages: messages,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io")
	req.Header.Set("X-Title", "wemob-web-builder")

	resp, err := llmHTTPClient.Do(req)
	if err != nil {
		slog.Error("Error making vision streaming request to LLM:", "error", err)
		return "", fmt.Errorf("error generating content. try again please")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("LLM vision streaming returned non-200", "status", resp.StatusCode, "body", string(respBody))
		return "", fmt.Errorf("error generating content. try again please")
	}

	var fullText strings.Builder
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

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			content := chunk.Choices[0].Delta.Content
			fullText.WriteString(content)
			if onChunk != nil {
				onChunk(content)
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

	return CleanLLMOutput(fullText.String()), nil
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
