package requests

type AndroidStoreAccessRequest struct {
	UserID            string `json:"user_id"`
	Name              string `json:"name"`
	ServiceAccountKey string `json:"service_account_key"`
}

type IOSStoreAccessRequest struct {
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	TeamID       string `json:"team_id"`
	IssuerID     string `json:"issuer_id"`
	PrivateP8Key string `json:"p8_key"`
}
