// loadtest-publish hammers the deploy endpoint with N concurrent publishes
// spread evenly over a configurable burst window. Reports response codes
// and latency stats. Used to verify admission control and capacity headroom
// before the ad campaign.
//
// Important: this only triggers DEPLOYS for app projects you've already
// created via the API (or seeded). It does NOT create AppProjects — that's
// out of scope for the load test (the existing app-creation flow is
// expensive and not what we want to stress here). Pre-seed test apps,
// pass their IDs via -app-ids, and the tool fires deploy:web requests
// against them in a burst.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

func main() {
	apiBase := flag.String("api", "https://staging-api.wemob.io", "API base URL")
	token := flag.String("token", "", "Bearer token for a test user (env BEARER_TOKEN also works)")
	appIDsFlag := flag.String("app-ids", "", "comma-separated AppProject IDs to publish")
	burstSec := flag.Int("burst", 60, "Window over which to fire (seconds)")
	flag.Parse()

	if *token == "" {
		*token = os.Getenv("BEARER_TOKEN")
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "-token (or BEARER_TOKEN env) is required")
		os.Exit(2)
	}
	if *appIDsFlag == "" {
		fmt.Fprintln(os.Stderr, "-app-ids is required (comma-separated)")
		os.Exit(2)
	}
	appIDs := strings.Split(*appIDsFlag, ",")
	for i := range appIDs {
		appIDs[i] = strings.TrimSpace(appIDs[i])
	}
	count := len(appIDs)
	if count == 0 {
		fmt.Fprintln(os.Stderr, "no app IDs after parsing")
		os.Exit(2)
	}

	type result struct {
		appID    string
		status   int
		latency  time.Duration
		deployID string
		errStr   string
	}
	results := make([]result, 0, count)
	var mu sync.Mutex
	var wg sync.WaitGroup

	interval := time.Duration(*burstSec) * time.Second / time.Duration(count)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	fmt.Printf("Firing %d publishes over %ds (~%v interval)\n", count, *burstSec, interval)

	for i, appID := range appIDs {
		wg.Add(1)
		go func(idx int, appID string) {
			defer wg.Done()
			time.Sleep(time.Duration(idx) * interval)

			start := time.Now()
			status, deployID, errStr := triggerDeploy(*apiBase, *token, appID)
			latency := time.Since(start)
			mu.Lock()
			results = append(results, result{appID, status, latency, deployID, errStr})
			mu.Unlock()
		}(i, appID)
	}
	wg.Wait()

	var (
		ok202       int
		busy503     int
		conflict409 int
		other       int
		errCount    int
	)
	latencies := make([]time.Duration, 0, len(results))
	for _, r := range results {
		latencies = append(latencies, r.latency)
		switch r.status {
		case http.StatusAccepted:
			ok202++
		case http.StatusServiceUnavailable:
			busy503++
		case http.StatusConflict:
			conflict409++
		case 0:
			errCount++
		default:
			other++
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p50 := latencies[len(latencies)/2]
	p95Idx := len(latencies) * 95 / 100
	if p95Idx >= len(latencies) {
		p95Idx = len(latencies) - 1
	}
	p95 := latencies[p95Idx]

	fmt.Println("\n=== Results ===")
	fmt.Printf("Total responses: %d (errors: %d)\n", len(results), errCount)
	fmt.Printf("  202 Accepted (queued):     %d\n", ok202)
	fmt.Printf("  503 Service Unavailable:   %d (admission control)\n", busy503)
	fmt.Printf("  409 Conflict (in-flight):  %d\n", conflict409)
	fmt.Printf("  Other status codes:        %d\n", other)
	fmt.Printf("Latency: p50=%v p95=%v\n", p50, p95)
}

func triggerDeploy(apiBase, token, appID string) (status int, deployID, errStr string) {
	req, err := http.NewRequest("POST", apiBase+"/v2/apps/"+appID+"/deploy/web", nil)
	if err != nil {
		return 0, "", err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		var out struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		deployID = out.ID
	}
	return resp.StatusCode, deployID, ""
}
