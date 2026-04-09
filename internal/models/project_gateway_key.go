package models

import (
	"time"

	"github.com/google/uuid"
)

// ProjectGatewayKey is a per-project API key for the AI gateway.
// Each project has at most one active key (enforced by unique index on project_id + soft delete).
type ProjectGatewayKey struct {
	ID           uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID    uuid.UUID `bun:",notnull,type:uuid"`
	UserID       uuid.UUID `bun:",notnull,type:uuid"`
	User         *User     `bun:"rel:belongs-to,join:user_id=id"`
	KeyHash      string    `bun:",notnull"`
	KeyPrefix    string    `bun:",notnull"`
	EncryptedKey string    `bun:",notnull"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time `bun:",soft_delete,nullzero"`
}
