package models

import (
	"time"

	"github.com/google/uuid"
)

type AppTheme struct {
	ColorScheme     map[string]any `json:"color_scheme"`
	DarkColorScheme map[string]any `json:"dark_color_scheme,omitempty"`
	Typography      map[string]any `json:"typography,omitempty"`
	Spacing         map[string]any `json:"spacing,omitempty"`
	BorderRadius    map[string]any `json:"border_radius,omitempty"`
}

type AppProject struct {
	ID              uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	UserID          uuid.UUID      `bun:",notnull,type:uuid"`
	User            *User          `bun:"rel:belongs-to,join:user_id=id"`
	OrganizationID  uuid.UUID      `bun:",type:uuid,nullzero"`
	Organization    *Organization  `bun:"rel:belongs-to,join:organization_id=id"`
	Name            string         `bun:",notnull"`
	Slug            string         `bun:",notnull,unique"`
	Prompt          string         `bun:",notnull,type:text"`
	ProjectType     string         `bun:",notnull,default:'app'"` // "app" or "ecommerce"
	AST             []byte         `bun:",type:jsonb,default:'[]'"`
	Theme           []byte         `bun:",type:jsonb,default:'{}'"` // stores AppTheme as JSON
	DataModels      []byte         `bun:",type:jsonb,default:'[]'"` // stores data model definitions
	Settings        map[string]any `bun:",type:jsonb,default:'{}'"`
	ScreenshotS3Key string         `bun:",notnull,default:''"`
	LogoS3Key       string         `bun:",notnull,default:''"`
	SandboxID       string         `bun:",notnull,default:''"`
	Status          string         `bun:",notnull,default:'draft'"` // draft, generating, ready, deployed
	CreatedAt       time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt       time.Time      `bun:",soft_delete,nullzero"`
}
