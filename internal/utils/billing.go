package utils

import "time"

// BillingPeriod computes the current monthly billing period boundaries
// based on the subscription start date and the current time.
// It handles month-end clamping (e.g., subscriptions starting on the 31st
// correctly roll to the 28th/29th/30th in shorter months).
func BillingPeriod(subscriptionStart, now time.Time) (start, end time.Time) {
	loc := now.Location()
	subDay := 1
	if !subscriptionStart.IsZero() {
		subDay = subscriptionStart.Day()
	}

	year, month, _ := now.Date()
	day := clampDay(year, month, subDay)
	periodStart := time.Date(year, month, day, 0, 0, 0, 0, loc)

	if now.Before(periodStart) {
		// Current period started in the previous month
		pm := time.Date(year, month-1, 1, 0, 0, 0, 0, loc)
		day = clampDay(pm.Year(), pm.Month(), subDay)
		periodStart = time.Date(pm.Year(), pm.Month(), day, 0, 0, 0, 0, loc)
	}

	// Next period boundary
	nm := time.Date(periodStart.Year(), periodStart.Month()+1, 1, 0, 0, 0, 0, loc)
	nextDay := clampDay(nm.Year(), nm.Month(), subDay)
	periodEnd := time.Date(nm.Year(), nm.Month(), nextDay, 0, 0, 0, 0, loc)

	return periodStart, periodEnd
}

// clampDay returns the given day or the last day of the month, whichever is smaller.
func clampDay(year int, month time.Month, day int) int {
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > lastDay {
		return lastDay
	}
	return day
}
