package requests

import "github.com/google/uuid"

type CreateAPIKeyRequest struct {
	Name           string    `json:"name" validate:"required,min=1,max=100" errormgs:"name is required"`
	Scopes         string    `json:"scopes" validate:"required" errormgs:"at least one scope is required"` // comma-separated
	OrganizationID uuid.UUID `json:"organization_id" validate:"required" errormgs:"organization_id is required"`
}

type DeleteAPIKeyRequest struct {
	ID string `json:"id" validate:"required" errormgs:"API key ID is required"`
}
