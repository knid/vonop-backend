package models

import (
	"time"

	"github.com/google/uuid"
)

type AppTemplate struct {
	ID              uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	Name            string         `bun:",notnull"`
	Slug            string         `bun:",notnull,unique"`
	Description     string         `bun:",type:text,default:''"`
	Tags            []string       `bun:",type:jsonb,default:'[]'"`
	TemplateType    string         `bun:",notnull,default:'web'"` // "web", "mobile", "both"
	PreviewImageKey string         `bun:",default:''"`
	PreviewURL      string         `bun:",default:''"`
	Theme           []byte         `bun:",type:jsonb,default:'{}'"`
	DataModels      []byte         `bun:",type:jsonb,default:'[]'"`
	Settings        map[string]any `bun:",type:jsonb,default:'{}'"`
	ProjectType     string         `bun:",notnull,default:'app'"` // "app", "ecommerce"
	SourceRepoID    string         `bun:",default:''"`
	Published       bool           `bun:",notnull,default:false"`
	CreatedByID     uuid.UUID      `bun:",notnull,type:uuid"`
	CreatedBy       *User          `bun:"rel:belongs-to,join:created_by_id=id"`
	SortOrder       int            `bun:",notnull,default:0"`
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt       time.Time      `bun:",soft_delete,nullzero"`
}
