package responses

import "github.com/knid/wemob/internal/constants"

// GatewayModelsResponse is the response for GET /v2/gateway/models.
type GatewayModelsResponse struct {
	Chat  []constants.GatewayModelInfo `json:"chat"`
	Image []constants.GatewayModelInfo `json:"image"`
}

// GatewayErrorResponse is the OpenAI-compatible error format.
type GatewayErrorResponse struct {
	Error GatewayErrorDetail `json:"error"`
}

type GatewayErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}
