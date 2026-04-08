package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

// DayCount holds a date string and a count for analytics aggregations.
type DayCount struct {
	Date  string `bun:"date"`
	Count int    `bun:"count"`
}

// GetSignupsByDay returns user signup counts grouped by day for the given period.
func (db *Database) GetSignupsByDay(since time.Time) ([]DayCount, error) {
	var results []DayCount
	err := db.DB.NewSelect().
		TableExpr("users").
		ColumnExpr("DATE(created_at) AS date").
		ColumnExpr("COUNT(*) AS count").
		Where("created_at >= ?", since).
		Where("deleted_at IS NULL").
		GroupExpr("DATE(created_at)").
		OrderExpr("date ASC").
		Scan(context.Background(), &results)
	return results, err
}

// GetActiveUsersByDay returns distinct active user counts by day based on token creation.
func (db *Database) GetActiveUsersByDay(since time.Time) ([]DayCount, error) {
	var results []DayCount
	err := db.DB.NewSelect().
		TableExpr("tokens").
		ColumnExpr("DATE(created_at) AS date").
		ColumnExpr("COUNT(DISTINCT user_id) AS count").
		Where("created_at >= ?", since).
		Where("deleted_at IS NULL").
		GroupExpr("DATE(created_at)").
		OrderExpr("date ASC").
		Scan(context.Background(), &results)
	return results, err
}

// GetAllSubscriptions returns all subscription plans ordered by monthly price ascending.
func (db *Database) GetAllSubscriptions() ([]models.StandartUserSubscription, error) {
	var subs []models.StandartUserSubscription
	err := db.DB.NewSelect().
		Model(&subs).
		Relation("Limits").
		Order("monthly_price ASC").
		Scan(context.Background())
	return subs, err
}

// CountUsersBySubscriptionID returns the number of users on a given subscription plan.
func (db *Database) CountUsersBySubscriptionID(subID string) (int, error) {
	count, err := db.DB.NewSelect().
		TableExpr("users").
		Where("subscription_id = ?", subID).
		Where("deleted_at IS NULL").
		Count(context.Background())
	return count, err
}
