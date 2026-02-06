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
)

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

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// create name for this func
func GetJSONFromLLM(model, system, message, apiKey string) (string, error) {
	if apiKey == "" {
		fmt.Println("Please set OPENROUTER_API_KEY in your environment.")
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("Error making request to LLM:", "error", err, "FROM", "LLM_UTILS", "resp", resp, "body", body)
		return "", fmt.Errorf("error generating items. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Println("LLM Response:", string(respBody))
	slog.Info("LLM Response received", "FROM", "LLM_UTILS", "resp", resp, "body", body)

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

	resp, err := http.DefaultClient.Do(req)
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

	resp, err := http.DefaultClient.Do(req)
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

	return fullText.String(), nil
}
