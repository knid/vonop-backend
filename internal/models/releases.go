package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Release status enum — covers the full Apple/Google publishing lifecycle.
//
// Lifecycle (happy path):
//
//	draft -> uploaded -> in_review -> approved -> live
//
// Lifecycle (sad paths):
//
//	uploaded -> rejected (Apple metadata/binary rejection)
//	uploaded -> manually_rejected (developer pulled it back)
//	uploaded -> invalid (Apple INVALID_BINARY)
//	live -> paused (phased rollout halted via HaltPhasedRollout)
//	live -> rolled_back (operator rolled back to a previous release)
//
// Strings are stable wire values — surfaced to the dashboard, used as
// PostHog event property values, persisted in the DB. Do not rename
// without coordinating with downstream consumers.
const (
	ReleaseStatusDraft            = "draft"
	ReleaseStatusUploaded         = "uploaded"
	ReleaseStatusInReview         = "in_review"
	ReleaseStatusApproved         = "approved"
	ReleaseStatusLive             = "live"
	ReleaseStatusRejected         = "rejected"
	ReleaseStatusManuallyRejected = "manually_rejected"
	ReleaseStatusInvalid          = "invalid"
	ReleaseStatusPaused           = "paused"
	ReleaseStatusRolledBack       = "rolled_back"
)

// IsTerminalReleaseStatus returns true for statuses that should not transition
// to anything else without an explicit rollback path. Used by MarkStatus to
// keep transitions idempotent.
func IsTerminalReleaseStatus(s string) bool {
	switch s {
	case ReleaseStatusLive,
		ReleaseStatusRejected,
		ReleaseStatusManuallyRejected,
		ReleaseStatusInvalid,
		ReleaseStatusRolledBack:
		return true
	}
	return false
}

// Release tracks a single submission of a build (via Codemagic) to either
// Apple App Store Connect or Google Play. One Release per
// (app_project, platform, track, version_code) tuple. Many Builds can map
// to one Release (e.g. a re-upload after a metadata rejection reuses the
// same version_code), but typically it's 1:1.
//
// "Production release" is just a Release where Track == "production" /
// "testflight" depending on the platform; HaltPhasedRollout / RollbackRelease
// operate on these.
//
// VersionNumber kept as a soft-required column to preserve the legacy stub
// schema (older deployments may still have rows). New writes should always
// set both VersionCode and VersionNumber to the same value for now.
type Release struct {
	bun.BaseModel `bun:"table:releases,alias:r" json:"-"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID   uuid.UUID `bun:",type:uuid" json:"app_project_id"`
	OrganizationID uuid.UUID `bun:",type:uuid" json:"organization_id"`
	Platform       string    `bun:",notnull,type:text" json:"platform"`
	Track          string    `bun:",notnull,type:text" json:"track"`
	VersionCode    int       `bun:",notnull,type:int" json:"version_code"`
	// VersionNumber kept for backwards compatibility with the original stub
	// schema. Writers populate it with the same value as VersionCode.
	VersionNumber    int    `bun:"version_number,nullzero,default:0" json:"version_number"`
	MarketingVersion string `bun:",type:text" json:"marketing_version"`
	Status           string `bun:",notnull,type:text,default:'draft'" json:"status"`
	StatusMessage    string `bun:",type:text" json:"status_message"`
	// CodemagicTaskID is the AppBuildTask whose Codemagic deploy workflow
	// produced this release (the submission, not the build).
	CodemagicTaskID *uuid.UUID `bun:",type:uuid,nullzero" json:"codemagic_task_id,omitempty"`
	// BuildID is the underlying Build (or AppBuildTask) row that produced the
	// uploaded artifact.
	BuildID *uuid.UUID `bun:",type:uuid,nullzero" json:"build_id,omitempty"`
	// StoreReleaseID is ASC AppStoreVersion.id (iOS) or
	// edits/{editId}/tracks/{track} version code (Android). Empty on draft.
	StoreReleaseID     string  `bun:",type:text" json:"store_release_id"`
	PhasedReleaseState string  `bun:",type:text" json:"phased_release_state"` // "INACTIVE" | "ACTIVE" | "PAUSED" | "COMPLETE"
	RolloutFraction    float64 `bun:",type:double precision,default:0" json:"rollout_fraction"`
	// RolledBackFromReleaseID is set on a Release that was promoted to live
	// after a rollback — points back to the Release that was halted. The
	// halted Release gets its status set to "rolled_back".
	RolledBackFromReleaseID *uuid.UUID `bun:",type:uuid,nullzero" json:"rolled_back_from_release_id,omitempty"`

	SubmittedAt      *time.Time `bun:",type:timestamptz" json:"submitted_at,omitempty"`
	ApprovedAt       *time.Time `bun:",type:timestamptz" json:"approved_at,omitempty"`
	LiveAt           *time.Time `bun:",type:timestamptz" json:"live_at,omitempty"`
	RejectedAt       *time.Time `bun:",type:timestamptz" json:"rejected_at,omitempty"`
	RejectionMessage string     `bun:",type:text" json:"rejection_message,omitempty"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
	DeletedAt time.Time `bun:",soft_delete,nullzero" json:"-"`
}

// Track values — exported for callers who need to compare without
// hardcoding strings.
const (
	ReleaseTrackInternal   = "internal"
	ReleaseTrackAlpha      = "alpha"
	ReleaseTrackBeta       = "beta"
	ReleaseTrackProduction = "production"
	ReleaseTrackTestFlight = "testflight"
)

// Platform values — same intent as the Deployment.Platforms array.
const (
	ReleasePlatformIOS     = "ios"
	ReleasePlatformAndroid = "android"
)
