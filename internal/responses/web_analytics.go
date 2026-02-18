package responses

import "time"

type AnalyticsOverviewResponse struct {
	Pageviews      int     `json:"pageviews"`
	UniqueVisitors int     `json:"unique_visitors"`
	Sessions       int     `json:"sessions"`
	AvgDuration    float64 `json:"avg_duration"`
	BounceRate     float64 `json:"bounce_rate"`
}

type AnalyticsTimeSeriesPoint struct {
	Timestamp      time.Time `json:"timestamp"`
	Views          int       `json:"views"`
	UniqueVisitors int       `json:"unique_visitors"`
}

type AnalyticsTimeSeriesResponse struct {
	Data []AnalyticsTimeSeriesPoint `json:"data"`
}

type AnalyticsTopPage struct {
	Path           string  `json:"path"`
	Views          int     `json:"views"`
	UniqueVisitors int     `json:"unique_visitors"`
	AvgDuration    float64 `json:"avg_duration"`
}

type AnalyticsTopPagesResponse struct {
	Data []AnalyticsTopPage `json:"data"`
}

type AnalyticsReferrer struct {
	Referrer string `json:"referrer"`
	Visits   int    `json:"visits"`
}

type AnalyticsReferrersResponse struct {
	Data []AnalyticsReferrer `json:"data"`
}

type AnalyticsDeviceEntry struct {
	Name    string  `json:"name"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

type AnalyticsDevicesResponse struct {
	Browsers    []AnalyticsDeviceEntry `json:"browsers"`
	ScreenSizes []AnalyticsDeviceEntry `json:"screen_sizes"`
}

type AnalyticsLocation struct {
	Country  string  `json:"country"`
	Visitors int     `json:"visitors"`
	Percent  float64 `json:"percent"`
}

type AnalyticsLocationsResponse struct {
	Data []AnalyticsLocation `json:"data"`
}

type AnalyticsCustomEvent struct {
	Name           string    `json:"name"`
	Count          int       `json:"count"`
	LastOccurrence time.Time `json:"last_occurrence"`
}

type AnalyticsEventsResponse struct {
	Data []AnalyticsCustomEvent `json:"data"`
}

type AnalyticsRealtimeResponse struct {
	ActiveVisitors int      `json:"active_visitors"`
	CurrentPages   []string `json:"current_pages"`
}
