// Studio admin endpoints for the per-tenant ecommerce database.
//
// Each generated app with the ecommerce starter has its own Neon database
// containing `config`, `products`, `product_categories`, `discounts`,
// `orders`, and `order_items` (schema in templates/nextjs/lib/schema/index.ts).
// These handlers proxy CRUD from the studio UI into that tenant DB using the
// existing openNeonDB helper.
//
// For apps without a Neon integration, all endpoints surface
// ErrNoEcommerceDB which the view layer renders as a 404.
package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/knid/wemob/internal/responses"
)

// ErrNoEcommerceDB indicates the app has no Neon-backed ecommerce database.
var ErrNoEcommerceDB = errors.New("no ecommerce database for this app")

// withNeonDB opens the tenant Neon DB and runs fn. The first call after a
// period of inactivity may hit a cold-start; fn is retried up to 3 times to
// absorb that latency, matching the pattern in app_database_controller.go.
func (c *Controller) withNeonDB(projectID string, fn func(ctx context.Context, db *sql.DB) error) error {
	if !c.isNeonBacked(projectID) {
		return ErrNoEcommerceDB
	}
	db, err := c.openNeonDB(projectID)
	if err != nil {
		return err
	}
	defer db.Close()

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
		err = fn(ctx, db)
		cancel()
		if err == nil {
			return nil
		}
		if attempt < maxRetries && isColdStartError(err) {
			slog.Warn("withNeonDB: retrying for Neon cold start", "attempt", attempt, "error", err, "project_id", projectID)
			time.Sleep(2 * time.Second)
			continue
		}
		return err
	}
	return err
}

// isColdStartError returns true for transport-level errors that typically
// resolve after Neon spins the compute back up.
func isColdStartError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "context deadline exceeded")
}

var slugStripRE = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugStripRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "item"
	}
	return s
}

// jsonbBytes marshals v to JSON for storage in a jsonb column, returning the
// canonical empty form when v is nil or empty.
func jsonbBytes(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	return json.Marshal(v)
}

// scanJSONB unmarshals a jsonb column into dst, treating empty/null bytes as
// the zero value of dst.
func scanJSONB(src []byte, dst any) error {
	if len(src) == 0 || string(src) == "null" {
		return nil
	}
	return json.Unmarshal(src, dst)
}

// ===================== STORE (config) =====================

const configSelect = `
	SELECT id, store_name, currency, locale, tax_rate,
	       shipping_config, payment_config, COALESCE(notify_email, ''),
	       created_at, updated_at
	FROM config LIMIT 1`

func scanStoreInfo(projectID string, row *sql.Row) (*responses.StoreInfo, error) {
	var info responses.StoreInfo
	var shipping, payment []byte
	if err := row.Scan(
		&info.ID, &info.StoreName, &info.Currency, &info.Locale, &info.TaxRate,
		&shipping, &payment, &info.NotifyEmail, &info.CreatedAt, &info.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := scanJSONB(shipping, &info.ShippingConfig); err != nil {
		return nil, fmt.Errorf("shipping_config: %w", err)
	}
	if err := scanJSONB(payment, &info.PaymentConfig); err != nil {
		return nil, fmt.Errorf("payment_config: %w", err)
	}
	if info.ShippingConfig == nil {
		info.ShippingConfig = map[string]any{}
	}
	if info.PaymentConfig == nil {
		info.PaymentConfig = map[string]any{}
	}
	info.AppProjectID = projectID
	info.Status = "active"
	return &info, nil
}

// GetAppStore returns the store config for an app. Returns ErrNoEcommerceDB
// if the app has no Neon-backed ecommerce database.
func (c *Controller) GetAppStore(projectID string) (*responses.StoreInfo, error) {
	var out *responses.StoreInfo
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		info, err := scanStoreInfo(projectID, db.QueryRowContext(ctx, configSelect))
		if err != nil {
			return err
		}
		out = info
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoEcommerceDB
	}
	return out, err
}

// UpdateAppStoreSettings PUT-merges store-wide settings into the config row.
func (c *Controller) UpdateAppStoreSettings(projectID string, body map[string]any) (*responses.StoreInfo, error) {
	allowed := map[string]string{
		"store_name":      "store_name",
		"currency":        "currency",
		"locale":          "locale",
		"tax_rate":        "tax_rate",
		"notify_email":    "notify_email",
		"shipping_config": "shipping_config",
		"payment_config":  "payment_config",
	}
	set, args, err := buildSet(allowed, body, []string{"shipping_config", "payment_config"})
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return c.GetAppStore(projectID)
	}

	query := fmt.Sprintf(
		"UPDATE config SET %s, updated_at = NOW() RETURNING id, store_name, currency, locale, tax_rate, shipping_config, payment_config, COALESCE(notify_email, ''), created_at, updated_at",
		strings.Join(set, ", "),
	)

	var out *responses.StoreInfo
	err = c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		info, err := scanStoreInfo(projectID, db.QueryRowContext(ctx, query, args...))
		if err != nil {
			return err
		}
		out = info
		return nil
	})
	return out, err
}

// buildSet returns a list of `col = $N` placeholders and the matching args,
// pulling values from body for any allowed key. JSONB columns named in jsonbCols
// are marshaled before being added as args.
func buildSet(allowed map[string]string, body map[string]any, jsonbCols []string) ([]string, []any, error) {
	jsonb := make(map[string]bool, len(jsonbCols))
	for _, c := range jsonbCols {
		jsonb[c] = true
	}
	set := make([]string, 0, len(body))
	args := make([]any, 0, len(body))
	i := 1
	for key, col := range allowed {
		v, ok := body[key]
		if !ok {
			continue
		}
		if jsonb[col] {
			b, err := jsonbBytes(v)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", col, err)
			}
			args = append(args, b)
		} else {
			args = append(args, v)
		}
		set = append(set, fmt.Sprintf("%s = $%d", col, i))
		i++
	}
	return set, args, nil
}

// ===================== PRODUCTS =====================

const productCols = `
	id, name, slug, COALESCE(description, ''), images, category_id, tags, status,
	price_in_cents, COALESCE(compare_at_price, 0), COALESCE(sku, ''),
	inventory, track_inventory, metadata, seo, created_at, updated_at`

func scanProduct(storeID string, scan func(...any) error) (*responses.StoreProduct, error) {
	var p responses.StoreProduct
	var images, tags, metadata, seo []byte
	var categoryID sql.NullString
	if err := scan(
		&p.ID, &p.Name, &p.Slug, &p.Description, &images, &categoryID, &tags, &p.Status,
		&p.PriceInCents, &p.CompareAtPrice, &p.SKU, &p.Inventory, &p.TrackInventory,
		&metadata, &seo, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	p.StoreID = storeID
	if categoryID.Valid {
		v := categoryID.String
		p.CategoryID = &v
	}
	if err := scanJSONB(images, &p.Images); err != nil {
		return nil, fmt.Errorf("images: %w", err)
	}
	if err := scanJSONB(tags, &p.Tags); err != nil {
		return nil, fmt.Errorf("tags: %w", err)
	}
	if err := scanJSONB(metadata, &p.Metadata); err != nil {
		return nil, fmt.Errorf("metadata: %w", err)
	}
	if err := scanJSONB(seo, &p.SEO); err != nil {
		return nil, fmt.Errorf("seo: %w", err)
	}
	if p.Images == nil {
		p.Images = []string{}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return &p, nil
}

// storeID returns the singleton config.id for the tenant DB. Used to populate
// StoreProduct.store_id, which the frontend treats as a stable handle.
func (c *Controller) storeID(ctx context.Context, db *sql.DB) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, "SELECT id FROM config LIMIT 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoEcommerceDB
	}
	return id, err
}

func (c *Controller) ListStoreProducts(projectID string) ([]responses.StoreProduct, error) {
	out := []responses.StoreProduct{}
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		sid, err := c.storeID(ctx, db)
		if err != nil {
			return err
		}
		rows, err := db.QueryContext(ctx, "SELECT "+productCols+" FROM products WHERE deleted_at IS NULL ORDER BY created_at DESC")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanProduct(sid, rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, *p)
		}
		return rows.Err()
	})
	return out, err
}

func (c *Controller) GetStoreProduct(projectID, productID string) (*responses.StoreProduct, error) {
	var out *responses.StoreProduct
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		sid, err := c.storeID(ctx, db)
		if err != nil {
			return err
		}
		row := db.QueryRowContext(ctx, "SELECT "+productCols+" FROM products WHERE id = $1 AND deleted_at IS NULL", productID)
		p, err := scanProduct(sid, row.Scan)
		if err != nil {
			return err
		}
		out = p
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("product not found")
	}
	return out, err
}

func (c *Controller) CreateStoreProduct(projectID string, body map[string]any) (*responses.StoreProduct, error) {
	name, _ := body["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("name is required")
	}
	images := jsonbOrDefault(body, "images", []any{})
	tags := jsonbOrDefault(body, "tags", []any{})
	metadata := jsonbOrDefault(body, "metadata", map[string]any{})
	seo := jsonbOrDefault(body, "seo", map[string]any{})

	var out *responses.StoreProduct
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		sid, err := c.storeID(ctx, db)
		if err != nil {
			return err
		}
		row := db.QueryRowContext(ctx, `
			INSERT INTO products (
				name, slug, description, images, category_id, tags, status,
				price_in_cents, compare_at_price, sku, inventory, track_inventory,
				metadata, seo
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			RETURNING `+productCols,
			name,
			slugify(name),
			nullableString(body, "description"),
			images, nullableString(body, "category_id"), tags,
			stringOrDefault(body, "status", "active"),
			intOrDefault(body, "price_in_cents", 0),
			intOrDefault(body, "compare_at_price", 0),
			nullableString(body, "sku"),
			intOrDefault(body, "inventory", 0),
			boolOrDefault(body, "track_inventory", false),
			metadata, seo,
		)
		p, err := scanProduct(sid, row.Scan)
		if err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

func (c *Controller) UpdateStoreProduct(projectID, productID string, body map[string]any) (*responses.StoreProduct, error) {
	allowed := map[string]string{
		"name":             "name",
		"description":      "description",
		"images":           "images",
		"category_id":      "category_id",
		"tags":             "tags",
		"status":           "status",
		"price_in_cents":   "price_in_cents",
		"compare_at_price": "compare_at_price",
		"sku":              "sku",
		"inventory":        "inventory",
		"track_inventory":  "track_inventory",
		"metadata":         "metadata",
		"seo":              "seo",
	}
	body = nullEmptyStringFields(body, "category_id")
	set, args, err := buildSet(allowed, body, []string{"images", "tags", "metadata", "seo"})
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return c.GetStoreProduct(projectID, productID)
	}
	args = append(args, productID)
	query := fmt.Sprintf(
		"UPDATE products SET %s, updated_at = NOW() WHERE id = $%d AND deleted_at IS NULL RETURNING %s",
		strings.Join(set, ", "), len(args), productCols,
	)

	var out *responses.StoreProduct
	err = c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		sid, err := c.storeID(ctx, db)
		if err != nil {
			return err
		}
		row := db.QueryRowContext(ctx, query, args...)
		p, err := scanProduct(sid, row.Scan)
		if err != nil {
			return err
		}
		out = p
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("product not found")
	}
	return out, err
}

func (c *Controller) DeleteStoreProduct(projectID, productID string) error {
	return c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		res, err := db.ExecContext(ctx, "UPDATE products SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL", productID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("product not found")
		}
		return nil
	})
}

// ===================== CATEGORIES =====================

const categoryCols = `
	id, name, slug, parent_id, COALESCE(image, ''),
	COALESCE(description, ''), sort_order, created_at`

func scanCategory(scan func(...any) error) (*responses.StoreCategory, error) {
	var cat responses.StoreCategory
	var parentID sql.NullString
	if err := scan(
		&cat.ID, &cat.Name, &cat.Slug, &parentID, &cat.Image,
		&cat.Description, &cat.SortOrder, &cat.CreatedAt,
	); err != nil {
		return nil, err
	}
	if parentID.Valid {
		v := parentID.String
		cat.ParentID = &v
	}
	return &cat, nil
}

func (c *Controller) ListStoreCategories(projectID string) ([]responses.StoreCategory, error) {
	out := []responses.StoreCategory{}
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT "+categoryCols+" FROM product_categories WHERE deleted_at IS NULL ORDER BY sort_order, name")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			cat, err := scanCategory(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, *cat)
		}
		return rows.Err()
	})
	return out, err
}

func (c *Controller) CreateStoreCategory(projectID string, body map[string]any) (*responses.StoreCategory, error) {
	name, _ := body["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("name is required")
	}
	var out *responses.StoreCategory
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `
			INSERT INTO product_categories (name, slug, parent_id, image, description, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6)
			RETURNING `+categoryCols,
			name,
			slugify(name),
			nullableString(body, "parent_id"),
			nullableString(body, "image"),
			nullableString(body, "description"),
			intOrDefault(body, "sort_order", 0),
		)
		cat, err := scanCategory(row.Scan)
		if err != nil {
			return err
		}
		out = cat
		return nil
	})
	return out, err
}

func (c *Controller) UpdateStoreCategory(projectID, categoryID string, body map[string]any) (*responses.StoreCategory, error) {
	allowed := map[string]string{
		"name":        "name",
		"parent_id":   "parent_id",
		"image":       "image",
		"description": "description",
		"sort_order":  "sort_order",
	}
	body = nullEmptyStringFields(body, "parent_id")
	set, args, err := buildSet(allowed, body, nil)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return c.getStoreCategory(projectID, categoryID)
	}
	args = append(args, categoryID)
	query := fmt.Sprintf(
		"UPDATE product_categories SET %s, updated_at = NOW() WHERE id = $%d AND deleted_at IS NULL RETURNING %s",
		strings.Join(set, ", "), len(args), categoryCols,
	)

	var out *responses.StoreCategory
	err = c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, query, args...)
		cat, err := scanCategory(row.Scan)
		if err != nil {
			return err
		}
		out = cat
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("category not found")
	}
	return out, err
}

func (c *Controller) getStoreCategory(projectID, categoryID string) (*responses.StoreCategory, error) {
	var out *responses.StoreCategory
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, "SELECT "+categoryCols+" FROM product_categories WHERE id = $1 AND deleted_at IS NULL", categoryID)
		cat, err := scanCategory(row.Scan)
		if err != nil {
			return err
		}
		out = cat
		return nil
	})
	return out, err
}

func (c *Controller) DeleteStoreCategory(projectID, categoryID string) error {
	return c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		res, err := db.ExecContext(ctx, "UPDATE product_categories SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL", categoryID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("category not found")
		}
		return nil
	})
}

// ===================== DISCOUNTS =====================

const discountCols = `
	id, code, type, value, min_order_cents, max_uses, used_count,
	applies_to, starts_at, ends_at, is_active, created_at`

func scanDiscount(scan func(...any) error) (*responses.StoreDiscount, error) {
	var d responses.StoreDiscount
	var applies []byte
	var startsAt, endsAt sql.NullTime
	if err := scan(
		&d.ID, &d.Code, &d.Type, &d.Value, &d.MinOrderCents, &d.MaxUses, &d.UsedCount,
		&applies, &startsAt, &endsAt, &d.IsActive, &d.CreatedAt,
	); err != nil {
		return nil, err
	}
	if startsAt.Valid {
		t := startsAt.Time
		d.StartsAt = &t
	}
	if endsAt.Valid {
		t := endsAt.Time
		d.EndsAt = &t
	}
	if err := scanJSONB(applies, &d.AppliesTo); err != nil {
		return nil, fmt.Errorf("applies_to: %w", err)
	}
	if d.AppliesTo == nil {
		d.AppliesTo = map[string]any{}
	}
	return &d, nil
}

func (c *Controller) ListStoreDiscounts(projectID string) ([]responses.StoreDiscount, error) {
	out := []responses.StoreDiscount{}
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT "+discountCols+" FROM discounts WHERE deleted_at IS NULL ORDER BY created_at DESC")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDiscount(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

func (c *Controller) CreateStoreDiscount(projectID string, body map[string]any) (*responses.StoreDiscount, error) {
	code, _ := body["code"].(string)
	dType, _ := body["type"].(string)
	if strings.TrimSpace(code) == "" || strings.TrimSpace(dType) == "" {
		return nil, errors.New("code and type are required")
	}
	applies := jsonbOrDefault(body, "applies_to", map[string]any{})

	var out *responses.StoreDiscount
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, `
			INSERT INTO discounts (
				code, type, value, min_order_cents, max_uses, applies_to,
				starts_at, ends_at, is_active
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING `+discountCols,
			strings.ToUpper(strings.TrimSpace(code)),
			dType,
			intOrDefault(body, "value", 0),
			intOrDefault(body, "min_order_cents", 0),
			intOrDefault(body, "max_uses", 0),
			applies,
			nullableTime(body, "starts_at"),
			nullableTime(body, "ends_at"),
			boolOrDefault(body, "is_active", true),
		)
		d, err := scanDiscount(row.Scan)
		if err != nil {
			return err
		}
		out = d
		return nil
	})
	return out, err
}

func (c *Controller) UpdateStoreDiscount(projectID, discountID string, body map[string]any) (*responses.StoreDiscount, error) {
	allowed := map[string]string{
		"code":            "code",
		"type":            "type",
		"value":           "value",
		"min_order_cents": "min_order_cents",
		"max_uses":        "max_uses",
		"applies_to":      "applies_to",
		"starts_at":       "starts_at",
		"ends_at":         "ends_at",
		"is_active":       "is_active",
	}
	if v, ok := body["code"].(string); ok {
		body["code"] = strings.ToUpper(strings.TrimSpace(v))
	}
	body = parseTimeFields(body, "starts_at", "ends_at")
	set, args, err := buildSet(allowed, body, []string{"applies_to"})
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return c.getStoreDiscount(projectID, discountID)
	}
	args = append(args, discountID)
	query := fmt.Sprintf(
		"UPDATE discounts SET %s, updated_at = NOW() WHERE id = $%d AND deleted_at IS NULL RETURNING %s",
		strings.Join(set, ", "), len(args), discountCols,
	)

	var out *responses.StoreDiscount
	err = c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, query, args...)
		d, err := scanDiscount(row.Scan)
		if err != nil {
			return err
		}
		out = d
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("discount not found")
	}
	return out, err
}

func (c *Controller) getStoreDiscount(projectID, discountID string) (*responses.StoreDiscount, error) {
	var out *responses.StoreDiscount
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, "SELECT "+discountCols+" FROM discounts WHERE id = $1 AND deleted_at IS NULL", discountID)
		d, err := scanDiscount(row.Scan)
		if err != nil {
			return err
		}
		out = d
		return nil
	})
	return out, err
}

func (c *Controller) DeleteStoreDiscount(projectID, discountID string) error {
	return c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		res, err := db.ExecContext(ctx, "UPDATE discounts SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL", discountID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("discount not found")
		}
		return nil
	})
}

// ===================== ORDERS =====================

const orderCols = `
	id, order_number, customer_email, customer_name,
	shipping_address, subtotal_cents, discount_cents, tax_cents, shipping_cents,
	total_cents, currency, status, payment_status, COALESCE(payment_provider, ''),
	created_at, updated_at`

func scanOrder(scan func(...any) error) (*responses.StoreOrder, error) {
	var o responses.StoreOrder
	var shipping []byte
	if err := scan(
		&o.ID, &o.OrderNumber, &o.CustomerEmail, &o.CustomerName,
		&shipping, &o.SubtotalCents, &o.DiscountCents, &o.TaxCents, &o.ShippingCents,
		&o.TotalCents, &o.Currency, &o.Status, &o.PaymentStatus, &o.PaymentProvider,
		&o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := scanJSONB(shipping, &o.ShippingAddress); err != nil {
		return nil, fmt.Errorf("shipping_address: %w", err)
	}
	if o.ShippingAddress == nil {
		o.ShippingAddress = map[string]any{}
	}
	return &o, nil
}

func (c *Controller) ListStoreOrders(projectID string) ([]responses.StoreOrder, error) {
	out := []responses.StoreOrder{}
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT "+orderCols+" FROM orders ORDER BY created_at DESC LIMIT 200")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanOrder(rows.Scan)
			if err != nil {
				return err
			}
			out = append(out, *o)
		}
		return rows.Err()
	})
	return out, err
}

func (c *Controller) GetStoreOrder(projectID, orderID string) (*responses.StoreOrder, error) {
	var out *responses.StoreOrder
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		row := db.QueryRowContext(ctx, "SELECT "+orderCols+" FROM orders WHERE id = $1", orderID)
		o, err := scanOrder(row.Scan)
		if err != nil {
			return err
		}

		itemRows, err := db.QueryContext(ctx, `
			SELECT id, product_name, quantity, price_in_cents, total_cents
			FROM order_items WHERE order_id = $1 ORDER BY created_at`, orderID)
		if err != nil {
			return err
		}
		defer itemRows.Close()
		o.Items = []responses.StoreOrderItem{}
		for itemRows.Next() {
			var it responses.StoreOrderItem
			if err := itemRows.Scan(&it.ID, &it.ProductName, &it.Quantity, &it.PriceInCents, &it.TotalCents); err != nil {
				return err
			}
			o.Items = append(o.Items, it)
		}
		if err := itemRows.Err(); err != nil {
			return err
		}
		out = o
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("order not found")
	}
	return out, err
}

// UpdateStoreOrderStatus changes only the order status (admin transition).
// Body shape: { "status": "paid" | "fulfilled" | "cancelled" | "refunded" | "pending" }
func (c *Controller) UpdateStoreOrderStatus(projectID, orderID string, body map[string]any) (*responses.StoreOrder, error) {
	status, ok := body["status"].(string)
	if !ok || strings.TrimSpace(status) == "" {
		return nil, errors.New("status is required")
	}
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		res, err := db.ExecContext(ctx, "UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2", status, orderID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("order not found")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c.GetStoreOrder(projectID, orderID)
}

// ===================== ANALYTICS =====================

func (c *Controller) GetStoreAnalytics(projectID string) (*responses.StoreAnalytics, error) {
	var out responses.StoreAnalytics
	err := c.withNeonDB(projectID, func(ctx context.Context, db *sql.DB) error {
		// One round-trip; LEFT JOIN-style coalesce keeps values numeric on
		// empty tables.
		return db.QueryRowContext(ctx, `
			SELECT
			  (SELECT COUNT(*) FROM orders),
			  COALESCE((SELECT SUM(total_cents) FROM orders WHERE payment_status = 'paid'), 0),
			  (SELECT COUNT(*) FROM products WHERE deleted_at IS NULL AND status = 'active'),
			  (SELECT COUNT(*) FROM orders WHERE status = 'pending'),
			  COALESCE((SELECT AVG(total_cents)::bigint FROM orders WHERE payment_status = 'paid'), 0)
		`).Scan(&out.TotalOrders, &out.TotalRevenueCents, &out.TotalProducts, &out.PendingOrders, &out.AverageOrderValueCents)
	})
	return &out, err
}

// ===================== body helpers =====================

func nullableString(body map[string]any, key string) any {
	v, ok := body[key]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullableTime(body map[string]any, key string) any {
	v, ok := body[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return t
}

func stringOrDefault(body map[string]any, key, def string) string {
	if v, ok := body[key].(string); ok && v != "" {
		return v
	}
	return def
}

func intOrDefault(body map[string]any, key string, def int64) int64 {
	switch v := body[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return def
}

func boolOrDefault(body map[string]any, key string, def bool) bool {
	if v, ok := body[key].(bool); ok {
		return v
	}
	return def
}

func jsonbOrDefault(body map[string]any, key string, def any) []byte {
	v, ok := body[key]
	if !ok {
		v = def
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// parseTimeFields rewrites RFC3339 string fields in body to time.Time so the
// generic buildSet helper passes them through as proper timestamps.
func parseTimeFields(body map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		s, ok := body[k].(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(s) == "" {
			body[k] = nil
			continue
		}
		t, err := time.Parse(time.RFC3339, s)
		if err == nil {
			body[k] = t
		}
	}
	return body
}

// nullEmptyStringFields rewrites empty-string values to nil so that UPDATE
// statements unlink foreign-key columns (e.g. category_id = NULL) instead of
// trying to cast "" to uuid.
func nullEmptyStringFields(body map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		if v, ok := body[k].(string); ok && strings.TrimSpace(v) == "" {
			body[k] = nil
		}
	}
	return body
}
