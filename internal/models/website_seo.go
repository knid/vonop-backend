package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteSEO struct {
	ID                uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID         uuid.UUID `bun:",notnull,type:uuid,unique"`
	Website           *Website  `bun:"rel:belongs-to,join:website_id=id"`
	MetaTitle         string    `bun:",nullzero"`
	MetaDescription   string    `bun:",nullzero,type:text"`
	OGImage           string    `bun:",nullzero"`
	OGTitle           string    `bun:",nullzero"`
	OGDescription     string    `bun:",nullzero,type:text"`
	FaviconURL        string    `bun:",nullzero"`
	GoogleAnalyticsID string    `bun:",nullzero"`
	Keywords          []string  `bun:",type:text[],default:'{}'"`
	CreatedAt         time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
