package models

import (
	"time"

	"github.com/google/uuid"
)

type AppGitHubExport struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID   `bun:",notnull,type:uuid"`
	RepoFullName string      `bun:",notnull"`
	RepoURL      string      `bun:",notnull"`
	Private      bool        `bun:",notnull"`
	LastPushedAt time.Time   `bun:",nullzero"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
