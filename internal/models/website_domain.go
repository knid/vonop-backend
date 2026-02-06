package models

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteDomain struct {
	ID                 uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID          uuid.UUID `bun:",notnull,type:uuid"`
	Website            *Website  `bun:"rel:belongs-to,join:website_id=id"`
	Domain             string    `bun:",notnull,unique"`
	VerificationStatus string    `bun:",notnull,default:'pending'"` // pending, verified, failed
	VerificationToken  string    `bun:",notnull"`
	SSLStatus          string    `bun:",notnull,default:'pending'"` // pending, active, failed
	CreatedAt          time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt          time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	VerifiedAt         time.Time `bun:",nullzero"`
	DeletedAt          time.Time `bun:",soft_delete,nullzero"`
}
