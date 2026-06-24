package responses

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
)

type AdminDashboardResponse struct {
	Users         *database.AdminStats   `json:"users"`
	Revenue       *database.RevenueStats `json:"revenue"`
	AIUsage       *database.AIUsageStats `json:"ai_usage"`
	TotalApps     int                    `json:"total_apps"`
	DeployedApps  int                    `json:"deployed_apps"`
	ActiveDeploys int                    `json:"active_deploys"`
	OpenTickets   int                    `json:"open_tickets"`
}

type PaginatedResponse struct {
	Data       any `json:"data"`
	Total      int `json:"total"`
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalPages int `json:"total_pages"`
}

type AdminUserResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	Photo              string    `json:"photo"`
	Verified           bool      `json:"verified"`
	Type               string    `json:"type"`
	Company            string    `json:"company"`
	Provider           string    `json:"provider"`
	SubscriptionStatus string    `json:"subscription_status"`
	SubscriptionName   string    `json:"subscription_name"`
	CustomerID         string    `json:"customer_id"`
	TrialUsed          bool      `json:"trial_used"`
	SetupFinished      bool      `json:"setup_finished"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type AdminUserDetailResponse struct {
	AdminUserResponse
	CreditsBalance int `json:"credits_balance"`
	AppCount       int `json:"app_count"`
	ProjectCount   int `json:"project_count"`
	OrgCount       int `json:"org_count"`
}

type AdminImpersonateResponse struct {
	Token     string    `json:"token"`
	ExpiredAt time.Time `json:"expired_at"`
	UserEmail string    `json:"user_email"`
}

type AdminResetPasswordResponse struct {
	TemporaryPassword string `json:"temporary_password"`
}

type AdminAppResponse struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	ProjectType string    `json:"project_type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AdminAppDetailResponse struct {
	AdminAppResponse
	OrganizationID  uuid.UUID      `json:"organization_id"`
	OrgName         string         `json:"org_name"`
	Prompt          string         `json:"prompt"`
	Theme           any            `json:"theme"`
	DataModels      any            `json:"data_models"`
	Settings        map[string]any `json:"settings"`
	ScreenshotS3Key string         `json:"screenshot_s3_key"`
	LogoS3Key       string         `json:"logo_s3_key"`
	SandboxID       string         `json:"sandbox_id"`
	TemplateID      *uuid.UUID     `json:"template_id"`
	DeployedURL     string         `json:"deployed_url"`
	FileCount       int            `json:"file_count"`
}

type AdminProjectResponse struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	URL         string    `json:"url"`
	PackageName string    `json:"package_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type AdminTicketResponse struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	Subject     string    `json:"subject"`
	Message     string    `json:"message"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Response    string    `json:"response"`
	SenderName  string    `json:"sender_name"`
	SenderEmail string    `json:"sender_email"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AdminJobResponse struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AdminJobDetailResponse struct {
	AdminJobResponse
	Events json.RawMessage `json:"events"`
	Result json.RawMessage `json:"result,omitempty"`
}

type AdminDeploymentResponse struct {
	ID        uuid.UUID `json:"id"`
	AppID     uuid.UUID `json:"app_id"`
	AppName   string    `json:"app_name"`
	UserEmail string    `json:"user_email"`
	Platform  string    `json:"platform"`
	Status    string    `json:"status"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminOrgResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	OwnerEmail  string    `json:"owner_email"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type AdminOrgDetailResponse struct {
	AdminOrgResponse
	Members []AdminOrgMemberResponse `json:"members"`
}

type AdminOrgMemberResponse struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Role  string    `json:"role"`
}

type AdminSubscriptionPlanResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	MonthlyPrice int       `json:"monthly_price"`
	AnnualPrice  int       `json:"annual_price"`
	UserCount    int       `json:"user_count"`
	LimitsID     uuid.UUID `json:"limits_id"`
	Limits       any       `json:"limits"`
	CreatedAt    time.Time `json:"created_at"`
}

type AdminCreditBalanceResponse struct {
	UserID    uuid.UUID `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Balance   int       `json:"balance"`
}

type AdminCreditTransactionResponse struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	UserEmail string    `json:"user_email"`
	Amount    int       `json:"amount"`
	Action    string    `json:"action"`
	Reference string    `json:"reference"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminTokenUsageResponse struct {
	Model           string `json:"model"`
	Action          string `json:"action"`
	TotalPrompt     int    `json:"total_prompt_tokens"`
	TotalCompletion int    `json:"total_completion_tokens"`
	TotalTokens     int    `json:"total_tokens"`
	TotalCalls      int    `json:"total_calls"`
}

type AdminAnalyticsOverviewResponse struct {
	SignupsByDay []DayCount `json:"signups_by_day"`
	ActiveByDay  []DayCount `json:"active_by_day"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
