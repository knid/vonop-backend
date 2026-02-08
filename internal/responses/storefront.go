package responses

import (
	"github.com/google/uuid"
)

type StorefrontProductResponse struct {
	ID          uuid.UUID           `json:"id"`
	Name        string              `json:"name"`
	Slug        string              `json:"slug"`
	Description string              `json:"description"`
	Images      []string            `json:"images"`
	Category    string              `json:"category,omitempty"`
	Tags        []string            `json:"tags"`
	Variants    []StorefrontVariant `json:"variants"`
	MinPrice    int                 `json:"min_price_cents"`
	MaxPrice    int                 `json:"max_price_cents"`
}

type StorefrontVariant struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	PriceInCents   int            `json:"price_in_cents"`
	CompareAtPrice int            `json:"compare_at_price,omitempty"`
	Inventory      int            `json:"inventory"`
	InStock        bool           `json:"in_stock"`
	Options        map[string]any `json:"options"`
}

type StorefrontCategoryResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Image       string    `json:"image"`
	Description string    `json:"description"`
}

type StorefrontCartResponse struct {
	ID            uuid.UUID            `json:"id"`
	SessionID     string               `json:"session_id"`
	Items         []StorefrontCartItem `json:"items"`
	SubtotalCents int                  `json:"subtotal_cents"`
	DiscountCode  string               `json:"discount_code,omitempty"`
	DiscountCents int                  `json:"discount_cents"`
	TotalCents    int                  `json:"total_cents"`
}

type StorefrontCartItem struct {
	ID           uuid.UUID `json:"id"`
	ProductID    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	ProductImage string    `json:"product_image"`
	VariantID    uuid.UUID `json:"variant_id,omitempty"`
	VariantName  string    `json:"variant_name"`
	Quantity     int       `json:"quantity"`
	PriceCents   int       `json:"price_cents"`
	TotalCents   int       `json:"total_cents"`
}

type StorefrontCheckoutResponse struct {
	OrderID      uuid.UUID `json:"order_id"`
	OrderNumber  string    `json:"order_number"`
	TotalCents   int       `json:"total_cents"`
	Currency     string    `json:"currency"`
	PaymentURL   string    `json:"payment_url,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
}

type CheckoutConfirmResponse struct {
	SuccessResponse
	OrderID     uuid.UUID `json:"order_id"`
	OrderNumber string    `json:"order_number"`
	Status      string    `json:"status"`
}
