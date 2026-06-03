package utils

import "time"

// BillingPeriod computes the current monthly credit period boundaries based on
// the subscription anchor and the current time.
//
// Stripe anchors include a time-of-day. Preserve that exact UTC time so UI
// "days remaining" calculations do not drift by up to a day around midnight or
// when the API server runs outside UTC.
func BillingPeriod(subscriptionStart, now time.Time) (start, end time.Time) {
	now = now.UTC()
	subDay := 1
	hour, min, sec, nsec := 0, 0, 0, 0
	if !subscriptionStart.IsZero() {
		subscriptionStart = subscriptionStart.UTC()
		subDay = subscriptionStart.Day()
		hour, min, sec = subscriptionStart.Clock()
		nsec = subscriptionStart.Nanosecond()
	}

	year, month, _ := now.Date()
	day := clampDay(year, month, subDay)
	periodStart := time.Date(year, month, day, hour, min, sec, nsec, time.UTC)

	if now.Before(periodStart) {
		// Current period started in the previous month
		pm := time.Date(year, month-1, 1, hour, min, sec, nsec, time.UTC)
		day = clampDay(pm.Year(), pm.Month(), subDay)
		periodStart = time.Date(pm.Year(), pm.Month(), day, hour, min, sec, nsec, time.UTC)
	}

	// Next period boundary
	nm := time.Date(periodStart.Year(), periodStart.Month()+1, 1, hour, min, sec, nsec, time.UTC)
	nextDay := clampDay(nm.Year(), nm.Month(), subDay)
	periodEnd := time.Date(nm.Year(), nm.Month(), nextDay, hour, min, sec, nsec, time.UTC)

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
