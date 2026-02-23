package responses

import "github.com/knid/wemob/internal/database"

type AdminDashboardResponse struct {
	Users   *database.AdminStats   `json:"users"`
	Revenue *database.RevenueStats `json:"revenue"`
	AIUsage *database.AIUsageStats `json:"ai_usage"`
}
