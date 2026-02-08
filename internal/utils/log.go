package utils

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/uptrace/bun"

	"log/slog"
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
		qh.logger.Error(maskedQuery, fields...)
		return
	}

	// Queries over a slow time duration
	// will be logged as debug
	if queryDuration >= qh.slowDuration {
		qh.logger.Debug(maskedQuery, fields...)
	}
}

func MaskSensitive(sql string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)'[a-f0-9]{64}'`), // SHA256/token/hex 64
	}

	masked := sql
	for _, re := range patterns {
		masked = re.ReplaceAllString(masked, "'********'")
	}
	return masked
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
