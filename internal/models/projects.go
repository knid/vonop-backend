package models

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID      uuid.UUID `bun:",notnull,type:uuid"`
	User        *User     `bun:"rel:belongs-to,join:user_id=id"`
	URL         string    `bun:",notnull"`
	PackageName string    `bun:",notnull,unique"`
	IconPath    string    `bun:",notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
