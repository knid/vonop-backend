package models

import (
	"time"

	"github.com/google/uuid"
)

type AppUsageEvent struct {
	ID          uuid.UUID `orm:"column(id);pk"`
	Project     Project   `orm:"rel(fk)"`
	IP          string    `orm:"colunm(ip)"`
	Device      *Device   `orm:"rel(fk)"`
	StartTime   time.Time
	EndTime     time.Time
	Geolocation string
	AppVersion  string
	CreatedAt   time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt   time.Time `orm:"auto_now;type(datetime)"`
}
