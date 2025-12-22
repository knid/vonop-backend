package models

import (
	"time"

	"github.com/google/uuid"
)

type AppStoreCredentials struct {
	ID            uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID        uuid.UUID `bun:",notnull,type:uuid"`
	User          Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name          string    `bun:",notnull"`
	KeyID         string    `bun:",notnull"`
	TeamID        string    `bun:",notnull"`
	IssuerID      string    `bun:",notnull"`
	P8KeyPath     string    `bun:",notnull"`
	APNSP8KeyPath string    `bun:",notnull"`
	CreatedAt     time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt     time.Time `bun:",soft_delete,nullzero"`
}

type GooglePlayCredentials struct {
	ID                 uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID             uuid.UUID `bun:",notnull,type:uuid"`
	User               Project   `bun:"rel:belongs-to,join:user_id=id"`
	Name               string    `bun:",notnull"`
	ServiceAccountPath string    `bun:",notnull"`
	CreatedAt          time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt          time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt          time.Time `bun:",soft_delete,nullzero"`
}
