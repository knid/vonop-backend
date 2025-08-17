package models

import (
	"time"

	"github.com/google/uuid"
)

type AppBuildTask struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	BuildID     uuid.UUID `bun:",notnull,unique,type:uuid"`
	Build       Build     `bun:"rel:belongs-to,join:build_id=id"`
	Finished    bool      `bun:",notnull"`
	ExecuteTime time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
