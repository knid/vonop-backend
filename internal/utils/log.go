package utils

import (
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
		slog.String("from", "bundb"),
	}

	// Log errors, but skip "not found" results which are routine query outcomes.
	// Bun wraps sql.ErrNoRows and multi-row queries may produce different "no rows"
	// error text, so we check both errors.Is and the error message string.
	if event.Err != nil {
		if errors.Is(event.Err, sql.ErrNoRows) || isNotFoundError(event.Err.Error()) {
			return
		}
		errMsg := event.Err.Error()
		fields = append(fields, slog.String("error", errMsg))
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

// isNotFoundError checks if an error message indicates a routine "not found"
// result. Bun wraps sql.ErrNoRows in ways that errors.Is may not catch, and
// multi-row queries that return zero rows produce distinct error text. These
// are normal application behavior, not infrastructure failures.
func isNotFoundError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "no rows") || strings.Contains(lower, "no result")
}

// Command Log Writer
type CommandLogWriter struct {
	Level slog.Level
	Meta  map[string]any
}

