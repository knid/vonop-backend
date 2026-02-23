package requests

type CreateWebhookRequest struct {
	URL    string `json:"url" validate:"required,url" errormgs:"valid URL is required"`
	Events string `json:"events" validate:"required" errormgs:"at least one event is required"` // comma-separated
}

type UpdateWebhookRequest struct {
	URL    string `json:"url,omitempty"`
	Events string `json:"events,omitempty"`
	Active *bool  `json:"active,omitempty"`
}
