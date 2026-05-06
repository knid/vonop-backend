package fly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AppBandwidthQuery is the Fly GraphQL query for an app's data-usage rollup.
// Returns aggregate egressGib + ingressGib across all source/destination
// region pairs in the current Stripe billing period. We sum egressGib only
// (egress is the cost driver; ingress is free per Fly pricing).
//
// Schema reference: https://api.fly.io/graphql (introspection). The exact
// field name is `billingDataUsage` and rows are `[BillingDataUsage]` with
// floats for the byte tallies.
const AppBandwidthQuery = `query AppBandwidth($appName: String!) {
  app(name: $appName) {
    billingDataUsage {
      egressGib
      ingressGib
    }
  }
}`

type bandwidthResponse struct {
	Data struct {
		App struct {
			BillingDataUsage []struct {
				EgressGib  float64 `json:"egressGib"`
				IngressGib float64 `json:"ingressGib"`
			} `json:"billingDataUsage"`
		} `json:"app"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// GetAppEgressBytes returns the cumulative egress bytes for the named Fly app
// in the current billing period. Used by the bandwidth-sync cron (E3 Phase 7);
// errors propagate so the cron's retry budget kicks in for transient Fly
// outages.
func (s *Service) GetAppEgressBytes(appName string) (int64, error) {
	body, err := json.Marshal(map[string]any{
		"query":     AppBandwidthQuery,
		"variables": map[string]any{"appName": appName},
	})
	if err != nil {
		return 0, fmt.Errorf("fly: marshal bandwidth query: %w", err)
	}
	req, err := http.NewRequest("POST", s.GraphQLBase, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("fly: bandwidth request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIToken)
	req.Header.Set("Content-Type", "application/json")

	// Bandwidth queries can be slow on apps with large traffic histories; use
	// the configured client (60s timeout) instead of a fresh one.
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fly: bandwidth request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("fly: read bandwidth response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return 0, &APIError{Status: resp.StatusCode, Body: string(respBody)}
	}
	var parsed bandwidthResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0, fmt.Errorf("fly: unmarshal bandwidth response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return 0, fmt.Errorf("fly: bandwidth GraphQL error: %s", parsed.Errors[0].Message)
	}
	var totalEgressGib float64
	for _, row := range parsed.Data.App.BillingDataUsage {
		totalEgressGib += row.EgressGib
	}
	const bytesPerGib = 1024 * 1024 * 1024
	return int64(totalEgressGib * bytesPerGib), nil
}

// LastSyncedSentinel is exported so tests + callers can identify a
// never-synced row without coupling to time.Time{}.
var LastSyncedSentinel = time.Time{}
