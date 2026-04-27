package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type RuntimeApp struct {
	bun.BaseModel `bun:"table:runtime_apps"`

	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`

	GiteaRepoName string `bun:",notnull"`
	ProductionURL string `bun:",nullzero"`
	CustomDomain  string `bun:",nullzero"`
	Framework     string `bun:",notnull,default:'nextjs'"`

	// Fly-specific columns (populated post-migration)
	FlyAppName      string    `bun:",nullzero"`
	CurrentImageRef string    `bun:",nullzero"`
	LastDeployedAt  time.Time `bun:",nullzero"`

	// Coolify-specific column — kept until cutover, then dropped
	CoolifyAppUUID string `bun:",nullzero"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
}
