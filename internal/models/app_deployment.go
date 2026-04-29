package models

import (
	"time"

	"github.com/google/uuid"
)

type AppDeployment struct {
	ID           uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID      `bun:",notnull,type:uuid"`
	AppProject   *AppProject    `bun:"rel:belongs-to,join:app_project_id=id"`
	UserID       uuid.UUID      `bun:",notnull,type:uuid"`
	User         *User          `bun:"rel:belongs-to,join:user_id=id"`
	Platform     string         `bun:",notnull"` // "web", "android", "ios"
	Provider     string         `bun:",notnull"` // "fly"
	Status       string         `bun:",notnull,default:'pending'"` // pending, building, deployed, failed
	ProviderData map[string]any `bun:",type:jsonb,default:'{}'"`
	DeployedURL  string         `bun:",nullzero"`
	Version      int            `bun:",notnull,default:1"`
	CommitHash   string         `bun:",nullzero"` // git commit hash at deploy time
	ErrorMessage string         `bun:",nullzero,type:text"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
