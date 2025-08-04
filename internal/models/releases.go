package models

import (
	"time"

	"github.com/google/uuid"
)

type Release struct {
	ID            uuid.UUID `orm:"column(id);pk"`
	ProjectID     Project   `orm:"column(project_id)"`
	VersionNumber int
	CreatedAt     time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt     time.Time `orm:"auto_now;type(datetime)"`
}
