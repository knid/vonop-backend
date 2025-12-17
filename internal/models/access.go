package models

import "github.com/google/uuid"

type AndroidStoreAccess struct {
	ID                    uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID                uuid.UUID `bun:",notnull,type:uuid"`
	User                  *User     `bun:"rel:belongs-to,join:user_id=id"`
	Name                  string    `bun:",notnull"`
	ServiceAccountKeyHash string    `bun:",notnull"`
}

type IOSStoreAccess struct {
	ID               uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID           uuid.UUID `bun:",notnull,type:uuid"`
	User             *User     `bun:"rel:belongs-to,join:user_id=id"`
	Name             string    `bun:",notnull"`
	TeamID           string    `bun:",notnull"`
	IssuerID         string    `bun:",notnull"`
	PrivateP8KeyHash string    `bun:",notnull"`
}
