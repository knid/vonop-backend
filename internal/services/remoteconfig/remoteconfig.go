package remoteconfig

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/posthog/posthog-go"
)

type cachedEntry struct {
	value     interface{}
	expiresAt time.Time
}

// disableFlagEvents is a stable pointer-to-false used to opt every flag read
// out of the SDK's automatic `$feature_flag_called` capture. Wemob uses
// PostHog flags as a remote-config backend (rc-credit-pack-*, rc-ai-*, etc.)
// rather than for A/B testing — the per-call events are pure noise and the
// hottest reads use distinct_id="__global__" so they aren't person-attributable
// anyway. PostHog can still derive STALE flag detection from non-flag
// activity-log signals after this is on.
var disableFlagEvents = func() *bool { v := false; return &v }()

// Service provides typed, cached access to PostHog feature flag payloads
// with hardcoded fallbacks. Safe to use when PostHog is nil (returns fallbacks).
type Service struct {
	posthog posthog.Client
	cache   sync.Map
	ttl     time.Duration
}

// New creates a RemoteConfig service. posthogClient may be nil (fallback-only mode).
func New(posthogClient posthog.Client) *Service {
	return &Service{
		posthog: posthogClient,
		ttl:     5 * time.Minute,
	}
}

// GetInt returns the integer value of a feature flag payload, or the key's fallback.
func (s *Service) GetInt(key Key, userID string) int {
	fallback, _ := key.Fallback.(int)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

// GetString returns the string value of a feature flag payload, or the key's fallback.
func (s *Service) GetString(key Key, userID string) string {
	fallback, _ := key.Fallback.(string)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	if v, ok := raw.(string); ok {
		return v
	}
	return fallback
}

// GetBool returns the boolean value of a feature flag payload, or the key's fallback.
func (s *Service) GetBool(key Key, userID string) bool {
	fallback, _ := key.Fallback.(bool)
	raw := s.getPayload(key.Flag, userID)
	if raw == nil {
		return fallback
	}
	if v, ok := raw.(bool); ok {
		return v
	}
	return fallback
}

// GetVariant returns the raw feature flag variant (e.g. "control", "test").
// Returns empty string if PostHog is nil or the flag doesn't exist.
func (s *Service) GetVariant(key Key, userID string) string {
	if s.posthog == nil {
		return ""
	}

	cacheKey := "variant:" + key.Flag + ":" + userID
	if entry, ok := s.cache.Load(cacheKey); ok {
		if ce := entry.(cachedEntry); time.Now().Before(ce.expiresAt) {
			if v, ok := ce.value.(string); ok {
				return v
			}
		}
	}

	variant, err := s.posthog.GetFeatureFlag(posthog.FeatureFlagPayload{
		Key:                   key.Flag,
		DistinctId:            userID,
		SendFeatureFlagEvents: disableFlagEvents,
	})
	if err != nil {
		slog.Debug("remoteconfig: GetVariant error", "flag", key.Flag, "error", err)
		return ""
	}

	result := fmt.Sprint(variant)
	s.cache.Store(cacheKey, cachedEntry{value: result, expiresAt: time.Now().Add(s.ttl)})
	return result
}

// getPayload fetches the feature flag payload from PostHog with caching.
// Returns nil on any error or when PostHog is disabled.
func (s *Service) getPayload(flag string, userID string) interface{} {
	if s.posthog == nil {
		return nil
	}

	cacheKey := flag + ":" + userID
	if entry, ok := s.cache.Load(cacheKey); ok {
		if ce := entry.(cachedEntry); time.Now().Before(ce.expiresAt) {
			return ce.value
		}
	}

	payload, err := s.posthog.GetFeatureFlagPayload(posthog.FeatureFlagPayload{
		Key:                   flag,
		DistinctId:            userID,
		SendFeatureFlagEvents: disableFlagEvents,
	})
	if err != nil {
		slog.Debug("remoteconfig: getPayload error", "flag", flag, "error", err)
		return nil
	}

	s.cache.Store(cacheKey, cachedEntry{value: payload, expiresAt: time.Now().Add(s.ttl)})
	return payload
}
