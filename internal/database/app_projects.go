package database

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetAppProjectsByUserID(userID string) ([]models.AppProject, error) {
	var projects []models.AppProject
	err := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("user_id = ?", userID).Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectsByUserIDAndTypes(userID string, types []string) ([]models.AppProject, error) {
	var projects []models.AppProject
	q := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("user_id = ?", userID)
	if len(types) > 0 {
		q = q.Where("project_type IN (?)", bun.In(types))
	}
	err := q.Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectsByOrgID(orgID string) ([]models.AppProject, error) {
	var projects []models.AppProject
	err := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("organization_id = ?", orgID).Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectsByOrgIDAndTypes(orgID string, types []string) ([]models.AppProject, error) {
	var projects []models.AppProject
	q := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("organization_id = ?", orgID)
	if len(types) > 0 {
		q = q.Where("project_type IN (?)", bun.In(types))
	}
	err := q.Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectByID(id string) (*models.AppProject, error) {
	if cached, ok := getProjectFromCache(id); ok {
		return cached, nil
	}
	var project models.AppProject
	err := db.DB.NewSelect().Model(&project).Where("id = ?", id).Scan(context.Background())
	if err == nil {
		setProjectInCache(id, &project)
	}
	return &project, err
}

func (db *Database) GetAppProjectBySlug(slug string) (*models.AppProject, error) {
	var project models.AppProject
	err := db.DB.NewSelect().Model(&project).Where("slug = ?", slug).Scan(context.Background())
	return &project, err
}

func (db *Database) CreateAppProject(p *models.AppProject) (*models.AppProject, error) {
	_, err := db.DB.NewInsert().Model(p).Exec(context.Background())
	return p, err
}

func (db *Database) UpdateAppProject(p *models.AppProject) (*models.AppProject, error) {
	// Guard against bloated JSONB columns
	if len(p.Theme) > 50*1024 {
		p.Theme = []byte("{}")
		slog.Warn("UpdateAppProject: theme too large, reset to empty", "project_id", p.ID, "size", len(p.Theme))
	}
	if len(p.DataModels) > 1<<20 {
		p.DataModels = []byte("[]")
		slog.Warn("UpdateAppProject: data_models too large, reset to empty", "project_id", p.ID, "size", len(p.DataModels))
	}
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(p.ID.String())
	}
	return p, err
}

func (db *Database) DeleteAppProject(p *models.AppProject) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(p.ID.String())
	}
	return err
}

func (db *Database) UpdateAppProjectSandboxID(projectID string, sandboxID string) error {
	_, err := db.DB.NewUpdate().Model((*models.AppProject)(nil)).
		Set("sandbox_id = ?", sandboxID).
		Where("id = ?", projectID).
		Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(projectID)
	}
	return err
}

// SetAppProjectV1ProjectID persists the legacy v1 Project ID into
// app_projects.settings via jsonb_set so the dashboard's Native Builds tab
// can resolve this mobile app to its Codemagic build pipeline entry.
// Uses jsonb_set to avoid wiping other setting keys, and avoids a full
// UpdateAppProject call that would require a hydrated struct.
func (db *Database) SetAppProjectV1ProjectID(projectID string, v1ProjectID string) error {
	_, err := db.DB.NewUpdate().Model((*models.AppProject)(nil)).
		Set(`settings = jsonb_set(coalesce(settings, '{}'::jsonb), '{v1_project_id}', ?::jsonb, true)`, `"`+v1ProjectID+`"`).
		Where("id = ?", projectID).
		Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(projectID)
	}
	return err
}

// UpdateAppProjectStatus performs a targeted single-column update on
// app_projects.status. Used by agent.Run to flip "draft" → "ready" without
// risking a constraint failure on unrelated JSONB columns the way a full
// UpdateAppProject would.
func (db *Database) UpdateAppProjectStatus(projectID string, status string) error {
	_, err := db.DB.NewUpdate().Model((*models.AppProject)(nil)).
		Set("status = ?", status).
		Where("id = ?", projectID).
		Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(projectID)
	}
	return err
}

// BackfillProjectStatusReady is a one-shot startup helper that flips any
// project with status="draft" to "ready" if it has at least one completed
// agent run. Fixes legacy data left behind by the pre-2026-04 agent which
// never updated project status after a successful first generation.
func (db *Database) BackfillProjectStatusReady() (int, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("status = ?", "ready").
		Where("status = ?", "draft").
		Where("id IN (SELECT app_project_id FROM app_agent_runs WHERE status = ?)", "completed").
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (db *Database) CountAppProjectsByUserID(userID string) (int, error) {
	count, err := db.DB.NewSelect().Model((*models.AppProject)(nil)).Where("user_id = ?", userID).Count(context.Background())
	return count, err
}

// CountAppProjectsByUserIDAndType counts total app projects for a user.
// projectType filter values:
//   "mobile"           — counts mobile_app_native rows (use for quota gates)
//   "mobile_app_native" — synonym for "mobile"
//   ""                 — counts non-mobile (web/ecommerce)
func (db *Database) CountAppProjectsByUserIDAndType(userID string, projectType string) (int, error) {
	q := db.DB.NewSelect().Model((*models.AppProject)(nil)).
		Where("user_id = ?", userID).
		Where("status != ?", "generating")
	switch projectType {
	case "mobile", "mobile_app_native":
		q = q.Where("project_type = ?", "mobile_app_native")
	default:
		q = q.Where("project_type != ?", "mobile_app_native")
	}
	return q.Count(context.Background())
}

// ReassignProjectsToOrgOwner reassigns UserID on all of fromUserID's projects
// in the given org to toUserID. Returns the IDs of affected projects so the
// caller can invalidate their per-row cache entries — AuthorizeProjectAccess
// reads UserID from the cached row, and a stale cache would re-grant access
// to the just-removed member.
func (db *Database) ReassignProjectsToOrgOwner(ctx context.Context, orgID, fromUserID, toUserID uuid.UUID) ([]string, error) {
	var ids []string
	if fromUserID == toUserID {
		return ids, nil
	}
	err := db.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("user_id = ?", toUserID).
		Set("updated_at = NOW()").
		Where("organization_id = ?", orgID).
		Where("user_id = ?", fromUserID).
		Where("deleted_at IS NULL").
		Returning("id").
		Scan(ctx, &ids)
	return ids, err
}

// ListAppProjectsWithoutOrg returns AppProjects with NULL organization_id.
// Used by the org-foundation backfill.
func (db *Database) ListAppProjectsWithoutOrg() ([]models.AppProject, error) {
	var projects []models.AppProject
	err := db.DB.NewSelect().
		Model(&projects).
		Where("organization_id IS NULL").
		Where("deleted_at IS NULL").
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return projects, nil
}

// AssignAppProjectOrg sets organization_id on a single AppProject row.
// Used by the org-foundation backfill.
func (db *Database) AssignAppProjectOrg(projectID string, orgID uuid.UUID) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("organization_id = ?", orgID).
		Where("id = ?", projectID).
		Exec(context.Background())
	return err
}

