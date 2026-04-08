package controllers

import (
	"fmt"
	"time"

	"github.com/knid/wemob/internal/responses"
)

// AdminGetAnalyticsOverview returns signups-by-day and active-users-by-day for the last 30 days.
func (c *Controller) AdminGetAnalyticsOverview() (*responses.AdminAnalyticsOverviewResponse, error) {
	since := time.Now().AddDate(0, 0, -30)

	signups, err := c.DB.GetSignupsByDay(since)
	if err != nil {
		return nil, fmt.Errorf("failed to get signups by day: %w", err)
	}

	active, err := c.DB.GetActiveUsersByDay(since)
	if err != nil {
		return nil, fmt.Errorf("failed to get active users by day: %w", err)
	}

	signupDays := make([]responses.DayCount, len(signups))
	for i, s := range signups {
		signupDays[i] = responses.DayCount{Date: s.Date, Count: s.Count}
	}

	activeDays := make([]responses.DayCount, len(active))
	for i, a := range active {
		activeDays[i] = responses.DayCount{Date: a.Date, Count: a.Count}
	}

	return &responses.AdminAnalyticsOverviewResponse{
		SignupsByDay: signupDays,
		ActiveByDay:  activeDays,
	}, nil
}
