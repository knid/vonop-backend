package models

import (
	"time"

	"github.com/google/uuid"
)

type FormField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

type WebsiteForm struct {
	ID                uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	WebsiteID         uuid.UUID      `bun:",notnull,type:uuid"`
	Website           *Website       `bun:"rel:belongs-to,join:website_id=id"`
	FormName          string         `bun:",notnull"`
	Fields            []FormField    `bun:",type:jsonb,default:'[]'"`
	RedirectURL       string         `bun:",nullzero"`
	EmailNotification bool           `bun:",notnull,default:false"`
	NotifyEmail       string         `bun:",nullzero"`
	CreatedAt         time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt         time.Time      `bun:",soft_delete,nullzero"`
}

type WebsiteFormSubmission struct {
	ID        uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	FormID    uuid.UUID      `bun:",notnull,type:uuid"`
	Form      *WebsiteForm   `bun:"rel:belongs-to,join:form_id=id"`
	Data      map[string]any `bun:",type:jsonb,default:'{}'"`
	IPAddress string         `bun:",nullzero"`
	UserAgent string         `bun:",nullzero,type:text"`
	CreatedAt time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}
