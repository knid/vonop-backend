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
var baseURL = "https://fal.run"

type generateRequest struct {
	Prompt       string   `json:"prompt"`
	ImageSize    string   `json:"image_size,omitempty"`
	NumImages    int      `json:"num_images,omitempty"`
	ImageURL     string   `json:"image_url,omitempty"`
	ImageURLs    []string `json:"image_urls,omitempty"`
	MaskImageURL string   `json:"mask_image_url,omitempty"`
	Quality      string   `json:"quality,omitempty"`
	OutputFormat string   `json:"output_format,omitempty"`
	AspectRatio  string   `json:"aspect_ratio,omitempty"`
	Resolution   string   `json:"resolution,omitempty"`
}

type generateResponse struct {
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
}

type GenerateOptions struct {
	Model        string
	Prompt       string
	ImageSize    string
	NumImages    int
	ImageURL     string
	ImageURLs    []string
	MaskImageURL string
	Quality      string
	OutputFormat string
	AspectRatio  string
	Resolution   string
}

// GenerateImages calls fal.ai to generate images and returns CDN URLs. If
// imageSize is empty, defaults to "square_hd".
func GenerateImages(ctx context.Context, apiKey, model, prompt, imageSize string, numImages int) ([]string, error) {
	return GenerateImagesWithOptions(ctx, apiKey, GenerateOptions{
		Model:     model,
		Prompt:    prompt,
		ImageSize: imageSize,
		NumImages: numImages,
	})
}

// GenerateImagesWithOptions calls fal.ai to generate or edit images and
// returns CDN URLs. Text-to-image callers can use Model/Prompt/ImageSize;
// image-edit endpoints can also pass image_url/image_urls/mask_image_url.
func GenerateImagesWithOptions(ctx context.Context, apiKey string, opts GenerateOptions) ([]string, error) {
	if opts.ImageSize == "" && opts.ImageURL == "" && len(opts.ImageURLs) == 0 {
		opts.ImageSize = "square_hd"
	}
	if opts.NumImages <= 0 {
		opts.NumImages = 1
	}
	body, err := json.Marshal(generateRequest{
		Prompt:       opts.Prompt,
		ImageSize:    opts.ImageSize,
		NumImages:    opts.NumImages,
		ImageURL:     opts.ImageURL,
		ImageURLs:    opts.ImageURLs,
		MaskImageURL: opts.MaskImageURL,
		Quality:      opts.Quality,
		OutputFormat: opts.OutputFormat,
		AspectRatio:  opts.AspectRatio,
		Resolution:   opts.Resolution,
	})
	if err != nil {
		return nil, fmt.Errorf("fal: marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s", strings.TrimRight(baseURL, "/"), opts.Model)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("fal: create request: %w", err)
	}
	req.Header.Set("Authorization", "Key "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fal: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fal: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fal: decode response: %w", err)
	}
	if len(result.Images) == 0 {
		return nil, fmt.Errorf("fal: no images returned")
	}

	urls := make([]string, 0, len(result.Images))
	for _, image := range result.Images {
		if image.URL != "" {
			urls = append(urls, image.URL)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("fal: no image URLs returned")
	}

	return urls, nil
}

// GenerateImage calls fal.ai to generate a single image and returns the CDN URL.
func GenerateImage(ctx context.Context, apiKey, model, prompt, imageSize string) (string, error) {
	urls, err := GenerateImages(ctx, apiKey, model, prompt, imageSize, 1)
	if err != nil {
		return "", err
	}
	return urls[0], nil
}

type rembgRequest struct {
	ImageURL string `json:"image_url"`
}

type rembgResponse struct {
	Image struct {
		URL string `json:"url"`
	} `json:"image"`
}
