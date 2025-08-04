package models

import (
	"time"

	"github.com/google/uuid"
)

type AppBuildTask struct {
	ID        uuid.UUID `orm:"column(id);pk"`
	Build     Build     `orm:"rel(fk)"`
	Finished  bool
	CreatedAt time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt time.Time `orm:"auto_now;type(datetime)"`
}
