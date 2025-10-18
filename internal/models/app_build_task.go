package models

import (
	"time"

	"github.com/google/uuid"
)

type AppBuildTask struct {
	ID         uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID  uuid.UUID `bun:",notnull,type:uuid"`
	Project    Project   `bun:"rel:belongs-to,join:project_id=id"`
	BuildID    uuid.UUID `bun:",notnull,type:uuid"`
	Build      Build     `bun:"rel:belongs-to,join:build_id=id"`
	Finished   bool      `bun:",notnull"`
	Platform   string    `bun:",notnull"`
	AutoDeploy bool      `bun:",notnull,default:false"`
	StatusText string    `bun:",nullzero"`
	// AutoDeploy  bool      `bun:",nullzero"`
	ExecuteTime time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
