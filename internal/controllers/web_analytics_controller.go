package controllers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// botPatterns is a minimal list of known bot user-agent substrings.
var botPatterns = []string{
	"bot", "crawl", "spider", "slurp", "mediapartners",
	"lighthouse", "pagespeed", "headlesschrome", "phantomjs",
}

// isBot returns true if the user-agent looks like a known bot.
func isBot(ua string) bool {
	lower := strings.ToLower(ua)
	for _, p := range botPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// resolveCountry extracts the visitor's country from CDN/proxy headers.
func resolveCountry(r *http.Request) string {
	if c := r.Header.Get("CF-IPCountry"); c != "" {
		return c
	}
	return ""
}

// CollectAnalyticsEvents validates and bulk-inserts analytics events.
func (c *Controller) CollectAnalyticsEvents(r *http.Request, req requests.CollectAnalyticsBatchRequest) error {
	validate := validator.New()
	if err := req.Validate(validate); err != nil {
		return err
	}

	ua := r.UserAgent()
	if isBot(ua) {
		return nil // silently drop bot traffic
	}

	country := resolveCountry(r)

	// Group events by project ID to validate existence once per project
	projectCache := make(map[string]bool)

	var events []models.WebAnalyticsEvent
	for _, e := range req.Events {
		pid, err := uuid.Parse(e.ProjectID)
		if err != nil {
			continue
		}

		// Validate project exists (cached per request)
		pidStr := pid.String()
		if exists, cached := projectCache[pidStr]; cached {
			if !exists {
				continue
			}
		} else {
			_, err := c.DB.GetAppProjectByID(pidStr)
			if err != nil {
				projectCache[pidStr] = false
				continue
			}
			projectCache[pidStr] = true
		}

		events = append(events, models.WebAnalyticsEvent{
			ProjectID:   pid,
			VisitorID:   e.VisitorID,
			SessionID:   e.SessionID,
			EventType:   e.EventType,
			EventName:   e.EventName,
			URL:         e.URL,
			Path:        e.Path,
			Referrer:    e.Referrer,
			Title:       e.Title,
			UserAgent:   ua,
			ScreenSize:  e.ScreenSize,
			Language:    e.Language,
			UTMSource:   e.UTMSource,
			UTMMedium:   e.UTMMedium,
			UTMCampaign: e.UTMCampaign,
			UTMTerm:     e.UTMTerm,
			UTMContent:  e.UTMContent,
			ScrollDepth: e.ScrollDepth,
			Duration:    e.Duration,
			Country:     country,
			Properties:  e.Properties,
		})
	}

	if len(events) == 0 {
		return nil
	}

	if err := c.DB.CreateWebAnalyticsEvents(events); err != nil {
		slog.Error("failed to insert analytics events", "error", err)
		return err
	}
	return nil
}

// parsePeriod converts a period string to start/end times.
func parsePeriod(period, customStart, customEnd string) (time.Time, time.Time) {
	now := time.Now()
	end := now

	switch period {
	case "24h":
		return now.Add(-24 * time.Hour), end
	case "7d":
		return now.AddDate(0, 0, -7), end
	case "30d":
		return now.AddDate(0, 0, -30), end
	case "90d":
		return now.AddDate(0, 0, -90), end
	case "custom":
		s, err := time.Parse(time.RFC3339, customStart)
		if err != nil {
			s = now.AddDate(0, 0, -7)
		}
		e, err := time.Parse(time.RFC3339, customEnd)
		if err != nil {
			e = now
		}
		return s, e
	default:
		return now.AddDate(0, 0, -7), end
	}
}

// ParsePeriodFromRequest extracts period parameters from query string.
func ParsePeriodFromRequest(r *http.Request) (time.Time, time.Time) {
	q := r.URL.Query()
	return parsePeriod(q.Get("period"), q.Get("start"), q.Get("end"))
}

// GetAnalyticsOverview delegates to DB and maps to response.
func (c *Controller) GetAnalyticsOverview(projectID string, start, end time.Time) (*responses.AnalyticsOverviewResponse, error) {
	ov, err := c.DB.GetAnalyticsOverview(projectID, start, end)
	if err != nil {
		return nil, err
	}
	return &responses.AnalyticsOverviewResponse{
		Pageviews:      ov.Pageviews,
		UniqueVisitors: ov.UniqueVisitors,
		Sessions:       ov.Sessions,
		AvgDuration:    ov.AvgDuration,
		BounceRate:     ov.BounceRate,
	}, nil
}

// GetAnalyticsPageviews auto-selects bucket based on range and returns time series.
func (c *Controller) GetAnalyticsPageviews(projectID string, start, end time.Time) (*responses.AnalyticsTimeSeriesResponse, error) {
	bucket := "day"
	if end.Sub(start) <= 48*time.Hour {
		bucket = "hour"
	}

	points, err := c.DB.GetAnalyticsPageviews(projectID, start, end, bucket)
	if err != nil {
		return nil, err
	}

	data := make([]responses.AnalyticsTimeSeriesPoint, len(points))
	for i, p := range points {
		data[i] = responses.AnalyticsTimeSeriesPoint{
			Timestamp:      p.Timestamp,
			Views:          p.Views,
			UniqueVisitors: p.UniqueVisitors,
		}
	}
	return &responses.AnalyticsTimeSeriesResponse{Data: data}, nil
}

// GetAnalyticsTopPages delegates to DB and maps to response.
func (c *Controller) GetAnalyticsTopPages(projectID string, start, end time.Time) (*responses.AnalyticsTopPagesResponse, error) {
	rows, err := c.DB.GetAnalyticsTopPages(projectID, start, end, 10)
	if err != nil {
		return nil, err
	}

	data := make([]responses.AnalyticsTopPage, len(rows))
	for i, r := range rows {
		data[i] = responses.AnalyticsTopPage{
			Path:           r.Path,
			Views:          r.Views,
			UniqueVisitors: r.UniqueVisitors,
			AvgDuration:    r.AvgDuration,
		}
	}
	return &responses.AnalyticsTopPagesResponse{Data: data}, nil
}

// GetAnalyticsReferrers delegates to DB and maps to response.
func (c *Controller) GetAnalyticsReferrers(projectID string, start, end time.Time) (*responses.AnalyticsReferrersResponse, error) {
	rows, err := c.DB.GetAnalyticsReferrers(projectID, start, end, 10)
	if err != nil {
		return nil, err
	}

	data := make([]responses.AnalyticsReferrer, len(rows))
	for i, r := range rows {
		data[i] = responses.AnalyticsReferrer{Referrer: r.Referrer, Visits: r.Visits}
	}
	return &responses.AnalyticsReferrersResponse{Data: data}, nil
}

// GetAnalyticsDevices delegates to DB and maps to response.
func (c *Controller) GetAnalyticsDevices(projectID string, start, end time.Time) (*responses.AnalyticsDevicesResponse, error) {
	result, err := c.DB.GetAnalyticsDevices(projectID, start, end)
	if err != nil {
		return nil, err
	}

	toEntries := func(rows []database.DeviceCountRow) []responses.AnalyticsDeviceEntry {
		entries := make([]responses.AnalyticsDeviceEntry, len(rows))
		for i, r := range rows {
			pct := float64(0)
			if result.TotalVisitors > 0 {
				pct = float64(r.Count) / float64(result.TotalVisitors) * 100
			}
			entries[i] = responses.AnalyticsDeviceEntry{Name: r.Name, Count: r.Count, Percent: pct}
		}
		return entries
	}

	return &responses.AnalyticsDevicesResponse{
		Browsers:    toEntries(result.Browsers),
		ScreenSizes: toEntries(result.ScreenSizes),
	}, nil
}

// GetAnalyticsLocations delegates to DB and maps to response.
func (c *Controller) GetAnalyticsLocations(projectID string, start, end time.Time) (*responses.AnalyticsLocationsResponse, error) {
	rows, err := c.DB.GetAnalyticsLocations(projectID, start, end)
	if err != nil {
		return nil, err
	}

	var total int
	for _, r := range rows {
		total += r.Visitors
	}

	data := make([]responses.AnalyticsLocation, len(rows))
	for i, r := range rows {
		pct := float64(0)
		if total > 0 {
			pct = float64(r.Visitors) / float64(total) * 100
		}
		data[i] = responses.AnalyticsLocation{Country: r.Country, Visitors: r.Visitors, Percent: pct}
	}
	return &responses.AnalyticsLocationsResponse{Data: data}, nil
}

// GetAnalyticsCustomEvents delegates to DB and maps to response.
func (c *Controller) GetAnalyticsCustomEvents(projectID string, start, end time.Time) (*responses.AnalyticsEventsResponse, error) {
	rows, err := c.DB.GetAnalyticsCustomEvents(projectID, start, end)
	if err != nil {
		return nil, err
	}

	data := make([]responses.AnalyticsCustomEvent, len(rows))
	for i, r := range rows {
		data[i] = responses.AnalyticsCustomEvent{Name: r.Name, Count: r.Count, LastOccurrence: r.LastOccurrence}
	}
	return &responses.AnalyticsEventsResponse{Data: data}, nil
}

// GetAnalyticsRealtime delegates to DB and maps to response.
func (c *Controller) GetAnalyticsRealtime(projectID string) (*responses.AnalyticsRealtimeResponse, error) {
	result, err := c.DB.GetAnalyticsRealtime(projectID)
	if err != nil {
		return nil, err
	}
	return &responses.AnalyticsRealtimeResponse{
		ActiveVisitors: result.ActiveVisitors,
		CurrentPages:   result.CurrentPages,
	}, nil
}
