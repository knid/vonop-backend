package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type GenerationJob struct {
	bun.BaseModel `bun:"table:generation_jobs"`
	ID            uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	UserID        uuid.UUID       `bun:",notnull,type:uuid" json:"user_id"`
	Type          string          `bun:",notnull" json:"type"`
	Status        string          `bun:",notnull,default:'running'" json:"status"` // running, completed, failed, cancelled
	Events        json.RawMessage `bun:",type:jsonb,default:'[]'" json:"events"`
	Result        json.RawMessage `bun:",type:jsonb,nullzero" json:"result,omitempty"`
	Error         string          `bun:",nullzero" json:"error,omitempty"`
	CreatedAt     time.Time       `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time       `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type JobEvent struct {
	Index int    `json:"index"`
	Event string `json:"event"`
	Data  any    `json:"data"`
	Time  int64  `json:"time"`
}
