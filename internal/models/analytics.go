package models

import (
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID   uuid.UUID `bun:",notnull,type:uuid"`
	Project     Project   `bun:"rel:belongs-to,join:project_id=id"`
	DeviceID    uuid.UUID `bun:",notnull,type:uuid"`
	Device      Device    `bun:"rel:belongs-to,join:device_id=id"`
	AppVersion  string    `bun:",notnull"`
	IP          string    `bun:",notnull"`
	Geolocation string    `bun:",notnull"`
	StartTime   time.Time `bun:",notnull"`
	EndTime     time.Time `bun:",notnull"`
	Duration    int64     `bun:",notnull"` // Duration in seconds
	Status      string    `bun:",notnull,default:'active'"`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}

type AppUsageEvent struct {
	ID        string         `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	EventType string         `bun:",notnull"`
	SessionID string         `bun:",nullzero,notnull,type:uuid"`
	Session   Session        `bun:"rel:belongs-to,join:session_id=id"`
	Data      map[string]any `bun:",type:jsonb,default:'{}'"`
	CreatedAt time.Time      `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time      `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time      `bun:",soft_delete,nullzero"`
}
