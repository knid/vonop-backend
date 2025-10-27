package models

import "github.com/google/uuid"

type JSInjectData struct {
	ID         uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID  uuid.UUID `bun:",notnull,type:uuid,unique"`
	Project    Project   `bun:"rel:belongs-to,join:project_id=id"`
	Base64Data string    `bun:",notnull,nullzero"`
}
