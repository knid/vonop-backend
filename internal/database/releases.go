package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// UpsertRelease writes a Release row, conflating on the unique tuple
// (app_project_id, platform, track, version_code). On conflict it merges
// the incoming "interesting" fields (status, store ids, phased state, rollout)
// but never overwrites previously-set timestamps with zeroes.
func (db *Database) UpsertRelease(ctx context.Context, r *models.Release) (*models.Release, error) {
	if r == nil {
		return nil, fmt.Errorf("release is required")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.Platform == "" || r.Track == "" {
		return nil, fmt.Errorf("release platform and track are required")
	}
	if r.Status == "" {
		r.Status = models.ReleaseStatusDraft
	}
	// Mirror VersionCode → VersionNumber for legacy schema compatibility.
	if r.VersionNumber == 0 {
		r.VersionNumber = r.VersionCode
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now

	// IMPORTANT: the Release model is declared with `bun:"...,alias:r"`,
	// so Bun emits `INSERT INTO releases AS r ...`. The conflict-update SET
	// clause must therefore reference the existing row as `r.<col>`, not
	// `releases.<col>` — Postgres rejects the table-name form with
	// "invalid reference to FROM-clause entry for table 'releases'".
	_, err := db.DB.NewInsert().Model(r).
		On("CONFLICT (app_project_id, platform, track, version_code) WHERE deleted_at IS NULL DO UPDATE").
		Set("status = EXCLUDED.status").
		Set("status_message = EXCLUDED.status_message").
		Set("marketing_version = EXCLUDED.marketing_version").
		Set("codemagic_task_id = COALESCE(EXCLUDED.codemagic_task_id, r.codemagic_task_id)").
		Set("build_id = COALESCE(EXCLUDED.build_id, r.build_id)").
		Set("store_release_id = CASE WHEN EXCLUDED.store_release_id <> '' THEN EXCLUDED.store_release_id ELSE r.store_release_id END").
		Set("phased_release_state = CASE WHEN EXCLUDED.phased_release_state <> '' THEN EXCLUDED.phased_release_state ELSE r.phased_release_state END").
		Set("rollout_fraction = EXCLUDED.rollout_fraction").
		Set("rolled_back_from_release_id = COALESCE(EXCLUDED.rolled_back_from_release_id, r.rolled_back_from_release_id)").
		Set("submitted_at = COALESCE(r.submitted_at, EXCLUDED.submitted_at)").
		Set("approved_at = COALESCE(r.approved_at, EXCLUDED.approved_at)").
		Set("live_at = COALESCE(r.live_at, EXCLUDED.live_at)").
		Set("rejected_at = COALESCE(r.rejected_at, EXCLUDED.rejected_at)").
		Set("rejection_message = CASE WHEN EXCLUDED.rejection_message <> '' THEN EXCLUDED.rejection_message ELSE r.rejection_message END").
		Set("updated_at = EXCLUDED.updated_at").
		Set("version_number = EXCLUDED.version_number").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("upsert release: %w", err)
	}
	return r, nil
}

// GetRelease loads a single Release by primary key.
func (db *Database) GetRelease(ctx context.Context, id uuid.UUID) (*models.Release, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("release id is required")
	}
	var r models.Release
	err := db.DB.NewSelect().Model(&r).Where("id = ?", id).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetReleaseByStoreID looks up a release by its store-side identifier
// (ASC appStoreVersion.id for iOS, edit-id-or-version-code string for Android).
// Useful when receiving a webhook that only carries the store-side reference.
func (db *Database) GetReleaseByStoreID(ctx context.Context, platform, storeReleaseID string) (*models.Release, error) {
	if storeReleaseID == "" {
		return nil, fmt.Errorf("store_release_id is required")
	}
	var r models.Release
	err := db.DB.NewSelect().Model(&r).
		Where("platform = ?", platform).
		Where("store_release_id = ?", storeReleaseID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListReleasesByProject returns all releases for an AppProject, newest first.
func (db *Database) ListReleasesByProject(ctx context.Context, appProjectID uuid.UUID) ([]models.Release, error) {
	if appProjectID == uuid.Nil {
		return nil, fmt.Errorf("app_project_id is required")
	}
	var out []models.Release
	err := db.DB.NewSelect().Model(&out).
		Where("app_project_id = ?", appProjectID).
		Order("created_at DESC").
		Scan(ctx)
	return out, err
}

// ListReleasesByOrg returns all releases for an Organization, newest first.
func (db *Database) ListReleasesByOrg(ctx context.Context, orgID uuid.UUID) ([]models.Release, error) {
	if orgID == uuid.Nil {
		return nil, fmt.Errorf("organization_id is required")
	}
	var out []models.Release
	err := db.DB.NewSelect().Model(&out).
		Where("organization_id = ?", orgID).
		Order("created_at DESC").
		Scan(ctx)
	return out, err
}

// ListReleasesInNonTerminalStatus returns releases that the polling worker
// should re-check on its next tick. Statuses excluded: live, rejected,
// manually_rejected, invalid, rolled_back, draft (draft means we haven't
// even uploaded yet — there's nothing to poll).
func (db *Database) ListReleasesInNonTerminalStatus(ctx context.Context) ([]models.Release, error) {
	var out []models.Release
	err := db.DB.NewSelect().Model(&out).
		Where("status IN (?, ?, ?, ?)",
			models.ReleaseStatusUploaded,
			models.ReleaseStatusInReview,
			models.ReleaseStatusApproved,
			models.ReleaseStatusPaused).
		Order("created_at DESC").
		Scan(ctx)
	return out, err
}

// GetCurrentLiveRelease returns the most-recently-live release for a
// (project, platform). Returns nil, nil when nothing is live yet.
func (db *Database) GetCurrentLiveRelease(ctx context.Context, appProjectID uuid.UUID, platform string) (*models.Release, error) {
	if appProjectID == uuid.Nil {
		return nil, fmt.Errorf("app_project_id is required")
	}
	var r models.Release
	err := db.DB.NewSelect().Model(&r).
		Where("app_project_id = ?", appProjectID).
		Where("platform = ?", platform).
		Where("status = ?", models.ReleaseStatusLive).
		Order("live_at DESC NULLS LAST").
		Limit(1).
		Scan(ctx)
	if err != nil {
		// Translate "no rows" into a nil result — this is a normal state for
		// projects that haven't shipped yet.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// MarkReleaseStatus transitions a release to a new status, idempotently
// touching the corresponding timestamp column (submitted_at / approved_at /
// live_at / rejected_at). occurredAt may be the zero value, in which case
// time.Now() is used.
//
// Returns the updated row. If the release is already in a terminal status
// other than the requested one, the row is left untouched and returned as-is
// (callers can detect this by comparing rows).
func (db *Database) MarkReleaseStatus(ctx context.Context, id uuid.UUID, status, statusMessage string, occurredAt time.Time) (*models.Release, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("release id is required")
	}
	r, err := db.GetRelease(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}
	// Idempotent: same status + non-empty message becomes a no-op.
	if r.Status == status {
		// Update status message + updated_at, but don't shuffle the lifecycle
		// timestamps when status hasn't changed.
		if statusMessage != "" && r.StatusMessage != statusMessage {
			r.StatusMessage = statusMessage
			r.UpdatedAt = time.Now().UTC()
			if _, err := db.DB.NewUpdate().Model(r).
				Column("status_message", "updated_at").
				Where("id = ?", r.ID).
				Exec(ctx); err != nil {
				return nil, fmt.Errorf("update status message: %w", err)
			}
		}
		return r, nil
	}
	// Block silent transitions out of terminal states. Callers that need to
	// move out of, e.g., live → rolled_back must call RollbackRelease, which
	// flips the gate explicitly.
	if models.IsTerminalReleaseStatus(r.Status) && status != models.ReleaseStatusRolledBack {
		return r, nil
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	r.Status = status
	if statusMessage != "" {
		r.StatusMessage = statusMessage
	}
	r.UpdatedAt = occurredAt
	cols := []string{"status", "status_message", "updated_at"}
	switch status {
	case models.ReleaseStatusUploaded:
		if r.SubmittedAt == nil {
			ts := occurredAt
			r.SubmittedAt = &ts
			cols = append(cols, "submitted_at")
		}
	case models.ReleaseStatusInReview:
		if r.SubmittedAt == nil {
			ts := occurredAt
			r.SubmittedAt = &ts
			cols = append(cols, "submitted_at")
		}
	case models.ReleaseStatusApproved:
		if r.ApprovedAt == nil {
			ts := occurredAt
			r.ApprovedAt = &ts
			cols = append(cols, "approved_at")
		}
	case models.ReleaseStatusLive:
		if r.LiveAt == nil {
			ts := occurredAt
			r.LiveAt = &ts
			cols = append(cols, "live_at")
		}
		// approved_at gets a synthetic value if it wasn't set (e.g. fast-track).
		if r.ApprovedAt == nil {
			ts := occurredAt
			r.ApprovedAt = &ts
			cols = append(cols, "approved_at")
		}
	case models.ReleaseStatusRejected,
		models.ReleaseStatusManuallyRejected,
		models.ReleaseStatusInvalid:
		if r.RejectedAt == nil {
			ts := occurredAt
			r.RejectedAt = &ts
			cols = append(cols, "rejected_at")
		}
		if statusMessage != "" {
			r.RejectionMessage = statusMessage
			cols = append(cols, "rejection_message")
		}
	}
	if _, err := db.DB.NewUpdate().Model(r).
		Column(cols...).
		Where("id = ?", r.ID).
		Exec(ctx); err != nil {
		return nil, fmt.Errorf("update release status: %w", err)
	}
	return r, nil
}

// SetReleasePhasedState updates the phased rollout state + status_message.
// Used by HaltPhasedRollout to persist Wemob's local view even when the
// upstream API call fails.
func (db *Database) SetReleasePhasedState(ctx context.Context, id uuid.UUID, phasedState string, status string, statusMessage string) (*models.Release, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("release id is required")
	}
	r, err := db.GetRelease(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}
	now := time.Now().UTC()
	r.PhasedReleaseState = phasedState
	if status != "" {
		r.Status = status
	}
	if statusMessage != "" {
		r.StatusMessage = statusMessage
	}
	r.UpdatedAt = now
	if _, err := db.DB.NewUpdate().Model(r).
		Column("phased_release_state", "status", "status_message", "updated_at").
		Where("id = ?", r.ID).
		Exec(ctx); err != nil {
		return nil, fmt.Errorf("update phased state: %w", err)
	}
	return r, nil
}

// SetReleaseRollbackTarget records that release `targetID` was promoted as
// the rollback replacement for `fromID`. Both rows are updated atomically
// in a single transaction.
func (db *Database) SetReleaseRollbackTarget(ctx context.Context, fromID, targetID uuid.UUID) error {
	if fromID == uuid.Nil || targetID == uuid.Nil {
		return fmt.Errorf("from and target release ids are required")
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	if _, err := tx.NewUpdate().
		Model((*models.Release)(nil)).
		Set("status = ?", models.ReleaseStatusRolledBack).
		Set("updated_at = ?", now).
		Where("id = ?", fromID).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark rolled-back: %w", err)
	}
	if _, err := tx.NewUpdate().
		Model((*models.Release)(nil)).
		Set("rolled_back_from_release_id = ?", fromID).
		Set("updated_at = ?", now).
		Where("id = ?", targetID).
		Exec(ctx); err != nil {
		return fmt.Errorf("set rollback target: %w", err)
	}
	return tx.Commit()
}

