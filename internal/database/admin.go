package database

import (
	"context"
	"time"
)

type AdminStats struct {
	TotalUsers       int `json:"total_users"`
	ActiveUsers      int `json:"active_users"`
	TrialUsers       int `json:"trial_users"`
	PaidUsers        int `json:"paid_users"`
	InactiveUsers    int `json:"inactive_users"`
	TotalProjects    int `json:"total_projects"`
	TotalBuilds      int `json:"total_builds"`
	NewUsersToday    int `json:"new_users_today"`
	NewUsersThisWeek int `json:"new_users_this_week"`
}

type RevenueStats struct {
	TotalSubscriptions int `json:"total_subscriptions"`
	ActiveSubscriptions int `json:"active_subscriptions"`
}

type AIUsageStats struct {
	TotalCreditsUsed   int `json:"total_credits_used"`
	TotalTransactions  int `json:"total_transactions"`
	AvgCreditsPerUser  int `json:"avg_credits_per_user"`
}

func (db *Database) GetAdminStats() (*AdminStats, error) {
	stats := &AdminStats{}

	ctx := context.Background()

	total, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalUsers = total

	active, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Where("subscription_status = 'active'").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.ActiveUsers = active

	free, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Where("subscription_status = 'free'").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TrialUsers = free // repurposed: counts free-tier users

	stats.PaidUsers = active
	stats.InactiveUsers = total - active - free

	projects, err := db.DB.NewSelect().TableExpr("projects").Where("deleted_at IS NULL").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalProjects = projects

	builds, err := db.DB.NewSelect().TableExpr("builds").Where("deleted_at IS NULL").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalBuilds = builds

	today := time.Now().Truncate(24 * time.Hour)
	newToday, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Where("created_at >= ?", today).Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.NewUsersToday = newToday

	weekAgo := today.AddDate(0, 0, -7)
	newWeek, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Where("created_at >= ?", weekAgo).Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.NewUsersThisWeek = newWeek

	return stats, nil
}

func (db *Database) GetRevenueStats() (*RevenueStats, error) {
	stats := &RevenueStats{}
	ctx := context.Background()

	total, err := db.DB.NewSelect().TableExpr("standard_user_subscriptions").Where("deleted_at IS NULL").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalSubscriptions = total

	active, err := db.DB.NewSelect().TableExpr("users").Where("deleted_at IS NULL").Where("subscription_status = 'active'").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.ActiveSubscriptions = active

	return stats, nil
}

func (db *Database) GetAIUsageStats() (*AIUsageStats, error) {
	stats := &AIUsageStats{}
	ctx := context.Background()

	totalTx, err := db.DB.NewSelect().TableExpr("ai_credit_transactions").Where("amount < 0").Count(ctx)
	if err != nil {
		return nil, err
	}
	stats.TotalTransactions = totalTx

	var totalUsed struct {
		Total int `bun:"total"`
	}
	err = db.DB.NewSelect().
		TableExpr("ai_credit_transactions").
		ColumnExpr("COALESCE(ABS(SUM(amount)), 0) AS total").
		Where("amount < 0").
		Scan(ctx, &totalUsed)
	if err != nil {
		return nil, err
	}
	stats.TotalCreditsUsed = totalUsed.Total

	if totalTx > 0 {
		var distinctUsers struct {
			Count int `bun:"count"`
		}
		err = db.DB.NewSelect().
			TableExpr("ai_credit_transactions").
			ColumnExpr("COUNT(DISTINCT user_id) AS count").
			Where("amount < 0").
			Scan(ctx, &distinctUsers)
		if err != nil {
			return nil, err
		}
		if distinctUsers.Count > 0 {
			stats.AvgCreditsPerUser = stats.TotalCreditsUsed / distinctUsers.Count
		}
	}

	return stats, nil
}

func (db *Database) GetAppStats() (int, int, error) {
	ctx := context.Background()
	total, err := db.DB.NewSelect().TableExpr("app_projects").Where("deleted_at IS NULL").Count(ctx)
	if err != nil {
		return 0, 0, err
	}
	deployed, err := db.DB.NewSelect().TableExpr("app_projects").Where("deleted_at IS NULL").Where("status = 'deployed'").Count(ctx)
	return total, deployed, err
}

func (db *Database) GetDeploymentStats() (int, error) {
	ctx := context.Background()
	return db.DB.NewSelect().TableExpr("app_deployments").Where("status = 'deployed'").Count(ctx)
}

func (db *Database) GetOpenTicketCount() (int, error) {
	ctx := context.Background()
	return db.DB.NewSelect().TableExpr("tickets").Where("deleted_at IS NULL").Where("status = 'open'").Count(ctx)
}
