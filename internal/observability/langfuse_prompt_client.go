package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var promptVariablePattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

type PromptClient struct {
	BaseURL    string
	PublicKey  string
	SecretKey  string
	Label      string
	HTTPClient *http.Client
}

type TextPrompt struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Prompt  string `json:"prompt"`
}

type CompiledPrompt struct {
	Name    string
	Version int
	Text    string
}

func PromptClientFromEnvAndSecrets(secrets SecretProvider) PromptClient {
	cfg := LangfuseConfigFromEnvAndSecrets(secrets)
	publicKey := envOrSecret("LANGFUSE_PUBLIC_KEY", secrets)
	secretKey := envOrSecret("LANGFUSE_SECRET_KEY", secrets)
	baseURL := strings.TrimRight(envOrSecret("LANGFUSE_BASE_URL", secrets), "/")
	if baseURL == "" {
		baseURL = baseURLFromOTELTracesEndpoint(envOrSecret("LANGFUSE_OTEL_TRACES_ENDPOINT", secrets))
	}
	if baseURL == "" {
		baseURL = defaultLangfuseBaseURL
	}
	label := strings.TrimSpace(envOrSecret("LANGFUSE_PROMPT_LABEL", secrets))
	if label == "" {
		label = "production"
	}
	if cfg.Enabled {
		return PromptClient{BaseURL: baseURL, PublicKey: publicKey, SecretKey: secretKey, Label: label}
	}
	return PromptClient{}
}

func (c PromptClient) GetTextPrompt(ctx context.Context, name, label string) (TextPrompt, error) {
	if c.BaseURL == "" || c.PublicKey == "" || c.SecretKey == "" || name == "" {
		return TextPrompt{}, fmt.Errorf("langfuse prompt client is not configured")
	}
	if label == "" {
		label = c.Label
	}
	if label == "" {
		label = "production"
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/public/v2/prompts/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?label="+url.QueryEscape(label), nil)
	if err != nil {
		return TextPrompt{}, err
	}
	req.SetBasicAuth(c.PublicKey, c.SecretKey)
	resp, err := client.Do(req)
	if err != nil {
		return TextPrompt{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TextPrompt{}, fmt.Errorf("langfuse prompt status %d", resp.StatusCode)
	}
	var prompt TextPrompt
	if err := json.NewDecoder(resp.Body).Decode(&prompt); err != nil {
		return TextPrompt{}, err
	}
	if prompt.Prompt == "" {
		return TextPrompt{}, fmt.Errorf("langfuse prompt %q has empty content", name)
	}
	if prompt.Name == "" {
		prompt.Name = name
	}
	return prompt, nil
}

func (c PromptClient) CompileTextPrompt(ctx context.Context, name string, vars map[string]string) (CompiledPrompt, error) {
	prompt, err := c.GetTextPrompt(ctx, name, c.Label)
	if err != nil {
		return CompiledPrompt{}, err
	}
	text, err := CompileTextPrompt(prompt.Prompt, vars)
	if err != nil {
		return CompiledPrompt{}, err
	}
	return CompiledPrompt{Name: prompt.Name, Version: prompt.Version, Text: text}, nil
}

func CompileTextPrompt(prompt string, vars map[string]string) (string, error) {
	var missing []string
	out := promptVariablePattern.ReplaceAllStringFunc(prompt, func(match string) string {
		sub := promptVariablePattern.FindStringSubmatch(match)
		if len(sub) != 2 {
			return match
		}
		value, ok := vars[sub[1]]
		if !ok {
			missing = append(missing, sub[1])
			return match
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("missing Langfuse prompt variables: %s", strings.Join(missing, ", "))
	}
	return out, nil
}
