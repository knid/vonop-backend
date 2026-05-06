package models

import "github.com/google/uuid"

type AndroidStoreAccess struct {
	ID                    uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID                uuid.UUID `bun:",notnull,type:uuid"`
	User                  *User     `bun:"rel:belongs-to,join:user_id=id"`
	// OrganizationID scopes the credential to a workspace. Plan/limits Phase 4
	// (§B): credential quota gates are org-scoped, not user-scoped, so this
	// must be populated for new rows. Backfilled from each user's personal org.
	OrganizationID        uuid.UUID `bun:",type:uuid"`
	Name                  string    `bun:",notnull"`
	ServiceAccountKeyHash string    `bun:",notnull"`
}

type IOSStoreAccess struct {
	ID               uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID           uuid.UUID `bun:",notnull,type:uuid"`
	User             *User     `bun:"rel:belongs-to,join:user_id=id"`
	// OrganizationID — see comment on AndroidStoreAccess.
	OrganizationID   uuid.UUID `bun:",type:uuid"`
	Name             string    `bun:",notnull"`
	TeamID           string    `bun:",notnull"`
	IssuerID         string    `bun:",notnull"`
	PrivateP8KeyHash string    `bun:",notnull"`
}
