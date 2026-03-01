package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type UpdateStoreSettingsRequest struct {
	StoreName      string         `json:"store_name" validate:"omitempty,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Currency       string         `json:"currency" validate:"omitempty,len=3" errormgs:"must be a 3-letter currency code"`
	Locale         string         `json:"locale"`
	TaxRate        *float64       `json:"tax_rate"`
	ShippingConfig map[string]any `json:"shipping_config"`
	PaymentConfig  map[string]any `json:"payment_config"`
	NotifyEmail    string         `json:"notify_email" validate:"omitempty,email" errormgs:"must be a valid email"`
}

func (r *UpdateStoreSettingsRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateStoreSettingsRequest](*r, validate)
}

type StoreChatRequest struct {
	Message string `json:"message" validate:"required,min=2" errormgs:"must be at least 2 characters long"`
}

func (r *StoreChatRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[StoreChatRequest](*r, validate)
}

type CreateProductRequest struct {
	Name           string         `json:"name" validate:"required,min=1,max=200" errormgs:"must be between 1 and 200 characters"`
	Description    string         `json:"description"`
	Images         []string       `json:"images"`
	CategoryID     string         `json:"category_id"`
	Tags           []string       `json:"tags"`
	Status         string         `json:"status" validate:"omitempty,oneof=active draft archived" errormgs:"must be active, draft, or archived"`
	PriceInCents   int            `json:"price_in_cents" validate:"min=0" errormgs:"must be a non-negative number"`
	CompareAtPrice int            `json:"compare_at_price"`
	SKU            string         `json:"sku"`
	Inventory      int            `json:"inventory"`
	TrackInventory bool           `json:"track_inventory"`
	Metadata       map[string]any `json:"metadata"`
	SEO            map[string]any `json:"seo"`
}

func (r *CreateProductRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateProductRequest](*r, validate)
}

type UpdateProductRequest struct {
	Name           *string        `json:"name" validate:"omitempty,min=1,max=200" errormgs:"must be between 1 and 200 characters"`
	Description    *string        `json:"description"`
	Images         []string       `json:"images"`
	CategoryID     *string        `json:"category_id"`
	Tags           []string       `json:"tags"`
	Status         *string        `json:"status" validate:"omitempty,oneof=active draft archived" errormgs:"must be active, draft, or archived"`
	PriceInCents   *int           `json:"price_in_cents"`
	CompareAtPrice *int           `json:"compare_at_price"`
	SKU            *string        `json:"sku"`
	Inventory      *int           `json:"inventory"`
	TrackInventory *bool          `json:"track_inventory"`
	Metadata       map[string]any `json:"metadata"`
	SEO            map[string]any `json:"seo"`
}

func (r *UpdateProductRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateProductRequest](*r, validate)
}

type CreateCategoryRequest struct {
	Name        string `json:"name" validate:"required,min=1,max=100" errormgs:"must be between 1 and 100 characters"`
	ParentID    string `json:"parent_id"`
	Image       string `json:"image"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

func (r *CreateCategoryRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateCategoryRequest](*r, validate)
}

type UpdateCategoryRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=100" errormgs:"must be between 1 and 100 characters"`
	ParentID    *string `json:"parent_id"`
	Image       *string `json:"image"`
	Description *string `json:"description"`
	SortOrder   *int    `json:"sort_order"`
}

type CreateDiscountRequest struct {
	Code          string         `json:"code" validate:"required,min=2,max=50" errormgs:"must be between 2 and 50 characters"`
	Type          string         `json:"type" validate:"required,oneof=percentage fixed" errormgs:"must be 'percentage' or 'fixed'"`
	Value         int            `json:"value" validate:"required,min=1" errormgs:"must be at least 1"`
	MinOrderCents int            `json:"min_order_cents"`
	MaxUses       int            `json:"max_uses"`
	AppliesTo     map[string]any `json:"applies_to"`
	StartsAt      string         `json:"starts_at"`
	EndsAt        string         `json:"ends_at"`
}

func (r *CreateDiscountRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateDiscountRequest](*r, validate)
}

type UpdateDiscountRequest struct {
	Code          *string        `json:"code"`
	Type          *string        `json:"type" validate:"omitempty,oneof=percentage fixed"`
	Value         *int           `json:"value"`
	MinOrderCents *int           `json:"min_order_cents"`
	MaxUses       *int           `json:"max_uses"`
	AppliesTo     map[string]any `json:"applies_to"`
	StartsAt      *string        `json:"starts_at"`
	EndsAt        *string        `json:"ends_at"`
	IsActive      *bool          `json:"is_active"`
}

type UpdateOrderStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=confirmed processing shipped delivered cancelled refunded" errormgs:"must be a valid order status"`
}

func (r *UpdateOrderStatusRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateOrderStatusRequest](*r, validate)
}
