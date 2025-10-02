package models

import (
	"time"

	"github.com/google/uuid"
)

type AppNotificationTask struct {
	ID             uuid.UUID    `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	NotificationID uuid.UUID    `bun:",notnull,type:uuid"`
	Notification   Notification `bun:"rel:belongs-to,join:notification_id=id"`
	Finished       bool         `bun:",notnull"`
	ExecuteTime    time.Time    `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt      time.Time    `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time    `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time    `bun:",soft_delete,nullzero"`
}
