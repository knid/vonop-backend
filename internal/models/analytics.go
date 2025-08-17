package models

import (
	"time"

	"github.com/google/uuid"
)

type AppUsageEvent struct {
	ID          string    `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID   uuid.UUID `bun:",notnull,unique,type:uuid"`
	Project     Project   `bun:"rel:belongs-to,join:project_id=id"`
	IP          string    `bun:",notnull"`
	DeviceID    uuid.UUID `bun:",notnull,unique,type:uuid"`
	Device      Device    `bun:"rel:belongs-to,join:device_id=id"`
	StartTime   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	EndTime     time.Time `bun:",nullzero"`
	Geolocation string    `bun:",notnull"`
	AppVersion  string    `bun:",notnull"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
