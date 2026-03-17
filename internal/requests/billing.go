package requests

type PurchaseCreditsRequest struct {
	PackSize int `json:"pack_size" validate:"required,oneof=100 200" errormgs:"pack_size must be 100 or 200"`
}

type CheckoutRequest struct {
	PriceID       string `json:"price_id" validate:"required" errormgs:"price_id is required"`
	BillingPeriod string `json:"billing_period" validate:"omitempty,oneof=monthly annual" errormgs:"billing_period must be monthly or annual"`
}
