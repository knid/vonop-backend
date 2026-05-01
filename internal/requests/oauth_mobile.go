package requests

// Email/Name/Photo are accepted for backward-compatibility with deployed mobile
// clients but are ignored server-side: identity is derived from the verified
// id_token payload, not the request body. Trusting these would re-introduce the
// audience-confusion bug the idtoken validator was added to fix.
type GoogleMobileLoginRequest struct {
	IDToken string `json:"id_token" validate:"required"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Photo   string `json:"photo"`
}
