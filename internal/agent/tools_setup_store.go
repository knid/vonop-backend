package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/knid/wemob/internal/database"
	_ "github.com/lib/pq"
)

// SetupStoreTool performs the initial bulk store seed in a single call:
// config UPDATE + product_categories INSERTs + products INSERTs in one
// transaction. Replaces three separate execute_sql roundtrips that the
// model used to make for ecommerce provisioning. After setup, further
// changes flow through execute_sql as usual.
type SetupStoreTool struct {
	Deps *RunDependencies
}

type storeConfigInput struct {
	StoreName      string         `json:"store_name"`
	Currency       string         `json:"currency"`
	Locale         string         `json:"locale"`
	TaxRate        *float64       `json:"tax_rate"`
	ShippingConfig map[string]any `json:"shipping_config"`
	PaymentConfig  map[string]any `json:"payment_config"`
	NotifyEmail    string         `json:"notify_email"`
}

type storeCategoryInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Image       string `json:"image"`
	SortOrder   int    `json:"sort_order"`
	ParentSlug  string `json:"parent_slug"`
}

type storeProductInput struct {
	Name            string   `json:"name"`
	Slug            string   `json:"slug"`
	Description     string   `json:"description"`
	CategorySlug    string   `json:"category_slug"`
	Images          []string `json:"images"`
	Tags            []string `json:"tags"`
	PriceInCents    int64    `json:"price_in_cents"`
	CompareAtPrice  *int64   `json:"compare_at_price"`
	SKU             string   `json:"sku"`
	Inventory       int64    `json:"inventory"`
	TrackInventory  bool     `json:"track_inventory"`
	Status          string   `json:"status"`
}

type setupStoreInput struct {
	Config     *storeConfigInput    `json:"config"`
	Categories []storeCategoryInput `json:"categories"`
	Products   []storeProductInput  `json:"products"`
}

func (SetupStoreTool) Name() string { return "setup_store" }

func (SetupStoreTool) Description() string {
	return "One-shot ecommerce seed: UPDATEs the config row and bulk-INSERTs categories and products in a single transaction. Use this on the first turn to populate the store instead of calling execute_sql 3 times. Categories reference each other by `parent_slug` (resolved server-side). Products reference categories by `category_slug`. For later edits (adding one product, tweaking a price, adding a column), use execute_sql — this tool is seed-only. Prices are bigint cents. tax_rate is a percentage (8 means 8%)."
}

func (SetupStoreTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config": map[string]any{
				"type":        "object",
				"description": "Store-wide settings. All fields optional; omitted fields keep their seeded defaults.",
				"properties": map[string]any{
					"store_name":      map[string]any{"type": "string"},
					"currency":        map[string]any{"type": "string", "description": "ISO 4217, e.g. USD, EUR, TRY"},
					"locale":          map[string]any{"type": "string", "description": "BCP 47, e.g. en, tr, de"},
					"tax_rate":        map[string]any{"type": "number", "description": "Percentage (8 means 8%)"},
					"shipping_config": map[string]any{"type": "object", "description": "Free-form jsonb"},
					"payment_config":  map[string]any{"type": "object", "description": "e.g. {\"stripe\":{\"enabled\":true}}"},
					"notify_email":    map[string]any{"type": "string"},
				},
			},
			"categories": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":        map[string]any{"type": "string"},
						"slug":        map[string]any{"type": "string", "description": "lowercase, unique"},
						"description": map[string]any{"type": "string"},
						"image":       map[string]any{"type": "string", "description": "URL from generate_image"},
						"sort_order":  map[string]any{"type": "integer"},
						"parent_slug": map[string]any{"type": "string", "description": "optional; must match another category's slug"},
					},
					"required": []string{"name", "slug"},
				},
			},
			"products": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":             map[string]any{"type": "string"},
						"slug":             map[string]any{"type": "string", "description": "lowercase, unique"},
						"description":      map[string]any{"type": "string"},
						"category_slug":    map[string]any{"type": "string", "description": "optional; must match a category slug"},
						"images":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "URLs from generate_image"},
						"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"price_in_cents":   map[string]any{"type": "integer"},
						"compare_at_price": map[string]any{"type": "integer", "description": "optional; renders as strike-through when > price_in_cents"},
						"sku":              map[string]any{"type": "string"},
						"inventory":        map[string]any{"type": "integer"},
						"track_inventory":  map[string]any{"type": "boolean"},
						"status":           map[string]any{"type": "string", "description": "active | draft | archived"},
					},
					"required": []string{"name", "slug", "price_in_cents"},
				},
			},
		},
	}
}

func (s SetupStoreTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args setupStoreInput
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if args.Config == nil && len(args.Categories) == 0 && len(args.Products) == 0 {
		return "nothing to do: provide at least one of config, categories, products", true, nil
	}

	// Catch slug references the LLM can't resolve itself before we open a tx.
	catSlugs := make(map[string]bool, len(args.Categories))
	for i, c := range args.Categories {
		if c.Slug == "" || c.Name == "" {
			return fmt.Sprintf("categories[%d]: name and slug are required", i), true, nil
		}
		if catSlugs[c.Slug] {
			return fmt.Sprintf("categories[%d]: duplicate slug %q", i, c.Slug), true, nil
		}
		catSlugs[c.Slug] = true
	}
	for i, c := range args.Categories {
		if c.ParentSlug != "" && !catSlugs[c.ParentSlug] {
			return fmt.Sprintf("categories[%d]: parent_slug %q does not match any category in this call", i, c.ParentSlug), true, nil
		}
	}
	prodSlugs := make(map[string]bool, len(args.Products))
	for i, p := range args.Products {
		if p.Slug == "" || p.Name == "" {
			return fmt.Sprintf("products[%d]: name and slug are required", i), true, nil
		}
		if prodSlugs[p.Slug] {
			return fmt.Sprintf("products[%d]: duplicate slug %q", i, p.Slug), true, nil
		}
		prodSlugs[p.Slug] = true
		if p.CategorySlug != "" && !catSlugs[p.CategorySlug] {
			return fmt.Sprintf("products[%d]: category_slug %q does not match any category in this call", i, p.CategorySlug), true, nil
		}
	}

	if s.Deps == nil || s.Deps.NeonConnString == nil {
		return "database access not available", true, nil
	}
	connStr, err := s.Deps.NeonConnString(rc.Project.ID.String())
	if err != nil || connStr == "" {
		return "this project does not have a database connected", true, nil
	}
	db, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
	if err != nil {
		return fmt.Sprintf("could not connect: %v", err), true, nil
	}
	defer db.Close()

	qCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := db.BeginTx(qCtx, nil)
	if err != nil {
		return fmt.Sprintf("begin tx: %v", err), true, nil
	}
	defer tx.Rollback()

	var configUpdated bool
	if args.Config != nil {
		if err := updateStoreConfig(qCtx, tx, args.Config); err != nil {
			return fmt.Sprintf("config update: %v", err), true, nil
		}
		configUpdated = true
	}

	// Two-pass category insert: parents first (rows where parent_slug == ""),
	// then children, so parent_id is resolvable from catIDs at insert time.
	catIDs := make(map[string]string, len(args.Categories))
	for _, c := range args.Categories {
		if c.ParentSlug != "" {
			continue
		}
		id, err := insertCategory(qCtx, tx, c, catIDs)
		if err != nil {
			return fmt.Sprintf("category %q: %v", c.Slug, err), true, nil
		}
		catIDs[c.Slug] = id
	}
	for _, c := range args.Categories {
		if c.ParentSlug == "" {
			continue
		}
		id, err := insertCategory(qCtx, tx, c, catIDs)
		if err != nil {
			return fmt.Sprintf("category %q: %v", c.Slug, err), true, nil
		}
		catIDs[c.Slug] = id
	}

	for _, p := range args.Products {
		if err := insertProduct(qCtx, tx, p, catIDs); err != nil {
			return fmt.Sprintf("product %q: %v", p.Slug, err), true, nil
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Sprintf("commit: %v", err), true, nil
	}

	parts := []string{}
	if configUpdated {
		parts = append(parts, "config updated")
	}
	if len(args.Categories) > 0 {
		parts = append(parts, fmt.Sprintf("%d categories", len(args.Categories)))
	}
	if len(args.Products) > 0 {
		parts = append(parts, fmt.Sprintf("%d products", len(args.Products)))
	}
	return "ok: " + strings.Join(parts, ", "), false, nil
}

func updateStoreConfig(ctx context.Context, tx *sql.Tx, c *storeConfigInput) error {
	// Only UPDATE columns the caller actually supplied. Empty strings are
	// treated as "don't change" — the seed defaults ('Your Store', USD, en)
	// are sensible and we don't want an omitted field to blank the row.
	sets := []string{}
	vals := []any{}
	idx := 1
	add := func(col string, v any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
		vals = append(vals, v)
		idx++
	}
	if c.StoreName != "" {
		add("store_name", c.StoreName)
	}
	if c.Currency != "" {
		add("currency", c.Currency)
	}
	if c.Locale != "" {
		add("locale", c.Locale)
	}
	if c.TaxRate != nil {
		add("tax_rate", *c.TaxRate)
	}
	if c.ShippingConfig != nil {
		b, err := json.Marshal(c.ShippingConfig)
		if err != nil {
			return fmt.Errorf("marshal shipping_config: %w", err)
		}
		add("shipping_config", string(b))
	}
	if c.PaymentConfig != nil {
		b, err := json.Marshal(c.PaymentConfig)
		if err != nil {
			return fmt.Errorf("marshal payment_config: %w", err)
		}
		add("payment_config", string(b))
	}
	if c.NotifyEmail != "" {
		add("notify_email", c.NotifyEmail)
	}
	if len(sets) == 0 {
		return nil
	}
	add("updated_at", time.Now())
	q := "UPDATE config SET " + strings.Join(sets, ", ")
	_, err := tx.ExecContext(ctx, q, vals...)
	return err
}

func insertCategory(ctx context.Context, tx *sql.Tx, c storeCategoryInput, catIDs map[string]string) (string, error) {
	var parentID any
	if c.ParentSlug != "" {
		parentID = catIDs[c.ParentSlug]
	} else {
		parentID = nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO product_categories (name, slug, parent_id, image, description, sort_order)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
		RETURNING id::text
	`, c.Name, c.Slug, parentID, c.Image, c.Description, c.SortOrder).Scan(&id)
	return id, err
}

func insertProduct(ctx context.Context, tx *sql.Tx, p storeProductInput, catIDs map[string]string) error {
	var categoryID any
	if p.CategorySlug != "" {
		categoryID = catIDs[p.CategorySlug]
	} else {
		categoryID = nil
	}
	images, err := json.Marshal(nonNilSlice(p.Images))
	if err != nil {
		return fmt.Errorf("marshal images: %w", err)
	}
	tags, err := json.Marshal(nonNilSlice(p.Tags))
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	status := p.Status
	if status == "" {
		status = "active"
	}
	var compareAt any
	if p.CompareAtPrice != nil {
		compareAt = *p.CompareAtPrice
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO products (
			name, slug, description, category_id, images, tags, status,
			price_in_cents, compare_at_price, sku, inventory, track_inventory
		) VALUES (
			$1, $2, NULLIF($3, ''), $4, $5::jsonb, $6::jsonb, $7,
			$8, $9, NULLIF($10, ''), $11, $12
		)
	`, p.Name, p.Slug, p.Description, categoryID, string(images), string(tags), status,
		p.PriceInCents, compareAt, p.SKU, p.Inventory, p.TrackInventory)
	return err
}

func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
