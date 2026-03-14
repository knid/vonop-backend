package models

import (
	"time"

	"github.com/google/uuid"
)

type PreviewCode struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Code         string      `bun:",notnull,unique"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID   `bun:",notnull,type:uuid"`
	User         *User       `bun:"rel:belongs-to,join:user_id=id"`
	SandboxID    string      `bun:",nullzero"`
	PreviewURL   string      `bun:",notnull"`
	ExpiresAt    time.Time   `bun:",notnull"`
	RedeemedAt   time.Time   `bun:",nullzero"`
	RedeemedBy   uuid.UUID   `bun:",type:uuid,nullzero"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
