package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// BuildAnnotation is a known failure-pattern entry. When a build fails,
// the report handler walks the annotation table, tries each LogRegex
// against the build's error_message, and attaches the first match's ID
// to the AppBuildTask so the UI can render the human-readable
// explanation + suggested fix instead of a raw log dump.
//
// Annotations are seeded at startup from annotations_seed.go and can
// also be edited by operators via the admin routes added in a later
// phase.
type BuildAnnotation struct {
	bun.BaseModel `bun:"table:build_annotations,alias:ba"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	Name           string    `bun:",notnull,unique" json:"name"`              // stable key, e.g. "pod_install_failed"
	LogRegex       string    `bun:",notnull" json:"log_regex"`
	HumanMessage   string    `bun:",notnull,type:text" json:"human_message"`
	SuggestedFix   string    `bun:",notnull,type:text" json:"suggested_fix"`
	RetryStrategy  string    `bun:",nullzero" json:"retry_strategy,omitempty"` // env-var set, e.g. "POD_REPO_UPDATE=1"
	SuccessCount   int       `bun:",notnull,default:0" json:"success_count"`
	FailureCount   int       `bun:",notnull,default:0" json:"failure_count"`
	Enabled        bool      `bun:",notnull,default:true" json:"enabled"`
	CreatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt      time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updated_at"`
}
