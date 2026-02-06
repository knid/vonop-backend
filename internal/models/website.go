package models

import (
	"time"

	"github.com/google/uuid"
)

type Website struct {
	ID           uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID       uuid.UUID      `bun:",notnull,type:uuid"`
	User         *User          `bun:"rel:belongs-to,join:user_id=id"`
	ProjectID    uuid.UUID      `bun:",type:uuid,nullzero"`
	Name         string         `bun:",notnull"`
	Slug         string         `bun:",notnull,unique"`
	Prompt       string         `bun:",notnull,type:text"`
	ColorScheme  map[string]any `bun:",type:jsonb,default:'{}'"`
	Status       string         `bun:",notnull,default:'draft'"`
	PublishedURL string         `bun:",nullzero"`
	TemplateID   uuid.UUID      `bun:",type:uuid,nullzero"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time      `bun:",soft_delete,nullzero"`
}

type WebsitePage struct {
	ID          uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID   uuid.UUID      `bun:",notnull,type:uuid"`
	Website     *Website       `bun:"rel:belongs-to,join:website_id=id"`
	Path        string         `bun:",notnull"`
	Title       string         `bun:",notnull"`
	HTMLContent string         `bun:",notnull,type:text"`
	CSSContent  string         `bun:",type:text,nullzero"`
	JSContent   string         `bun:",type:text,nullzero"`
	Metadata    map[string]any `bun:",type:jsonb,default:'{}'"`
	SortOrder   int            `bun:",notnull,default:0"`
	CreatedAt   time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt   time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt   time.Time      `bun:",soft_delete,nullzero"`
}

type WebsiteAsset struct {
	ID           uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID    uuid.UUID `bun:",notnull,type:uuid"`
	Website      *Website  `bun:"rel:belongs-to,join:website_id=id"`
	Type         string    `bun:",notnull"`
	S3Path       string    `bun:",notnull"`
	OriginalName string    `bun:",notnull"`
	ContentType  string    `bun:",notnull"`
	Size         int64     `bun:",notnull,default:0"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time `bun:",soft_delete,nullzero"`
}
