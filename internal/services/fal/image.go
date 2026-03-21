package fal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 120 * time.Second}

type generateRequest struct {
	Prompt    string `json:"prompt"`
	ImageSize string `json:"image_size"`
	NumImages int    `json:"num_images"`
}

type generateResponse struct {
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
}

// GenerateImage calls fal.ai to generate an image and returns the CDN URL.
func GenerateImage(ctx context.Context, apiKey, model, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		Prompt:    prompt,
		ImageSize: "square_hd",
		NumImages: 1,
	})
	if err != nil {
		return "", fmt.Errorf("fal: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("https://fal.run/%s", model)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("fal: create request: %w", err)
	}
	req.Header.Set("Authorization", "Key "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fal: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("fal: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("fal: decode response: %w", err)
	}
	if len(result.Images) == 0 {
		return "", fmt.Errorf("fal: no images returned")
	}

	return result.Images[0].URL, nil
}

type rembgRequest struct {
	ImageURL string `json:"image_url"`
}

type rembgResponse struct {
	Image struct {
		URL string `json:"url"`
	} `json:"image"`
}

// RemoveBackground calls fal.ai's rembg model to remove the background from an image.
func RemoveBackground(ctx context.Context, apiKey, imageURL string) (string, error) {
	body, err := json.Marshal(rembgRequest{ImageURL: imageURL})
	if err != nil {
		return "", fmt.Errorf("fal rembg: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://fal.run/fal-ai/imageutils/rembg", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("fal rembg: create request: %w", err)
	}
	req.Header.Set("Authorization", "Key "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fal rembg: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("fal rembg: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result rembgResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("fal rembg: decode response: %w", err)
	}

	return result.Image.URL, nil
}
