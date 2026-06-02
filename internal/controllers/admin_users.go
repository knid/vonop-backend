package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// AdminListUsers returns a paginated list of users with subscription info.
func (c *Controller) AdminListUsers(params requests.AdminListParams) (*responses.PaginatedResponse, error) {
	params.Defaults()

	dbParams := database.AdminUserListParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Search:   params.Search,
		Status:   params.Status,
		SortBy:   params.SortBy,
		SortDir:  params.SortDir,
		From:     params.From,
		To:       params.To,
	}

	users, total, err := c.DB.AdminListUsers(dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}

	items := make([]responses.AdminUserResponse, len(users))
	for i, u := range users {
		// Source billing from the user's personal org (Plan A — org-billed).
		// N+1 is acceptable here: admin lists are paginated to ~50 users.
		uCopy := u
		billing := c.GetUserBilling(&uCopy)
		subName := ""
		if billing.Subscription != nil {
			subName = billing.Subscription.Name
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
			SubscriptionStatus: billing.SubscriptionStatus,
			SubscriptionName:   subName,
			CustomerID:         billing.StripeCustomerID,
			TrialUsed:          billing.TrialUsed,
			SetupFinished:      u.SetupFinished,
			CreatedAt:          u.CreatedAt,
			UpdatedAt:          u.UpdatedAt,
		}
	}

	totalPages := total / params.PageSize
	if total%params.PageSize != 0 {
		totalPages++
	}

	return &responses.PaginatedResponse{
		Data:       items,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}

// adminUserDetail builds a detailed user response including counts and credit balance.
func (c *Controller) adminUserDetail(user *models.User) (*responses.AdminUserDetailResponse, error) {
	billing := c.GetUserBilling(user)
	subName := ""
	if billing.Subscription != nil {
		subName = billing.Subscription.Name
	}

	// Credit balance — resolved via the user's personal org so admin views
	// reflect the same balance the user sees.
	creditsBalance := 0
	if org, err := c.DB.GetPersonalOrgByUserID(user.ID.String()); err == nil && org != nil {
		if bal, err := c.DB.GetAICreditsBalanceByOrg(org.ID.String()); err == nil {
			creditsBalance = bal.Balance
		}
	}

	// Counts
	appCount, _ := c.DB.CountAppProjectsByUserID(user.ID.String())
	projectCount, _ := c.DB.CountProjectsByUserID(user.ID.String())
	orgCount, _ := c.DB.CountOrganizationsByUserID(user.ID.String())

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
			SubscriptionStatus: billing.SubscriptionStatus,
			SubscriptionName:   subName,
			CustomerID:         billing.StripeCustomerID,
			TrialUsed:          billing.TrialUsed,
			SetupFinished:      user.SetupFinished,
			CreatedAt:          user.CreatedAt,
			UpdatedAt:          user.UpdatedAt,
		},
		CreditsBalance: creditsBalance,
		AppCount:       appCount,
		ProjectCount:   projectCount,
		OrgCount:       orgCount,
	}, nil
}

// AdminGetUser returns detailed info for a single user.
func (c *Controller) AdminGetUser(userID string) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.AdminGetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}
	return c.adminUserDetail(user)
}

// AdminUpdateUser updates a user's basic fields and returns the updated detail.
func (c *Controller) AdminUpdateUser(userID string, req requests.AdminUpdateUserRequest) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.AdminGetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
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

	user.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update user: %w", err)
	}

	// Re-fetch with relations
	user, err = c.DB.AdminGetUserByID(userID)
	if err != nil {
		return nil, err
	}
	return c.adminUserDetail(user)
}

// AdminDeleteUser soft-deletes a user and enqueues a full async purge of their
// projects, deployments, Fly apps, Neon DBs, git repos, S3 assets, owned orgs,
// and user-scoped records (via user:account-purge Asynq task).
func (c *Controller) AdminDeleteUser(userID string) error {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// Best-effort enqueue of the purge task (same task used by self-delete).
	// TaskID dedup prevents duplicate work if called multiple times.
	if enqErr := c.EnqueueUserAccountPurge(user.ID); enqErr != nil {
		slog.Warn("failed to enqueue user account purge task (admin)", "user_id", user.ID, "error", enqErr)
	}

	// Hard-delete the user row immediately (see self-delete path for rationale).
	_, err = c.DB.DB.NewDelete().Model((*models.User)(nil)).
		Where("id = ?", user.ID).
		ForceDelete().
		Exec(context.Background())
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	return nil
}

// AdminImpersonateUser creates a short-lived token allowing admin to act as the target user.
func (c *Controller) AdminImpersonateUser(userID string) (*responses.AdminImpersonateResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	expiredAt := time.Now().Add(1 * time.Hour)

	token := models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: expiredAt,
	}

	createdToken, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, fmt.Errorf("failed to create token: %w", err)
	}

	slog.Info("admin impersonation token created", "target_user", user.Email, "expires_at", expiredAt)

	return &responses.AdminImpersonateResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
		UserEmail: user.Email,
	}, nil
}

// AdminResetPassword generates a temporary password, hashes and saves it, and returns the plaintext.
func (c *Controller) AdminResetPassword(userID string) (*responses.AdminResetPasswordResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	tempPassword, err := utils.RandomHex(8) // 16-char hex string
	if err != nil {
		return nil, fmt.Errorf("failed to generate password: %w", err)
	}

	hashedPassword, err := utils.HashPassword(tempPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user.Password = hashedPassword
	user.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update password: %w", err)
	}

	slog.Info("admin reset password for user", "user_email", user.Email)

	return &responses.AdminResetPasswordResponse{
		TemporaryPassword: tempPassword,
	}, nil
}

// AdminGrantCredits adjusts a user's credit balance and records a transaction.
// Operates on the user's personal-org balance — the same balance the user
// sees in their UI and that the rest of the credit pipeline writes to.
func (c *Controller) AdminGrantCredits(userID string, req requests.AdminGrantCreditsRequest) error {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		return fmt.Errorf("personal org not found: %w", err)
	}

	balance, err := c.DB.GetAICreditsBalanceByOrg(org.ID.String())
	if err != nil {
		return fmt.Errorf("credits balance not found: %w", err)
	}

	balance.Balance += req.Amount
	if balance.Balance < 0 {
		return errors.New("resulting balance would be negative")
	}

	if _, err := c.DB.UpdateAICreditsBalance(balance); err != nil {
		return fmt.Errorf("failed to update balance: %w", err)
	}

	tx := &models.AICreditTransaction{
		OrganizationID: org.ID,
		UserID:         user.ID,
		Amount:         req.Amount,
		Action:         "admin_grant",
		Reference:      req.Reason,
	}
	if _, err := c.DB.CreateAICreditTransaction(tx); err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	slog.Info("admin granted credits", "user_email", user.Email, "amount", req.Amount, "reason", req.Reason)

	return nil
}

// AdminForceSubscription sets a user's subscription plan and status.
func (c *Controller) AdminForceSubscription(userID string, req requests.AdminForceSubscriptionRequest) (*responses.AdminUserDetailResponse, error) {
	user, err := c.DB.GetUserById(userID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	sub, err := c.DB.GetSubscriptionById(req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("subscription plan not found: %w", err)
	}

	user.SubscriptionID = sub.ID
	user.SubscriptionStatus = req.Status
	if user.SubscriptionStartAt.IsZero() {
		user.SubscriptionStartAt = time.Now()
	}
	user.UpdatedAt = time.Now()

	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to update user subscription: %w", err)
	}

	slog.Info("admin forced subscription", "user_email", user.Email, "subscription", sub.Name, "status", req.Status)

	// Re-fetch with relations
	updatedUser, err := c.DB.AdminGetUserByID(userID)
	if err != nil {
		return nil, err
	}
	return c.adminUserDetail(updatedUser)
}
