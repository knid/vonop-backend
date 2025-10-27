package models

import "github.com/google/uuid"

type NavBarItems struct {
	ID              uuid.UUID        `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID       uuid.UUID        `bun:",notnull,type:uuid,unique"`
	Project         Project          `bun:"rel:belongs-to,join:project_id=id"`
	BackgroundColor string           `bun:",notnull,default:'#fff'"`
	Items           []map[string]any `bun:""`
}
