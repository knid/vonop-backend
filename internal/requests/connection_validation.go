package requests

// ValidateAppleConnectionRequest validates an App Store Connect API
// key before it's saved, so the user gets "✓ We saw 3 apps" feedback
// instantly instead of discovering a bad key mid-deploy.
type ValidateAppleConnectionRequest struct {
	KeyID    string `json:"key_id"`
	IssuerID string `json:"issuer_id"`
	TeamID   string `json:"team_id"`
	P8KeyID  string `json:"p8_key_id"` // S3 storage id of the uploaded .p8
}

// ValidateGooglePlayConnectionRequest validates a Play Console service
// account JSON via the Play Publishing API.
type ValidateGooglePlayConnectionRequest struct {
	ServiceAccountID string `json:"service_account_id"` // S3 storage id of the uploaded .json
}
