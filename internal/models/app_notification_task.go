package models

import (
	"time"

	"github.com/google/uuid"
)

type AppNotificationTask struct {
	ID           uuid.UUID    `orm:"column(id);pk"`
	Notification Notification `orm:"rel(fk)"`
	Finished     bool
	CreatedAt    time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt    time.Time `orm:"auto_now;type(datetime)"`
}
