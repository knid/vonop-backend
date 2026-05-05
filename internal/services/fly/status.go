package fly

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const defaultStatusPageURL = "https://status.flyio.net/api/v2/status.json"

// StatusPageStatus mirrors the status object from Fly's Statuspage.io API.
type StatusPageStatus struct {
	Indicator   string `json:"indicator"`   // none, minor, major, critical
	Description string `json:"description"` // e.g. "All Systems Operational"
}

// StatusClient fetches and caches Fly's public status page.
// It is safe for concurrent use and shields health endpoints from
// hitting the status API on every request.
type StatusClient struct {
	url        string
	httpClient *http.Client
	cache      *statusCache
}

type statusCache struct {
	mu        sync.RWMutex
	data      *StatusPageStatus
	fetchedAt time.Time
	ttl       time.Duration
}

// NewStatusClient creates a cached client for https://status.flyio.net.
func NewStatusClient() *StatusClient {
	return &StatusClient{
		url:        defaultStatusPageURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cache: &statusCache{
			ttl: 60 * time.Second,
		},
	}
}

// GetStatus returns the cached status, refreshing in the background if stale.
func (c *StatusClient) GetStatus() *StatusPageStatus {
	c.cache.mu.RLock()
	if c.cache.data != nil && time.Since(c.cache.fetchedAt) < c.cache.ttl {
		c.cache.mu.RUnlock()
		return c.cache.data
	}
	c.cache.mu.RUnlock()

	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	// Double-check after acquiring write lock.
	if c.cache.data != nil && time.Since(c.cache.fetchedAt) < c.cache.ttl {
		return c.cache.data
	}

	status := c.fetch()
	c.cache.data = status
	c.cache.fetchedAt = time.Now()
	return status
}

func (c *StatusClient) fetch() *StatusPageStatus {
	req, err := http.NewRequest("GET", c.url, nil)
	if err != nil {
		return unknownStatus()
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return unknownStatus()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return unknownStatus()
	}

	var result struct {
		Status StatusPageStatus `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return unknownStatus()
	}

	return &result.Status
}

func unknownStatus() *StatusPageStatus {
	return &StatusPageStatus{
		Indicator:   "unknown",
		Description: "Unable to reach Fly status page",
	}
}
