package models

import (
	"time"

	"github.com/google/uuid"
)

type Release struct {
	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID     uuid.UUID `bun:",notnull,unique,type:uuid"`
	Project       Project   `bun:"rel:belongs-to,join:project_id=id"`
	VersionNumber int       `bun:",notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt     time.Time `bun:",soft_delete,nullzero"`
}
