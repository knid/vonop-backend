package requests

// GatewayChatMessage is a single message in the OpenAI chat format.
type GatewayChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or array for vision
}

// GatewayChatRequest is the OpenAI-compatible chat completions request.
type GatewayChatRequest struct {
	Model       string               `json:"model" validate:"required"`
	Messages    []GatewayChatMessage `json:"messages" validate:"required,min=1"`
	Stream      bool                 `json:"stream"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	Temperature *float64             `json:"temperature,omitempty"`
	TopP        *float64             `json:"top_p,omitempty"`
	Stop        any                  `json:"stop,omitempty"`
}

// GatewayImageRequest is the fal.ai-native image generation request.
type GatewayImageRequest struct {
	Model     string `json:"model" validate:"required"`
	Prompt    string `json:"prompt" validate:"required"`
	ImageSize string `json:"image_size,omitempty"`
	NumImages int    `json:"num_images,omitempty"`
}
