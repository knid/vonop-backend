package models

import (
	"github.com/google/uuid"
)

type MobileConversation struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID uuid.UUID `bun:",notnull,type:uuid,unique"`
	Project   Project   `bun:"rel:belongs-to,join:project_id=id"`
	Messages  []byte    `bun:",type:jsonb,default:'[]'"`
	Summary   string    `bun:",nullzero,type:text"`
}
