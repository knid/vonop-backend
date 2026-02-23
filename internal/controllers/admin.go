package controllers

import (
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetAdminDashboard(user *models.User) (*responses.AdminDashboardResponse, error) {
	if user.Type != "admin" {
		return nil, errors.New("admin access required")
	}

	userStats, err := c.DB.GetAdminStats()
	if err != nil {
		return nil, err
	}

	revenueStats, err := c.DB.GetRevenueStats()
	if err != nil {
		return nil, err
	}

	aiStats, err := c.DB.GetAIUsageStats()
	if err != nil {
		return nil, err
	}

	return &responses.AdminDashboardResponse{
		Users:   userStats,
		Revenue: revenueStats,
		AIUsage: aiStats,
	}, nil
}
