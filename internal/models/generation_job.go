package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type GenerationJob struct {
	bun.BaseModel      `bun:"table:generation_jobs"`
	ID                 uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	UserID             uuid.UUID       `bun:",notnull,type:uuid" json:"user_id"`
	AppID              uuid.UUID       `bun:"app_id,type:uuid,nullzero" json:"app_id,omitempty"`
	Type               string          `bun:",notnull" json:"type"`
	Status             string          `bun:",notnull,default:'running'" json:"status"` // running, completed, failed, cancelled
	Events             json.RawMessage `bun:",type:jsonb,default:'[]'" json:"events"`
	StreamState        json.RawMessage `bun:"stream_state,type:jsonb,default:'{}'" json:"stream_state"`
	Result             json.RawMessage `bun:",type:jsonb,nullzero" json:"result,omitempty"`
	Error              string          `bun:",nullzero" json:"error,omitempty"`
	AgentRuntimeRunID  *uuid.UUID      `bun:",type:uuid,nullzero" json:"agent_runtime_run_id,omitempty"`
	DeepAgentsThreadID string          `bun:"deepagents_thread_id,notnull,default:''" json:"deepagents_thread_id,omitempty"`
	DeepAgentsRunID    string          `bun:"deepagents_run_id,notnull,default:''" json:"deepagents_run_id,omitempty"`
	CreatedAt          time.Time       `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt          time.Time       `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// JobEvent is one persisted milestone event. Seq is the per-job monotonic
// sequence number assigned by handleStreamOrAsync — it matches the seq sent
// to live clients as the SSE `id:` line, which means reconnect via
// `Last-Event-ID` can deduplicate against the persisted log without an
// out-of-band index.
//
// Renamed from `Index` (per-milestone counter) to `Seq` (per-event counter)
// in Phase 2; nothing in the dashboard reads the old field.
type JobEvent struct {
	Seq   int64  `json:"seq"`
	Event string `json:"event"`
	Data  any    `json:"data"`
	Time  int64  `json:"time"`
}
