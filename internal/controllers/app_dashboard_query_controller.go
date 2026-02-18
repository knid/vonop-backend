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

// QueryPanel executes the query for a single panel and returns the result.
func (c *Controller) QueryPanel(dashboard *models.AppDashboard, panel *models.AppDashboardPanel, integration *models.AppIntegration) responses.PanelDataResponse {
	var query models.DashboardQuery
	if err := json.Unmarshal(panel.Query, &query); err != nil {
		return responses.PanelDataResponse{
			PanelID: panel.ID,
			Title:   panel.Title,
			Type:    panel.PanelType,
			Error:   "invalid query configuration",
		}
	}

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

// queryPlatform executes a read-only SQL query against the platform's own Postgres database.
func (c *Controller) queryPlatform(query *models.DashboardQuery) (any, error) {
	if query.SQL == "" {
		return nil, fmt.Errorf("sql query is required for platform provider")
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
