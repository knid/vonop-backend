package database

import (
	"context"
	"fmt"
)

// CountRowsWithoutOrg returns the count of rows in `table` where organization_id is NULL.
// `table` is hardcoded against a whitelist to prevent SQL injection.
func (db *Database) CountRowsWithoutOrg(table string) (int, error) {
	if !isAllowedMigrationTable(table) {
		return 0, fmt.Errorf("table %q not allowed", table)
	}
	var count int
	q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE organization_id IS NULL`, table)
	err := db.DB.QueryRowContext(context.Background(), q).Scan(&count)
	return count, err
}

// BulkAssignOrgFromPersonalOrg fills organization_id on every row of `table`
// using the row's user_id → user's personal org mapping. Returns rows affected.
func (db *Database) BulkAssignOrgFromPersonalOrg(table string) (int, error) {
	if !isAllowedMigrationTable(table) {
		return 0, fmt.Errorf("table %q not allowed", table)
	}
	q := fmt.Sprintf(`
		UPDATE %s t
		SET organization_id = o.id
		FROM organizations o
		WHERE t.organization_id IS NULL
		  AND t.user_id IS NOT NULL
		  AND o.owner_id = t.user_id
		  AND o.is_personal = true
		  AND o.deleted_at IS NULL
	`, table)
	res, err := db.DB.ExecContext(context.Background(), q)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func isAllowedMigrationTable(table string) bool {
	switch table {
	case "apple_connections", "google_play_connections", "deployments", "api_keys", "webhook_endpoints":
		return true
	}
	return false
}
