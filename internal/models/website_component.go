package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteComponent struct {
	ID           uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name         string    `bun:",notnull"`
	Category     string    `bun:",notnull"` // hero, pricing, faq, testimonials, cta, footer, contact, features, team, gallery
	HTMLTemplate string    `bun:",notnull,type:text"`
	PreviewImage string    `bun:",nullzero"`
	Tags         []string  `bun:",type:text[],default:'{}'"`
	IsActive     bool      `bun:",notnull,default:true"`
	CreatedAt    time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
