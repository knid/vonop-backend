package models

import (
	"time"

	"github.com/google/uuid"
)

type StorePageImage struct {
	ID        uuid.UUID `orm:"column(id);pk"`
	Project   Project   `orm:"rel(fk)"`
	Image     string
	CreatedAt time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt time.Time `orm:"auto_now;type(datetime)"`
}
