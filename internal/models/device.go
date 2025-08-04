package models

import (
	"time"

	"github.com/google/uuid"
)

type Device struct {
	ID          uuid.UUID `orm:"column(id);pk"`
	DeviceToken uuid.UUID
	Platform    string
	Model       string
	CreatedAt   time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt   time.Time `orm:"auto_now;type(datetime)"`
}
