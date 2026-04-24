package agent

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	_ "github.com/lib/pq"
)

// destructiveSQLPatterns are regexes matching SQL statements that can
// permanently destroy user data or elevate privileges. Even with
// readonly=false, these are blocked at the tool layer; the underlying
// Neon role should ALSO be provisioned without these privileges as
// defense in depth.
var destructiveSQLPatterns = []*regexp.Regexp{
	// DROP of high-blast-radius objects. ALTER TABLE / ALTER COLUMN is
	// intentionally allowed — the LLM needs it for schema evolution.
	regexp.MustCompile(`(?i)\bdrop\s+(schema|database|role|user|table|view|index|function|procedure|trigger|extension|materialized\s+view)\b`),
	regexp.MustCompile(`(?i)\btruncate\b`),
	regexp.MustCompile(`(?i)\bgrant\b`),
	regexp.MustCompile(`(?i)\brevoke\b`),
	regexp.MustCompile(`(?i)\bcreate\s+role\b`),
	regexp.MustCompile(`(?i)\balter\s+role\b`),
	regexp.MustCompile(`(?i)\balter\s+system\b`),
	// Role switching — would let a query escape the restricted DB role the
	// tenant connection is meant to run as.
	regexp.MustCompile(`(?i)\bset\s+role\b`),
	regexp.MustCompile(`(?i)\bset\s+session\s+authorization\b`),
	regexp.MustCompile(`(?i)\breset\s+role\b`),
	// COPY ... FROM/TO PROGRAM reaches the server's shell.
	regexp.MustCompile(`(?is)\bcopy\b.*\b(from|to)\s+program\b`),
}

var (
	deleteFromRe = regexp.MustCompile(`(?i)\bdelete\s+from\b`)
	// updateSetRe matches UPDATE ... SET across the target-table forms
	// Postgres accepts: bare (orders), schema-qualified (public.orders),
	// quoted ("orders", "public"."orders"), and with an alias (orders o /
	// orders AS o). The previous `\w+` variant silently missed the
	// qualified/quoted/aliased cases, which let UPDATE-without-WHERE
	// bypass the destructive-SQL guard entirely.
	// The identifier group intentionally excludes whitespace so that
	// `INSERT ... ON CONFLICT ... DO UPDATE SET` (where `UPDATE` is
	// immediately followed by `SET`) does not false-positive.
	updateSetRe    = regexp.MustCompile(`(?is)\bupdate\s+(?:only\s+)?["\w.]+(?:\s+(?:as\s+)?["\w]+)?\s+set\b`)
	whereRe        = regexp.MustCompile(`(?i)\bwhere\b`)
	lineCommentRe  = regexp.MustCompile(`--[^\n]*`)
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	returningRe    = regexp.MustCompile(`(?i)\breturning\b`)
)

// stripSQLComments removes -- line comments and /* ... */ block comments
// so the destructive-pattern check can't be bypassed by wrapping keywords
// in a comment (e.g. `/*drop*/ table foo`). Nested block comments — a
// Postgres-only extension — aren't unwrapped, but are enough of a smell
// that the DB role's privilege set is the real backstop.
func stripSQLComments(query string) string {
	q := lineCommentRe.ReplaceAllString(query, " ")
	q = blockCommentRe.ReplaceAllString(q, " ")
	return q
}

// isDestructiveSQL returns true if the query contains any operation from
// the deny-list. The check is a coarse string/regex match — a determined
// LLM could obfuscate (unicode tricks, creative whitespace) but the tool
// is also backed by Postgres transaction semantics and an app-level DB
// role with restricted privileges.
func isDestructiveSQL(query string) bool {
	cleaned := stripSQLComments(query)
	for _, re := range destructiveSQLPatterns {
		if re.MatchString(cleaned) {
			return true
		}
	}
	// DELETE / UPDATE without WHERE is usually a mistake and always risky.
	if (deleteFromRe.MatchString(cleaned) || updateSetRe.MatchString(cleaned)) && !whereRe.MatchString(cleaned) {
		return true
	}
	return false
}

type ExecuteSQLTool struct {
	Deps *RunDependencies
}

func (ExecuteSQLTool) Name() string { return "execute_sql" }
func (ExecuteSQLTool) Description() string {
	return "Run a SQL query against the project's database. Set readonly=true for SELECT queries; readonly=false for INSERT/UPDATE/DELETE/CREATE/ALTER. Timeout 10s, max 50 rows returned. On writes, append `RETURNING id, ...` to get the inserted/updated row values back — UUID columns come back as 36-char strings, not base64. Writes run inside a transaction that rolls back on error."
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

	// Write path. Wrap in a transaction so a multi-statement write that
	// fails halfway doesn't leave orphan rows behind — the LLM sends bulk
	// INSERT/UPDATE blocks routinely, and partial success was the hardest
	// failure mode to unpick before this.
	tx, err := db.BeginTx(qCtx, nil)
	if err != nil {
		return fmt.Sprintf("begin tx: %v", err), true, nil
	}
	// Rollback is a no-op after a successful Commit, so deferring it is safe.
	defer tx.Rollback()

	// If the query has a RETURNING clause, run it through QueryContext and
	// surface the returned rows. This gives the LLM INSERTed IDs in the same
	// round-trip rather than forcing a follow-up SELECT — cuts the worst
	// turn-count offender in ecommerce seeding flows.
	if returningRe.MatchString(stripSQLComments(args.Query)) {
		rows, err := tx.QueryContext(qCtx, args.Query)
		if err != nil {
			return fmt.Sprintf("query error: %v", err), true, nil
		}
		out, isErr, fErr := formatRows(rows, 50)
		rows.Close()
		if fErr != nil || isErr {
			return out, isErr, fErr
		}
		if err := tx.Commit(); err != nil {
			return fmt.Sprintf("commit error: %v", err), true, nil
		}
		return out, false, nil
	}

	res, err := tx.ExecContext(qCtx, args.Query)
	if err != nil {
		return fmt.Sprintf("exec error: %v", err), true, nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Sprintf("commit error: %v", err), true, nil
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
			row[c] = convertSQLValue(vals[i])
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

// convertSQLValue normalises values scanned into `any` so JSON marshalling
// doesn't silently corrupt them. The main offender is []byte: lib/pq hands
// back uuid/bytea/text columns as raw bytes when the Scan target is `*any`,
// and Go's encoding/json base64-encodes every []byte. That is how UUIDs ended
// up as base64 in tool results and forced the LLM to rewrite queries around
// the seemingly-garbage ids. Three cases, in order:
//  1. exactly 16 bytes → interpret as binary UUID (lib/pq binary mode).
//  2. valid UTF-8 → treat as text (lib/pq text mode gives uuid as "xxxxxxxx-..").
//  3. otherwise → hex-encode so real bytea still round-trips deterministically.
func convertSQLValue(v any) any {
	b, ok := v.([]byte)
	if !ok {
		return v
	}
	if len(b) == 16 {
		if u, err := uuid.FromBytes(b); err == nil {
			return u.String()
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return hex.EncodeToString(b)
}
