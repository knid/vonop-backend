package models

import (
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID      uuid.UUID `bun:",type:uuid"`
	User        User      `bun:"rel:belongs-to,join:user_id=id"`
	Subject     string    `bun:",notnull"`
	Message     string    `bun:",notnull"`
	Status      string    `bun:",notnull,default:'open'"`
	Priority    string    `bun:",notnull,default:'normal'"`
	Response    string    `bun:""`
	SenderName  string    `bun:""`
	SenderEmail string    `bun:""`
	CreatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
