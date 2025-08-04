package models

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID                uuid.UUID `orm:"column(id);pk"`
	User              *User `orm:"rel(fk);on_delete(do_nothing);column(id)"`
	Title             string
	Description       string
	URL               string `orm:"column(url)"`
	Icon              string
	SplashScreenColor string
	PackageName       string
	OneSignalAppID    string    `orm:"column(onesignal_app_id)"`
	ActiveRelease     *Release  `orm:"rel(one)"`
	CreatedAt         time.Time `orm:"auto_now_add;type(datetime)"`
	UpdatedAt         time.Time `orm:"auto_now;type(datetime)"`
}
