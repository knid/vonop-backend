package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

// CreateWebAnalyticsEvents bulk-inserts analytics events.
func (db *Database) CreateWebAnalyticsEvents(events []models.WebAnalyticsEvent) error {
	if len(events) == 0 {
		return nil
	}
	_, err := db.DB.NewInsert().Model(&events).Exec(context.Background())
	return err
}

// AnalyticsOverview holds aggregate metrics for a project.
type AnalyticsOverview struct {
	Pageviews      int
	UniqueVisitors int
	Sessions       int
	AvgDuration    float64
	BounceRate     float64
}

// GetAnalyticsOverview returns aggregate metrics for a project within a time range.
func (db *Database) GetAnalyticsOverview(projectID string, start, end time.Time) (*AnalyticsOverview, error) {
	var result AnalyticsOverview
	ctx := context.Background()

	// Pageviews count
	pvCount, err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Count(ctx)
	if err != nil {
		return nil, err
	}
	result.Pageviews = pvCount

	// Unique visitors
	err = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("COUNT(DISTINCT visitor_id)").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Scan(ctx, &result.UniqueVisitors)
	if err != nil {
		return nil, err
	}

	// Sessions
	err = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("COUNT(DISTINCT session_id)").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Scan(ctx, &result.Sessions)
	if err != nil {
		return nil, err
	}

	// Average duration (from pageleave events)
	err = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("COALESCE(AVG(duration), 0)").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageleave'").
		Where("duration > 0").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Scan(ctx, &result.AvgDuration)
	if err != nil {
		return nil, err
	}

	// Bounce rate: sessions with exactly 1 pageview / total sessions
	if result.Sessions > 0 {
		var bounceCount int
		err = db.DB.NewSelect().
			TableExpr("(?) AS s",
				db.DB.NewSelect().
					TableExpr("web_analytics_events").
					Column("session_id").
					ColumnExpr("COUNT(*) AS pv_count").
					Where("project_id = ?", projectID).
					Where("event_type = 'pageview'").
					Where("created_at >= ?", start).
					Where("created_at <= ?", end).
					GroupExpr("session_id"),
			).
			ColumnExpr("COUNT(*)").
			Where("s.pv_count = 1").
			Scan(ctx, &bounceCount)
		if err != nil {
			return nil, err
		}
		result.BounceRate = float64(bounceCount) / float64(result.Sessions) * 100
	}

	return &result, nil
}

// TimeSeriesPoint holds a single data point in a time series.
type TimeSeriesPoint struct {
	Timestamp      time.Time `bun:"timestamp"`
	Views          int       `bun:"views"`
	UniqueVisitors int       `bun:"unique_visitors"`
}

// GetAnalyticsPageviews returns a time series of pageviews grouped by the given bucket (hour or day).
func (db *Database) GetAnalyticsPageviews(projectID string, start, end time.Time, bucket string) ([]TimeSeriesPoint, error) {
	truncExpr := "date_trunc('day', created_at)"
	if bucket == "hour" {
		truncExpr = "date_trunc('hour', created_at)"
	}

	var points []TimeSeriesPoint
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr(truncExpr+" AS timestamp").
		ColumnExpr("COUNT(*) AS views").
		ColumnExpr("COUNT(DISTINCT visitor_id) AS unique_visitors").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr(truncExpr).
		OrderExpr(truncExpr+" ASC").
		Scan(context.Background(), &points)
	if err != nil {
		return nil, err
	}
	if points == nil {
		points = []TimeSeriesPoint{}
	}
	return points, nil
}

// TopPageRow holds data for a top page.
type TopPageRow struct {
	Path           string  `bun:"path"`
	Views          int     `bun:"views"`
	UniqueVisitors int     `bun:"unique_visitors"`
	AvgDuration    float64 `bun:"avg_duration"`
}

// GetAnalyticsTopPages returns the most-viewed pages for a project.
func (db *Database) GetAnalyticsTopPages(projectID string, start, end time.Time, limit int) ([]TopPageRow, error) {
	if limit <= 0 {
		limit = 10
	}

	var rows []TopPageRow
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events AS pv").
		ColumnExpr("pv.path").
		ColumnExpr("COUNT(*) AS views").
		ColumnExpr("COUNT(DISTINCT pv.visitor_id) AS unique_visitors").
		ColumnExpr("COALESCE(AVG(pl.duration), 0) AS avg_duration").
		Join("LEFT JOIN web_analytics_events AS pl ON pl.project_id = pv.project_id AND pl.session_id = pv.session_id AND pl.path = pv.path AND pl.event_type = 'pageleave'").
		Where("pv.project_id = ?", projectID).
		Where("pv.event_type = 'pageview'").
		Where("pv.created_at >= ?", start).
		Where("pv.created_at <= ?", end).
		GroupExpr("pv.path").
		OrderExpr("views DESC").
		Limit(limit).
		Scan(context.Background(), &rows)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []TopPageRow{}
	}
	return rows, nil
}

// ReferrerRow holds data for a referrer source.
type ReferrerRow struct {
	Referrer string `bun:"referrer"`
	Visits   int    `bun:"visits"`
}

// GetAnalyticsReferrers returns the top referrer sources.
func (db *Database) GetAnalyticsReferrers(projectID string, start, end time.Time, limit int) ([]ReferrerRow, error) {
	if limit <= 0 {
		limit = 10
	}

	var rows []ReferrerRow
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("referrer").
		ColumnExpr("COUNT(*) AS visits").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("referrer != ''").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr("referrer").
		OrderExpr("visits DESC").
		Limit(limit).
		Scan(context.Background(), &rows)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []ReferrerRow{}
	}
	return rows, nil
}

// DeviceCountRow holds a name/count pair for device breakdowns.
type DeviceCountRow struct {
	Name  string `bun:"name"`
	Count int    `bun:"count"`
}

// DevicesResult holds browser and screen size breakdowns.
type DevicesResult struct {
	Browsers      []DeviceCountRow
	ScreenSizes   []DeviceCountRow
	TotalVisitors int
}

// GetAnalyticsDevices returns browser and screen size breakdowns.
func (db *Database) GetAnalyticsDevices(projectID string, start, end time.Time) (*DevicesResult, error) {
	ctx := context.Background()

	// Screen sizes
	var screenRows []DeviceCountRow
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("screen_size AS name").
		ColumnExpr("COUNT(DISTINCT visitor_id) AS count").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("screen_size != ''").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr("screen_size").
		OrderExpr("count DESC").
		Limit(10).
		Scan(ctx, &screenRows)
	if err != nil {
		return nil, err
	}

	// Total visitors for percentages
	var totalVisitors int
	_ = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("COUNT(DISTINCT visitor_id)").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		Scan(ctx, &totalVisitors)

	// Browser breakdown from user_agent
	var browserRows []DeviceCountRow
	err = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr(`CASE
			WHEN user_agent LIKE '%Firefox%' THEN 'Firefox'
			WHEN user_agent LIKE '%Edg/%' THEN 'Edge'
			WHEN user_agent LIKE '%OPR/%' OR user_agent LIKE '%Opera%' THEN 'Opera'
			WHEN user_agent LIKE '%Chrome%' AND user_agent NOT LIKE '%Edg/%' THEN 'Chrome'
			WHEN user_agent LIKE '%Safari%' AND user_agent NOT LIKE '%Chrome%' THEN 'Safari'
			ELSE 'Other'
		END AS name`).
		ColumnExpr("COUNT(DISTINCT visitor_id) AS count").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("user_agent != ''").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr("name").
		OrderExpr("count DESC").
		Scan(ctx, &browserRows)
	if err != nil {
		return nil, err
	}

	if screenRows == nil {
		screenRows = []DeviceCountRow{}
	}
	if browserRows == nil {
		browserRows = []DeviceCountRow{}
	}

	return &DevicesResult{
		Browsers:      browserRows,
		ScreenSizes:   screenRows,
		TotalVisitors: totalVisitors,
	}, nil
}

// LocationRow holds country/visitor data.
type LocationRow struct {
	Country  string `bun:"country"`
	Visitors int    `bun:"visitors"`
}

// GetAnalyticsLocations returns visitor counts by country.
func (db *Database) GetAnalyticsLocations(projectID string, start, end time.Time) ([]LocationRow, error) {
	var rows []LocationRow
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("country").
		ColumnExpr("COUNT(DISTINCT visitor_id) AS visitors").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("country != ''").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr("country").
		OrderExpr("visitors DESC").
		Scan(context.Background(), &rows)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []LocationRow{}
	}
	return rows, nil
}

// CustomEventRow holds data for a custom event.
type CustomEventRow struct {
	Name           string    `bun:"name"`
	Count          int       `bun:"count"`
	LastOccurrence time.Time `bun:"last_occurrence"`
}

// GetAnalyticsCustomEvents returns custom event names and their counts.
func (db *Database) GetAnalyticsCustomEvents(projectID string, start, end time.Time) ([]CustomEventRow, error) {
	var rows []CustomEventRow
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("event_name AS name").
		ColumnExpr("COUNT(*) AS count").
		ColumnExpr("MAX(created_at) AS last_occurrence").
		Where("project_id = ?", projectID).
		Where("event_type = 'event'").
		Where("event_name != ''").
		Where("created_at >= ?", start).
		Where("created_at <= ?", end).
		GroupExpr("event_name").
		OrderExpr("count DESC").
		Scan(context.Background(), &rows)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []CustomEventRow{}
	}
	return rows, nil
}

// RealtimeResult holds real-time analytics data.
type RealtimeResult struct {
	ActiveVisitors int
	CurrentPages   []string
}

// GetAnalyticsRealtime returns visitors active in the last 5 minutes.
func (db *Database) GetAnalyticsRealtime(projectID string) (*RealtimeResult, error) {
	cutoff := time.Now().Add(-5 * time.Minute)
	ctx := context.Background()

	var activeVisitors int
	err := db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("COUNT(DISTINCT visitor_id)").
		Where("project_id = ?", projectID).
		Where("created_at >= ?", cutoff).
		Scan(ctx, &activeVisitors)
	if err != nil {
		return nil, err
	}

	var pages []string
	err = db.DB.NewSelect().
		TableExpr("web_analytics_events").
		ColumnExpr("DISTINCT path").
		Where("project_id = ?", projectID).
		Where("event_type = 'pageview'").
		Where("created_at >= ?", cutoff).
		Where("path != ''").
		Scan(ctx, &pages)
	if err != nil {
		return nil, err
	}
	if pages == nil {
		pages = []string{}
	}

	return &RealtimeResult{
		ActiveVisitors: activeVisitors,
		CurrentPages:   pages,
	}, nil
}
