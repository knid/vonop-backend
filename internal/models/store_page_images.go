package models

import (
	"time"

	"github.com/google/uuid"
)

type StorePageImage struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID uuid.UUID `bun:",notnull,type:uuid"`
	Project   Project   `bun:"rel:belongs-to,join:project_id=id"`
	Image     string    `bun:",notnull"`
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
}
