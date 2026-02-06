package models

import (
	"time"

	"github.com/google/uuid"
)

type SnapshotFile struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	FileType string `json:"file_type"`
	Layer    string `json:"layer"`
}

type WebsiteSnapshot struct {
	ID          uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID   uuid.UUID      `bun:",notnull,type:uuid"`
	Website     *Website       `bun:"rel:belongs-to,join:website_id=id"`
	Files       []SnapshotFile `bun:",type:jsonb,default:'[]'"`
	Description string         `bun:",notnull,type:text"`
	Source      string         `bun:",notnull,default:'auto'"` // auto, manual
	CreatedBy   uuid.UUID      `bun:",notnull,type:uuid"`
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
