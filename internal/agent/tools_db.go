package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/knid/wemob/internal/database"
	_ "github.com/lib/pq"
)

// destructiveSQLPatterns are regexes matching SQL statements that can
// permanently destroy user data or elevate privileges. Even with
// readonly=false, these are blocked at the tool layer; the underlying
// Neon role should ALSO be provisioned without these privileges as
// defense in depth.
var destructiveSQLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bdrop\s+(schema|database|role|user)\b`),
	regexp.MustCompile(`(?i)\btruncate\b`),
	regexp.MustCompile(`(?i)\bgrant\b`),
	regexp.MustCompile(`(?i)\brevoke\b`),
	regexp.MustCompile(`(?i)\bcreate\s+role\b`),
	regexp.MustCompile(`(?i)\balter\s+role\b`),
	regexp.MustCompile(`(?i)\balter\s+system\b`),
}

var (
	deleteFromRe = regexp.MustCompile(`(?i)\bdelete\s+from\b`)
	updateSetRe  = regexp.MustCompile(`(?i)\bupdate\s+\w+\s+set\b`)
	whereRe      = regexp.MustCompile(`(?i)\bwhere\b`)
)

// isDestructiveSQL returns true if the query contains any operation from
// the deny-list. The check is a coarse string/regex match — a determined
// LLM could obfuscate (comments, unicode tricks) but the tool is also
// backed by Postgres transaction semantics and an app-level DB role with
// restricted privileges.
func isDestructiveSQL(query string) bool {
	for _, re := range destructiveSQLPatterns {
		if re.MatchString(query) {
			return true
		}
	}
	// DELETE / UPDATE without WHERE is usually a mistake and always risky.
	if (deleteFromRe.MatchString(query) || updateSetRe.MatchString(query)) && !whereRe.MatchString(query) {
		return true
	}
	return false
}

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
	if !args.ReadOnly && isDestructiveSQL(args.Query) {
		return "query blocked: destructive or privilege-changing statements are not allowed (DROP SCHEMA/DATABASE, TRUNCATE, GRANT/REVOKE, ALTER SYSTEM, and DELETE/UPDATE without WHERE are blocked)", true, nil
	}

	if e.Deps == nil || e.Deps.NeonConnString == nil {
		return "database access not available", true, nil
	}
	connStr, err := e.Deps.NeonConnString(rc.Project.ID.String())
	if err != nil || connStr == "" {
		return "this project does not have a database connected", true, nil
	}
	// lib/pq doesn't send SNI, so Neon's router can't find the endpoint
	// without the endpoint ID embedded in the connection string's options
	// parameter. NeonConnStrWithEndpoint is the same helper every other
	// neon caller in the project uses.
	db, err := sql.Open("postgres", database.NeonConnStrWithEndpoint(connStr))
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
