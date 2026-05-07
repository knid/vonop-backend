package models

import (
	"time"

	"github.com/google/uuid"
)

type Build struct {
	ID                  uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID           uuid.UUID `bun:",notnull,type:uuid"`
	Project             Project   `bun:"rel:belongs-to,join:project_id=id"`
	// AppProjectID is the new direct FK target (replaces the v1 shadow Project
	// indirection). Pointer + nullzero through Phase 1; later phases will tighten.
	AppProjectID *uuid.UUID  `bun:"app_project_id,type:uuid,nullzero"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	Active              bool      `bun:",notnull"`
	Title               string    `bun:",notnull"`
	Description         string    `bun:",notnull"`
	Icon                string    `bun:",notnull"`
	SplashScreenColor   string    `bun:",notnull"`
	NavBarStyle         string    `bun:",notnull"`
	RemoveWemobBranding bool      `bun:",notnull,default:false"`
	BuildNumber         int       `bun:",notnull"`
	VersionString       string    `bun:",notnull"`
	CreatedAt           time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt           time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt           time.Time `bun:",soft_delete,nullzero"`
}
