package models

import (
	"time"

	"github.com/google/uuid"
)

type AppReleaseTask struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ReleaseID   uuid.UUID `bun:",notnull,type:uuid"`
	Release     Release   `bun:"rel:belongs-to,join:release_id=id"`
	Finished    bool      `bun:",notnull"`
	ExecuteTime time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
