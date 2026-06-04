package utils

import (
	"testing"
	"time"
)

func TestBillingPeriodPreservesStripeAnchorTimeInUTC(t *testing.T) {
	anchor := time.Date(2026, time.May, 3, 18, 45, 12, 0, time.UTC)
	now := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.FixedZone("TRT", 3*60*60))

	start, end := BillingPeriod(anchor, now)

	wantStart := time.Date(2026, time.June, 3, 18, 45, 12, 0, time.UTC)
	wantEnd := time.Date(2026, time.July, 3, 18, 45, 12, 0, time.UTC)
	if !start.Equal(wantStart) {
		t.Fatalf("start = %s, want %s", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Fatalf("end = %s, want %s", end, wantEnd)
	}
	if start.Location() != time.UTC || end.Location() != time.UTC {
		t.Fatalf("period should be UTC, got start=%s end=%s", start.Location(), end.Location())
	}
}

func TestBillingPeriodUsesPreviousMonthWhenBeforeAnchorDay(t *testing.T) {
	anchor := time.Date(2026, time.May, 20, 14, 30, 0, 0, time.UTC)
	now := time.Date(2026, time.June, 20, 14, 29, 59, 0, time.UTC)

	start, end := BillingPeriod(anchor, now)

	wantStart := time.Date(2026, time.May, 20, 14, 30, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.June, 20, 14, 30, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("period = %s - %s, want %s - %s", start, end, wantStart, wantEnd)
	}
}

func TestBillingPeriodClampsMonthEndWithAnchorTime(t *testing.T) {
	anchor := time.Date(2026, time.January, 31, 7, 15, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)

	start, end := BillingPeriod(anchor, now)

	wantStart := time.Date(2026, time.January, 31, 7, 15, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.February, 28, 7, 15, 0, 0, time.UTC)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("period = %s - %s, want %s - %s", start, end, wantStart, wantEnd)
	}
}
