package models

import (
	"time"

	"github.com/google/uuid"
)

type AppSnapshot struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	AST          []byte      `bun:",type:jsonb,default:'[]'"` // snapshot of AST
	Theme        []byte      `bun:",type:jsonb,default:'{}'"` // snapshot of theme
	Description  string      `bun:",notnull"`
	Source       string      `bun:",notnull,default:'manual'"` // "manual", "auto", "pre_deploy"
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
