package models

import (
	"time"

	"github.com/google/uuid"
)

type AppGeneratedFile struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Platform     string      `bun:",notnull"` // "nextjs", "flutter"
	FilePath     string      `bun:",notnull"`
	Content      string      `bun:",notnull,type:text"`
	FileType     string      `bun:",notnull"` // "tsx", "ts", "css", "json", "dart", "yaml"
	IsGenerated  bool        `bun:",notnull,default:true"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
