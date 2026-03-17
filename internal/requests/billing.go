package requests

type PurchaseCreditsRequest struct {
	PackageID string `json:"package_id" validate:"required" errormgs:"package_id is required"`
}

type CheckoutRequest struct {
	PriceID       string `json:"price_id" validate:"required" errormgs:"price_id is required"`
	BillingPeriod string `json:"billing_period" validate:"omitempty,oneof=monthly annual" errormgs:"billing_period must be monthly or annual"`
}
