package requests

type CreateAPIKeyRequest struct {
	Name   string `json:"name" validate:"required,min=1,max=100" errormgs:"name is required"`
	Scopes string `json:"scopes" validate:"required" errormgs:"at least one scope is required"` // comma-separated
}

type DeleteAPIKeyRequest struct {
	ID string `json:"id" validate:"required" errormgs:"API key ID is required"`
}
