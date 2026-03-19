package models

import (
	"time"

	"github.com/google/uuid"
)

type CoolifyProject struct {
	ID             uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject     *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	CoolifyAppUUID string      `bun:",notnull"`
	GiteaRepoName  string      `bun:",notnull"`
	ProductionURL  string      `bun:",nullzero"`
	CustomDomain   string      `bun:",nullzero"`
	Framework      string      `bun:",notnull,default:'nextjs'"`
	CreatedAt      time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
