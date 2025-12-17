package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
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
func GetJSONFromLLM(model, system, message string) (string, error) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
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
		return "", fmt.Errorf("error generating items. try again please")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Println("LLM Response:", string(respBody))
	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("error creating items. try again please")
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from LLM")
	}

	outputJson := CleanLLMOutput(parsed.Choices[0].Message.Content)

	return outputJson, nil
}
