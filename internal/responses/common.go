package responses

type SuccessResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}
