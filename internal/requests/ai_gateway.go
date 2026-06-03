package requests

// Gateway chat requests are proxied as raw maps in internal/views so that
// unknown OpenAI fields (tools, tool_choice, response_format, seed,
// frequency_penalty, etc.) survive the round-trip to the upstream. See
// HttpViews.GatewayChat for the decode path.

// GatewayImageRequest is the fal.ai-native image generation request.
type GatewayImageRequest struct {
	Model        string   `json:"model" validate:"required"`
	Prompt       string   `json:"prompt" validate:"required"`
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
