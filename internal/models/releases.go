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
	bun.BaseModel `bun:"table:releases,alias:r"`

	ID             uuid.UUID `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID   uuid.UUID `bun:",type:uuid"`
	OrganizationID uuid.UUID `bun:",type:uuid"`
	Platform       string    `bun:",notnull,type:text"`
	Track          string    `bun:",notnull,type:text"`
	VersionCode    int       `bun:",notnull,type:int"`
	// VersionNumber kept for backwards compatibility with the original stub
	// schema. Writers populate it with the same value as VersionCode.
	VersionNumber    int    `bun:"version_number,nullzero,default:0"`
	MarketingVersion string `bun:",type:text"`
	Status           string `bun:",notnull,type:text,default:'draft'"`
	StatusMessage    string `bun:",type:text"`
	// CodemagicTaskID is the AppBuildTask whose Codemagic deploy workflow
	// produced this release (the submission, not the build).
	CodemagicTaskID *uuid.UUID `bun:",type:uuid,nullzero"`
	// BuildID is the underlying Build (or AppBuildTask) row that produced the
	// uploaded artifact.
	BuildID *uuid.UUID `bun:",type:uuid,nullzero"`
	// StoreReleaseID is ASC AppStoreVersion.id (iOS) or
	// edits/{editId}/tracks/{track} version code (Android). Empty on draft.
	StoreReleaseID    string  `bun:",type:text"`
	PhasedReleaseState string `bun:",type:text"` // "INACTIVE" | "ACTIVE" | "PAUSED" | "COMPLETE"
	RolloutFraction    float64 `bun:",type:double precision,default:0"`
	// RolledBackFromReleaseID is set on a Release that was promoted to live
	// after a rollback — points back to the Release that was halted. The
	// halted Release gets its status set to "rolled_back".
	RolledBackFromReleaseID *uuid.UUID `bun:",type:uuid,nullzero"`

	SubmittedAt      *time.Time `bun:",type:timestamptz"`
	ApprovedAt       *time.Time `bun:",type:timestamptz"`
	LiveAt           *time.Time `bun:",type:timestamptz"`
	RejectedAt       *time.Time `bun:",type:timestamptz"`
	RejectionMessage string     `bun:",type:text"`

	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt time.Time `bun:",soft_delete,nullzero"`
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
