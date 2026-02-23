package models

import (
	"time"

	"github.com/google/uuid"
)

type VercelProject struct {
	ID              uuid.UUID         `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID    uuid.UUID         `bun:",notnull,type:uuid,unique"`
	AppProject      *AppProject       `bun:"rel:belongs-to,join:app_project_id=id"`
	VercelProjectID string            `bun:",notnull"` // Vercel's project ID
	ProductionURL   string            `bun:",nullzero"`
	CustomDomain    string            `bun:",nullzero"`
	EnvVars         map[string]string `bun:",type:jsonb,default:'{}'"`
	Framework       string            `bun:",notnull,default:'nextjs'"`
	CreatedAt       time.Time         `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time         `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
