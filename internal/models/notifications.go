package models

import (
	"time"

	"github.com/google/uuid"
)

type Notification struct {
	ID          uuid.UUID `orm:"column(id);pk"`
	ProjectID   Project   `orm:"column(project_id)"`
	Title       string
	Description string
	Image       string
	CreatedAt   time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt   time.Time `orm:"auto_now;type(datetime)"`
}
