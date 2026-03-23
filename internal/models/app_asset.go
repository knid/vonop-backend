package models

import (
	"time"

	"github.com/google/uuid"
)

type AppAsset struct {
	ID           uuid.UUID   `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID uuid.UUID   `bun:",notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	FileName     string      `bun:",notnull"`
	ContentType  string      `bun:",notnull"`
	FileSize     int64       `bun:",notnull,default:0"`
	S3Key        string      `bun:",notnull"`
	PublicURL    string      `bun:",notnull"`
	CreatedAt    time.Time   `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time   `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time   `bun:",soft_delete,nullzero"`
}
