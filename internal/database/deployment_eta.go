package database

import (
	"context"
	"sort"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// MedianElapsedSeconds computes the median number of seconds between
// a "from" event and a "to" event across recent deployments. Only
// deployments where both events exist contribute. lookbackDays bounds
// the window so old anomalies don't skew the median as the system
// matures. Returns 0 when there aren't enough data points (< 3) —
// callers treat 0 as "no ETA yet, show a static fallback."
func MedianElapsedSeconds(
	ctx context.Context,
	db *bun.DB,
	fromType, toType string,
	lookbackDays int,
) (int, error) {
	since := time.Now().Add(-time.Duration(lookbackDays) * 24 * time.Hour)

	// Pull all relevant events in one query. Filtering in Go rather
	// than SQL keeps the query simple and lets us pair events by
	// deployment_id without a window function.
	var events []models.DeploymentEvent
	if err := db.NewSelect().
		Model(&events).
		Where("event_type IN (?, ?)", fromType, toType).
		Where("occurred_at > ?", since).
		Order("occurred_at ASC").
		Scan(ctx); err != nil {
		return 0, err
	}

	// Group by deployment_id, capturing the earliest fromType + the
	// earliest subsequent toType. If either is missing, skip.
	type pair struct {
		from *time.Time
		to   *time.Time
	}
	byDep := map[string]*pair{}
	for _, ev := range events {
		key := ev.DeploymentID.String()
		p, ok := byDep[key]
		if !ok {
			p = &pair{}
			byDep[key] = p
		}
		if ev.EventType == fromType && p.from == nil {
			t := ev.OccurredAt
			p.from = &t
		}
		if ev.EventType == toType && p.from != nil && p.to == nil {
			t := ev.OccurredAt
			p.to = &t
		}
	}

	elapsed := make([]int, 0, len(byDep))
	for _, p := range byDep {
		if p.from == nil || p.to == nil {
			continue
		}
		d := int(p.to.Sub(*p.from).Seconds())
		if d > 0 {
			elapsed = append(elapsed, d)
		}
	}
	if len(elapsed) < 3 {
		return 0, nil
	}

	sort.Ints(elapsed)
	mid := len(elapsed) / 2
	if len(elapsed)%2 == 1 {
		return elapsed[mid], nil
	}
	return (elapsed[mid-1] + elapsed[mid]) / 2, nil
}
