package models

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID         uuid.UUID `bun:",notnull,unique,type:uuid"`
	User           User      `bun:"rel:belongs-to,join:user_id=id"`
	URL            string    `bun:",notnull"`
	PackageName    string    `bun:",notnull,unique"`
	OneSignalAppID string    `bun:",notnull,unique"`
	CreatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt      time.Time `bun:",soft_delete,nullzero"`
}
