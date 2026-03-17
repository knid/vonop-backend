package utils

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// Field names
const (
	OperationFieldName     = "operation"
	OperationTimeFieldName = "operation_time_ms"
)

// QueryHook defines the
// structure of our query hook
// it implements the bun.QueryHook
// interface
type QueryHook struct {
	bun.QueryHook

	logger       *slog.Logger
	slowDuration time.Duration
}

// QueryHookOptions defines the
// available options for a new
// query hook.
type QueryHookOptions struct {
	Logger       *slog.Logger
	SlowDuration time.Duration
}

// NewQueryHook returns a new query hook for use with
// uptrace/bun.
func NewQueryHook(options QueryHookOptions) QueryHook {
	return QueryHook{
		logger:       options.Logger,
		slowDuration: options.SlowDuration,
	}
}

func (qh QueryHook) BeforeQuery(ctx context.Context, event *bun.QueryEvent) context.Context {
	return ctx
}

func (qh QueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	queryDuration := time.Since(event.StartTime)
	maskedQuery := MaskSensitive(event.Query)
	fields := []any{
		slog.String(OperationFieldName, event.Operation()),
		slog.Int64(OperationTimeFieldName, queryDuration.Milliseconds()),
		slog.String("from", "BUNDB"),
	}

	// Log errors, but skip sql.ErrNoRows (normal "not found" result)
	if event.Err != nil {
		if errors.Is(event.Err, sql.ErrNoRows) {
			return
		}
		fields = append(fields, slog.String("ERROR", event.Err.Error()))
		errMsg := event.Err.Error()
		if isConstraintViolation(errMsg) {
			qh.logger.Warn(maskedQuery, fields...)
		} else {
			qh.logger.Error(maskedQuery, fields...)
		}
		return
	}

	// Queries over a slow time duration
	// will be logged as debug
	if queryDuration >= qh.slowDuration {
		qh.logger.Debug(maskedQuery, fields...)
	}
}

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)'[a-f0-9]{64}'`),                                              // SHA256/token/hex 64
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`),           // JWT tokens
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-/+=]+`),                                    // Bearer tokens in strings
	regexp.MustCompile(`[a-f0-9]{32,}`),                                                    // Generic long hex strings
	regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`),                                        // Base64-encoded API keys
}

func MaskSensitive(sql string) string {
	masked := sql
	for _, re := range sensitivePatterns {
		masked = re.ReplaceAllString(masked, "********")
	}
	return masked
}

// isConstraintViolation checks if a database error is a Postgres constraint
// violation, which is typically a client/logic error rather than an infrastructure
// failure. These are logged at Warn level instead of Error.
func isConstraintViolation(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	constraintPatterns := []string{
		"unique_violation",
		"foreign_key_violation",
		"check_violation",
		"duplicate key value violates unique constraint",
		"violates foreign key constraint",
		"violates check constraint",
		"23505", // unique_violation SQLSTATE
		"23503", // foreign_key_violation SQLSTATE
		"23514", // check_violation SQLSTATE
	}
	for _, p := range constraintPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// Command Log Writer
type CommandLogWriter struct {
	Level slog.Level
	Meta  map[string]any
}

func (lw *CommandLogWriter) Write(p []byte) (n int, err error) {
	logger := slog.Default().With("from", "BUILD CMDS")
	msg := string(bytes.TrimSpace(p))
	args := []any{}
	for k, v := range lw.Meta {
		args = append(args, k, v)
	}
	logger.Log(context.Background(), lw.Level, msg, args...)
	return len(p), nil
}
