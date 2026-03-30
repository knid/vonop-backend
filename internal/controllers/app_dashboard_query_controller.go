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
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/uptrace/bun/driver/pgdriver"
)

const (
	supabaseHTTPTimeout  = 10 * time.Second
	maxConcurrentQueries = 5
)

// platformAllowedTables is the strict allowlist of tables that the "platform"
// dashboard provider may read from. Only e-commerce tables are permitted;
// every other table in the shared database is off-limits.
var platformAllowedTables = map[string]bool{
	"stores":             true,
	"products":           true,
	"product_categories": true,
	"discounts":          true,
	"carts":              true,
	"cart_items":         true,
	"orders":             true,
	"order_items":        true,
	"product_reviews":    true,
	"wishlists":          true,
}

// platformSensitiveColumns are columns stripped from the schema even on allowed tables.
var platformSensitiveColumns = map[string]bool{
	"payment_config": true,
	"payment_id":     true,
	"metadata":       true,
	"app_project_id": true,
	"user_id":        true,
}


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

// sqlKeywords used to detect table aliases (word after table name that isn't a keyword).
var sqlKeywords = map[string]bool{
	"WHERE": true, "ORDER": true, "GROUP": true, "HAVING": true, "LIMIT": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true,
	"CROSS": true, "NATURAL": true, "FULL": true, "ON": true, "USING": true,
	"UNION": true, "INTERSECT": true, "EXCEPT": true, "OFFSET": true, "FETCH": true,
	"FOR": true, "WINDOW": true, "RETURNING": true,
}

// TableRef represents a table reference in a SQL FROM/JOIN clause.
type TableRef struct {
	Name  string // actual table name
	Alias string // alias (or table name if no alias)
}

// ParseTableReferences extracts all table references (main + JOINed) from a SQL query.
func ParseTableReferences(sqlQuery string) []TableRef {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx == -1 {
		return nil
	}

	afterFrom := strings.TrimSpace(trimmed[fromIdx+6:])
	if strings.HasPrefix(afterFrom, "(") {
		return nil
	}

	// Parse main table
	rawName := extractNextWord(afterFrom)
	name := strings.Trim(rawName, `"'`)
	if name == "" {
		return nil
	}

	rest := strings.TrimSpace(afterFrom[len(rawName):])
	if strings.HasPrefix(rest, ",") {
		return nil // comma-separated tables, skip
	}

	alias := name
	nextWord := strings.ToUpper(extractNextWord(rest))
	if nextWord != "" && !sqlKeywords[nextWord] {
		alias = extractNextWord(rest)
	}

	refs := []TableRef{{Name: name, Alias: alias}}

	// Parse JOINed tables: look for " JOIN " keywords
	searchPos := fromIdx + 6
	for {
		joinIdx := strings.Index(upper[searchPos:], " JOIN ")
		if joinIdx == -1 {
			break
		}
		absIdx := searchPos + joinIdx + 6 // skip " JOIN "
		afterJoin := strings.TrimSpace(trimmed[absIdx:])

		jRaw := extractNextWord(afterJoin)
		jName := strings.Trim(jRaw, `"'`)
		if jName == "" || jName == "(" {
			searchPos = absIdx + 1
			continue
		}

		jRest := strings.TrimSpace(afterJoin[len(jRaw):])
		jAlias := jName
		jNext := strings.ToUpper(extractNextWord(jRest))
		if jNext != "" && !sqlKeywords[jNext] {
			jAlias = extractNextWord(jRest)
		}

		refs = append(refs, TableRef{Name: jName, Alias: jAlias})
		searchPos = absIdx + len(jRaw)
	}

	return refs
}

// ensurePKColumnsInSQL injects primary key columns for ALL tables (main + JOINed)
// into a SQL SELECT using __pk__tablename__colname aliases to avoid conflicts.
// Skips subqueries, CTEs, and UNIONs.
func ensurePKColumnsInSQL(sqlQuery string, schema json.RawMessage) string {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	// Only handle SELECT — skip WITH/CTEs, UNION, subqueries
	if !strings.HasPrefix(upper, "SELECT") {
		return sqlQuery
	}
	for _, kw := range []string{" UNION ", " INTERSECT ", " EXCEPT "} {
		if strings.Contains(upper, kw) {
			return sqlQuery
		}
	}

	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx == -1 {
		return sqlQuery
	}

	// Parse all table references
	refs := ParseTableReferences(sqlQuery)
	if len(refs) == 0 {
		return sqlQuery
	}

	var tables []schemaTable
	if err := json.Unmarshal(schema, &tables); err != nil {
		return sqlQuery
	}

	selectPart := trimmed[6:fromIdx]
	selectUpper := strings.ToUpper(strings.TrimSpace(selectPart))

	// SELECT * already includes everything (but we still need aliased PKs for identification)
	isSelectStar := selectUpper == "*" || strings.HasPrefix(selectUpper, "* ") || strings.HasPrefix(selectUpper, "*,") || strings.Contains(selectUpper, ".*")

	// Build injections for ALL tables
	var injections []string
	for _, ref := range refs {
		for _, t := range tables {
			if t.Name == ref.Name {
				for _, col := range t.Columns {
					if col.IsPrimaryKey {
						alias := fmt.Sprintf("__pk__%s__%s", ref.Name, col.Name)
						injection := fmt.Sprintf(`%s.%s AS "%s"`, ref.Alias, quoteIdentifier(col.Name), alias)
						injections = append(injections, injection)
					}
				}
				break
			}
		}
	}

	if len(injections) == 0 {
		return sqlQuery
	}

	if isSelectStar {
		// For SELECT *, prepend aliased PKs so they're available for identification
		newSelect := strings.Join(injections, ", ") + "," + selectPart
		return "SELECT " + newSelect + trimmed[fromIdx:]
	}

	newSelect := strings.Join(injections, ", ") + "," + selectPart
	return "SELECT " + newSelect + trimmed[fromIdx:]
}

// extractNextWord returns the first whitespace-delimited word from s.
func extractNextWord(s string) string {
	s = strings.TrimSpace(s)
	for i, ch := range s {
		if ch == ' ' || ch == '\n' || ch == '\t' || ch == ',' || ch == ';' || ch == '(' {
			return s[:i]
		}
	}
	return s
}

// splitSelectItems splits a SELECT column list by commas, respecting parentheses.
func splitSelectItems(selectPart string) []string {
	var items []string
	depth := 0
	start := 0
	for i, ch := range selectPart {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, selectPart[start:i])
				start = i + 1
			}
		}
	}
	items = append(items, selectPart[start:])
	return items
}

// ResolveColumnFromSQL parses the SELECT clause to find the real column name
// and table reference for a given result column name (which may be an alias).
// Returns (realColumn, tableAlias). If no alias is found, returns (columnName, "").
func ResolveColumnFromSQL(sqlQuery, columnName string) (string, string) {
	trimmed := strings.TrimSpace(sqlQuery)
	upper := strings.ToUpper(trimmed)

	if !strings.HasPrefix(upper, "SELECT") {
		return columnName, ""
	}

	fromIdx := strings.Index(upper, " FROM ")
	if fromIdx == -1 {
		return columnName, ""
	}

	selectPart := trimmed[7:fromIdx] // skip "SELECT " (7 chars including space)
	items := splitSelectItems(selectPart)

	for _, item := range items {
		item = strings.TrimSpace(item)
		itemUpper := strings.ToUpper(item)

		// Check for AS alias
		asIdx := strings.LastIndex(itemUpper, " AS ")
		if asIdx != -1 {
			itemAlias := strings.TrimSpace(item[asIdx+4:])
			itemAlias = strings.Trim(itemAlias, `"' `)

			if strings.EqualFold(itemAlias, columnName) {
				expr := strings.TrimSpace(item[:asIdx])
				expr = strings.Trim(expr, `"'`)
				if dotIdx := strings.LastIndex(expr, "."); dotIdx != -1 {
					return strings.Trim(expr[dotIdx+1:], `"'`), strings.Trim(expr[:dotIdx], `"'`)
				}
				return expr, ""
			}
			continue
		}

		// No alias — check if this is a [table.]column that matches the column name
		stripped := strings.Trim(item, `"' `)
		if dotIdx := strings.LastIndex(stripped, "."); dotIdx != -1 {
			col := strings.Trim(stripped[dotIdx+1:], `"'`)
			tRef := strings.Trim(stripped[:dotIdx], `"'`)
			if strings.EqualFold(col, columnName) {
				return col, tRef
			}
		}
	}

	return columnName, ""
}

// ResolveTableFromRef maps a table alias back to the real table name using parsed references.
func ResolveTableFromRef(refs []TableRef, alias string) string {
	for _, ref := range refs {
		if strings.EqualFold(ref.Alias, alias) || strings.EqualFold(ref.Name, alias) {
			return ref.Name
		}
	}
	return ""
}

// QueryPanel executes the query for a single panel and returns the result.
// storeID is required for "platform" provider queries to enforce row-level scoping.
func (c *Controller) QueryPanel(dashboard *models.AppDashboard, panel *models.AppDashboardPanel, integration *models.AppIntegration, storeID string) responses.PanelDataResponse {
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

	// For table panels, ensure PK columns are in the SQL so rows can be identified for editing
	if panel.PanelType == "table" && query.SQL != "" && len(dashboard.Schema) > 0 {
		query.SQL = ensurePKColumnsInSQL(query.SQL, dashboard.Schema)
	}

	var data any
	var err error

	switch dashboard.Provider {
	case "supabase":
		data, err = c.querySupabase(integration, &query)
	case "neon":
		data, err = c.queryNeon(integration, &query)
	case "platform":
		if storeID == "" {
			err = fmt.Errorf("platform queries require a store context")
		} else {
			data, err = c.queryPlatform(&query, storeID)
		}
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
// storeID is required for "platform" provider queries to enforce row-level scoping.
func (c *Controller) QueryAllPanels(dashboard *models.AppDashboard, panels []models.AppDashboardPanel, integration *models.AppIntegration, storeID string) []responses.PanelDataResponse {
	results := make([]responses.PanelDataResponse, len(panels))
	sem := make(chan struct{}, maxConcurrentQueries)
	var wg sync.WaitGroup

	for i, panel := range panels {
		wg.Add(1)
		go func(idx int, p models.AppDashboardPanel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.QueryPanel(dashboard, &p, integration, storeID)
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
	if limit <= 0 || limit > c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global) {
		limit = c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global)
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
	// Try Credentials.connection_string first, then fall back to EnvVars.DATABASE_URL
	// (the database editor and codegen store the DSN in EnvVars, not Credentials).
	var connStr string
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err == nil {
		connStr, _ = creds["connection_string"].(string)
	}
	if connStr == "" {
		connStr = integration.EnvVars["DATABASE_URL"]
	}
	if connStr == "" {
		return nil, fmt.Errorf("missing neon connection string")
	}

	if query.SQL == "" {
		return nil, fmt.Errorf("sql query is required for neon provider")
	}

	if err := validateReadOnlySQL(query.SQL); err != nil {
		return nil, err
	}

	// Open a connection with pool limits to prevent connection exhaustion
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(connStr)))
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
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
		if rowCount >= c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global) {
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

// isValidSQLIdentifier checks if a string is a valid simple SQL identifier
// (letters, digits, underscores; must not start with a digit).
func isValidSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, ch := range s {
		if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			continue
		}
		if i > 0 && ch >= '0' && ch <= '9' {
			continue
		}
		return false
	}
	return true
}

// sanitizeOrderClause validates and quotes an ORDER BY clause.
// Only allows identifier [ASC|DESC] [NULLS FIRST|LAST] parts, comma-separated.
func sanitizeOrderClause(order string) (string, error) {
	parts := strings.Split(order, ",")
	var sanitized []string
	for _, part := range parts {
		tokens := strings.Fields(strings.TrimSpace(part))
		if len(tokens) == 0 {
			continue
		}
		col := tokens[0]
		var quotedCol string
		if dotIdx := strings.Index(col, "."); dotIdx != -1 {
			tbl := col[:dotIdx]
			c := col[dotIdx+1:]
			if !isValidSQLIdentifier(tbl) || !isValidSQLIdentifier(c) {
				return "", fmt.Errorf("invalid ORDER BY identifier: %s", col)
			}
			quotedCol = quoteIdentifier(tbl) + "." + quoteIdentifier(c)
		} else {
			if !isValidSQLIdentifier(col) {
				return "", fmt.Errorf("invalid ORDER BY identifier: %s", col)
			}
			quotedCol = quoteIdentifier(col)
		}
		result := quotedCol
		idx := 1
		if idx < len(tokens) {
			dir := strings.ToUpper(tokens[idx])
			switch dir {
			case "ASC", "DESC":
				result += " " + dir
				idx++
			case "NULLS":
				// handled below
			default:
				return "", fmt.Errorf("invalid ORDER BY modifier: %s", tokens[idx])
			}
		}
		if idx < len(tokens) && strings.ToUpper(tokens[idx]) == "NULLS" {
			idx++
			if idx < len(tokens) {
				nullDir := strings.ToUpper(tokens[idx])
				if nullDir != "FIRST" && nullDir != "LAST" {
					return "", fmt.Errorf("invalid NULLS modifier: %s", tokens[idx])
				}
				result += " NULLS " + nullDir
				idx++
			}
		}
		sanitized = append(sanitized, result)
	}
	if len(sanitized) == 0 {
		return "", fmt.Errorf("empty ORDER BY clause")
	}
	return strings.Join(sanitized, ", "), nil
}

// sanitizeSelectClause validates a SELECT column list for table-based queries.
// Allows *, column names, and table.column patterns. Rejects subqueries and expressions.
func sanitizeSelectClause(sel string) (string, error) {
	if sel == "" || sel == "*" {
		return "*", nil
	}
	items := splitSelectItems(sel)
	var result []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			result = append(result, "*")
			continue
		}
		if dotIdx := strings.Index(item, "."); dotIdx != -1 {
			tbl := strings.TrimSpace(item[:dotIdx])
			col := strings.TrimSpace(item[dotIdx+1:])
			if !isValidSQLIdentifier(tbl) {
				return "", fmt.Errorf("invalid select identifier: %s", tbl)
			}
			if col == "*" {
				result = append(result, quoteIdentifier(tbl)+".*")
			} else if isValidSQLIdentifier(col) {
				result = append(result, quoteIdentifier(tbl)+"."+quoteIdentifier(col))
			} else {
				return "", fmt.Errorf("invalid select identifier: %s", col)
			}
			continue
		}
		if isValidSQLIdentifier(item) {
			result = append(result, quoteIdentifier(item))
			continue
		}
		return "", fmt.Errorf("invalid select expression: %s", item)
	}
	if len(result) == 0 {
		return "*", nil
	}
	return strings.Join(result, ", "), nil
}

// buildSQLFromTableQuery converts a Supabase-style table/select/filters query to a
// parameterized SQL statement. Returns the SQL string, query arguments, and any error.
func buildSQLFromTableQuery(query *models.DashboardQuery, maxRows int) (string, []any, error) {
	if !isValidSQLIdentifier(query.Table) {
		return "", nil, fmt.Errorf("invalid table name: %s", query.Table)
	}
	quotedTable := quoteIdentifier(query.Table)

	selectClause, err := sanitizeSelectClause(query.Select)
	if err != nil {
		return "", nil, err
	}

	var args []any
	paramIdx := 1
	sqlStr := fmt.Sprintf("SELECT %s FROM %s", selectClause, quotedTable)

	if len(query.Filters) > 0 {
		var conditions []string
		for _, f := range query.Filters {
			if !isValidSQLIdentifier(f.Column) {
				return "", nil, fmt.Errorf("invalid filter column: %s", f.Column)
			}
			quotedCol := quoteIdentifier(f.Column)

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
				upperVal := strings.ToUpper(strings.TrimSpace(f.Value))
				if upperVal == "NULL" {
					conditions = append(conditions, fmt.Sprintf("%s IS NULL", quotedCol))
				} else if upperVal == "NOT NULL" {
					conditions = append(conditions, fmt.Sprintf("%s IS NOT NULL", quotedCol))
				}
				continue
			}

			conditions = append(conditions, fmt.Sprintf("%s %s $%d", quotedCol, op, paramIdx))
			args = append(args, f.Value)
			paramIdx++
		}
		if len(conditions) > 0 {
			sqlStr += " WHERE " + strings.Join(conditions, " AND ")
		}
	}

	if query.Order != "" {
		sanitizedOrder, err := sanitizeOrderClause(query.Order)
		if err != nil {
			return "", nil, err
		}
		sqlStr += " ORDER BY " + sanitizedOrder
	}

	limit := query.Limit
	if limit <= 0 || limit > maxRows {
		limit = maxRows
	}
	sqlStr += fmt.Sprintf(" LIMIT %d", limit)

	return sqlStr, args, nil
}

// queryPlatform executes a read-only SQL query against the platform's own Postgres database.
// SECURITY: storeID is mandatory. Every query is wrapped in a CTE that restricts all
// e-commerce tables to only rows belonging to this store, preventing cross-store leakage.
func (c *Controller) queryPlatform(query *models.DashboardQuery, storeID string) (any, error) {
	if storeID == "" {
		return nil, fmt.Errorf("store ID is required for platform queries")
	}

	var queryArgs []any

	// Auto-build SQL from table-based query if no SQL provided
	if query.SQL == "" && query.Table != "" {
		// SECURITY: validate table is in the e-commerce allowlist
		if !platformAllowedTables[query.Table] {
			return nil, fmt.Errorf("table %q is not accessible", query.Table)
		}
		var err error
		query.SQL, queryArgs, err = buildSQLFromTableQuery(query, c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global))
		if err != nil {
			return nil, err
		}
	}
	if query.SQL == "" {
		return nil, fmt.Errorf("sql query is required for platform provider")
	}

	// For count transform, wrap the query
	if query.Transform == "count" && query.Table != "" {
		if !isValidSQLIdentifier(query.Table) {
			return nil, fmt.Errorf("invalid table name: %s", query.Table)
		}
		if !platformAllowedTables[query.Table] {
			return nil, fmt.Errorf("table %q is not accessible", query.Table)
		}
		quotedTable := quoteIdentifier(query.Table)
		query.SQL = fmt.Sprintf("SELECT COUNT(*) AS value FROM %s", quotedTable)
		queryArgs = nil
		if len(query.Filters) > 0 {
			base, baseArgs, err := buildSQLFromTableQuery(&models.DashboardQuery{Table: query.Table, Select: "*", Filters: query.Filters}, c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global))
			if err != nil {
				return nil, err
			}
			query.SQL = fmt.Sprintf("SELECT COUNT(*) AS value FROM (%s) sub", base)
			queryArgs = baseArgs
		}
	}

	if err := validateReadOnlySQL(query.SQL); err != nil {
		return nil, err
	}

	// SECURITY: validate all table references in the SQL are in the allowlist
	if err := validatePlatformSQLTables(query.SQL); err != nil {
		return nil, err
	}

	// SECURITY: wrap the user query inside a CTE that scopes all e-commerce tables
	// to the current store. This ensures cross-store data is never returned regardless
	// of what SQL the LLM generated.
	scopedSQL, scopedArgs := wrapWithStoreScope(query.SQL, queryArgs, storeID)

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

	rows, err := tx.Query(scopedSQL, scopedArgs...)
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
		if rowCount >= c.RemoteConfig.GetInt(remoteconfig.MaxQueryRows, remoteconfig.Global) {
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

// platformTablesWithStoreID lists tables that have a direct store_id column.
// Tables like order_items and cart_items are scoped through their parent JOINs.
var platformTablesWithStoreID = map[string]bool{
	"stores":             true,
	"products":           true,
	"product_categories": true,
	"discounts":          true,
	"carts":              true,
	"orders":             true,
	"product_reviews":    true,
	"wishlists":          true,
}

// platformTablesWithParentScope lists tables scoped through a FK to a store-scoped parent.
var platformTablesWithParentScope = map[string]struct {
	parentTable string
	fkColumn    string
	parentPK    string
}{
	"order_items": {parentTable: "orders", fkColumn: "order_id", parentPK: "id"},
	"cart_items":  {parentTable: "carts", fkColumn: "cart_id", parentPK: "id"},
}

// wrapWithStoreScope wraps a SQL query in CTEs that restrict every e-commerce table
// to only rows belonging to the given store. The original query references the CTE
// names (same as table names) so it works transparently.
func wrapWithStoreScope(originalSQL string, originalArgs []any, storeID string) (string, []any) {
	// Find the next param index after existing args
	nextParam := len(originalArgs) + 1

	var ctes []string
	paramCount := 0

	// Tables with direct store_id
	for table := range platformTablesWithStoreID {
		if table == "stores" {
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT * FROM %s WHERE id = $%d)",
				quoteIdentifier(table), quoteIdentifier(table), nextParam+paramCount))
		} else {
			ctes = append(ctes, fmt.Sprintf("%s AS (SELECT * FROM %s WHERE store_id = $%d)",
				quoteIdentifier(table), quoteIdentifier(table), nextParam+paramCount))
		}
		paramCount++
	}

	// Tables scoped through parent FK
	for table, scope := range platformTablesWithParentScope {
		ctes = append(ctes, fmt.Sprintf(
			"%s AS (SELECT c.* FROM %s c INNER JOIN %s p ON c.%s = p.%s WHERE p.store_id = $%d)",
			quoteIdentifier(table), quoteIdentifier(table),
			quoteIdentifier(scope.parentTable),
			quoteIdentifier(scope.fkColumn), quoteIdentifier(scope.parentPK),
			nextParam+paramCount))
		paramCount++
	}

	if len(ctes) == 0 {
		return originalSQL, originalArgs
	}

	// Build args: original args + storeID repeated for each CTE
	scopedArgs := make([]any, len(originalArgs), len(originalArgs)+paramCount)
	copy(scopedArgs, originalArgs)
	for i := 0; i < paramCount; i++ {
		scopedArgs = append(scopedArgs, storeID)
	}

	return "WITH " + strings.Join(ctes, ", ") + " " + originalSQL, scopedArgs
}

// validatePlatformSQLTables checks that all table references in a SQL query
// are in the e-commerce allowlist. Prevents querying users, tokens, etc.
func validatePlatformSQLTables(sqlQuery string) error {
	refs := ParseTableReferences(sqlQuery)
	for _, ref := range refs {
		if !platformAllowedTables[ref.Name] {
			return fmt.Errorf("table %q is not accessible from dashboard", ref.Name)
		}
	}
	return nil
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

	// Block dangerous Postgres functions that can escape read-only transactions
	dangerousFuncs := []string{
		"PG_READ_FILE", "PG_READ_BINARY_FILE", "PG_WRITE_FILE",
		"DBLINK", "DBLINK_EXEC", "DBLINK_CONNECT",
		"LO_IMPORT", "LO_EXPORT", "LO_GET", "LO_PUT",
		"COPY", "PG_SLEEP",
	}
	for _, fn := range dangerousFuncs {
		if containsKeyword(cleaned, fn) {
			return fmt.Errorf("forbidden SQL function: %s", fn)
		}
	}

	return nil
}

// removeStringLiterals strips single-quoted, dollar-quoted string literals and SQL comments from SQL for keyword checking.
func removeStringLiterals(sql string) string {
	var result strings.Builder
	inString := false
	for i := 0; i < len(sql); i++ {
		// Strip block comments: /* ... */
		if !inString && i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			end := strings.Index(sql[i+2:], "*/")
			if end >= 0 {
				i += 2 + end + 1 // skip past closing */
				continue
			}
			// Unclosed block comment — strip the rest
			break
		}
		// Strip line comments: -- ...
		if !inString && i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			nl := strings.IndexByte(sql[i:], '\n')
			if nl >= 0 {
				i += nl // loop will increment past the newline
				continue
			}
			// No newline — comment runs to end
			break
		}
		// Handle dollar-quoted strings: $tag$...$tag$
		if sql[i] == '$' && !inString {
			// Find the end of the opening tag
			end := strings.Index(sql[i+1:], "$")
			if end >= 0 {
				tag := sql[i : i+end+2] // e.g. "$tag$" or "$$"
				closeIdx := strings.Index(sql[i+len(tag):], tag)
				if closeIdx >= 0 {
					i += len(tag) + closeIdx + len(tag) - 1
					continue
				}
			}
		}
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

// validateCellUpdate verifies that the table and PK columns exist in the schema.
func validateCellUpdate(schema json.RawMessage, table string, primaryKey map[string]any) error {
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

	// Column name is not validated against schema because SQL queries may use
	// aliases (e.g. "price AS price_in_cents"). PostgreSQL will reject invalid
	// column names with a clear error.

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

// extractPrefixedPKs extracts PK values from __pk__tablename__colname entries.
func extractPrefixedPKs(primaryKey map[string]any, tableName string) map[string]any {
	prefix := "__pk__" + tableName + "__"
	result := map[string]any{}
	for k, v := range primaryKey {
		if strings.HasPrefix(k, prefix) {
			realCol := strings.TrimPrefix(k, prefix)
			result[realCol] = v
		}
	}
	return result
}

// UpdateCell updates a single cell value in the database.
// The caller (view handler) is responsible for resolving column aliases and table names.
// storeID is required for "platform" provider to enforce ownership scoping.
func (c *Controller) UpdateCell(dashboard *models.AppDashboard, integration *models.AppIntegration, table, column string, value any, primaryKey map[string]any, storeID string) error {
	resolvedPK := primaryKey

	// Extract real PK values from __pk__ prefixed entries if present
	extracted := extractPrefixedPKs(primaryKey, table)
	if len(extracted) > 0 {
		resolvedPK = extracted
	}

	if err := validateCellUpdate(dashboard.Schema, table, resolvedPK); err != nil {
		return err
	}

	switch dashboard.Provider {
	case "supabase":
		return c.updateCellSupabase(integration, table, column, value, resolvedPK)
	case "neon":
		return c.updateCellNeon(integration, table, column, value, resolvedPK)
	case "platform":
		// SECURITY: only e-commerce tables are writable
		if !platformAllowedTables[table] {
			return fmt.Errorf("table %q is not writable from dashboard", table)
		}
		if storeID == "" {
			return fmt.Errorf("platform writes require a store context")
		}
		return c.updateCellPlatform(table, column, value, resolvedPK, storeID)
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
	var connStr string
	var creds map[string]any
	if err := json.Unmarshal(integration.Credentials, &creds); err == nil {
		connStr, _ = creds["connection_string"].(string)
	}
	if connStr == "" {
		connStr = integration.EnvVars["DATABASE_URL"]
	}
	if connStr == "" {
		return fmt.Errorf("missing neon connection string")
	}

	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(connStr)))
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
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
// SECURITY: storeID is mandatory. The UPDATE includes a store ownership check
// to prevent modifying rows belonging to other stores.
func (c *Controller) updateCellPlatform(table, column string, value any, primaryKey map[string]any, storeID string) error {
	if storeID == "" {
		return fmt.Errorf("store ID is required for platform writes")
	}
	if !platformAllowedTables[table] {
		return fmt.Errorf("table %q is not writable from dashboard", table)
	}

	db := c.DB.DB.DB

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET statement_timeout = '5s'"); err != nil {
		slog.Warn("failed to set statement_timeout", "error", err)
	}

	// SECURITY: build UPDATE with store_id ownership check
	sqlStr, args := buildScopedUpdateSQL(table, column, value, primaryKey, storeID)

	result, err := tx.Exec(sqlStr, args...)
	if err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no rows updated — row may not exist or does not belong to this store")
	}

	return tx.Commit()
}

// buildScopedUpdateSQL constructs a parameterized UPDATE with a store ownership guard.
// For tables with direct store_id, adds AND store_id = $N.
// For child tables (order_items, cart_items), adds a subquery check on the parent.
func buildScopedUpdateSQL(table, column string, value any, primaryKey map[string]any, storeID string) (string, []any) {
	args := []any{value}
	paramIdx := 2

	whereParts := []string{}
	for k, v := range primaryKey {
		whereParts = append(whereParts, fmt.Sprintf("%s = $%d", quoteIdentifier(k), paramIdx))
		args = append(args, v)
		paramIdx++
	}

	// Add store ownership constraint
	if platformTablesWithStoreID[table] {
		if table == "stores" {
			whereParts = append(whereParts, fmt.Sprintf("id = $%d", paramIdx))
		} else {
			whereParts = append(whereParts, fmt.Sprintf("store_id = $%d", paramIdx))
		}
		args = append(args, storeID)
	} else if scope, ok := platformTablesWithParentScope[table]; ok {
		// For child tables, verify the parent row belongs to this store
		whereParts = append(whereParts, fmt.Sprintf(
			"%s IN (SELECT %s FROM %s WHERE store_id = $%d)",
			quoteIdentifier(scope.fkColumn),
			quoteIdentifier(scope.parentPK),
			quoteIdentifier(scope.parentTable),
			paramIdx))
		args = append(args, storeID)
	}

	sqlStr := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s",
		quoteIdentifier(table),
		quoteIdentifier(column),
		strings.Join(whereParts, " AND "),
	)

	return sqlStr, args
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
