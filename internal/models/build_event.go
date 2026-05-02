package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// BuildEvent is one entry in the append-only event stream for a build task.
// Replaces the legacy "overwrite app_build_task.status_text" pattern (T9 audit).
//
// The same task_id + message_hash will be deduped — Codemagic sends multiple
// status updates per build, and we want the webhook handler to be idempotent.
type BuildEvent struct {
	bun.BaseModel `bun:"table:build_events,alias:be"`

	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	TaskID      uuid.UUID `bun:",notnull,type:uuid"`
	EventType   string    `bun:",type:varchar(64),notnull"` // "preparing", "building", "uploading", "built", "error"
	Payload     []byte    `bun:",type:jsonb"`
	MessageHash string    `bun:",type:varchar(64),notnull"` // sha256(raw_body) hex
	OccurredAt  time.Time `bun:",notnull"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
