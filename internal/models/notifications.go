package models

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID   uuid.UUID `bun:",notnull,type:uuid"`
	Project     Project   `bun:"rel:belongs-to,join:project_id=id"`
	Title       string    `bun:",notnull"`
	Description string    `bun:",notnull"`
	Image       string    `bun:",notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
