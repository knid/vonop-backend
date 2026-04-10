package models

import (
	"time"

	"github.com/google/uuid"
)

type AppDomain struct {
	ID             uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject     *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Domain         string      `bun:",notnull"`
	Status         string      `bun:",notnull,default:'pending'"` // pending, deploying, active, misconfigured, failed
	DNSRecord      string      `bun:",nullzero"`                  // CNAME value to point to
	DeploymentUUID string      `bun:",nullzero"`                  // Coolify deployment UUID currently rolling out this domain change
	CreatedAt      time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time   `bun:",soft_delete,nullzero"`
}
