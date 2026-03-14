package requests

type GoogleMobileLoginRequest struct {
	IDToken string `json:"id_token" validate:"required"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Photo   string `json:"photo"`
}
