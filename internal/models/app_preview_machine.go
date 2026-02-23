package models

import (
	"time"

	"github.com/google/uuid"
)

type AppPreviewMachine struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid,unique"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	MachineID    string      `bun:",notnull"`
	SyncToken    string      `bun:",notnull"`
	Region       string      `bun:",notnull,default:'iad'"`
	Status       string      `bun:",notnull,default:'starting'"` // starting, ready, stopped, error
	PreviewURL   string      `bun:",nullzero"`
	LastAccessAt time.Time   `bun:"last_access_at,nullzero,notnull,default:current_timestamp"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
