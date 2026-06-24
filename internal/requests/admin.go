package requests

type AdminListParams struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Search   string `json:"search"`
	Status   string `json:"status"`
	SortBy   string `json:"sort_by"`
	SortDir  string `json:"sort_dir"`
	From     string `json:"from"`
	To       string `json:"to"`
}

func (p *AdminListParams) Defaults() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 || p.PageSize > 100 {
		p.PageSize = 20
	}
	if p.SortDir != "asc" {
		p.SortDir = "desc"
	}
}

func (p *AdminListParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

type AdminUpdateUserRequest struct {
	Name               *string `json:"name"`
	Email              *string `json:"email"`
	Type               *string `json:"type"`
	SubscriptionStatus *string `json:"subscription_status"`
}

type AdminResetPasswordRequest struct{}

type AdminGrantCreditsRequest struct {
	Amount int    `json:"amount" validate:"required,ne=0" errormgs:"amount is required and must be non-zero"`
	Reason string `json:"reason" validate:"required" errormgs:"reason is required"`
}

type AdminForceSubscriptionRequest struct {
	SubscriptionID string `json:"subscription_id" validate:"required" errormgs:"subscription_id is required"`
	Status         string `json:"status" validate:"required,oneof=active inactive free trial" errormgs:"status must be active, inactive, free, or trial"`
}

type AdminUpdateTicketRequest struct {
	Status   *string `json:"status"`
	Response *string `json:"response"`
	Priority *string `json:"priority"`
}

type AdminUpdateAppRequest struct {
	Status   *string        `json:"status"`
	Settings map[string]any `json:"settings,omitempty"`
}

type AdminUpdateSubscriptionPlanRequest struct {
	Name                    *string `json:"name"`
	MonthlyPrice            *int    `json:"monthly_price"`
	AnnualPrice             *int    `json:"annual_price"`
	MaximumCredentialCount  *int    `json:"maximum_credential_count"`
	MontlyNotificationCount *int    `json:"montly_notification_count"`
	MonthlyAICredits        *int    `json:"monthly_ai_credits"`
	MaxWebApps              *int    `json:"max_web_apps"`
	MaxMobileApps           *int    `json:"max_mobile_apps"`
	MaxDeployedWebApps      *int    `json:"max_deployed_web_apps"`
	MaxDeployedMobileApps   *int    `json:"max_deployed_mobile_apps"`
	CanDeployMobile         *bool   `json:"can_deploy_mobile"`
	CanUseAnalytics         *bool   `json:"can_use_analytics"`
	CanRemoveBrandLogo      *bool   `json:"can_remove_brand_logo"`
	CanUseEcommerce         *bool   `json:"can_use_ecommerce"`
	CanDeployWeb            *bool   `json:"can_deploy_web"`
	CanAccessCode           *bool   `json:"can_access_code"`
	CanBuyDomain            *bool   `json:"can_buy_domain"`
	CanExportToGitHub       *bool   `json:"can_export_to_github"`
	CanUseEmailService      *bool   `json:"can_use_email_service"`
	MinMachinesRunning      *int    `json:"min_machines_running"`
	MonthlyBandwidthGB      *int    `json:"monthly_bandwidth_gb"`
	CanChooseRegion         *bool   `json:"can_choose_region"`
}
