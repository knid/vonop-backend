package models

import (
	"time"

	"github.com/google/uuid"
)

type AppBuildTask struct {
	ID uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	// AppProjectID is the canonical FK to AppProject. The legacy v1 Project
	// indirection (ProjectID + Project) was removed 2026-05-07.
	AppProjectID uuid.UUID   `bun:"app_project_id,notnull,type:uuid"`
	AppProject   *AppProject `bun:"rel:belongs-to,join:app_project_id=id"`
	BuildID      uuid.UUID   `bun:",notnull,type:uuid"`
	Build        *Build      `bun:"rel:belongs-to,join:build_id=id"`
	Finished     bool        `bun:",notnull"`
	Platform     string      `bun:",notnull"`
	AutoDeploy   bool        `bun:",notnull,default:false"`
	StatusText   string      `bun:",nullzero"`
	// ErrorMessage captures the last failure message reported by the Codemagic
	// workflow. Surfaced to the user on failed build rows so they can see why
	// a build failed without chasing us in support.
	ErrorMessage string `bun:",nullzero,type:text"`
	// AnnotationID is set by the build-completed webhook when the
	// ErrorMessage matches a known pattern in build_annotations. Nil means
	// either the build succeeded, or no pattern matched.
	AnnotationID *uuid.UUID       `bun:",type:uuid,nullzero"`
	Annotation   *BuildAnnotation `bun:"rel:belongs-to,join:annotation_id=id"`
	// CodemagicBuildID is the Codemagic-side build identifier returned by the
	// trigger API. Stored so we can call Codemagic's POST /builds/{id}/cancel
	// when a user cancels mid-build. Empty for legacy rows or builds whose
	// trigger response we couldn't persist.
	CodemagicBuildID string `bun:"codemagic_build_id,nullzero"`
	ExecuteTime  time.Time        `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt    time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time        `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt    time.Time        `bun:",soft_delete,nullzero"`
}
