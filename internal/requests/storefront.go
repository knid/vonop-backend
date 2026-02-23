package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type CreateCartRequest struct {
	SessionID string `json:"session_id" validate:"required" errormgs:"is required"`
}

func (r *CreateCartRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateCartRequest](*r, validate)
}

type AddCartItemRequest struct {
	ProductID string `json:"product_id" validate:"required" errormgs:"is required"`
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity" validate:"required,min=1" errormgs:"must be at least 1"`
}

func (r *AddCartItemRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AddCartItemRequest](*r, validate)
}

type UpdateCartItemRequest struct {
	Quantity int `json:"quantity" validate:"required,min=0" errormgs:"must be 0 or more"`
}

func (r *UpdateCartItemRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateCartItemRequest](*r, validate)
}

type ApplyDiscountRequest struct {
	Code string `json:"code" validate:"required" errormgs:"is required"`
}

func (r *ApplyDiscountRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ApplyDiscountRequest](*r, validate)
}

type StorefrontCheckoutRequest struct {
	SessionID       string         `json:"session_id" validate:"required" errormgs:"is required"`
	CustomerEmail   string         `json:"customer_email" validate:"required,email" errormgs:"must be a valid email"`
	CustomerName    string         `json:"customer_name" validate:"required,min=2" errormgs:"must be at least 2 characters"`
	ShippingAddress map[string]any `json:"shipping_address" validate:"required" errormgs:"is required"`
	BillingAddress  map[string]any `json:"billing_address"`
	PaymentProvider string         `json:"payment_provider" validate:"required,oneof=stripe iyzico" errormgs:"must be 'stripe' or 'iyzico'"`
}

func (r *StorefrontCheckoutRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[StorefrontCheckoutRequest](*r, validate)
}

type ConfirmCheckoutRequest struct {
	OrderID   string `json:"order_id" validate:"required" errormgs:"is required"`
	PaymentID string `json:"payment_id" validate:"required" errormgs:"is required"`
}

func (r *ConfirmCheckoutRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ConfirmCheckoutRequest](*r, validate)
}
