package models

import (
	"time"

	"github.com/google/uuid"
)

type Build struct {
	ID          uuid.UUID `orm:"column(id);pk"`
	Project     Project   `orm:"rel(fk)"`
	BuildNumber int
	CreatedAt   time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt   time.Time `orm:"auto_now;type(datetime)"`
}
