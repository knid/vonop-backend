package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetRuntimeAppByAppProjectID(appProjectID string) (*models.RuntimeApp, error) {
	var ra models.RuntimeApp
	err := db.DB.NewSelect().Model(&ra).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &ra, nil
}

func (db *Database) CreateRuntimeApp(ra *models.RuntimeApp) (*models.RuntimeApp, error) {
	_, err := db.DB.NewInsert().Model(ra).Exec(context.Background())
	return ra, err
}

func (db *Database) UpdateRuntimeApp(ra *models.RuntimeApp) (*models.RuntimeApp, error) {
	ra.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(ra).WherePK().Exec(context.Background())
	return ra, err
}

func (db *Database) DeleteRuntimeApp(ra *models.RuntimeApp) error {
	_, err := db.DB.NewDelete().Model(ra).Where("id = ?", ra.ID).Exec(context.Background())
	return err
}

func (db *Database) GetRuntimeAppByFlyAppName(flyAppName string) (*models.RuntimeApp, error) {
	var ra models.RuntimeApp
	err := db.DB.NewSelect().Model(&ra).Where("fly_app_name = ?", flyAppName).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &ra, nil
}

// RuntimeAppOrg is a thin row used by the bandwidth-sync cron to walk every
// live Fly app and tally egress by org without N+1 lookups.
type RuntimeAppOrg struct {
	FlyAppName     string
	OrganizationID string
}

// ListRuntimeAppsWithOrg returns every runtime_app row whose Fly app is still
// provisioned (fly_app_name != ''), joined to its parent app_project's
// organization_id. Used by the daily bandwidth-sync cron.
func (db *Database) ListRuntimeAppsWithOrg(ctx context.Context) ([]RuntimeAppOrg, error) {
	var rows []RuntimeAppOrg
	err := db.DB.NewSelect().
		TableExpr("runtime_apps AS r").
		Join("JOIN app_projects AS p ON p.id = r.app_project_id").
		ColumnExpr("r.fly_app_name AS fly_app_name").
		ColumnExpr("p.organization_id AS organization_id").
		Where("r.fly_app_name != ''").
		Where("p.deleted_at IS NULL").
		Scan(ctx, &rows)
	return rows, err
}
