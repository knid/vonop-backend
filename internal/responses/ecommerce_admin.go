package responses

import "time"

// StoreInfo mirrors the `config` row in the tenant's Neon DB (one row per app).
// `app_project_id` and `status` are synthesized by the server; they are not
// columns in the tenant DB.
type StoreInfo struct {
	ID             string         `json:"id"`
	AppProjectID   string         `json:"app_project_id"`
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

type StoreProduct struct {
	ID             string         `json:"id"`
	StoreID        string         `json:"store_id"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description"`
	Images         []string       `json:"images"`
	CategoryID     *string        `json:"category_id,omitempty"`
	Tags           []string       `json:"tags"`
	Status         string         `json:"status"`
	PriceInCents   int64          `json:"price_in_cents"`
	CompareAtPrice int64          `json:"compare_at_price"`
	SKU            string         `json:"sku"`
	Inventory      int64          `json:"inventory"`
	TrackInventory bool           `json:"track_inventory"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	SEO            map[string]any `json:"seo,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type StoreCategory struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	ParentID    *string   `json:"parent_id,omitempty"`
	Image       string    `json:"image"`
	Description string    `json:"description"`
	SortOrder   int64     `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
}

type StoreDiscount struct {
	ID            string         `json:"id"`
	Code          string         `json:"code"`
	Type          string         `json:"type"`
	Value         int64          `json:"value"`
	MinOrderCents int64          `json:"min_order_cents"`
	MaxUses       int64          `json:"max_uses"`
	UsedCount     int64          `json:"used_count"`
	AppliesTo     map[string]any `json:"applies_to"`
	StartsAt      *time.Time     `json:"starts_at,omitempty"`
	EndsAt        *time.Time     `json:"ends_at,omitempty"`
	IsActive      bool           `json:"is_active"`
	CreatedAt     time.Time      `json:"created_at"`
}

type StoreOrderItem struct {
	ID           string `json:"id"`
	ProductName  string `json:"product_name"`
	Quantity     int64  `json:"quantity"`
	PriceInCents int64  `json:"price_in_cents"`
	TotalCents   int64  `json:"total_cents"`
}

type StoreOrder struct {
	ID              string           `json:"id"`
	OrderNumber     string           `json:"order_number"`
	CustomerEmail   string           `json:"customer_email"`
	CustomerName    string           `json:"customer_name"`
	ShippingAddress map[string]any   `json:"shipping_address"`
	SubtotalCents   int64            `json:"subtotal_cents"`
	DiscountCents   int64            `json:"discount_cents"`
	TaxCents        int64            `json:"tax_cents"`
	ShippingCents   int64            `json:"shipping_cents"`
	TotalCents      int64            `json:"total_cents"`
	Currency        string           `json:"currency"`
	Status          string           `json:"status"`
	PaymentStatus   string           `json:"payment_status"`
	PaymentProvider string           `json:"payment_provider"`
	Items           []StoreOrderItem `json:"items,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type StoreAnalytics struct {
	TotalOrders            int64 `json:"total_orders"`
	TotalRevenueCents      int64 `json:"total_revenue_cents"`
	TotalProducts          int64 `json:"total_products"`
	PendingOrders          int64 `json:"pending_orders"`
	AverageOrderValueCents int64 `json:"average_order_value_cents"`
}
