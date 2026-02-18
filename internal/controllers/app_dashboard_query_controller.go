package controllers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	maxQueryRows         = 1000
	supabaseHTTPTimeout  = 10 * time.Second
	maxConcurrentQueries = 5
)

var queryHTTPClient = &http.Client{Timeout: supabaseHTTPTimeout}

// parseQueryFromRaw builds a DashboardQuery from a generic map, handling LLM type quirks.
func parseQueryFromRaw(raw map[string]any) models.DashboardQuery {
	var q models.DashboardQuery
	if v, ok := raw["sql"].(string); ok {
		q.SQL = v
	}
	if v, ok := raw["table"].(string); ok {
		q.Table = v
	}
	if v, ok := raw["select"].(string); ok {
		q.Select = v
	}
	if v, ok := raw["transform"].(string); ok {
		q.Transform = v
	}
	if v, ok := raw["order"].(string); ok {
		q.Order = v
	}
	if v, ok := raw["group_by"].(string); ok {
		q.GroupBy = v
	}
	if v, ok := raw["agg_field"].(string); ok {
		q.AggField = v
	}
	if v, ok := raw["time_field"].(string); ok {
		q.TimeField = v
	}
	if v, ok := raw["time_range"].(string); ok {
		q.TimeRange = v
	}
	if v, ok := raw["limit"].(float64); ok {
		q.Limit = int(v)
	}
	// Parse filters array
	if filters, ok := raw["filters"].([]any); ok {
		for _, f := range filters {
			if fm, ok := f.(map[string]any); ok {
				col, _ := fm["column"].(string)
				op, _ := fm["operator"].(string)
				val := fmt.Sprintf("%v", fm["value"])
				if col != "" {
					q.Filters = append(q.Filters, models.QueryFilter{Column: col, Operator: op, Value: val})
				}
			}
		}
	}
	return q
}

// QueryPanel executes the query for a single panel and returns the result.
func (c *Controller) QueryPanel(dashboard *models.AppDashboard, panel *models.AppDashboardPanel, integration *models.AppIntegration) responses.PanelDataResponse {
	// Always parse via generic map to handle LLM type quirks (e.g. number vs string)
	var raw map[string]any
	if err := json.Unmarshal(panel.Query, &raw); err != nil || raw == nil {
		slog.Warn("failed to parse panel query", "panel_id", panel.ID, "raw", string(panel.Query), "error", err)
		return responses.PanelDataResponse{
			PanelID: panel.ID,
			Title:   panel.Title,
			Type:    panel.PanelType,
			Error:   fmt.Sprintf("invalid query configuration: %s", string(panel.Query)),
		}
	}
	query := parseQueryFromRaw(raw)

	var data any
	var err error

	switch dashboard.Provider {
	case "supabase":
		data, err = c.querySupabase(integration, &query)
	case "neon":
		data, err = c.queryNeon(integration, &query)
	case "platform":
		data, err = c.queryPlatform(&query)
	default:
		err = fmt.Errorf("unsupported provider: %s", dashboard.Provider)
	}

	if err != nil {
		return responses.PanelDataResponse{
			PanelID: panel.ID,
			Title:   panel.Title,
			Type:    panel.PanelType,
			Error:   err.Error(),
		}
	}

	return responses.PanelDataResponse{
		PanelID: panel.ID,
		Title:   panel.Title,
		Type:    panel.PanelType,
		Data:    data,
	}
}

// QueryAllPanels queries all panels concurrently with bounded concurrency.
func (c *Controller) QueryAllPanels(dashboard *models.AppDashboard, panels []models.AppDashboardPanel, integration *models.AppIntegration) []responses.PanelDataResponse {
	results := make([]responses.PanelDataResponse, len(panels))
	sem := make(chan struct{}, maxConcurrentQueries)
	var wg sync.WaitGroup

	for i, panel := range panels {
		wg.Add(1)
		go func(idx int, p models.AppDashboardPanel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.QueryPanel(dashboard, &p, integration)
		}(i, panel)
	}

	wg.Wait()
	return results
}

// querySupabase executes a query via the Supabase PostgREST API.
func (c *Controller) querySupabase(integration *models.AppIntegration, query *models.DashboardQuery) (any, error) {
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("invalid integration credentials")
	}

	url, _ := creds["url"].(string)
	anonKey, _ := creds["anon_key"].(string)
	if url == "" || anonKey == "" {
		return nil, fmt.Errorf("missing supabase url or anon_key")
	}

	if query.Table == "" {
		return nil, fmt.Errorf("table name is required for supabase queries")
	}

	// Build request URL
	endpoint := strings.TrimRight(url, "/") + "/rest/v1/" + query.Table

	// Build query params
	params := []string{}
	selectClause := "*"
	if query.Select != "" {
		selectClause = query.Select
	}
	params = append(params, "select="+selectClause)

	// Filters
	for _, f := range query.Filters {
		params = append(params, fmt.Sprintf("%s=%s.%s", f.Column, f.Operator, f.Value))
	}

	// Order
	if query.Order != "" {
		params = append(params, "order="+query.Order)
	}

	// Limit
	limit := query.Limit
	if limit <= 0 || limit > maxQueryRows {
		limit = maxQueryRows
	}
	params = append(params, fmt.Sprintf("limit=%d", limit))

	fullURL := endpoint + "?" + strings.Join(params, "&")

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("apikey", anonKey)
	req.Header.Set("Authorization", "Bearer "+anonKey)

	// For count transforms, request exact count
	if query.Transform == "count" {
		req.Header.Set("Prefer", "count=exact")
		req.Header.Set("Range-Unit", "items")
		req.Header.Set("Range", "0-0")
	}

	resp, err := queryHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("supabase request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("supabase error (status %d): %s", resp.StatusCode, string(body))
	}

	// Handle count transform
	if query.Transform == "count" {
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			// Format: "0-0/123" or "*/123"
			parts := strings.Split(contentRange, "/")
			if len(parts) == 2 {
				return map[string]any{"value": parts[1]}, nil
			}
		}
		// Fallback: count results
		body, _ := io.ReadAll(resp.Body)
		var rows []any
		json.Unmarshal(body, &rows)
		return map[string]any{"value": len(rows)}, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

// queryNeon executes a read-only SQL query against a Neon Postgres database.
func (c *Controller) queryNeon(integration *models.AppIntegration, query *models.DashboardQuery) (any, error) {
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err != nil {
		return nil, fmt.Errorf("invalid integration credentials")
	}

	connStr, _ := creds["connection_string"].(string)
	if connStr == "" {
		return nil, fmt.Errorf("missing neon connection string")
	}

	if query.SQL == "" {
		return nil, fmt.Errorf("sql query is required for neon provider")
	}

	if err := validateReadOnlySQL(query.SQL); err != nil {
		return nil, err
	}

	// Open a connection
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(connStr)))
	defer db.Close()

	// Set read-only and timeout
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("failed to set read-only: %w", err)
	}
	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	rows, err := tx.Query(query.SQL)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any
	rowCount := 0
	for rows.Next() {
		if rowCount >= maxQueryRows {
			break
		}
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
			// Convert []byte to string for readability
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
		rowCount++
	}

	if query.Transform == "count" {
		return map[string]any{"value": len(results)}, nil
	}

	return results, nil
}

// buildSQLFromTableQuery converts a Supabase-style table/select/filters query to SQL.
func buildSQLFromTableQuery(query *models.DashboardQuery) string {
	selectClause := "*"
	if query.Select != "" {
		selectClause = query.Select
	}

	sql := fmt.Sprintf("SELECT %s FROM %s", selectClause, query.Table)

	if len(query.Filters) > 0 {
		var conditions []string
		for _, f := range query.Filters {
			op := "="
			switch f.Operator {
			case "eq":
				op = "="
			case "neq":
				op = "!="
			case "gt":
				op = ">"
			case "gte":
				op = ">="
			case "lt":
				op = "<"
			case "lte":
				op = "<="
			case "like":
				op = "LIKE"
			case "ilike":
				op = "ILIKE"
			case "is":
				op = "IS"
			}
			conditions = append(conditions, fmt.Sprintf("%s %s '%s'", f.Column, op, f.Value))
		}
		sql += " WHERE " + strings.Join(conditions, " AND ")
	}

	if query.Order != "" {
		sql += " ORDER BY " + query.Order
	}

	limit := query.Limit
	if limit <= 0 || limit > maxQueryRows {
		limit = maxQueryRows
	}
	sql += fmt.Sprintf(" LIMIT %d", limit)

	return sql
}

// queryPlatform executes a read-only SQL query against the platform's own Postgres database.
func (c *Controller) queryPlatform(query *models.DashboardQuery) (any, error) {
	// Auto-build SQL from table-based query if no SQL provided
	if query.SQL == "" && query.Table != "" {
		query.SQL = buildSQLFromTableQuery(query)
	}
	if query.SQL == "" {
		return nil, fmt.Errorf("sql query is required for platform provider")
	}

	// For count transform, wrap the query
	if query.Transform == "count" && query.Table != "" {
		query.SQL = fmt.Sprintf("SELECT COUNT(*) AS value FROM %s", query.Table)
		if len(query.Filters) > 0 {
			// Re-build with just filters
			base := buildSQLFromTableQuery(&models.DashboardQuery{Table: query.Table, Select: "*", Filters: query.Filters})
			query.SQL = fmt.Sprintf("SELECT COUNT(*) AS value FROM (%s) sub", base)
		}
	}

	if err := validateReadOnlySQL(query.SQL); err != nil {
		return nil, err
	}

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("failed to set read-only: %w", err)
	}
	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	rows, err := tx.Query(query.SQL)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	var results []map[string]any
	rowCount := 0
	for rows.Next() {
		if rowCount >= maxQueryRows {
			break
		}
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
		rowCount++
	}

	if query.Transform == "count" {
		return map[string]any{"value": len(results)}, nil
	}

	return results, nil
}

// validateReadOnlySQL ensures a SQL query is safe (read-only).
func validateReadOnlySQL(sqlQuery string) error {
	trimmed := strings.TrimSpace(strings.ToUpper(sqlQuery))

	// Must start with SELECT or WITH
	if !strings.HasPrefix(trimmed, "SELECT") && !strings.HasPrefix(trimmed, "WITH") {
		return fmt.Errorf("only SELECT queries are allowed")
	}

	// Block dangerous keywords
	blocked := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "GRANT", "REVOKE", "EXECUTE", "EXEC"}
	// Remove string literals before checking (rough heuristic)
	cleaned := removeStringLiterals(trimmed)
	for _, kw := range blocked {
		// Check for keyword as a word boundary
		if containsKeyword(cleaned, kw) {
			return fmt.Errorf("forbidden SQL keyword: %s", kw)
		}
	}

	return nil
}

// removeStringLiterals strips single-quoted string literals from SQL for keyword checking.
func removeStringLiterals(sql string) string {
	var result strings.Builder
	inString := false
	for i := 0; i < len(sql); i++ {
		if sql[i] == '\'' {
			inString = !inString
			continue
		}
		if !inString {
			result.WriteByte(sql[i])
		}
	}
	return result.String()
}

// containsKeyword checks if a SQL string contains a keyword as a standalone word.
func containsKeyword(sql, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(sql[idx:], keyword)
		if pos == -1 {
			return false
		}
		absPos := idx + pos
		before := absPos == 0 || !isAlphanumeric(sql[absPos-1])
		after := absPos+len(keyword) >= len(sql) || !isAlphanumeric(sql[absPos+len(keyword)])
		if before && after {
			return true
		}
		idx = absPos + len(keyword)
	}
}

func isAlphanumeric(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

// quoteIdentifier wraps an identifier in double quotes, escaping internal quotes.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// schemaTable/schemaColumn represent the parsed cached schema for validation.
type schemaTable struct {
	Name    string         `json:"name"`
	Columns []schemaColumn `json:"columns"`
}

type schemaColumn struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Nullable     bool   `json:"nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
}

// validateCellUpdate verifies that table, column, and PK columns exist in the schema.
func validateCellUpdate(schema json.RawMessage, table, column string, primaryKey map[string]any) error {
	var tables []schemaTable
	if err := json.Unmarshal(schema, &tables); err != nil {
		return fmt.Errorf("failed to parse schema")
	}

	var targetTable *schemaTable
	for i, t := range tables {
		if t.Name == table {
			targetTable = &tables[i]
			break
		}
	}
	if targetTable == nil {
		return fmt.Errorf("table %q not found in schema", table)
	}

	// Verify column exists
	columnFound := false
	for _, col := range targetTable.Columns {
		if col.Name == column {
			columnFound = true
			break
		}
	}
	if !columnFound {
		return fmt.Errorf("column %q not found in table %q", column, table)
	}

	// Verify all PK columns from schema are present in primaryKey map
	var pkCols []string
	for _, col := range targetTable.Columns {
		if col.IsPrimaryKey {
			pkCols = append(pkCols, col.Name)
		}
	}
	if len(pkCols) == 0 {
		return fmt.Errorf("no primary key found for table %q", table)
	}
	for _, pk := range pkCols {
		if _, ok := primaryKey[pk]; !ok {
			return fmt.Errorf("missing primary key column %q", pk)
		}
	}

	return nil
}

// UpdateCell updates a single cell value in the database.
func (c *Controller) UpdateCell(dashboard *models.AppDashboard, integration *models.AppIntegration, table, column string, value any, primaryKey map[string]any) error {
	if err := validateCellUpdate(dashboard.Schema, table, column, primaryKey); err != nil {
		return err
	}

	switch dashboard.Provider {
	case "supabase":
		return c.updateCellSupabase(integration, table, column, value, primaryKey)
	case "neon":
		return c.updateCellNeon(integration, table, column, value, primaryKey)
	case "platform":
		return c.updateCellPlatform(table, column, value, primaryKey)
	default:
		return fmt.Errorf("unsupported provider: %s", dashboard.Provider)
	}
}

// updateCellSupabase updates a cell via Supabase PostgREST PATCH.
func (c *Controller) updateCellSupabase(integration *models.AppIntegration, table, column string, value any, primaryKey map[string]any) error {
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err != nil {
		return fmt.Errorf("invalid integration credentials")
	}

	url, _ := creds["url"].(string)
	anonKey, _ := creds["anon_key"].(string)
	if url == "" || anonKey == "" {
		return fmt.Errorf("missing supabase url or anon_key")
	}

	// Build PostgREST PATCH URL with PK filter
	endpoint := strings.TrimRight(url, "/") + "/rest/v1/" + table
	params := []string{}
	for k, v := range primaryKey {
		params = append(params, fmt.Sprintf("%s=eq.%v", k, v))
	}
	fullURL := endpoint + "?" + strings.Join(params, "&")

	body, _ := json.Marshal(map[string]any{column: value})
	req, err := http.NewRequest("PATCH", fullURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("apikey", anonKey)
	req.Header.Set("Authorization", "Bearer "+anonKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=minimal")

	resp, err := queryHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("supabase request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// updateCellNeon updates a cell via a parameterized UPDATE on a Neon Postgres database.
func (c *Controller) updateCellNeon(integration *models.AppIntegration, table, column string, value any, primaryKey map[string]any) error {
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err != nil {
		return fmt.Errorf("invalid integration credentials")
	}

	connStr, _ := creds["connection_string"].(string)
	if connStr == "" {
		return fmt.Errorf("missing neon connection string")
	}

	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(connStr)))
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	// Build parameterized UPDATE
	sqlStr, args := buildUpdateSQL(table, column, value, primaryKey)

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist")
	}

	return tx.Commit()
}

// updateCellPlatform updates a cell on the platform's own Postgres database.
func (c *Controller) updateCellPlatform(table, column string, value any, primaryKey map[string]any) error {
	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	sqlStr, args := buildUpdateSQL(table, column, value, primaryKey)

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist")
	}

	return tx.Commit()
}

// buildUpdateSQL constructs a parameterized UPDATE statement.
func buildUpdateSQL(table, column string, value any, primaryKey map[string]any) (string, []any) {
	args := []any{value}
	paramIdx := 2

	whereParts := []string{}
	for k, v := range primaryKey {
		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", quoteIdentifier(k), paramIdx))
		args = append(args, v)
		paramIdx++
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s",
		quoteIdentifier(table),
		quoteIdentifier(column),
		strings.Join(whereParts, " AND "),
	)

	return sqlStr, args
}
