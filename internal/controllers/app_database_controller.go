package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/responses"

	_ "github.com/lib/pq"
)

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
	db, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Second)
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	return db, nil
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

// GetDatabaseSchema returns the tables in the app's Neon database, or an
// empty list for apps without a Neon integration.
func (c *Controller) GetDatabaseSchema(projectID string) (*responses.DatabaseSchemaResponse, error) {
	if c.isNeonBacked(projectID) {
		return c.getNeonDatabaseSchema(projectID)
	}
	return &responses.DatabaseSchemaResponse{Tables: []responses.DatabaseSchemaTable{}}, nil
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

// GetTableRows queries paginated rows from the app's Neon database.
func (c *Controller) GetTableRows(projectID, tableName string, page, pageSize int, sortCol, sortDir string) (*responses.TableRowsResponse, error) {
	if !c.isNeonBacked(projectID) {
		return nil, fmt.Errorf("no database available for this project")
	}
	return c.getNeonTableRows(projectID, tableName, page, pageSize, sortCol, sortDir)
}

// InsertTableRow inserts a new row into the app's Neon database.
func (c *Controller) InsertTableRow(projectID, tableName string, data map[string]any) (map[string]any, error) {
	if !c.isNeonBacked(projectID) {
		return nil, fmt.Errorf("no database available for this project")
	}
	return c.insertNeonTableRow(projectID, tableName, data)
}

// DeleteTableRow deletes a row from the app's Neon database.
func (c *Controller) DeleteTableRow(projectID, tableName string, primaryKey map[string]any) error {
	if !c.isNeonBacked(projectID) {
		return fmt.Errorf("no database available for this project")
	}
	return c.deleteNeonTableRow(projectID, tableName, primaryKey)
}

// UpdateTableCell updates a single cell in the app's Neon database.
func (c *Controller) UpdateTableCell(projectID, tableName, column string, value any, primaryKey map[string]any) error {
	if !c.isNeonBacked(projectID) {
		return fmt.Errorf("no database available for this project")
	}
	return c.updateNeonTableCell(projectID, tableName, column, value, primaryKey)
}

// UpdateTableRow updates all columns in a row at once (bulk update).
func (c *Controller) UpdateTableRow(projectID, tableName string, data map[string]any, primaryKey map[string]any) error {
	if !c.isNeonBacked(projectID) {
		return fmt.Errorf("no database available for this project")
	}
	return c.updateNeonTableRow(projectID, tableName, data, primaryKey)
}

// --- Neon-backed CRUD operations ---

// getNeonTableRows queries rows from a Neon database table.
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

// scanRowsToMaps converts sql.Rows into a slice of maps.
func scanRowsToMaps(rows interface {
	Columns() ([]string, error)
	Next() bool
	Scan(dest ...any) error
}) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any
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

	return results, nil
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
