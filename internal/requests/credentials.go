package requests

import "github.com/google/uuid"

type AppleConnectionCreateRequest struct {
	Name           string    `json:"name" validate:"required" errormgs:"is required"`
	KeyID          string    `json:"key_id" validate:"required" errormgs:"is required"`
	TeamID         string    `json:"team_id" validate:"required" errormgs:"is required"`
	IssuerID       string    `json:"issuer_id" validate:"required" errormgs:"is required"`
	P8Key          string    `json:"p8_key" validate:"required" errormgs:"is required"`
	OrganizationID uuid.UUID `json:"organization_id" validate:"required" errormgs:"is required"`
}

type GooglePlayConnectionCreateRequest struct {
	Name              string    `json:"name" validate:"required" errormgs:"is required"`
	ServiceAccountKey string    `json:"service_account_key" validate:"required" errormgs:"is required"`
	OrganizationID    uuid.UUID `json:"organization_id" validate:"required" errormgs:"is required"`
}

// AppleConnectionUpdateRequest mirrors the create request but without the
// organization_id requirement — updates do not move connections between orgs.
// P8Key is optional on update — empty string means "keep the existing key".
type AppleConnectionUpdateRequest struct {
	Name     string `json:"name" validate:"required" errormgs:"is required"`
	KeyID    string `json:"key_id" validate:"required" errormgs:"is required"`
	TeamID   string `json:"team_id" validate:"required" errormgs:"is required"`
	IssuerID string `json:"issuer_id" validate:"required" errormgs:"is required"`
	P8Key    string `json:"p8_key,omitempty"`
}

// GooglePlayConnectionUpdateRequest mirrors the create request but without the
// organization_id requirement. ServiceAccountKey is optional on update —
// empty string means "keep the existing key".
type GooglePlayConnectionUpdateRequest struct {
	Name              string `json:"name" validate:"required" errormgs:"is required"`
	ServiceAccountKey string `json:"service_account_key,omitempty"`
}
