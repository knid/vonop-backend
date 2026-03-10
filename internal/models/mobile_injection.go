package models

import (
	"time"

	"github.com/google/uuid"
)

type MobileInjection struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID   uuid.UUID `bun:",notnull,type:uuid"`
	Project     Project   `bun:"rel:belongs-to,join:project_id=id"`
	Name        string    `bun:",notnull"`
	Description string    `bun:",nullzero"`
	CSS         string    `bun:",nullzero,type:text"`
	JS          string    `bun:",nullzero,type:text"`
	HTML        string    `bun:",nullzero,type:text"`
	IsEnabled   bool      `bun:",notnull,default:true"`
	Order       int       `bun:",notnull,default:0"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
