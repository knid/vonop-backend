package models

import (
	"time"

	// "github.com/beego/beego/v2/client/orm"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type User struct {
	ID        uuid.UUID `orm:"pk;column(id)"`
	Name      string
	Email     string 	`orm:"unique"`
	Password  string
	Photo     string
	Verified  bool      `default:"false"`
	Provider  string    `default:"Wemob"`
	CreatedAt time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt time.Time `orm:"auto_now;type(datetime)"`
}
