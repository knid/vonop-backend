// Package jobstream carries SSE event frames and cancel signals for
// generation_jobs across the API ↔ worker boundary, using Redis pub/sub.
//
// Why pub/sub and not a list/stream:
//   - Live-tail SSE clients are subscribers; on disconnect Redis drops their
//     buffer automatically. We don't need durable history — milestone
//     events are persisted to Postgres in the worker handler, and clients
//     replay from the DB on reconnect via StreamJob.
//   - A Redis stream would force us to manage trim/ack/consumer-group state
//     for every job, which is overkill for a transport that's only "live".
package jobstream

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/responses"
	"github.com/redis/go-redis/v9"
)

// EventsChannel is the pub/sub channel name carrying SSEEvent frames for one
// generation_job. Both worker (publisher) and API (subscriber) agree on this
// shape.
func EventsChannel(jobID string) string { return "job:events:" + jobID }

// CancelChannel carries cancel signals from API → worker for one generation_job.
// Worker subscribes for the duration of the run; API publishes when the user
// hits DELETE /v1/jobs/{id}.
func CancelChannel(jobID string) string { return "job:cancel:" + jobID }

// PublishEvent serialises an SSEEvent frame as JSON and publishes it on the
// job's events channel. Returns the publish error (Redis network failure
// etc.) — callers log; they do not retry. Best-effort by design: persisted
// milestones in Postgres are the durable source of truth.
func PublishEvent(ctx context.Context, r *redis.Client, jobID string, ev responses.SSEEvent) error {
	if r == nil {
		return fmt.Errorf("nil redis client")
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return r.Publish(ctx, EventsChannel(jobID), raw).Err()
}

// PublishCancel signals the worker to cancel its in-flight run for jobID.
// Best-effort: if no worker is subscribed (job already finished, retry budget
// exhausted, etc.) the publish silently succeeds with 0 receivers — the API
// still flips the DB row to "cancelled" so the user sees the right state.
func PublishCancel(ctx context.Context, r *redis.Client, jobID string) error {
	if r == nil {
		return fmt.Errorf("nil redis client")
	}
	return r.Publish(ctx, CancelChannel(jobID), []byte("1")).Err()
}

// SubscribeEvents returns a Redis subscription bound to the job's events
// channel. Caller must Close() it when done. Receive in a loop on
// sub.Channel() (handles reconnects internally per go-redis/v9).
func SubscribeEvents(ctx context.Context, r *redis.Client, jobID string) (*redis.PubSub, error) {
	if r == nil {
		return nil, fmt.Errorf("nil redis client")
	}
	sub := r.Subscribe(ctx, EventsChannel(jobID))
	// Wait for confirmation so the caller doesn't race the publisher.
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("subscribe events: %w", err)
	}
	return sub, nil
}

// SubscribeCancel mirrors SubscribeEvents for the cancel channel.
func SubscribeCancel(ctx context.Context, r *redis.Client, jobID string) (*redis.PubSub, error) {
	if r == nil {
		return nil, fmt.Errorf("nil redis client")
	}
	sub := r.Subscribe(ctx, CancelChannel(jobID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, fmt.Errorf("subscribe cancel: %w", err)
	}
	return sub, nil
}

// DecodeEvent parses a Redis pub/sub message payload back into an SSEEvent.
// Returns the empty event + error if the payload is malformed; callers log
// and skip — one bad frame should not kill the stream.
func DecodeEvent(raw []byte) (responses.SSEEvent, error) {
	var ev responses.SSEEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return responses.SSEEvent{}, err
	}
	return ev, nil
}
