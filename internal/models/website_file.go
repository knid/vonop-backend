package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteFile struct {
	ID        uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID uuid.UUID `bun:",notnull,type:uuid"`
	Website   *Website  `bun:"rel:belongs-to,join:website_id=id"`
	FilePath  string    `bun:",notnull"`
	Content   string    `bun:",notnull,type:text"`
	FileType  string    `bun:",notnull"` // html, css, js, json
	Layer     string    `bun:",notnull,default:'frontend'"` // frontend, config
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
}
