package requests

import "github.com/google/uuid"

// ValidateAppleConnectionRequest validates an App Store Connect API
// key before it's saved, so the user gets "✓ We saw 3 apps" feedback
// instantly instead of discovering a bad key mid-deploy.
// ConnectionID is optional: set it when validating a saved connection so
// the outcome is persisted to connection_validations; omit for "test before
// save" flows where there is no connection row yet.
type ValidateAppleConnectionRequest struct {
	KeyID        string     `json:"key_id"`
	IssuerID     string     `json:"issuer_id"`
	TeamID       string     `json:"team_id"`
	P8KeyID      string     `json:"p8_key_id"`               // S3 storage id of the uploaded .p8
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"` // set when validating a saved connection
}

// ValidateGooglePlayConnectionRequest validates a Play Console service
// account JSON via the Play Publishing API.
// ConnectionID is optional: set it when validating a saved connection so
// the outcome is persisted to connection_validations.
type ValidateGooglePlayConnectionRequest struct {
	ServiceAccountID string     `json:"service_account_id"`      // S3 storage id of the uploaded .json
	ConnectionID     *uuid.UUID `json:"connection_id,omitempty"` // set when validating a saved connection
}
