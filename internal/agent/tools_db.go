package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type ExecuteSQLTool struct {
	Deps *RunDependencies
}

func (ExecuteSQLTool) Name() string { return "execute_sql" }
func (ExecuteSQLTool) Description() string {
	return "Run a SQL query against the project's database. Set readonly=true for SELECT queries; readonly=false for INSERT/UPDATE/DELETE/CREATE/ALTER/DROP. Timeout 10s, max 50 rows returned."
}
func (ExecuteSQLTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":    map[string]any{"type": "string"},
			"readonly": map[string]any{"type": "boolean"},
		},
		"required": []string{"query", "readonly"},
	}
}

func (e ExecuteSQLTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Query    string `json:"query"`
		ReadOnly bool   `json:"readonly"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return fmt.Sprintf("invalid input: %v", err), true, nil
	}
	if strings.TrimSpace(args.Query) == "" {
		return "query is required", true, nil
	}

	if e.Deps == nil || e.Deps.NeonConnString == nil {
		return "database access not available", true, nil
	}
	connStr, err := e.Deps.NeonConnString(rc.Project.ID.String())
	if err != nil || connStr == "" {
		return "this project does not have a database connected", true, nil
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Sprintf("could not connect: %v", err), true, nil
	}
	defer db.Close()

	qCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if args.ReadOnly {
		tx, err := db.BeginTx(qCtx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return fmt.Sprintf("begin read-only tx: %v", err), true, nil
		}
		defer tx.Rollback()
		rows, err := tx.QueryContext(qCtx, args.Query)
		if err != nil {
			return fmt.Sprintf("query error: %v", err), true, nil
		}
		defer rows.Close()
		return formatRows(rows, 50)
	}

	// Write path — plain exec, no tx wrapping.
	res, err := db.ExecContext(qCtx, args.Query)
	if err != nil {
		return fmt.Sprintf("exec error: %v", err), true, nil
	}
	affected, _ := res.RowsAffected()
	return fmt.Sprintf("ok (%d rows affected)", affected), false, nil
}

// formatRows serialises up to maxRows into a JSON string for the tool result.
func formatRows(rows *sql.Rows, maxRows int) (string, bool, error) {
	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("columns error: %v", err), true, nil
	}
	var out []map[string]any
	count := 0
	truncated := false
	for rows.Next() {
		if count >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Sprintf("scan error: %v", err), true, nil
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
		count++
	}
	result := map[string]any{
		"rows":      out,
		"row_count": count,
		"truncated": truncated,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return string(b), false, nil
}
