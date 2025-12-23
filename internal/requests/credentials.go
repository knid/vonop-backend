package requests

type AppStoreCredentialsCreateRequest struct {
	Name      string `json:"name" validate:"required" errormgs:"is required"`
	KeyID     string `json:"key_id" validate:"required" errormgs:"is required"`
	TeamID    string `json:"team_id" validate:"required" errormgs:"is required"`
	IssuerID  string `json:"issuer_id" validate:"required" errormgs:"is required"`
	P8Key     string `json:"p8_key" validate:"required" errormgs:"is required"`
	APNSP8Key string `json:"apns_p8_key" validate:"required" errormgs:"is required"`
	APNSKeyID string `json:"apns_key_id" validate:"required" errormgs:"is required"`
}

type GooglePlayCredentialsCreateRequest struct {
	Name              string `json:"name" validate:"required" errormgs:"is required"`
	ServiceAccountKey string `json:"service_account_key" validate:"required" errormgs:"is required"`
}
