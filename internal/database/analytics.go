package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

// GetAppUsageEventsByProject returns events for a project within a time range.
func (db *Database) GetAppUsageEventsByProject(projectID string, start, end time.Time) ([]models.AppUsageEvent, error) {
	var events []models.AppUsageEvent
	err := db.DB.NewSelect().
		Model(&events).
		Join("JOIN sessions AS s ON s.id = app_usage_event.session_id").
		Where("s.project_id = ?", projectID).
		Where("app_usage_event.created_at >= ?", start).
		Where("app_usage_event.created_at <= ?", end).
		Order("app_usage_event.created_at DESC").
		Scan(context.Background())
	return events, err
}

func (db *Database) CreateAppUsageEvent(appUsageEvent *models.AppUsageEvent) (*models.AppUsageEvent, error) {
	_, err := db.DB.NewInsert().Model(appUsageEvent).Exec(context.Background())
	return appUsageEvent, err
}

func (db *Database) GetDeviceById(deviceId string) (models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("id = ?", deviceId).Scan(context.Background())
	return device, err
}

func (db *Database) GetDeviceByToken(token string) (*models.Device, error) {
	var device models.Device
	err := db.DB.NewSelect().Model(&device).Where("device_token = ?", token).Scan(context.Background())
	return &device, err
}

func (db *Database) CreateDevice(device *models.Device) (*models.Device, error) {
	_, err := db.DB.NewInsert().Model(device).Exec(context.Background())
	return device, err
}

func (db *Database) UpdateDevice(device *models.Device) (*models.Device, error) {
	_, err := db.DB.NewUpdate().Model(device).Where("id = ?", device.ID).Exec(context.Background())
	return device, err
}

func (db *Database) GetDevicesByProject(projectID string) ([]models.Device, error) {
	var devices []models.Device
	err := db.DB.NewSelect().
		Model(&devices).
		Distinct().
		Join("JOIN sessions AS s ON s.device_id = device.id").
		Where("s.project_id = ?", projectID).
		Scan(context.Background())
	return devices, err
}

func (db *Database) CreateSession(session *models.Session) (*models.Session, error) {
	_, err := db.DB.NewInsert().Model(session).Exec(context.Background())
	return session, err
}

// UpdateSessionFields updates only the specified columns on a session.
func (db *Database) UpdateSessionFields(session *models.Session, columns ...string) error {
	_, err := db.DB.NewUpdate().
		Model(session).
		Column(columns...).
		Where("id = ?", session.ID).
		Exec(context.Background())
	return err
}

func (db *Database) GetSessionsByProject(projectID string, start, end time.Time) ([]models.Session, error) {
	var sessions []models.Session
	err := db.DB.NewSelect().
		Model(&sessions).
		Where("project_id = ?", projectID).
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Order("created_at DESC").
		Scan(context.Background())
	return sessions, err
}

func (db *Database) GetSessionById(sessionId string) (*models.Session, error) {
	var session models.Session
	err := db.DB.NewSelect().Model(&session).Where("id = ?", sessionId).Scan(context.Background())
	return &session, err
}

// GetActiveSessionsWithLastEvent returns active sessions along with the timestamp
// of the last event for each. Done in a single query to avoid N+1.
func (db *Database) GetActiveSessionsWithLastEvent() ([]ActiveSessionInfo, error) {
	var results []ActiveSessionInfo
	err := db.DB.NewSelect().
		TableExpr("sessions AS s").
		ColumnExpr("s.id").
		ColumnExpr("s.duration").
		ColumnExpr("s.created_at").
		ColumnExpr("s.updated_at").
		ColumnExpr("COALESCE(MAX(e.created_at), s.created_at) AS last_event_at").
		Join("LEFT JOIN app_usage_events AS e ON e.session_id = s.id").
		Where("s.status = ?", "active").
		Where("s.deleted_at IS NULL").
		GroupExpr("s.id").
		Scan(context.Background(), &results)
	return results, err
}

// ActiveSessionInfo holds session data with last event time from a single query.
type ActiveSessionInfo struct {
	ID          string    `bun:"id"`
	Duration    int64     `bun:"duration"`
	CreatedAt   time.Time `bun:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at"`
	LastEventAt time.Time `bun:"last_event_at"`
}

// TimeoutStaleSessions marks active sessions as inactive if their last event
// is older than the cutoff, and increments duration by the actual elapsed time.
func (db *Database) TimeoutStaleSessions(cutoff time.Duration) (int, error) {
	threshold := time.Now().Add(-cutoff)
	res, err := db.DB.NewUpdate().
		TableExpr("sessions AS s").
		SetColumn("status", "?", "inactive").
		SetColumn("updated_at", "current_timestamp").
		SetColumn("duration", "s.duration + EXTRACT(EPOCH FROM (current_timestamp - s.updated_at))::bigint").
		Where("s.status = ?", "active").
		Where("s.deleted_at IS NULL").
		Where(`(
			SELECT COALESCE(MAX(e.created_at), s.created_at)
			FROM app_usage_events e
			WHERE e.session_id = s.id
		) < ?`, threshold).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetActiveDevicesByProject returns devices with activity within the given duration.
func (db *Database) GetActiveDevicesByProject(projectID string, since time.Duration) ([]models.Device, error) {
	threshold := time.Now().Add(-since)
	var devices []models.Device
	err := db.DB.NewSelect().
		Model(&devices).
		Distinct().
		Join("JOIN sessions AS s ON s.device_id = device.id").
		Join("JOIN app_usage_events AS e ON e.session_id = s.id").
		Where("s.project_id = ?", projectID).
		Where("e.created_at >= ?", threshold).
		Scan(context.Background())
	return devices, err
}

// GetUniqueDevicesByProject returns devices whose first session falls within the range.
func (db *Database) GetUniqueDevicesByProject(projectID string, start, end time.Time) ([]models.Device, error) {
	var devices []models.Device
	err := db.DB.NewSelect().
		Model(&devices).
		Distinct().
		Join("JOIN sessions AS s ON s.device_id = device.id").
		Where("s.project_id = ?", projectID).
		Where("s.created_at >= ?", start).
		Where("s.created_at <= ?", end).
		Scan(context.Background())
	return devices, err
}
