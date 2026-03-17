package controllers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/knid/wemob/internal/responses"

	_ "github.com/lib/pq"
)

// scopeKind describes how a table's rows are scoped to a specific app project.
type scopeKind int

const (
	// scopeByStoreID: table has a store_id column → filter WHERE store_id = <app's store ID>
	scopeByStoreID scopeKind = iota
	// scopeByStoreViaProduct: table has product_id → JOIN products to get store_id
	scopeByStoreViaProduct
	// scopeByStoreViaCart: table has cart_id → JOIN carts to get store_id
	scopeByStoreViaCart
	// scopeByStoreViaOrder: table has order_id → JOIN orders to get store_id
	scopeByStoreViaOrder
	// scopeByProjectID: table has a project_id column → filter directly
	scopeByProjectID
	// scopeByAppProjectID: table has an app_project_id column → filter directly
	scopeByAppProjectID
)

// appScopedTable defines a table the database manager is allowed to expose,
// along with how it is scoped to a particular app project.
type appScopedTable struct {
	DisplayName   string    // friendly name shown in the UI
	Scope         scopeKind // how rows are scoped to the app
	ReadOnly      bool      // if true, INSERT and DELETE are forbidden
	HasSoftDelete bool      // if true, add "deleted_at IS NULL" filter to queries
}

// appScopedTables is the allowlist of tables users can browse/edit.
var appScopedTables = map[string]appScopedTable{
	// Direct store-scoped tables
	"stores":              {DisplayName: "Stores", Scope: scopeByAppProjectID, HasSoftDelete: true},
	"products":            {DisplayName: "Products", Scope: scopeByStoreID, HasSoftDelete: true},
	"product_categories":  {DisplayName: "Categories", Scope: scopeByStoreID, HasSoftDelete: true},
	"discounts":           {DisplayName: "Discounts", Scope: scopeByStoreID, HasSoftDelete: true},
	"carts":               {DisplayName: "Carts", Scope: scopeByStoreID},
	"cart_items":          {DisplayName: "Cart Items", Scope: scopeByStoreViaCart},
	"orders":              {DisplayName: "Orders", Scope: scopeByStoreID},
	"order_items":         {DisplayName: "Order Items", Scope: scopeByStoreViaOrder},
	"web_analytics_events": {DisplayName: "Analytics Events", Scope: scopeByProjectID, ReadOnly: true},
}

// inferFieldHint returns a semantic hint for the frontend editor based on column name and type.
func inferFieldHint(columnName, columnType string) string {
	name := strings.ToLower(columnName)

	// Image fields
	imageKeywords := []string{"image", "photo", "avatar", "logo", "cover", "thumbnail", "banner", "icon", "picture", "img"}
	for _, kw := range imageKeywords {
		if strings.Contains(name, kw) {
			return "image"
		}
	}

	// Rich text fields (only when type is text-like)
	typeLower := strings.ToLower(columnType)
	isTextType := typeLower == "text" || typeLower == "character varying" || typeLower == "varchar"
	richTextKeywords := []string{"content", "description", "body", "bio", "summary", "about", "notes", "details"}
	for _, kw := range richTextKeywords {
		if strings.Contains(name, kw) && isTextType {
			return "rich_text"
		}
	}

	// Email
	if strings.Contains(name, "email") {
		return "email"
	}

	// URL
	urlKeywords := []string{"url", "website", "link", "href"}
	for _, kw := range urlKeywords {
		if strings.Contains(name, kw) {
			return "url"
		}
	}

	// Color
	if strings.Contains(name, "color") || strings.Contains(name, "colour") {
		return "color"
	}

	return ""
}

// applyFieldHints sets FieldHint on each column in the slice.
func applyFieldHints(columns []responses.DatabaseSchemaColumn) {
	for i := range columns {
		columns[i].FieldHint = inferFieldHint(columns[i].Name, columns[i].Type)
	}
}

// IsSystemTable returns true if the given table is NOT in the app-scoped allowlist.
func IsSystemTable(name string) bool {
	_, allowed := appScopedTables[name]
	return !allowed
}

// isNeonBacked returns true if the project has a Neon integration.
func (c *Controller) isNeonBacked(projectID string) bool {
	neon, err := c.getNeonIntegration(projectID)
	if err != nil {
		slog.Error("isNeonBacked: failed to check neon integration", "project_id", projectID, "error", err)
		return false
	}
	if neon != nil {
		slog.Info("isNeonBacked: found Neon integration", "project_id", projectID, "integration_id", neon.ID)
		return true
	}
	slog.Info("isNeonBacked: no Neon integration found", "project_id", projectID)
	return false
}

// isEcommerceProject returns true if the project is of type "ecommerce".
func (c *Controller) isEcommerceProject(projectID string) bool {
	project, err := c.DB.GetAppProjectByID(projectID)
	if err != nil {
		return false
	}
	return project.ProjectType == "ecommerce"
}

// neonConnString returns the DATABASE_URL from the Neon integration, or "".
func (c *Controller) neonConnString(projectID string) string {
	integ, err := c.getNeonIntegration(projectID)
	if err != nil {
		slog.Error("neonConnString: failed to get neon integration", "project_id", projectID, "error", err)
		return ""
	}
	if integ == nil {
		return ""
	}
	return integ.EnvVars["DATABASE_URL"]
}

// openNeonDB opens a direct connection to the app's Neon database.
func (c *Controller) openNeonDB(projectID string) (*sql.DB, error) {
	connStr := c.neonConnString(projectID)
	if connStr == "" {
		return nil, fmt.Errorf("no Neon connection string found")
	}
	db, err := sql.Open("postgres", neonConnStrWithEndpoint(connStr))
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Second)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	return db, nil
}

// softDeleteFilter returns an additional SQL fragment to exclude soft-deleted rows,
// or "" if the table does not use soft deletes.
func softDeleteFilter(tableName string) string {
	info, ok := appScopedTables[tableName]
	if !ok || !info.HasSoftDelete {
		return ""
	}
	return fmt.Sprintf(" AND %s.%s IS NULL", quoteIdentifier(tableName), quoteIdentifier("deleted_at"))
}

// neonQueryTimeout is the default timeout for Neon database queries.
const neonQueryTimeout = 15 * time.Second

// validateNeonTable checks that the given table exists in the Neon database.
func validateNeonTable(ctx context.Context, db *sql.DB, tableName string) error {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)
	`, tableName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check table: %w", err)
	}
	if !exists {
		return fmt.Errorf("table %q not found", tableName)
	}
	return nil
}

// validatePrimaryKey checks that the primary key map is non-empty.
func validatePrimaryKey(pk map[string]any) error {
	if len(pk) == 0 {
		return fmt.Errorf("primary key must contain at least one column")
	}
	return nil
}

// scopeWhereClause returns a WHERE clause and args that limit rows to the given project.
// storeID may be empty if the app has no store — in that case store-scoped tables return no rows.
// Automatically appends a soft-delete filter for tables that use soft deletes.
func scopeWhereClause(tableName, projectID, storeID string) (string, []any) {
	info, ok := appScopedTables[tableName]
	if !ok {
		return "WHERE 1=0", nil // should never happen
	}

	sdFilter := softDeleteFilter(tableName)

	switch info.Scope {
	case scopeByAppProjectID:
		return fmt.Sprintf("WHERE %s.%s = $1%s", quoteIdentifier(tableName), quoteIdentifier("app_project_id"), sdFilter), []any{projectID}

	case scopeByProjectID:
		return fmt.Sprintf("WHERE %s.%s = $1%s", quoteIdentifier(tableName), quoteIdentifier("project_id"), sdFilter), []any{projectID}

	case scopeByStoreID:
		if storeID == "" {
			return "WHERE 1=0", nil
		}
		return fmt.Sprintf("WHERE %s.%s = $1%s", quoteIdentifier(tableName), quoteIdentifier("store_id"), sdFilter), []any{storeID}

	case scopeByStoreViaProduct:
		if storeID == "" {
			return "WHERE 1=0", nil
		}
		return fmt.Sprintf(
			`WHERE %s."product_id" IN (SELECT "id" FROM "products" WHERE "store_id" = $1)%s`,
			quoteIdentifier(tableName), sdFilter,
		), []any{storeID}

	case scopeByStoreViaCart:
		if storeID == "" {
			return "WHERE 1=0", nil
		}
		return fmt.Sprintf(
			`WHERE %s."cart_id" IN (SELECT "id" FROM "carts" WHERE "store_id" = $1)%s`,
			quoteIdentifier(tableName), sdFilter,
		), []any{storeID}

	case scopeByStoreViaOrder:
		if storeID == "" {
			return "WHERE 1=0", nil
		}
		return fmt.Sprintf(
			`WHERE %s."order_id" IN (SELECT "id" FROM "orders" WHERE "store_id" = $1)%s`,
			quoteIdentifier(tableName), sdFilter,
		), []any{storeID}
	}

	return "WHERE 1=0", nil
}

// resolveStoreID returns the store ID for a project, or "" if no store exists.
func (c *Controller) resolveStoreID(projectID string) string {
	store, err := c.GetStore(projectID)
	if err != nil || store == nil {
		return ""
	}
	return store.ID.String()
}

// GetDatabaseSchema returns the allowlisted tables with their column definitions.
func (c *Controller) GetDatabaseSchema(projectID string) (*responses.DatabaseSchemaResponse, error) {
	// For Neon-backed apps, query the Neon database directly
	if c.isNeonBacked(projectID) {
		return c.getNeonDatabaseSchema(projectID)
	}

	// Only show platform ecommerce tables for ecommerce projects that use the storefront.
	// Non-ecommerce apps without Neon have no database to show.
	if !c.isEcommerceProject(projectID) {
		return &responses.DatabaseSchemaResponse{Tables: []responses.DatabaseSchemaTable{}}, nil
	}

	raw, err := c.ExtractSchemaFromPlatformDB()
	if err != nil {
		return nil, fmt.Errorf("failed to extract schema: %w", err)
	}

	var allTables []responses.DatabaseSchemaTable
	if err := json.Unmarshal(raw, &allTables); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Only return tables in the allowlist
	var tables []responses.DatabaseSchemaTable
	for _, t := range allTables {
		if _, ok := appScopedTables[t.Name]; ok {
			applyFieldHints(t.Columns)
			tables = append(tables, t)
		}
	}

	return &responses.DatabaseSchemaResponse{Tables: tables}, nil
}

// getNeonDatabaseSchema queries a Neon database's information_schema to get all tables.
// Includes retry logic to handle Neon cold starts (compute auto-suspends after inactivity).
func (c *Controller) getNeonDatabaseSchema(projectID string) (*responses.DatabaseSchemaResponse, error) {
	db, err := c.openNeonDB(projectID)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Query all user tables — retry up to 3 times for Neon cold starts
	var tableRows *sql.Rows
	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		tableRows, err = db.Query(`
			SELECT table_name FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
			ORDER BY table_name
		`)
		if err == nil {
			break
		}
		if attempt < maxRetries {
			slog.Warn("getNeonDatabaseSchema: query failed, retrying for cold start",
				"attempt", attempt, "error", err, "project_id", projectID)
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer tableRows.Close()

	var tableNames []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tableNames = append(tableNames, name)
	}
	if err := tableRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating table rows: %w", err)
	}

	var tables []responses.DatabaseSchemaTable
	for _, tableName := range tableNames {
		colRows, err := db.Query(`
			SELECT DISTINCT ON (c.column_name)
				c.column_name,
				c.data_type,
				c.is_nullable = 'YES' AS nullable,
				COALESCE(tc.constraint_type = 'PRIMARY KEY', false) AS is_pk
			FROM information_schema.columns c
			LEFT JOIN information_schema.key_column_usage kcu
				ON c.table_name = kcu.table_name AND c.column_name = kcu.column_name AND c.table_schema = kcu.table_schema
			LEFT JOIN information_schema.table_constraints tc
				ON kcu.constraint_name = tc.constraint_name AND tc.constraint_type = 'PRIMARY KEY'
			WHERE c.table_schema = 'public' AND c.table_name = $1
			ORDER BY c.column_name, is_pk DESC, c.ordinal_position
		`, tableName)
		if err != nil {
			slog.Warn("Failed to query columns for Neon table", "table", tableName, "error", err)
			continue
		}

		var columns []responses.DatabaseSchemaColumn
		for colRows.Next() {
			var col responses.DatabaseSchemaColumn
			if err := colRows.Scan(&col.Name, &col.Type, &col.Nullable, &col.IsPrimaryKey); err != nil {
				colRows.Close()
				return nil, fmt.Errorf("failed to scan column: %w", err)
			}
			columns = append(columns, col)
		}
		colRows.Close()

		applyFieldHints(columns)
		tables = append(tables, responses.DatabaseSchemaTable{
			Name:    tableName,
			Columns: columns,
		})
	}

	return &responses.DatabaseSchemaResponse{Tables: tables}, nil
}

// validateAppTable checks that the table is in the allowlist and exists in the DB.
func (c *Controller) validateAppTable(tableName string) (*schemaTable, error) {
	if _, ok := appScopedTables[tableName]; !ok {
		return nil, fmt.Errorf("access denied: table %q is not available", tableName)
	}

	schema, err := c.ExtractSchemaFromPlatformDB()
	if err != nil {
		return nil, fmt.Errorf("failed to extract schema: %w", err)
	}

	var tables []schemaTable
	if err := json.Unmarshal(schema, &tables); err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	for i, t := range tables {
		if t.Name == tableName {
			return &tables[i], nil
		}
	}
	return nil, fmt.Errorf("table %q not found", tableName)
}

// GetTableRows queries paginated rows scoped to the given app project.
func (c *Controller) GetTableRows(projectID, tableName string, page, pageSize int, sortCol, sortDir string) (*responses.TableRowsResponse, error) {
	// For Neon-backed apps, query the Neon database directly (no scoping needed)
	if c.isNeonBacked(projectID) {
		return c.getNeonTableRows(projectID, tableName, page, pageSize, sortCol, sortDir)
	}

	if !c.isEcommerceProject(projectID) {
		return nil, fmt.Errorf("no database available for this project")
	}

	targetTable, err := c.validateAppTable(tableName)
	if err != nil {
		return nil, err
	}

	// Validate sort column
	if sortCol != "" {
		valid := false
		for _, col := range targetTable.Columns {
			if col.Name == sortCol {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("sort column %q not found in table %q", sortCol, tableName)
		}
	}

	storeID := c.resolveStoreID(projectID)
	whereClause, whereArgs := scopeWhereClause(tableName, projectID, storeID)

	db := c.DB.DB.DB
	quotedTable := quoteIdentifier(tableName)

	// Count total rows (scoped)
	var totalCount int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s %s", quotedTable, whereClause)
	row := db.QueryRow(countSQL, whereArgs...)
	if err := row.Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	// Build SELECT query (scoped)
	selectSQL := fmt.Sprintf("SELECT %s.* FROM %s %s", quotedTable, quotedTable, whereClause)

	// Sorting
	if sortCol != "" {
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		selectSQL += fmt.Sprintf(" ORDER BY %s %s", quoteIdentifier(sortCol), dir)
	}

	// Pagination
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize
	nextParam := len(whereArgs) + 1
	selectSQL += fmt.Sprintf(" LIMIT $%d OFFSET $%d", nextParam, nextParam+1)
	queryArgs := append(whereArgs, pageSize, offset)

	rows, err := db.Query(selectSQL, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	results, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, err
	}

	// Build column info
	var columns []responses.DatabaseSchemaColumn
	for _, col := range targetTable.Columns {
		columns = append(columns, responses.DatabaseSchemaColumn{
			Name:         col.Name,
			Type:         col.Type,
			Nullable:     col.Nullable,
			IsPrimaryKey: col.IsPrimaryKey,
		})
	}
	applyFieldHints(columns)

	return &responses.TableRowsResponse{
		Rows:       results,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		Columns:    columns,
	}, nil
}

// InsertTableRow inserts a new row, auto-injecting the scoping FK.
func (c *Controller) InsertTableRow(projectID, tableName string, data map[string]any) (map[string]any, error) {
	// For Neon-backed apps, insert directly into the Neon database
	if c.isNeonBacked(projectID) {
		return c.insertNeonTableRow(projectID, tableName, data)
	}

	if !c.isEcommerceProject(projectID) {
		return nil, fmt.Errorf("no database available for this project")
	}

	info, ok := appScopedTables[tableName]
	if ok && info.ReadOnly {
		return nil, fmt.Errorf("table %q is read-only", tableName)
	}

	_, err := c.validateAppTable(tableName)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided")
	}

	// Auto-inject the scoping foreign key so users can't insert into another app
	switch info.Scope {
	case scopeByAppProjectID:
		data["app_project_id"] = projectID
	case scopeByProjectID:
		data["project_id"] = projectID
	case scopeByStoreID:
		storeID := c.resolveStoreID(projectID)
		if storeID == "" {
			return nil, fmt.Errorf("no store found for this app")
		}
		data["store_id"] = storeID
	case scopeByStoreViaProduct:
		// product_id must be provided and must belong to the app's store
		storeID := c.resolveStoreID(projectID)
		if storeID == "" {
			return nil, fmt.Errorf("no store found for this app")
		}
		if err := c.verifyFKBelongsToStore(data, "product_id", "products", storeID); err != nil {
			return nil, err
		}
	case scopeByStoreViaCart:
		storeID := c.resolveStoreID(projectID)
		if storeID == "" {
			return nil, fmt.Errorf("no store found for this app")
		}
		if err := c.verifyFKBelongsToStore(data, "cart_id", "carts", storeID); err != nil {
			return nil, err
		}
	case scopeByStoreViaOrder:
		storeID := c.resolveStoreID(projectID)
		if storeID == "" {
			return nil, fmt.Errorf("no store found for this app")
		}
		if err := c.verifyFKBelongsToStore(data, "order_id", "orders", storeID); err != nil {
			return nil, err
		}
	}

	// Build parameterized INSERT
	var cols []string
	var placeholders []string
	var args []any
	idx := 1
	for col, val := range data {
		cols = append(cols, quoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx))
		args = append(args, val)
		idx++
	}

	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		quoteIdentifier(tableName),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	rows, err := tx.Query(sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("insert failed: %w", err)
	}
	defer rows.Close()

	results, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("insert returned no rows")
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return results[0], nil
}

// DeleteTableRow deletes a row, ensuring it belongs to the given app project.
func (c *Controller) DeleteTableRow(projectID, tableName string, primaryKey map[string]any) error {
	// For Neon-backed apps, delete directly from the Neon database
	if c.isNeonBacked(projectID) {
		return c.deleteNeonTableRow(projectID, tableName, primaryKey)
	}

	if !c.isEcommerceProject(projectID) {
		return fmt.Errorf("no database available for this project")
	}

	info, ok := appScopedTables[tableName]
	if !ok {
		return fmt.Errorf("access denied: table %q is not available", tableName)
	}
	if info.ReadOnly {
		return fmt.Errorf("table %q is read-only", tableName)
	}

	schema, err := c.ExtractSchemaFromPlatformDB()
	if err != nil {
		return fmt.Errorf("failed to extract schema: %w", err)
	}

	if err := validateCellUpdate(schema, tableName, primaryKey); err != nil {
		return err
	}

	storeID := c.resolveStoreID(projectID)
	scopeWhere, scopeArgs := scopeWhereClause(tableName, projectID, storeID)

	// Build parameterized DELETE with scoping
	var pkParts []string
	var args []any
	// First N args are from scope
	args = append(args, scopeArgs...)
	idx := len(scopeArgs) + 1
	for col, val := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(col), idx))
		args = append(args, val)
		idx++
	}

	// Merge the scope WHERE and the PK WHERE
	// scopeWhere starts with "WHERE <condition>", we need to AND the PK conditions
	sqlStr := fmt.Sprintf("DELETE FROM %s %s AND %s",
		quoteIdentifier(tableName),
		scopeWhere,
		strings.Join(pkParts, " AND "),
	)

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows deleted — row may not exist or does not belong to this app")
	}

	return tx.Commit()
}

// UpdateTableCell updates a single cell, ensuring the row belongs to the given app project.
func (c *Controller) UpdateTableCell(projectID, tableName, column string, value any, primaryKey map[string]any) error {
	// For Neon-backed apps, update directly in the Neon database
	if c.isNeonBacked(projectID) {
		return c.updateNeonTableCell(projectID, tableName, column, value, primaryKey)
	}

	if !c.isEcommerceProject(projectID) {
		return fmt.Errorf("no database available for this project")
	}

	if _, ok := appScopedTables[tableName]; !ok {
		return fmt.Errorf("access denied: table %q is not available", tableName)
	}

	schema, err := c.ExtractSchemaFromPlatformDB()
	if err != nil {
		return fmt.Errorf("failed to extract schema: %w", err)
	}

	if err := validateCellUpdate(schema, tableName, primaryKey); err != nil {
		return err
	}

	storeID := c.resolveStoreID(projectID)
	scopeWhere, scopeArgs := scopeWhereClause(tableName, projectID, storeID)

	// Build: UPDATE table SET col = $N WHERE <scope $1> AND <pk conditions>
	// Scope args come first so $1 in scopeWhere matches correctly.
	var args []any
	args = append(args, scopeArgs...)
	setParamIdx := len(args) + 1
	args = append(args, value)
	paramIdx := len(args) + 1

	var pkParts []string
	for k, v := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(k), paramIdx))
		args = append(args, v)
		paramIdx++
	}

	scopeCond := strings.TrimPrefix(scopeWhere, "WHERE ")

	sqlStr := fmt.Sprintf("UPDATE %s SET %s = $%d WHERE %s AND %s",
		quoteIdentifier(tableName),
		quoteIdentifier(column),
		setParamIdx,
		scopeCond,
		strings.Join(pkParts, " AND "),
	)

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist or does not belong to this app")
	}

	return tx.Commit()
}

// verifyFKBelongsToStore checks that the FK value in data belongs to the app's store.
func (c *Controller) verifyFKBelongsToStore(data map[string]any, fkColumn, parentTable, storeID string) error {
	fkVal, ok := data[fkColumn]
	if !ok {
		return fmt.Errorf("%s is required", fkColumn)
	}

	db := c.DB.DB.DB
	var count int
	err := db.QueryRow(
		fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE "id" = $1 AND "store_id" = $2`, quoteIdentifier(parentTable)),
		fkVal, storeID,
	).Scan(&count)
	if err != nil || count == 0 {
		return fmt.Errorf("%s does not belong to this app", fkColumn)
	}
	return nil
}

// --- Neon-backed CRUD operations ---

// getNeonTableRows queries rows from a Neon database table (no scoping — entire DB is the app's own).
func (c *Controller) getNeonTableRows(projectID, tableName string, page, pageSize int, sortCol, sortDir string) (*responses.TableRowsResponse, error) {
	neonDB, err := c.openNeonDB(projectID)
	if err != nil {
		return nil, err
	}
	defer neonDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
	defer cancel()

	if err := validateNeonTable(ctx, neonDB, tableName); err != nil {
		return nil, err
	}

	quotedTable := quoteIdentifier(tableName)

	// Count total rows
	var totalCount int
	if err := neonDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quotedTable).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("count query failed: %w", err)
	}

	// Build SELECT query
	selectSQL := "SELECT * FROM " + quotedTable

	// Validate sort column exists before using it
	if sortCol != "" {
		var colExists bool
		err := neonDB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
			)
		`, tableName, sortCol).Scan(&colExists)
		if err != nil || !colExists {
			return nil, fmt.Errorf("sort column %q not found in table %q", sortCol, tableName)
		}
		dir := "ASC"
		if strings.ToUpper(sortDir) == "DESC" {
			dir = "DESC"
		}
		selectSQL += fmt.Sprintf(" ORDER BY %s %s", quoteIdentifier(sortCol), dir)
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize
	selectSQL += " LIMIT $1 OFFSET $2"

	rows, err := neonDB.QueryContext(ctx, selectSQL, pageSize, offset)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	results, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, err
	}

	// Get column info from information_schema (DISTINCT ON to avoid duplicates from multi-constraint columns)
	colRows, err := neonDB.QueryContext(ctx, `
		SELECT DISTINCT ON (c.column_name)
			c.column_name,
			c.data_type,
			c.is_nullable = 'YES' AS nullable,
			COALESCE(tc.constraint_type = 'PRIMARY KEY', false) AS is_pk
		FROM information_schema.columns c
		LEFT JOIN information_schema.key_column_usage kcu
			ON c.table_name = kcu.table_name AND c.column_name = kcu.column_name AND c.table_schema = kcu.table_schema
		LEFT JOIN information_schema.table_constraints tc
			ON kcu.constraint_name = tc.constraint_name AND tc.constraint_type = 'PRIMARY KEY'
		WHERE c.table_schema = 'public' AND c.table_name = $1
		ORDER BY c.column_name, is_pk DESC, c.ordinal_position
	`, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get column info: %w", err)
	}
	defer colRows.Close()

	var columns []responses.DatabaseSchemaColumn
	for colRows.Next() {
		var col responses.DatabaseSchemaColumn
		if err := colRows.Scan(&col.Name, &col.Type, &col.Nullable, &col.IsPrimaryKey); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}
		columns = append(columns, col)
	}
	if err := colRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating column rows: %w", err)
	}
	applyFieldHints(columns)

	return &responses.TableRowsResponse{
		Rows:       results,
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		Columns:    columns,
	}, nil
}

// insertNeonTableRow inserts a row directly into a Neon database table.
func (c *Controller) insertNeonTableRow(projectID, tableName string, data map[string]any) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no data provided")
	}

	neonDB, err := c.openNeonDB(projectID)
	if err != nil {
		return nil, err
	}
	defer neonDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
	defer cancel()

	if err := validateNeonTable(ctx, neonDB, tableName); err != nil {
		return nil, err
	}

	var cols []string
	var placeholders []string
	var args []any
	idx := 1
	for col, val := range data {
		cols = append(cols, quoteIdentifier(col))
		placeholders = append(placeholders, fmt.Sprintf("$%d", idx))
		args = append(args, val)
		idx++
	}

	sqlStr := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		quoteIdentifier(tableName),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	rows, err := neonDB.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("insert failed: %w", err)
	}
	defer rows.Close()

	results, err := scanRowsToMaps(rows)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("insert returned no rows")
	}
	return results[0], nil
}

// deleteNeonTableRow deletes a row from a Neon database table.
func (c *Controller) deleteNeonTableRow(projectID, tableName string, primaryKey map[string]any) error {
	if err := validatePrimaryKey(primaryKey); err != nil {
		return err
	}

	neonDB, err := c.openNeonDB(projectID)
	if err != nil {
		return err
	}
	defer neonDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
	defer cancel()

	var pkParts []string
	var args []any
	idx := 1
	for col, val := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(col), idx))
		args = append(args, val)
		idx++
	}

	sqlStr := fmt.Sprintf("DELETE FROM %s WHERE %s",
		quoteIdentifier(tableName),
		strings.Join(pkParts, " AND "),
	)

	result, err := neonDB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows deleted — row may not exist")
	}
	return nil
}

// updateNeonTableCell updates a single cell in a Neon database table.
func (c *Controller) updateNeonTableCell(projectID, tableName, column string, value any, primaryKey map[string]any) error {
	if err := validatePrimaryKey(primaryKey); err != nil {
		return err
	}

	neonDB, err := c.openNeonDB(projectID)
	if err != nil {
		return err
	}
	defer neonDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
	defer cancel()

	args := []any{value}
	paramIdx := 2

	var pkParts []string
	for k, v := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(k), paramIdx))
		args = append(args, v)
		paramIdx++
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s",
		quoteIdentifier(tableName),
		quoteIdentifier(column),
		strings.Join(pkParts, " AND "),
	)

	result, err := neonDB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist")
	}
	return nil
}

// UpdateTableRow updates all columns in a row at once (bulk update).
func (c *Controller) UpdateTableRow(projectID, tableName string, data map[string]any, primaryKey map[string]any) error {
	if c.isNeonBacked(projectID) {
		return c.updateNeonTableRow(projectID, tableName, data, primaryKey)
	}

	if !c.isEcommerceProject(projectID) {
		return fmt.Errorf("no database available for this project")
	}

	if _, ok := appScopedTables[tableName]; !ok {
		return fmt.Errorf("access denied: table %q is not available", tableName)
	}

	if len(data) == 0 {
		return fmt.Errorf("no data provided")
	}

	storeID := c.resolveStoreID(projectID)
	scopeWhere, scopeArgs := scopeWhereClause(tableName, projectID, storeID)

	// Auto-update updated_at for tables that have it
	if appScopedTables[tableName].HasSoftDelete {
		if _, hasUpdatedAt := data["updated_at"]; !hasUpdatedAt {
			data["updated_at"] = "now()"
		}
	}

	// Build SET clause
	var setParts []string
	var args []any
	args = append(args, scopeArgs...)
	paramIdx := len(scopeArgs) + 1

	for col, val := range data {
		// Handle raw SQL expressions like now()
		if s, ok := val.(string); ok && s == "now()" {
			setParts = append(setParts, fmt.Sprintf("%s = now()", quoteIdentifier(col)))
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = $%d", quoteIdentifier(col), paramIdx))
		args = append(args, val)
		paramIdx++
	}

	// Build PK WHERE conditions
	var pkParts []string
	for col, val := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(col), paramIdx))
		args = append(args, val)
		paramIdx++
	}

	scopeCond := strings.TrimPrefix(scopeWhere, "WHERE ")

	sqlStr := fmt.Sprintf("UPDATE %s SET %s WHERE %s AND %s",
		quoteIdentifier(tableName),
		strings.Join(setParts, ", "),
		scopeCond,
		strings.Join(pkParts, " AND "),
	)

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist or does not belong to this app")
	}

	return tx.Commit()
}

// updateNeonTableRow updates all columns in a row in a Neon database.
func (c *Controller) updateNeonTableRow(projectID, tableName string, data map[string]any, primaryKey map[string]any) error {
	if err := validatePrimaryKey(primaryKey); err != nil {
		return err
	}

	neonDB, err := c.openNeonDB(projectID)
	if err != nil {
		return err
	}
	defer neonDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), neonQueryTimeout)
	defer cancel()

	var setParts []string
	var args []any
	paramIdx := 1

	for col, val := range data {
		setParts = append(setParts, fmt.Sprintf("%s = $%d", quoteIdentifier(col), paramIdx))
		args = append(args, val)
		paramIdx++
	}

	var pkParts []string
	for k, v := range primaryKey {
		pkParts = append(pkParts, fmt.Sprintf("%s = $%d", quoteIdentifier(k), paramIdx))
		args = append(args, v)
		paramIdx++
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		quoteIdentifier(tableName),
		strings.Join(setParts, ", "),
		strings.Join(pkParts, " AND "),
	)

	result, err := neonDB.ExecContext(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist")
	}
	return nil
}

// scanRowsToMaps converts sql.Rows into a slice of maps.
// Always returns a non-nil slice (empty, not null) for JSON serialization.
func scanRowsToMaps(rows interface {
	Columns() ([]string, error)
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}
