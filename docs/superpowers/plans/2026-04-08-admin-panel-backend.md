# Admin Panel Backend API — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive admin API endpoints to the wemob-backend for managing all platform entities.

**Architecture:** New admin routes under `/v1/admin/*` protected by `RequireAuth` + `RequireAdmin` middleware. Each entity group gets its own database, controller, view, request, and response files following existing patterns. Admin checks use `user.Type == "admin"` via middleware (no inline checks needed).

**Tech Stack:** Go, chi/v5 router, Bun ORM, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-04-08-admin-panel-design.md`

---

## File Structure

### New files to create:
- `internal/requests/admin.go` — all admin request structs
- `internal/responses/admin.go` — already exists, will expand with all admin response structs
- `internal/database/admin_users.go` — admin user queries (paginated list, search, count)
- `internal/database/admin_apps.go` — admin app project queries
- `internal/database/admin_projects.go` — admin legacy project queries
- `internal/database/admin_credits.go` — admin AI credit queries (cross-user)
- `internal/database/admin_tickets.go` — admin ticket queries
- `internal/database/admin_jobs.go` — admin generation job queries
- `internal/database/admin_deployments.go` — admin deployment queries
- `internal/database/admin_organizations.go` — admin organization queries
- `internal/database/admin_analytics.go` — platform-wide analytics queries
- `internal/controllers/admin_users.go` — admin user business logic
- `internal/controllers/admin_apps.go` — admin app project logic
- `internal/controllers/admin_projects.go` — admin legacy project logic
- `internal/controllers/admin_credits.go` — admin credit operations
- `internal/controllers/admin_tickets.go` — admin ticket logic
- `internal/controllers/admin_jobs.go` — admin job logic
- `internal/controllers/admin_deployments.go` — admin deployment logic
- `internal/controllers/admin_organizations.go` — admin org logic
- `internal/controllers/admin_subscriptions.go` — admin subscription/billing logic
- `internal/controllers/admin_analytics.go` — platform analytics logic
- `internal/views/admin_users_view.go` — admin user HTTP handlers
- `internal/views/admin_apps_view.go` — admin app HTTP handlers
- `internal/views/admin_projects_view.go` — admin legacy project HTTP handlers
- `internal/views/admin_credits_view.go` — admin credit HTTP handlers
- `internal/views/admin_tickets_view.go` — admin ticket HTTP handlers
- `internal/views/admin_jobs_view.go` — admin job HTTP handlers
- `internal/views/admin_deployments_view.go` — admin deployment HTTP handlers
- `internal/views/admin_organizations_view.go` — admin org HTTP handlers
- `internal/views/admin_subscriptions_view.go` — admin subscription HTTP handlers
- `internal/views/admin_analytics_view.go` — admin analytics HTTP handlers

### Files to modify:
- `internal/responses/admin.go` — expand with all admin response structs
- `internal/database/admin.go` — expand dashboard stats
- `internal/controllers/admin.go` — expand dashboard controller
- `internal/views/admin_view.go` — expand dashboard view
- `cmd/main.go` — register all new admin routes

---

### Task 1: Shared Admin Infrastructure (Requests, Responses, Pagination)

**Files:**
- Create: `internal/requests/admin.go`
- Modify: `internal/responses/admin.go`

This task sets up the shared request/response types and pagination pattern used by all admin endpoints.

- [ ] **Step 1: Create admin request structs**

Create `internal/requests/admin.go`:

```go
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
	Status *string         `json:"status"`
	Settings map[string]any `json:"settings,omitempty"`
}

type AdminUpdateSubscriptionPlanRequest struct {
	Name         *string `json:"name"`
	MonthlyPrice *int    `json:"monthly_price"`
	AnnualPrice  *int    `json:"annual_price"`
	// Limits
	MaximumProjectCount     *int  `json:"maximum_project_count"`
	MaximumCredentialCount  *int  `json:"maximum_credential_count"`
	MontlyNotificationCount *int  `json:"montly_notification_count"`
	MontlyBuildCount        *int  `json:"montly_build_count"`
	MonthlyAICredits        *int  `json:"monthly_ai_credits"`
	MaxWebApps              *int  `json:"max_web_apps"`
	MaxMobileApps           *int  `json:"max_mobile_apps"`
	MaxDeployedWebApps      *int  `json:"max_deployed_web_apps"`
	CanBuildToAndroid       *bool `json:"can_build_to_android"`
	CanBuildToIOS           *bool `json:"can_build_to_ios"`
	CanUseAnalytics         *bool `json:"can_use_analytics"`
	CanRemoveBrandLogo      *bool `json:"can_remove_brand_logo"`
	CanUseEcommerce         *bool `json:"can_use_ecommerce"`
	CanDeployWeb            *bool `json:"can_deploy_web"`
	CanAccessCode           *bool `json:"can_access_code"`
	CanBuyDomain            *bool `json:"can_buy_domain"`
	CanExportToGitHub       *bool `json:"can_export_to_github"`
	CanUseEmailService      *bool `json:"can_use_email_service"`
	CanBuildMobile          *bool `json:"can_build_mobile"`
}
```

- [ ] **Step 2: Expand admin response structs**

Replace `internal/responses/admin.go` with:

```go
package responses

import (
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
)

type AdminDashboardResponse struct {
	Users   *database.AdminStats   `json:"users"`
	Revenue *database.RevenueStats `json:"revenue"`
	AIUsage *database.AIUsageStats `json:"ai_usage"`
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
	Prompt          string         `json:"prompt"`
	Settings        map[string]any `json:"settings"`
	ScreenshotS3Key string         `json:"screenshot_s3_key"`
	SandboxID       string         `json:"sandbox_id"`
	TemplateID      *uuid.UUID     `json:"template_id"`
}

type AdminProjectResponse struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	URL         string    `json:"url"`
	PackageName string    `json:"package_name"`
	BuildCount  int       `json:"build_count"`
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
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	MonthlyPrice  int       `json:"monthly_price"`
	AnnualPrice   int       `json:"annual_price"`
	UserCount     int       `json:"user_count"`
	LimitsID      uuid.UUID `json:"limits_id"`
	Limits        any       `json:"limits"`
	CreatedAt     time.Time `json:"created_at"`
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
	Model            string `json:"model"`
	Action           string `json:"action"`
	TotalPrompt      int    `json:"total_prompt_tokens"`
	TotalCompletion  int    `json:"total_completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	TotalCalls       int    `json:"total_calls"`
}

type AdminAnalyticsOverviewResponse struct {
	SignupsByDay    []DayCount `json:"signups_by_day"`
	ActiveByDay     []DayCount `json:"active_by_day"`
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`
Expected: compiles with no errors

- [ ] **Step 4: Commit**

```bash
git add internal/requests/admin.go internal/responses/admin.go
git commit -m "feat(admin): add shared request/response types for admin API"
```

---

### Task 2: Admin User Endpoints (Database + Controller + Views)

**Files:**
- Create: `internal/database/admin_users.go`
- Create: `internal/controllers/admin_users.go`
- Create: `internal/views/admin_users_view.go`

- [ ] **Step 1: Create admin user database queries**

Create `internal/database/admin_users.go`:

```go
package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListUsers(params requests.AdminListParams) ([]models.User, int, error) {
	var users []models.User
	q := db.DB.NewSelect().Model(&users).
		Relation("Subscription").
		Where("\"user\".deleted_at IS NULL")

	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("\"user\".name ILIKE ?", search).WhereOr("\"user\".email ILIKE ?", search)
		})
	}
	if params.Status != "" {
		q = q.Where("\"user\".subscription_status = ?", params.Status)
	}
	if params.From != "" {
		if t, err := time.Parse("2006-01-02", params.From); err == nil {
			q = q.Where("\"user\".created_at >= ?", t)
		}
	}
	if params.To != "" {
		if t, err := time.Parse("2006-01-02", params.To); err == nil {
			q = q.Where("\"user\".created_at <= ?", t.Add(24*time.Hour))
		}
	}

	sortCol := "created_at"
	if params.SortBy == "name" || params.SortBy == "email" || params.SortBy == "subscription_status" {
		sortCol = params.SortBy
	}
	q = q.OrderExpr("\"user\"." + sortCol + " " + params.SortDir)

	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return users, total, err
}

func (db *Database) AdminGetUserByID(id string) (*models.User, error) {
	var user models.User
	err := db.DB.NewSelect().Model(&user).
		Relation("Subscription").
		Where("\"user\".id = ?", id).
		Scan(context.Background())
	return &user, err
}
```

- [ ] **Step 2: Create admin user controller**

Create `internal/controllers/admin_users.go`:

```go
package controllers

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) AdminListUsers(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	users, total, err := c.DB.AdminListUsers(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminUserResponse, len(users))
	for i, u := range users {
		subName := ""
		if u.Subscription != nil {
			subName = u.Subscription.Name
		}
		items[i] = responses.AdminUserResponse{
			ID:                 u.ID,
			Name:               u.Name,
			Email:              u.Email,
			Photo:              u.Photo,
			Verified:           u.Verified,
			Type:               u.Type,
			Company:            u.Company,
			Provider:           u.Provider,
			SubscriptionStatus: u.SubscriptionStatus,
			SubscriptionName:   subName,
			CustomerID:         u.CustomerID,
			TrialUsed:          u.TrialUsed,
			SetupFinished:      u.SetupFinished,
			CreatedAt:          u.CreatedAt,
			UpdatedAt:          u.UpdatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetUser(userID string) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.AdminGetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	subName := ""
	if user.Subscription != nil {
		subName = user.Subscription.Name
	}

	balance, _ := c.DB.GetAICreditsBalance(userID)
	creditsBal := 0
	if balance != nil {
		creditsBal = balance.Balance
	}
	appCount, _ := c.DB.CountAppProjectsByUserID(userID)
	projectCount, _ := c.DB.CountProjectsByUserID(userID)
	orgCount, _ := c.DB.CountOrganizationsByUserID(userID)

	return &responses.AdminUserDetailResponse{
		AdminUserResponse: responses.AdminUserResponse{
			ID:                 user.ID,
			Name:               user.Name,
			Email:              user.Email,
			Photo:              user.Photo,
			Verified:           user.Verified,
			Type:               user.Type,
			Company:            user.Company,
			Provider:           user.Provider,
			SubscriptionStatus: user.SubscriptionStatus,
			SubscriptionName:   subName,
			CustomerID:         user.CustomerID,
			TrialUsed:          user.TrialUsed,
			SetupFinished:      user.SetupFinished,
			CreatedAt:          user.CreatedAt,
			UpdatedAt:          user.UpdatedAt,
		},
		CreditsBalance: creditsBal,
		AppCount:       appCount,
		ProjectCount:   projectCount,
		OrgCount:       orgCount,
	}, nil
}

func (c *Controller) AdminUpdateUser(userID string, req requests.AdminUpdateUserRequest) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if req.Name != nil {
		user.Name = *req.Name
	}
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.Type != nil {
		user.Type = *req.Type
	}
	if req.SubscriptionStatus != nil {
		user.SubscriptionStatus = *req.SubscriptionStatus
	}

	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update user")
	}

	return c.AdminGetUser(userID)
}

func (c *Controller) AdminDeleteUser(userID string) error {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return fmt.Errorf("user not found")
	}
	_, err = c.DB.DeleteUser(user)
	return err
}

func (c *Controller) AdminImpersonateUser(userID string) (*responses.AdminImpersonateResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token")
	}

	token := models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().Add(1 * time.Hour), // Short-lived for impersonation
	}

	created, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, fmt.Errorf("failed to create token")
	}

	slog.Info("Admin impersonating user", "target_user_id", userID, "target_email", user.Email)

	return &responses.AdminImpersonateResponse{
		Token:     created.String,
		ExpiredAt: created.ExpiredAt,
		UserEmail: user.Email,
	}, nil
}

func (c *Controller) AdminResetPassword(userID string) (*responses.AdminResetPasswordResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	tempPass, err := utils.RandomHex(8) // 16 char hex string
	if err != nil {
		return nil, fmt.Errorf("failed to generate password")
	}

	hashed, err := utils.HashPassword(tempPass)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password")
	}

	user.Password = hashed
	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update password")
	}

	slog.Info("Admin reset password", "target_user_id", userID, "target_email", user.Email)

	return &responses.AdminResetPasswordResponse{
		TemporaryPassword: tempPass,
	}, nil
}

func (c *Controller) AdminGrantCredits(userID string, req requests.AdminGrantCreditsRequest) error {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return fmt.Errorf("user not found")
	}

	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return fmt.Errorf("failed to get credits balance")
	}

	balance.Balance += req.Amount
	if err := c.DB.UpdateAICreditsBalance(balance); err != nil {
		return fmt.Errorf("failed to update balance")
	}

	tx := &models.AICreditTransaction{
		UserID:    user.ID,
		Amount:    req.Amount,
		Action:    "admin_grant",
		Reference: req.Reason,
	}
	if err := c.DB.CreateAICreditTransaction(tx); err != nil {
		return fmt.Errorf("failed to record transaction")
	}

	slog.Info("Admin granted credits", "target_user_id", userID, "amount", req.Amount, "reason", req.Reason)
	return nil
}

func (c *Controller) AdminForceSubscription(userID string, req requests.AdminForceSubscriptionRequest) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	sub, err := c.DB.GetSubscriptionById(req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("subscription plan not found")
	}

	user.SubscriptionID = sub.ID
	user.SubscriptionStatus = req.Status
	if req.Status == "active" && user.SubscriptionStartAt.IsZero() {
		user.SubscriptionStartAt = time.Now()
	}

	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update subscription")
	}

	slog.Info("Admin forced subscription", "target_user_id", userID, "plan", sub.Name, "status", req.Status)

	return c.AdminGetUser(userID)
}
```

- [ ] **Step 3: Create admin user view handlers**

Create `internal/views/admin_users_view.go`:

```go
package views

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func adminListParamsFromQuery(r *http.Request) requests.AdminListParams {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return requests.AdminListParams{
		Page:     page,
		PageSize: pageSize,
		Search:   r.URL.Query().Get("search"),
		Status:   r.URL.Query().Get("status"),
		SortBy:   r.URL.Query().Get("sort_by"),
		SortDir:  r.URL.Query().Get("sort_dir"),
		From:     r.URL.Query().Get("from"),
		To:       r.URL.Query().Get("to"),
	}
}

func (v *HttpViews) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListUsers(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	result, err := v.Controller.AdminGetUser(userID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var req requests.AdminUpdateUserRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := v.Controller.AdminUpdateUser(userID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	if err := v.Controller.AdminDeleteUser(userID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "deleted"}, http.StatusOK)
}

func (v *HttpViews) AdminImpersonateUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	result, err := v.Controller.AdminImpersonateUser(userID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminResetPassword(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	result, err := v.Controller.AdminResetPassword(userID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGrantCredits(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var req requests.AdminGrantCreditsRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		utils.JSONErrorResponse(w, "amount and reason are required", http.StatusBadRequest)
		return
	}
	if err := v.Controller.AdminGrantCredits(userID, req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}

func (v *HttpViews) AdminForceSubscription(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	var req requests.AdminForceSubscriptionRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		utils.JSONErrorResponse(w, "subscription_id and status are required", http.StatusBadRequest)
		return
	}
	result, err := v.Controller.AdminForceSubscription(userID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`

Note: This may fail due to missing helper DB methods like `CountAppProjectsByUserID`, `CountProjectsByUserID`, `CountOrganizationsByUserID`, `GetSubscriptionById`. Check which exist already by grepping, and add stubs for any that are missing. Add missing methods to the appropriate existing database files (e.g. `internal/database/users.go`, `internal/database/subscriptions.go`). These are simple count/get queries following the same Bun ORM pattern used throughout the codebase.

- [ ] **Step 5: Commit**

```bash
git add internal/database/admin_users.go internal/controllers/admin_users.go internal/views/admin_users_view.go
git commit -m "feat(admin): add user management endpoints"
```

---

### Task 3: Admin App Projects, Legacy Projects, and Organizations Endpoints

**Files:**
- Create: `internal/database/admin_apps.go`
- Create: `internal/database/admin_projects.go`
- Create: `internal/database/admin_organizations.go`
- Create: `internal/controllers/admin_apps.go`
- Create: `internal/controllers/admin_projects.go`
- Create: `internal/controllers/admin_organizations.go`
- Create: `internal/views/admin_apps_view.go`
- Create: `internal/views/admin_projects_view.go`
- Create: `internal/views/admin_organizations_view.go`

- [ ] **Step 1: Create admin apps database queries**

Create `internal/database/admin_apps.go`:

```go
package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListApps(params requests.AdminListParams) ([]models.AppProject, int, error) {
	var apps []models.AppProject
	q := db.DB.NewSelect().Model(&apps).
		Relation("User").
		Where("app_project.deleted_at IS NULL")

	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			return sq.Where("app_project.name ILIKE ?", search).WhereOr("app_project.slug ILIKE ?", search)
		})
	}
	if params.Status != "" {
		q = q.Where("app_project.status = ?", params.Status)
	}
	if params.From != "" {
		if t, err := time.Parse("2006-01-02", params.From); err == nil {
			q = q.Where("app_project.created_at >= ?", t)
		}
	}
	if params.To != "" {
		if t, err := time.Parse("2006-01-02", params.To); err == nil {
			q = q.Where("app_project.created_at <= ?", t.Add(24*time.Hour))
		}
	}

	q = q.OrderExpr("app_project.created_at " + params.SortDir)

	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return apps, total, err
}

func (db *Database) AdminGetAppByID(id string) (*models.AppProject, error) {
	var app models.AppProject
	err := db.DB.NewSelect().Model(&app).
		Relation("User").
		Where("app_project.id = ?", id).
		Scan(context.Background())
	return &app, err
}

func (db *Database) AdminDeleteApp(id string) error {
	_, err := db.DB.NewUpdate().TableExpr("app_projects").
		Set("deleted_at = NOW()").
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Exec(context.Background())
	return err
}
```

- [ ] **Step 2: Create admin projects database queries**

Create `internal/database/admin_projects.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListProjects(params requests.AdminListParams) ([]models.Project, int, error) {
	var projects []models.Project
	q := db.DB.NewSelect().Model(&projects).
		Relation("User").
		Where("project.deleted_at IS NULL")

	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.Where("project.package_name ILIKE ?", search)
	}

	q = q.OrderExpr("project.created_at " + params.SortDir)

	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return projects, total, err
}

func (db *Database) AdminGetProjectByID(id string) (*models.Project, error) {
	var project models.Project
	err := db.DB.NewSelect().Model(&project).
		Relation("User").
		Where("project.id = ?", id).
		Scan(context.Background())
	return &project, err
}

func (db *Database) AdminGetProjectBuildCount(projectID string) (int, error) {
	return db.DB.NewSelect().TableExpr("builds").
		Where("project_id = ?", projectID).
		Where("deleted_at IS NULL").
		Count(context.Background())
}

func (db *Database) AdminDeleteProject(id string) error {
	_, err := db.DB.NewUpdate().TableExpr("projects").
		Set("deleted_at = NOW()").
		Where("id = ?", id).
		Where("deleted_at IS NULL").
		Exec(context.Background())
	return err
}
```

- [ ] **Step 3: Create admin organizations database queries**

Create `internal/database/admin_organizations.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListOrganizations(params requests.AdminListParams) ([]models.Organization, int, error) {
	var orgs []models.Organization
	q := db.DB.NewSelect().Model(&orgs).
		Relation("Owner").
		Where("organization.deleted_at IS NULL")

	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.Where("organization.name ILIKE ?", search)
	}

	q = q.OrderExpr("organization.created_at " + params.SortDir)

	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return orgs, total, err
}

func (db *Database) AdminGetOrganizationByID(id string) (*models.Organization, error) {
	var org models.Organization
	err := db.DB.NewSelect().Model(&org).
		Relation("Owner").
		Where("organization.id = ?", id).
		Scan(context.Background())
	return &org, err
}

func (db *Database) AdminGetOrganizationMembers(orgID string) ([]models.OrganizationMember, error) {
	var members []models.OrganizationMember
	err := db.DB.NewSelect().Model(&members).
		Relation("User").
		Where("organization_id = ?", orgID).
		Where("organization_member.deleted_at IS NULL").
		Scan(context.Background())
	return members, err
}

func (db *Database) AdminCountOrgMembers(orgID string) (int, error) {
	return db.DB.NewSelect().TableExpr("organization_members").
		Where("organization_id = ?", orgID).
		Where("deleted_at IS NULL").
		Count(context.Background())
}
```

- [ ] **Step 4: Create controllers for apps, projects, organizations**

Create `internal/controllers/admin_apps.go`:

```go
package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListApps(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	apps, total, err := c.DB.AdminListApps(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminAppResponse, len(apps))
	for i, a := range apps {
		email := ""
		if a.User != nil {
			email = a.User.Email
		}
		items[i] = responses.AdminAppResponse{
			ID:          a.ID,
			UserID:      a.UserID,
			UserEmail:   email,
			Name:        a.Name,
			Slug:        a.Slug,
			ProjectType: a.ProjectType,
			Status:      a.Status,
			CreatedAt:   a.CreatedAt,
			UpdatedAt:   a.UpdatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetApp(appID string) (*responses.AdminAppDetailResponse, error) {
	app, err := c.DB.AdminGetAppByID(appID)
	if err != nil {
		return nil, fmt.Errorf("app not found")
	}
	email := ""
	if app.User != nil {
		email = app.User.Email
	}
	return &responses.AdminAppDetailResponse{
		AdminAppResponse: responses.AdminAppResponse{
			ID: app.ID, UserID: app.UserID, UserEmail: email,
			Name: app.Name, Slug: app.Slug, ProjectType: app.ProjectType,
			Status: app.Status, CreatedAt: app.CreatedAt, UpdatedAt: app.UpdatedAt,
		},
		Prompt: app.Prompt, Settings: app.Settings,
		ScreenshotS3Key: app.ScreenshotS3Key, SandboxID: app.SandboxID,
		TemplateID: app.TemplateID,
	}, nil
}

func (c *Controller) AdminUpdateApp(appID string, req requests.AdminUpdateAppRequest) (*responses.AdminAppDetailResponse, error) {
	app, err := c.DB.AdminGetAppByID(appID)
	if err != nil {
		return nil, fmt.Errorf("app not found")
	}
	if req.Status != nil {
		app.Status = *req.Status
	}
	if req.Settings != nil {
		app.Settings = req.Settings
	}
	if _, err := c.DB.UpdateAppProject(app); err != nil {
		return nil, fmt.Errorf("failed to update app")
	}
	return c.AdminGetApp(appID)
}

func (c *Controller) AdminDeleteApp(appID string) error {
	return c.DB.AdminDeleteApp(appID)
}
```

Create `internal/controllers/admin_projects.go`:

```go
package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListProjects(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	projects, total, err := c.DB.AdminListProjects(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminProjectResponse, len(projects))
	for i, p := range projects {
		email := ""
		if p.User != nil {
			email = p.User.Email
		}
		buildCount, _ := c.DB.AdminGetProjectBuildCount(p.ID.String())
		items[i] = responses.AdminProjectResponse{
			ID: p.ID, UserID: p.UserID, UserEmail: email,
			URL: p.URL, PackageName: p.PackageName,
			BuildCount: buildCount, CreatedAt: p.CreatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetProject(projectID string) (*responses.AdminProjectResponse, error) {
	p, err := c.DB.AdminGetProjectByID(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	email := ""
	if p.User != nil {
		email = p.User.Email
	}
	buildCount, _ := c.DB.AdminGetProjectBuildCount(p.ID.String())
	return &responses.AdminProjectResponse{
		ID: p.ID, UserID: p.UserID, UserEmail: email,
		URL: p.URL, PackageName: p.PackageName,
		BuildCount: buildCount, CreatedAt: p.CreatedAt,
	}, nil
}

func (c *Controller) AdminDeleteProject(projectID string) error {
	return c.DB.AdminDeleteProject(projectID)
}
```

Create `internal/controllers/admin_organizations.go`:

```go
package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListOrganizations(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	orgs, total, err := c.DB.AdminListOrganizations(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminOrgResponse, len(orgs))
	for i, o := range orgs {
		email := ""
		if o.Owner != nil {
			email = o.Owner.Email
		}
		memberCount, _ := c.DB.AdminCountOrgMembers(o.ID.String())
		items[i] = responses.AdminOrgResponse{
			ID: o.ID, Name: o.Name, Slug: o.Slug,
			OwnerEmail: email, MemberCount: memberCount, CreatedAt: o.CreatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetOrganization(orgID string) (*responses.AdminOrgDetailResponse, error) {
	org, err := c.DB.AdminGetOrganizationByID(orgID)
	if err != nil {
		return nil, fmt.Errorf("organization not found")
	}
	email := ""
	if org.Owner != nil {
		email = org.Owner.Email
	}
	memberCount, _ := c.DB.AdminCountOrgMembers(org.ID.String())

	members, _ := c.DB.AdminGetOrganizationMembers(orgID)
	memberResponses := make([]responses.AdminOrgMemberResponse, len(members))
	for i, m := range members {
		name, eml := "", ""
		if m.User != nil {
			name = m.User.Name
			eml = m.User.Email
		}
		memberResponses[i] = responses.AdminOrgMemberResponse{
			ID: m.UserID, Name: name, Email: eml, Role: m.Role,
		}
	}

	return &responses.AdminOrgDetailResponse{
		AdminOrgResponse: responses.AdminOrgResponse{
			ID: org.ID, Name: org.Name, Slug: org.Slug,
			OwnerEmail: email, MemberCount: memberCount, CreatedAt: org.CreatedAt,
		},
		Members: memberResponses,
	}, nil
}
```

- [ ] **Step 5: Create view handlers for apps, projects, organizations**

Create `internal/views/admin_apps_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListApps(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListApps(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetApp(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetApp(chi.URLParam(r, "appId"))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminUpdateApp(w http.ResponseWriter, r *http.Request) {
	var req requests.AdminUpdateAppRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := v.Controller.AdminUpdateApp(chi.URLParam(r, "appId"), req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminDeleteApp(w http.ResponseWriter, r *http.Request) {
	if err := v.Controller.AdminDeleteApp(chi.URLParam(r, "appId")); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "deleted"}, http.StatusOK)
}
```

Create `internal/views/admin_projects_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListProjects(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListProjects(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetProject(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetProject(chi.URLParam(r, "projectId"))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminDeleteProject(w http.ResponseWriter, r *http.Request) {
	if err := v.Controller.AdminDeleteProject(chi.URLParam(r, "projectId")); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "deleted"}, http.StatusOK)
}
```

Create `internal/views/admin_organizations_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListOrganizations(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListOrganizations(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetOrganization(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetOrganization(chi.URLParam(r, "orgId"))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

- [ ] **Step 6: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`

Fix any missing DB methods (like `UpdateAppProject`, `CountAppProjectsByUserID`, etc.) by checking what already exists and adding simple wrappers if missing.

- [ ] **Step 7: Commit**

```bash
git add internal/database/admin_apps.go internal/database/admin_projects.go internal/database/admin_organizations.go \
  internal/controllers/admin_apps.go internal/controllers/admin_projects.go internal/controllers/admin_organizations.go \
  internal/views/admin_apps_view.go internal/views/admin_projects_view.go internal/views/admin_organizations_view.go
git commit -m "feat(admin): add apps, projects, and organizations admin endpoints"
```

---

### Task 4: Admin Credits, Tickets, Jobs, and Deployments Endpoints

**Files:**
- Create: `internal/database/admin_credits.go`
- Create: `internal/database/admin_tickets.go`
- Create: `internal/database/admin_jobs.go`
- Create: `internal/database/admin_deployments.go`
- Create: `internal/controllers/admin_credits.go`
- Create: `internal/controllers/admin_tickets.go`
- Create: `internal/controllers/admin_jobs.go`
- Create: `internal/controllers/admin_deployments.go`
- Create: `internal/views/admin_credits_view.go`
- Create: `internal/views/admin_tickets_view.go`
- Create: `internal/views/admin_jobs_view.go`
- Create: `internal/views/admin_deployments_view.go`

- [ ] **Step 1: Create admin credits database queries**

Create `internal/database/admin_credits.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListCreditTransactions(params requests.AdminListParams) ([]models.AICreditTransaction, int, error) {
	var txs []models.AICreditTransaction
	q := db.DB.NewSelect().Model(&txs).Relation("User")

	if params.Search != "" {
		q = q.Where("\"user\".email ILIKE ?", "%"+params.Search+"%")
	}
	if params.Status != "" {
		q = q.Where("ai_credit_transaction.action = ?", params.Status)
	}

	q = q.OrderExpr("ai_credit_transaction.created_at " + params.SortDir)
	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return txs, total, err
}

func (db *Database) AdminListCreditBalances(params requests.AdminListParams) ([]models.AICreditsBalance, int, error) {
	var balances []models.AICreditsBalance
	q := db.DB.NewSelect().Model(&balances).Relation("User")

	if params.Search != "" {
		q = q.Where("\"user\".email ILIKE ?", "%"+params.Search+"%")
	}

	q = q.OrderExpr("ai_credits_balance.balance ASC") // lowest balance first
	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return balances, total, err
}

type AdminTokenUsageRow struct {
	Model           string `bun:"model"`
	Action          string `bun:"action"`
	TotalPrompt     int    `bun:"total_prompt"`
	TotalCompletion int    `bun:"total_completion"`
	TotalTokens     int    `bun:"total_tokens"`
	TotalCalls      int    `bun:"total_calls"`
}

func (db *Database) AdminGetTokenUsageAggregates() ([]AdminTokenUsageRow, error) {
	var rows []AdminTokenUsageRow
	err := db.DB.NewSelect().
		TableExpr("llm_token_usages").
		ColumnExpr("model, action").
		ColumnExpr("SUM(prompt_tokens) AS total_prompt").
		ColumnExpr("SUM(completion_tokens) AS total_completion").
		ColumnExpr("SUM(total_tokens) AS total_tokens").
		ColumnExpr("SUM(call_count) AS total_calls").
		GroupExpr("model, action").
		OrderExpr("total_tokens DESC").
		Scan(context.Background(), &rows)
	return rows, err
}
```

- [ ] **Step 2: Create admin tickets database queries**

Create `internal/database/admin_tickets.go`:

```go
package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListTickets(params requests.AdminListParams) ([]models.Ticket, int, error) {
	var tickets []models.Ticket
	q := db.DB.NewSelect().Model(&tickets).
		Relation("User").
		Where("ticket.deleted_at IS NULL")

	if params.Status != "" {
		q = q.Where("ticket.status = ?", params.Status)
	}
	if params.Search != "" {
		search := "%" + params.Search + "%"
		q = q.Where("ticket.subject ILIKE ?", search)
	}

	q = q.OrderExpr("ticket.created_at " + params.SortDir)
	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return tickets, total, err
}

func (db *Database) AdminGetTicketByID(id string) (*models.Ticket, error) {
	var ticket models.Ticket
	err := db.DB.NewSelect().Model(&ticket).
		Relation("User").
		Where("ticket.id = ?", id).
		Scan(context.Background())
	return &ticket, err
}

func (db *Database) AdminUpdateTicket(ticket *models.Ticket) (*models.Ticket, error) {
	ticket.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(ticket).Where("id = ?", ticket.ID).Exec(context.Background())
	return ticket, err
}
```

- [ ] **Step 3: Create admin jobs database queries**

Create `internal/database/admin_jobs.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListJobs(params requests.AdminListParams) ([]models.GenerationJob, int, error) {
	var jobs []models.GenerationJob
	q := db.DB.NewSelect().Model(&jobs)

	if params.Status != "" {
		q = q.Where("status = ?", params.Status)
	}
	if params.Search != "" {
		q = q.Where("type ILIKE ?", "%"+params.Search+"%")
	}

	q = q.OrderExpr("created_at " + params.SortDir)
	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return jobs, total, err
}

func (db *Database) AdminGetJobByID(id string) (*models.GenerationJob, error) {
	var job models.GenerationJob
	err := db.DB.NewSelect().Model(&job).Where("id = ?", id).Scan(context.Background())
	return &job, err
}
```

- [ ] **Step 4: Create admin deployments database queries**

Create `internal/database/admin_deployments.go`:

```go
package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func (db *Database) AdminListDeployments(params requests.AdminListParams) ([]models.AppDeployment, int, error) {
	var deps []models.AppDeployment
	q := db.DB.NewSelect().Model(&deps).
		Relation("AppProject").
		Relation("AppProject.User")

	if params.Status != "" {
		q = q.Where("app_deployment.status = ?", params.Status)
	}

	q = q.OrderExpr("app_deployment.created_at " + params.SortDir)
	total, err := q.Limit(params.PageSize).Offset(params.Offset()).ScanAndCount(context.Background())
	return deps, total, err
}
```

The model is `AppDeployment` in `internal/models/app_deployment.go` with fields: `ID`, `AppProjectID`, `AppProject`, `UserID`, `User`, `Platform`, `Provider`, `Status`, `DeployedURL`, `Version`, `CommitHash`, `ErrorMessage`, `CreatedAt`, `UpdatedAt`.

- [ ] **Step 5: Create controllers for credits, tickets, jobs, deployments**

Create `internal/controllers/admin_credits.go`:

```go
package controllers

import (
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListCreditTransactions(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	txs, total, err := c.DB.AdminListCreditTransactions(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminCreditTransactionResponse, len(txs))
	for i, tx := range txs {
		email := ""
		if tx.User != nil {
			email = tx.User.Email
		}
		items[i] = responses.AdminCreditTransactionResponse{
			ID: tx.ID, UserID: tx.UserID, UserEmail: email,
			Amount: tx.Amount, Action: tx.Action, Reference: tx.Reference,
			CreatedAt: tx.CreatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminListCreditBalances(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	balances, total, err := c.DB.AdminListCreditBalances(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminCreditBalanceResponse, len(balances))
	for i, b := range balances {
		email := ""
		if b.User != nil {
			email = b.User.Email
		}
		items[i] = responses.AdminCreditBalanceResponse{
			UserID: b.UserID, UserEmail: email, Balance: b.Balance,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetTokenUsage() ([]responses.AdminTokenUsageResponse, error) {
	rows, err := c.DB.AdminGetTokenUsageAggregates()
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminTokenUsageResponse, len(rows))
	for i, r := range rows {
		items[i] = responses.AdminTokenUsageResponse{
			Model: r.Model, Action: r.Action,
			TotalPrompt: r.TotalPrompt, TotalCompletion: r.TotalCompletion,
			TotalTokens: r.TotalTokens, TotalCalls: r.TotalCalls,
		}
	}
	return items, nil
}
```

Create `internal/controllers/admin_tickets.go`:

```go
package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListTickets(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	tickets, total, err := c.DB.AdminListTickets(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminTicketResponse, len(tickets))
	for i, t := range tickets {
		email := ""
		if t.User.Email != "" {
			email = t.User.Email
		}
		items[i] = responses.AdminTicketResponse{
			ID: t.ID, UserID: t.UserID, UserEmail: email,
			Subject: t.Subject, Message: t.Message,
			Status: t.Status, Priority: t.Priority,
			Response: t.Response, SenderName: t.SenderName, SenderEmail: t.SenderEmail,
			CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetTicket(ticketID string) (*responses.AdminTicketResponse, error) {
	t, err := c.DB.AdminGetTicketByID(ticketID)
	if err != nil {
		return nil, fmt.Errorf("ticket not found")
	}
	email := ""
	if t.User.Email != "" {
		email = t.User.Email
	}
	return &responses.AdminTicketResponse{
		ID: t.ID, UserID: t.UserID, UserEmail: email,
		Subject: t.Subject, Message: t.Message,
		Status: t.Status, Priority: t.Priority,
		Response: t.Response, SenderName: t.SenderName, SenderEmail: t.SenderEmail,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}, nil
}

func (c *Controller) AdminUpdateTicket(ticketID string, req requests.AdminUpdateTicketRequest) (*responses.AdminTicketResponse, error) {
	ticket, err := c.DB.AdminGetTicketByID(ticketID)
	if err != nil {
		return nil, fmt.Errorf("ticket not found")
	}

	if req.Status != nil {
		ticket.Status = *req.Status
	}
	if req.Response != nil {
		ticket.Response = *req.Response
	}
	if req.Priority != nil {
		ticket.Priority = *req.Priority
	}

	if _, err := c.DB.AdminUpdateTicket(ticket); err != nil {
		return nil, fmt.Errorf("failed to update ticket")
	}

	// Send email notification if response was added
	if req.Response != nil && *req.Response != "" && ticket.SenderEmail != "" {
		go c.EmailService.SendTicketResponseEmail(ticket.SenderEmail, ticket.Subject, *req.Response)
	}

	return c.AdminGetTicket(ticketID)
}
```

Create `internal/controllers/admin_jobs.go`:

```go
package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListJobs(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	jobs, total, err := c.DB.AdminListJobs(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminJobResponse, len(jobs))
	for i, j := range jobs {
		items[i] = responses.AdminJobResponse{
			ID: j.ID, UserID: j.UserID,
			Type: j.Type, Status: j.Status, Error: j.Error,
			CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}

func (c *Controller) AdminGetJob(jobID string) (map[string]any, error) {
	job, err := c.DB.AdminGetJobByID(jobID)
	if err != nil {
		return nil, fmt.Errorf("job not found")
	}

	result := map[string]any{
		"id":         job.ID,
		"user_id":    job.UserID,
		"type":       job.Type,
		"status":     job.Status,
		"error":      job.Error,
		"created_at": job.CreatedAt,
		"updated_at": job.UpdatedAt,
	}

	if len(job.Events) > 0 {
		var events []any
		json.Unmarshal(job.Events, &events)
		result["events"] = events
	}
	if len(job.Result) > 0 {
		var res any
		json.Unmarshal(job.Result, &res)
		result["result"] = res
	}

	return result, nil
}
```

Create `internal/controllers/admin_deployments.go`:

```go
package controllers

import (
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListDeployments(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()
	deps, total, err := c.DB.AdminListDeployments(params)
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminDeploymentResponse, len(deps))
	for i, d := range deps {
		appName, email := "", ""
		if d.AppProject != nil {
			appName = d.AppProject.Name
			if d.AppProject.User != nil {
				email = d.AppProject.User.Email
			}
		}
		items[i] = responses.AdminDeploymentResponse{
			ID: d.ID, AppID: d.AppProjectID,
			AppName: appName, UserEmail: email,
			Platform: d.Platform, Status: d.Status,
			URL: d.DeployedURL, CreatedAt: d.CreatedAt,
		}
	}

	totalPages := (total + params.PageSize - 1) / params.PageSize
	return &responses.PaginatedResponse{
		Data: items, Total: total, Page: params.Page, PageSize: params.PageSize, TotalPages: totalPages,
	}, nil
}
```

Note: Read `internal/models/app_deployment.go` first to confirm exact field names (AppProjectID vs ProjectID, URL field name, etc.) and adjust the code above accordingly.

- [ ] **Step 6: Create view handlers for credits, tickets, jobs, deployments**

Create `internal/views/admin_credits_view.go`:

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListCreditTransactions(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListCreditTransactions(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminListCreditBalances(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListCreditBalances(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetTokenUsage(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetTokenUsage()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, map[string]any{"usage": result}, http.StatusOK)
}
```

Create `internal/views/admin_tickets_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListTickets(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListTickets(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetTicket(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetTicket(chi.URLParam(r, "ticketId"))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminUpdateTicket(w http.ResponseWriter, r *http.Request) {
	var req requests.AdminUpdateTicketRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := v.Controller.AdminUpdateTicket(chi.URLParam(r, "ticketId"), req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

Create `internal/views/admin_jobs_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListJobs(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListJobs(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) AdminGetJob(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetJob(chi.URLParam(r, "jobId"))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

Create `internal/views/admin_deployments_view.go`:

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListDeployments(w http.ResponseWriter, r *http.Request) {
	params := adminListParamsFromQuery(r)
	result, err := v.Controller.AdminListDeployments(params)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

- [ ] **Step 7: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`

The `SendTicketResponseEmail` method may not exist on `EmailService`. If not, skip the email notification for now — just update the ticket in the DB. The email can be added later when the Brevo template exists.

- [ ] **Step 8: Commit**

```bash
git add internal/database/admin_credits.go internal/database/admin_tickets.go internal/database/admin_jobs.go internal/database/admin_deployments.go \
  internal/controllers/admin_credits.go internal/controllers/admin_tickets.go internal/controllers/admin_jobs.go internal/controllers/admin_deployments.go \
  internal/views/admin_credits_view.go internal/views/admin_tickets_view.go internal/views/admin_jobs_view.go internal/views/admin_deployments_view.go
git commit -m "feat(admin): add credits, tickets, jobs, and deployments admin endpoints"
```

---

### Task 5: Admin Subscriptions and Analytics Endpoints

**Files:**
- Create: `internal/database/admin_analytics.go`
- Create: `internal/controllers/admin_subscriptions.go`
- Create: `internal/controllers/admin_analytics.go`
- Create: `internal/views/admin_subscriptions_view.go`
- Create: `internal/views/admin_analytics_view.go`

- [ ] **Step 1: Create admin analytics database queries**

Create `internal/database/admin_analytics.go`:

```go
package database

import (
	"context"
	"time"
)

type SignupsByDay struct {
	Date  string `bun:"date"`
	Count int    `bun:"count"`
}

func (db *Database) AdminGetSignupsByDay(days int) ([]SignupsByDay, error) {
	var rows []SignupsByDay
	since := time.Now().AddDate(0, 0, -days)
	err := db.DB.NewSelect().
		TableExpr("users").
		ColumnExpr("DATE(created_at) AS date").
		ColumnExpr("COUNT(*) AS count").
		Where("created_at >= ?", since).
		Where("deleted_at IS NULL").
		GroupExpr("DATE(created_at)").
		OrderExpr("date ASC").
		Scan(context.Background(), &rows)
	return rows, err
}

func (db *Database) AdminGetActiveUsersByDay(days int) ([]SignupsByDay, error) {
	var rows []SignupsByDay
	since := time.Now().AddDate(0, 0, -days)
	err := db.DB.NewSelect().
		TableExpr("tokens").
		ColumnExpr("DATE(created_at) AS date").
		ColumnExpr("COUNT(DISTINCT user_id) AS count").
		Where("created_at >= ?", since).
		Where("deleted_at IS NULL").
		GroupExpr("DATE(created_at)").
		OrderExpr("date ASC").
		Scan(context.Background(), &rows)
	return rows, err
}
```

- [ ] **Step 2: Create admin subscriptions controller**

Create `internal/controllers/admin_subscriptions.go`:

```go
package controllers

import (
	"fmt"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminListSubscriptions() ([]responses.AdminSubscriptionPlanResponse, error) {
	subs, err := c.DB.GetAllSubscriptions()
	if err != nil {
		return nil, err
	}

	items := make([]responses.AdminSubscriptionPlanResponse, len(subs))
	for i, s := range subs {
		userCount, _ := c.DB.CountUsersBySubscriptionID(s.ID.String())

		var limits any
		if l, err := c.DB.GetStandartUserLimitsById(s.LimitsID.String()); err == nil {
			limits = l
		}

		items[i] = responses.AdminSubscriptionPlanResponse{
			ID:           s.ID,
			Name:         s.Name,
			MonthlyPrice: s.MonthlyPrice,
			AnnualPrice:  s.AnnualPrice,
			UserCount:    userCount,
			LimitsID:     s.LimitsID,
			Limits:       limits,
			CreatedAt:    s.CreatedAt,
		}
	}
	return items, nil
}

func (c *Controller) AdminUpdateSubscription(subID string, req requests.AdminUpdateSubscriptionPlanRequest) (*responses.AdminSubscriptionPlanResponse, error) {
	sub, err := c.DB.GetSubscriptionById(subID)
	if err != nil {
		return nil, fmt.Errorf("subscription not found")
	}

	if req.Name != nil {
		sub.Name = *req.Name
	}
	if req.MonthlyPrice != nil {
		sub.MonthlyPrice = *req.MonthlyPrice
	}
	if req.AnnualPrice != nil {
		sub.AnnualPrice = *req.AnnualPrice
	}

	if _, err := c.DB.UpdateSubscription(sub); err != nil {
		return nil, fmt.Errorf("failed to update subscription")
	}

	// Update limits if any limit fields are provided
	limits, err := c.DB.GetStandartUserLimitsById(sub.LimitsID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get limits")
	}

	if req.MaximumProjectCount != nil { limits.MaximumProjectCount = *req.MaximumProjectCount }
	if req.MaximumCredentialCount != nil { limits.MaximumCredentialCount = *req.MaximumCredentialCount }
	if req.MontlyNotificationCount != nil { limits.MontlyNotificationCount = *req.MontlyNotificationCount }
	if req.MontlyBuildCount != nil { limits.MontlyBuildCount = *req.MontlyBuildCount }
	if req.MonthlyAICredits != nil { limits.MonthlyAICredits = *req.MonthlyAICredits }
	if req.MaxWebApps != nil { limits.MaxWebApps = *req.MaxWebApps }
	if req.MaxMobileApps != nil { limits.MaxMobileApps = *req.MaxMobileApps }
	if req.MaxDeployedWebApps != nil { limits.MaxDeployedWebApps = *req.MaxDeployedWebApps }
	if req.CanBuildToAndroid != nil { limits.CanBuildToAndroid = *req.CanBuildToAndroid }
	if req.CanBuildToIOS != nil { limits.CanBuildToIOS = *req.CanBuildToIOS }
	if req.CanUseAnalytics != nil { limits.CanUseAnalytics = *req.CanUseAnalytics }
	if req.CanRemoveBrandLogo != nil { limits.CanRemoveBrandLogo = *req.CanRemoveBrandLogo }
	if req.CanUseEcommerce != nil { limits.CanUseEcommerce = *req.CanUseEcommerce }
	if req.CanDeployWeb != nil { limits.CanDeployWeb = *req.CanDeployWeb }
	if req.CanAccessCode != nil { limits.CanAccessCode = *req.CanAccessCode }
	if req.CanBuyDomain != nil { limits.CanBuyDomain = *req.CanBuyDomain }
	if req.CanExportToGitHub != nil { limits.CanExportToGitHub = *req.CanExportToGitHub }
	if req.CanUseEmailService != nil { limits.CanUseEmailService = *req.CanUseEmailService }
	if req.CanBuildMobile != nil { limits.CanBuildMobile = *req.CanBuildMobile }

	if _, err := c.DB.UpdateStandartUserLimits(limits); err != nil {
		return nil, fmt.Errorf("failed to update limits")
	}

	userCount, _ := c.DB.CountUsersBySubscriptionID(sub.ID.String())

	return &responses.AdminSubscriptionPlanResponse{
		ID: sub.ID, Name: sub.Name, MonthlyPrice: sub.MonthlyPrice,
		AnnualPrice: sub.AnnualPrice, UserCount: userCount,
		LimitsID: sub.LimitsID, Limits: limits, CreatedAt: sub.CreatedAt,
	}, nil
}
```

- [ ] **Step 3: Create admin analytics controller**

Create `internal/controllers/admin_analytics.go`:

```go
package controllers

import (
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) AdminGetAnalyticsOverview() (*responses.AdminAnalyticsOverviewResponse, error) {
	signups, err := c.DB.AdminGetSignupsByDay(30)
	if err != nil {
		return nil, err
	}
	active, err := c.DB.AdminGetActiveUsersByDay(30)
	if err != nil {
		return nil, err
	}

	signupCounts := make([]responses.DayCount, len(signups))
	for i, s := range signups {
		signupCounts[i] = responses.DayCount{Date: s.Date, Count: s.Count}
	}
	activeCounts := make([]responses.DayCount, len(active))
	for i, a := range active {
		activeCounts[i] = responses.DayCount{Date: a.Date, Count: a.Count}
	}

	return &responses.AdminAnalyticsOverviewResponse{
		SignupsByDay: signupCounts,
		ActiveByDay:  activeCounts,
	}, nil
}
```

- [ ] **Step 4: Create view handlers**

Create `internal/views/admin_subscriptions_view.go`:

```go
package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminListSubscriptions(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminListSubscriptions()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, map[string]any{"subscriptions": result}, http.StatusOK)
}

func (v *HttpViews) AdminUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	var req requests.AdminUpdateSubscriptionPlanRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := v.Controller.AdminUpdateSubscription(chi.URLParam(r, "subId"), req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

Create `internal/views/admin_analytics_view.go`:

```go
package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) AdminGetAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	result, err := v.Controller.AdminGetAnalyticsOverview()
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	utils.JSONResponse(w, result, http.StatusOK)
}
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`

Check for missing DB methods: `GetAllSubscriptions`, `CountUsersBySubscriptionID`, `GetSubscriptionById`, `UpdateSubscription`. Add any missing ones as simple Bun queries in the appropriate database files.

- [ ] **Step 6: Commit**

```bash
git add internal/database/admin_analytics.go \
  internal/controllers/admin_subscriptions.go internal/controllers/admin_analytics.go \
  internal/views/admin_subscriptions_view.go internal/views/admin_analytics_view.go
git commit -m "feat(admin): add subscriptions and analytics admin endpoints"
```

---

### Task 6: Expand Dashboard and Register All Admin Routes

**Files:**
- Modify: `internal/database/admin.go` — add more stats
- Modify: `internal/controllers/admin.go` — expand dashboard
- Modify: `internal/responses/admin.go` — expand dashboard response
- Modify: `cmd/main.go` — register all new admin routes

- [ ] **Step 1: Expand dashboard stats**

Add to `internal/database/admin.go`, after the existing `GetAIUsageStats` function:

```go
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
	return db.DB.NewSelect().TableExpr("app_deployments").Where("status = 'active'").Count(ctx)
}

func (db *Database) GetOpenTicketCount() (int, error) {
	ctx := context.Background()
	return db.DB.NewSelect().TableExpr("tickets").Where("deleted_at IS NULL").Where("status = 'open'").Count(ctx)
}
```

- [ ] **Step 2: Expand dashboard response**

In `internal/responses/admin.go`, update the `AdminDashboardResponse`:

```go
type AdminDashboardResponse struct {
	Users          *database.AdminStats   `json:"users"`
	Revenue        *database.RevenueStats `json:"revenue"`
	AIUsage        *database.AIUsageStats `json:"ai_usage"`
	TotalApps      int                    `json:"total_apps"`
	DeployedApps   int                    `json:"deployed_apps"`
	ActiveDeploys  int                    `json:"active_deploys"`
	OpenTickets    int                    `json:"open_tickets"`
}
```

- [ ] **Step 3: Expand dashboard controller**

Update `internal/controllers/admin.go` to populate new fields:

```go
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

	totalApps, deployedApps, _ := c.DB.GetAppStats()
	activeDeploys, _ := c.DB.GetDeploymentStats()
	openTickets, _ := c.DB.GetOpenTicketCount()

	return &responses.AdminDashboardResponse{
		Users:         userStats,
		Revenue:       revenueStats,
		AIUsage:       aiStats,
		TotalApps:     totalApps,
		DeployedApps:  deployedApps,
		ActiveDeploys: activeDeploys,
		OpenTickets:   openTickets,
	}, nil
}
```

- [ ] **Step 4: Register all admin routes in cmd/main.go**

Replace the existing admin route block (around line 1138) with:

```go
r.Route("/admin", func(r chi.Router) {
	r.Use(appMiddleware.RequireAuth(controller))
	r.Use(appMiddleware.RequireAdmin())

	r.Get("/dashboard", httpViews.GetAdminDashboard)

	r.Route("/users", func(r chi.Router) {
		r.Get("/", httpViews.AdminListUsers)
		r.Route("/{userId}", func(r chi.Router) {
			r.Get("/", httpViews.AdminGetUser)
			r.Put("/", httpViews.AdminUpdateUser)
			r.Delete("/", httpViews.AdminDeleteUser)
			r.Post("/impersonate", httpViews.AdminImpersonateUser)
			r.Post("/reset-password", httpViews.AdminResetPassword)
			r.Post("/credits", httpViews.AdminGrantCredits)
			r.Post("/subscription", httpViews.AdminForceSubscription)
		})
	})

	r.Route("/apps", func(r chi.Router) {
		r.Get("/", httpViews.AdminListApps)
		r.Route("/{appId}", func(r chi.Router) {
			r.Get("/", httpViews.AdminGetApp)
			r.Put("/", httpViews.AdminUpdateApp)
			r.Delete("/", httpViews.AdminDeleteApp)
		})
	})

	r.Route("/projects", func(r chi.Router) {
		r.Get("/", httpViews.AdminListProjects)
		r.Route("/{projectId}", func(r chi.Router) {
			r.Get("/", httpViews.AdminGetProject)
			r.Delete("/", httpViews.AdminDeleteProject)
		})
	})

	r.Route("/subscriptions", func(r chi.Router) {
		r.Get("/", httpViews.AdminListSubscriptions)
		r.Put("/{subId}", httpViews.AdminUpdateSubscription)
	})

	r.Route("/credits", func(r chi.Router) {
		r.Get("/transactions", httpViews.AdminListCreditTransactions)
		r.Get("/balances", httpViews.AdminListCreditBalances)
		r.Get("/usage", httpViews.AdminGetTokenUsage)
	})

	r.Route("/organizations", func(r chi.Router) {
		r.Get("/", httpViews.AdminListOrganizations)
		r.Get("/{orgId}", httpViews.AdminGetOrganization)
	})

	r.Route("/tickets", func(r chi.Router) {
		r.Get("/", httpViews.AdminListTickets)
		r.Route("/{ticketId}", func(r chi.Router) {
			r.Get("/", httpViews.AdminGetTicket)
			r.Put("/", httpViews.AdminUpdateTicket)
		})
	})

	r.Route("/jobs", func(r chi.Router) {
		r.Get("/", httpViews.AdminListJobs)
		r.Get("/{jobId}", httpViews.AdminGetJob)
	})

	r.Get("/deployments", httpViews.AdminListDeployments)

	r.Get("/analytics/overview", httpViews.AdminGetAnalyticsOverview)
})
```

- [ ] **Step 5: Add CORS support for admin panel origin**

The admin panel runs on a different origin (e.g. `localhost:3000`). Check if the existing CORS middleware in `cmd/main.go` already allows this. If not, ensure the CORS configuration includes the admin panel's origin. Look for `cors.Handler` or `cors.New` in main.go and add `localhost:3000` to allowed origins.

- [ ] **Step 6: Verify it compiles and run**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`

- [ ] **Step 7: Commit**

```bash
git add internal/database/admin.go internal/controllers/admin.go internal/responses/admin.go cmd/main.go
git commit -m "feat(admin): expand dashboard and register all admin routes"
```

---

### Task 7: Add Missing Database Helper Methods

During the previous tasks, some controller code references DB methods that may not exist yet. This task ensures all are present.

**Files:**
- Modify: various `internal/database/*.go` files

- [ ] **Step 1: Audit and add missing methods**

Grep for any compilation errors after running `go build ./...`. For each missing method, add it to the appropriate database file. Common ones likely needed:

In `internal/database/users.go` (or wherever user queries live):
```go
func (db *Database) CountProjectsByUserID(userID string) (int, error) {
	return db.DB.NewSelect().TableExpr("projects").Where("user_id = ?", userID).Where("deleted_at IS NULL").Count(context.Background())
}

func (db *Database) CountAppProjectsByUserID(userID string) (int, error) {
	return db.DB.NewSelect().TableExpr("app_projects").Where("user_id = ?", userID).Where("deleted_at IS NULL").Count(context.Background())
}

func (db *Database) CountOrganizationsByUserID(userID string) (int, error) {
	return db.DB.NewSelect().TableExpr("organization_members").Where("user_id = ?", userID).Where("deleted_at IS NULL").Count(context.Background())
}
```

In `internal/database/subscriptions.go`:
```go
func (db *Database) GetAllSubscriptions() ([]models.StandartUserSubscription, error) {
	var subs []models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&subs).Where("deleted_at IS NULL").Order("monthly_price ASC").Scan(context.Background())
	return subs, err
}

func (db *Database) GetSubscriptionById(id string) (*models.StandartUserSubscription, error) {
	var sub models.StandartUserSubscription
	err := db.DB.NewSelect().Model(&sub).Where("id = ?", id).Scan(context.Background())
	return &sub, err
}

func (db *Database) UpdateSubscription(sub *models.StandartUserSubscription) (*models.StandartUserSubscription, error) {
	sub.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(sub).Where("id = ?", sub.ID).Exec(context.Background())
	return sub, err
}

func (db *Database) CountUsersBySubscriptionID(subID string) (int, error) {
	return db.DB.NewSelect().TableExpr("users").Where("subscription_id = ?", subID).Where("deleted_at IS NULL").Count(context.Background())
}
```

Check which of these already exist by grepping first. Only add what's missing.

- [ ] **Step 2: Verify full build**

Run: `cd /home/knid/Projects/wemob/wemob-backend && go build ./...`
Expected: clean build, no errors

- [ ] **Step 3: Commit**

```bash
git add -A internal/database/
git commit -m "feat(admin): add missing database helper methods for admin endpoints"
```
