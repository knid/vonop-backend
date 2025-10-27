package models

import (
	"time"

	"github.com/google/uuid"
)

type Device struct {
	ID          uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	DeviceToken uuid.UUID `bun:",notnull,unique,type:uuid"`
	Platform    string    `bun:",notnull"`
	Model       string    `bun:",notnull"`
	CreatedAt   time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time `bun:",soft_delete,nullzero"`
}
