package responses

import (
	"time"

	"github.com/google/uuid"
)

type StorefrontProductResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description"`
	Images         []string  `json:"images"`
	Category       string    `json:"category,omitempty"`
	Tags           []string  `json:"tags"`
	PriceInCents   int       `json:"price_in_cents"`
	CompareAtPrice int       `json:"compare_at_price,omitempty"`
	MinPrice       int       `json:"min_price_cents"`
	MaxPrice       int       `json:"max_price_cents"`
	InStock        bool      `json:"in_stock"`
	AverageRating  float64   `json:"average_rating"`
	ReviewCount    int       `json:"review_count"`
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

type StoreConfigResponse struct {
	Name             string   `json:"name"`
	Currency         string   `json:"currency"`
	Locale           string   `json:"locale"`
	TaxRate          float64  `json:"tax_rate"`
	PaymentProviders []string `json:"payment_providers"`
	ShippingEnabled  bool     `json:"shipping_enabled"`
}

type OrderTrackingResponse struct {
	OrderID       uuid.UUID                  `json:"order_id"`
	OrderNumber   string                     `json:"order_number"`
	Status        string                     `json:"status"`
	PaymentStatus string                     `json:"payment_status"`
	Items         []OrderTrackingItemResponse `json:"items"`
	SubtotalCents int                        `json:"subtotal_cents"`
	DiscountCents int                        `json:"discount_cents"`
	TaxCents      int                        `json:"tax_cents"`
	TotalCents    int                        `json:"total_cents"`
	Currency      string                     `json:"currency"`
	CreatedAt     time.Time                  `json:"created_at"`
}

type OrderTrackingItemResponse struct {
	ProductName  string `json:"product_name"`
	Quantity     int    `json:"quantity"`
	PriceInCents int    `json:"price_in_cents"`
	TotalCents   int    `json:"total_cents"`
}
