package requests

import "github.com/google/uuid"

type CreateWebhookRequest struct {
	URL            string    `json:"url" validate:"required,url" errormgs:"valid URL is required"`
	Events         string    `json:"events" validate:"required" errormgs:"at least one event is required"` // comma-separated
	OrganizationID uuid.UUID `json:"organization_id" validate:"required" errormgs:"organization_id is required"`
}

type UpdateWebhookRequest struct {
	URL    string `json:"url,omitempty"`
	Events string `json:"events,omitempty"`
	Active *bool  `json:"active,omitempty"`
}
