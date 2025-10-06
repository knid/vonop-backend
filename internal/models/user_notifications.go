package models

import (
	"time"

	"github.com/google/uuid"
)

type UserNotifications struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID    uuid.UUID `bun:",notnull,type:uuid"`
	User      *User     `bun:"rel:belongs-to,join:user_id=id"`
	Content   string    `bun:",nullzero"`
	Readed    bool      `bun:",nullzero,notnull"`
	CreatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
}
