package responses

import (
	"time"

	"github.com/google/uuid"
)

type StoreResponse struct {
	ID             uuid.UUID      `json:"id"`
	AppProjectID   uuid.UUID      `json:"app_project_id"`
	StoreName      string         `json:"store_name"`
	Currency       string         `json:"currency"`
	Locale         string         `json:"locale"`
	TaxRate        float64        `json:"tax_rate"`
	ShippingConfig map[string]any `json:"shipping_config"`
	PaymentConfig  map[string]any `json:"payment_config"`
	NotifyEmail    string         `json:"notify_email"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type ProductResponse struct {
	ID          uuid.UUID            `json:"id"`
	StoreID     uuid.UUID            `json:"store_id"`
	Name        string               `json:"name"`
	Slug        string               `json:"slug"`
	Description string               `json:"description"`
	Images      []string             `json:"images"`
	CategoryID  uuid.UUID            `json:"category_id,omitempty"`
	Tags        []string             `json:"tags"`
	Status      string               `json:"status"`
	Variants    []ProductVariantResp `json:"variants,omitempty"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
	SEO         map[string]any       `json:"seo,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type ProductVariantResp struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	SKU            string         `json:"sku"`
	PriceInCents   int            `json:"price_in_cents"`
	CompareAtPrice int            `json:"compare_at_price"`
	CostInCents    int            `json:"cost_in_cents"`
	Inventory      int            `json:"inventory"`
	TrackInventory bool           `json:"track_inventory"`
	Weight         float64        `json:"weight"`
	Options        map[string]any `json:"options"`
	IsDefault      bool           `json:"is_default"`
}

type CategoryResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	ParentID    uuid.UUID `json:"parent_id,omitempty"`
	Image       string    `json:"image"`
	Description string    `json:"description"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

type DiscountResponse struct {
	ID            uuid.UUID      `json:"id"`
	Code          string         `json:"code"`
	Type          string         `json:"type"`
	Value         int            `json:"value"`
	MinOrderCents int            `json:"min_order_cents"`
	MaxUses       int            `json:"max_uses"`
	UsedCount     int            `json:"used_count"`
	AppliesTo     map[string]any `json:"applies_to"`
	StartsAt      time.Time      `json:"starts_at,omitempty"`
	EndsAt        time.Time      `json:"ends_at,omitempty"`
	IsActive      bool           `json:"is_active"`
	CreatedAt     time.Time      `json:"created_at"`
}

type OrderResponse struct {
	ID              uuid.UUID       `json:"id"`
	OrderNumber     string          `json:"order_number"`
	CustomerEmail   string          `json:"customer_email"`
	CustomerName    string          `json:"customer_name"`
	ShippingAddress map[string]any  `json:"shipping_address"`
	SubtotalCents   int             `json:"subtotal_cents"`
	DiscountCents   int             `json:"discount_cents"`
	TaxCents        int             `json:"tax_cents"`
	ShippingCents   int             `json:"shipping_cents"`
	TotalCents      int             `json:"total_cents"`
	Currency        string          `json:"currency"`
	Status          string          `json:"status"`
	PaymentStatus   string          `json:"payment_status"`
	PaymentProvider string          `json:"payment_provider"`
	Items           []OrderItemResp `json:"items,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type OrderItemResp struct {
	ID           uuid.UUID `json:"id"`
	ProductName  string    `json:"product_name"`
	VariantName  string    `json:"variant_name"`
	Quantity     int       `json:"quantity"`
	PriceInCents int       `json:"price_in_cents"`
	TotalCents   int       `json:"total_cents"`
}

type StoreAnalyticsResponse struct {
	TotalOrders       int `json:"total_orders"`
	TotalRevenue      int `json:"total_revenue_cents"`
	TotalProducts     int `json:"total_products"`
	PendingOrders     int `json:"pending_orders"`
	AverageOrderValue int `json:"average_order_value_cents"`
}
