package responses

import "github.com/knid/wemob/internal/constants"

// GatewayModelsResponse is the response for GET /v2/gateway/models.
type GatewayModelsResponse struct {
	Chat  []constants.GatewayModelInfo `json:"chat"`
	Image []constants.GatewayModelInfo `json:"image"`
}

// BillingGatewayModelsResponse is the response for GET /v1/billing/gateway-models.
// Mirrors the /v2/gateway/models response but authenticates with the user's
// session (not a project gateway key) and surfaces the user's tier so the
// dashboard can label the card.
type BillingGatewayModelsResponse struct {
	Tier  string                       `json:"tier"`
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
