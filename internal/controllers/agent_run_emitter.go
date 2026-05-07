package controllers

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/redis/go-redis/v9"
)

// redisEventEmitter implements agent.EventEmitter. Each Emit call:
//  1. Stamps a per-job monotonic seq into the SSEEvent.
//  2. Publishes the JSON-encoded event on jobstream.EventsChannel(jobID).
//
// Milestone-event persistence + stream_state buffering is handled by the
// task handler in agent_run_handler.go — NOT here. Keeping the emitter
// transport-only mirrors agentSSEAdapter's split of responsibilities and
// keeps Emit cheap (one Redis publish, no DB write).
//
// Buffer() returns nil — replay-on-reconnect goes through DB-persisted
// milestones plus stream_state, the same way it does today.
type redisEventEmitter struct {
	ctx     context.Context
	redis   *redis.Client
	jobID   string
	seq     atomic.Int64
	onEvent func(seq int64, ev responses.SSEEvent) // optional persistence hook
}

func newRedisEventEmitter(
	ctx context.Context,
	r *redis.Client,
	jobID string,
	onEvent func(seq int64, ev responses.SSEEvent),
) *redisEventEmitter {
	return &redisEventEmitter{
		ctx:     ctx,
		redis:   r,
		jobID:   jobID,
		onEvent: onEvent,
	}
}

func (e *redisEventEmitter) Emit(eventType string, data any) bool {
	seq := e.seq.Add(1)
	ev := responses.SSEEvent{Seq: seq, Event: eventType, Data: data}

	// Persist (DB) BEFORE publish so any reconnected subscriber that reads
	// the DB after subscribing sees no event in the bus that isn't also
	// already in the DB. Mirrors handleStreamOrAsync's invariant.
	if e.onEvent != nil {
		e.onEvent(seq, ev)
	}

	if err := jobstream.PublishEvent(e.ctx, e.redis, e.jobID, ev); err != nil {
		// Best-effort transport. Persistence is the durable record;
		// clients reconnect and replay from DB.
		slog.Warn("redisEventEmitter publish failed",
			"job_id", e.jobID,
			"event_type", eventType,
			"seq", seq,
			"error", err,
		)
		return false
	}
	return true
}

func (e *redisEventEmitter) Buffer() []agent.Event { return nil }

func (e *redisEventEmitter) Close() {}

// Compile-time check.
var _ agent.EventEmitter = (*redisEventEmitter)(nil)
