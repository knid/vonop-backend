package models

import (
	"time"

	"github.com/google/uuid"
)

type AppEnvVar struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Key          string      `bun:",notnull"`
	Value        string      `bun:",notnull"`
	IsPublic     bool        `bun:",notnull,default:false"` // if true, prefixed NEXT_PUBLIC_ in deploy
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time   `bun:",soft_delete,nullzero"`
}
