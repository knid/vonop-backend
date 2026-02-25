package models

import (
	"time"

	"github.com/google/uuid"
)

// Store represents an e-commerce store attached to an AppProject
type Store struct {
	ID             uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID      `bun:",notnull,type:uuid,unique"`
	AppProject     *AppProject    `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID         uuid.UUID      `bun:",notnull,type:uuid"`
	User           *User          `bun:"rel:belongs-to,join:user_id=id"`
	StoreName      string         `bun:",notnull"`
	Currency       string         `bun:",notnull,default:'USD'"`
	Locale         string         `bun:",notnull,default:'en'"`
	TaxRate        float64        `bun:",notnull,default:0"`
	ShippingConfig map[string]any `bun:",type:jsonb,default:'{}'"`
	PaymentConfig  map[string]any `bun:",type:jsonb,default:'{}'"`
	NotifyEmail    string         `bun:",nullzero"`
	Status         string         `bun:",notnull,default:'active'"` // active, paused, closed
	CreatedAt      time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time      `bun:",soft_delete,nullzero"`
}

// Product represents a product in a store
type Product struct {
	ID          uuid.UUID        `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID     uuid.UUID        `bun:",notnull,type:uuid"`
	Store       *Store           `bun:"rel:belongs-to,join:store_id=id"`
	Name        string           `bun:",notnull"`
	Slug        string           `bun:",notnull"`
	Description string           `bun:",type:text,nullzero"`
	Images      []string         `bun:",type:jsonb,default:'[]'"`
	CategoryID  uuid.UUID        `bun:",type:uuid,nullzero"`
	Category    *ProductCategory `bun:"rel:belongs-to,join:category_id=id"`
	Tags        []string         `bun:",type:jsonb,default:'[]'"`
	Status      string           `bun:",notnull,default:'active'"` // active, draft, archived
	Metadata    map[string]any   `bun:",type:jsonb,default:'{}'"`
	SEO         map[string]any   `bun:",type:jsonb,default:'{}'"`
	CreatedAt   time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time        `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time        `bun:",soft_delete,nullzero"`
}

// ProductVariant represents a variant of a product (size, color, etc.)
type ProductVariant struct {
	ID             uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProductID      uuid.UUID      `bun:",notnull,type:uuid"`
	Product        *Product       `bun:"rel:belongs-to,join:product_id=id"`
	Name           string         `bun:",notnull"`
	SKU            string         `bun:",nullzero"`
	PriceInCents   int            `bun:",notnull"`
	CompareAtPrice int            `bun:",nullzero,default:0"` // original price for showing discounts
	CostInCents    int            `bun:",nullzero,default:0"`
	Inventory      int            `bun:",notnull,default:0"`
	TrackInventory bool           `bun:",notnull,default:false"`
	Weight         float64        `bun:",nullzero,default:0"`
	Options        map[string]any `bun:",type:jsonb,default:'{}'"` // e.g., {"size": "L", "color": "Blue"}
	IsDefault      bool           `bun:",notnull,default:false"`
	CreatedAt      time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time      `bun:",soft_delete,nullzero"`
}

// ProductCategory represents a product category in a store
type ProductCategory struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID     uuid.UUID `bun:",notnull,type:uuid"`
	Store       *Store    `bun:"rel:belongs-to,join:store_id=id"`
	Name        string    `bun:",notnull"`
	Slug        string    `bun:",notnull"`
	ParentID    uuid.UUID `bun:",type:uuid,nullzero"` // for nested categories
	Image       string    `bun:",nullzero"`
	Description string    `bun:",type:text,nullzero"`
	SortOrder   int       `bun:",notnull,default:0"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}

// Discount represents a discount code
type Discount struct {
	ID            uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID       uuid.UUID      `bun:",notnull,type:uuid"`
	Store         *Store         `bun:"rel:belongs-to,join:store_id=id"`
	Code          string         `bun:",notnull"`
	Type          string         `bun:",notnull"` // "percentage", "fixed"
	Value         int            `bun:",notnull"` // percentage (0-100) or fixed amount in cents
	MinOrderCents int            `bun:",notnull,default:0"`
	MaxUses       int            `bun:",notnull,default:0"` // 0 = unlimited
	UsedCount     int            `bun:",notnull,default:0"`
	AppliesTo     map[string]any `bun:",type:jsonb,default:'{}'"` // product/category filters
	StartsAt      time.Time      `bun:",nullzero"`
	EndsAt        time.Time      `bun:",nullzero"`
	IsActive      bool           `bun:",notnull,default:true"`
	CreatedAt     time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt     time.Time      `bun:",soft_delete,nullzero"`
}

// Cart represents a shopping cart
type Cart struct {
	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID       uuid.UUID `bun:",notnull,type:uuid"`
	Store         *Store    `bun:"rel:belongs-to,join:store_id=id"`
	SessionID     string    `bun:",notnull"` // anonymous session identifier
	CustomerEmail string    `bun:",nullzero"`
	DiscountID    uuid.UUID `bun:",type:uuid,nullzero"`
	Discount      *Discount `bun:"rel:belongs-to,join:discount_id=id"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// CartItem represents an item in a shopping cart
type CartItem struct {
	ID              uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	CartID          uuid.UUID       `bun:",notnull,type:uuid"`
	Cart            *Cart           `bun:"rel:belongs-to,join:cart_id=id"`
	ProductID       uuid.UUID       `bun:",notnull,type:uuid"`
	Product         *Product        `bun:"rel:belongs-to,join:product_id=id"`
	VariantID       uuid.UUID       `bun:",type:uuid,nullzero"`
	Variant         *ProductVariant `bun:"rel:belongs-to,join:variant_id=id"`
	Quantity        int             `bun:",notnull,default:1"`
	PriceAtAddCents int             `bun:",notnull"` // price snapshot at time of adding
	CreatedAt       time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time       `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// Order represents a placed order
type Order struct {
	ID              uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID         uuid.UUID      `bun:",notnull,type:uuid"`
	Store           *Store         `bun:"rel:belongs-to,join:store_id=id"`
	OrderNumber     string         `bun:",notnull,unique"`
	CustomerEmail   string         `bun:",notnull"`
	CustomerName    string         `bun:",notnull"`
	ShippingAddress map[string]any `bun:",type:jsonb,default:'{}'"`
	BillingAddress  map[string]any `bun:",type:jsonb,default:'{}'"`
	SubtotalCents   int            `bun:",notnull"`
	DiscountCents   int            `bun:",notnull,default:0"`
	TaxCents        int            `bun:",notnull,default:0"`
	ShippingCents   int            `bun:",notnull,default:0"`
	TotalCents      int            `bun:",notnull"`
	Currency        string         `bun:",notnull,default:'USD'"`
	Status          string         `bun:",notnull,default:'pending'"` // pending, confirmed, processing, shipped, delivered, cancelled, refunded
	PaymentStatus   string         `bun:",notnull,default:'pending'"` // pending, paid, failed, refunded
	PaymentProvider string         `bun:",nullzero"`                  // "stripe", "iyzico"
	PaymentID       string         `bun:",nullzero"`                  // external payment ID
	DiscountID      uuid.UUID      `bun:",type:uuid,nullzero"`
	Discount        *Discount      `bun:"rel:belongs-to,join:discount_id=id"`
	Metadata        map[string]any `bun:",type:jsonb,default:'{}'"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// OrderItem represents a line item in an order
type OrderItem struct {
	ID           uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	OrderID      uuid.UUID `bun:",notnull,type:uuid"`
	Order        *Order    `bun:"rel:belongs-to,join:order_id=id"`
	ProductID    uuid.UUID `bun:",notnull,type:uuid"`
	VariantID    uuid.UUID `bun:",type:uuid,nullzero"`
	ProductName  string    `bun:",notnull"`
	VariantName  string    `bun:",nullzero"`
	Quantity     int       `bun:",notnull"`
	PriceInCents int       `bun:",notnull"`
	TotalCents   int       `bun:",notnull"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// ProductReview represents a customer review with a 1-5 star rating
type ProductReview struct {
	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID       uuid.UUID `bun:",notnull,type:uuid"`
	Store         *Store    `bun:"rel:belongs-to,join:store_id=id"`
	ProductID     uuid.UUID `bun:",notnull,type:uuid"`
	Product       *Product  `bun:"rel:belongs-to,join:product_id=id"`
	CustomerName  string    `bun:",notnull"`
	CustomerEmail string    `bun:",notnull"`
	Rating        int       `bun:",notnull"` // 1-5
	Title         string    `bun:",nullzero"`
	Body          string    `bun:",type:text,nullzero"`
	IsVerified    bool      `bun:",notnull,default:false"` // verified purchase
	Status        string    `bun:",notnull,default:'approved'"` // approved, pending, rejected
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// Wishlist represents a saved product per session
type Wishlist struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	StoreID   uuid.UUID `bun:",notnull,type:uuid"`
	Store     *Store    `bun:"rel:belongs-to,join:store_id=id"`
	ProductID uuid.UUID `bun:",notnull,type:uuid"`
	Product   *Product  `bun:"rel:belongs-to,join:product_id=id"`
	SessionID string    `bun:",notnull"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
