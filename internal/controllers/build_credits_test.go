//go:build integration

package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
)

func TestChargeBuildCredits_DeductsCorrectAmountPerKind(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.Organization)(nil),
		(*models.OrganizationMember)(nil),
		(*models.AICreditsBalance)(nil),
		(*models.AICreditTransaction)(nil),
		(*models.StandardUserLimits)(nil),
		(*models.StandardUserSubscription)(nil),
	)
	c, _ := NewTestController(t, db, TestControllerOptions{})
	c.RemoteConfig = remoteconfig.New(nil) // nil PostHog → fallback-only mode

	user, org := seedUserOrgAndCredits(t, c, 200)

	cases := []struct {
		kind     constants.BuildCreditKind
		expected int
	}{
		{constants.BuildCreditKindAndroid, 20},
		{constants.BuildCreditKindIOS, 30},
		{constants.BuildCreditKindOTA, 3},
	}

	totalExpected := 0
	for _, tc := range cases {
		ref := uuid.New().String()
		if err := c.ChargeBuildCredits(user, org.ID, tc.kind, ref); err != nil {
			t.Fatalf("ChargeBuildCredits(%s): %v", tc.kind, err)
		}
		totalExpected += tc.expected
	}

	bal, err := c.GetAICreditsBalanceNoReset(user, org.ID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if got := 200 - bal.Balance; got != totalExpected {
		t.Fatalf("expected total deduction of %d credits, got %d (balance=%d)", totalExpected, got, bal.Balance)
	}
}

func TestChargeBuildCredits_UnknownKindErrors(t *testing.T) {
	db := database.NewTestDB(t, (*models.User)(nil))
	c, _ := NewTestController(t, db, TestControllerOptions{})
	c.RemoteConfig = remoteconfig.New(nil) // guard against nil-deref if the kind-lookup guard is ever reordered

	err := c.ChargeBuildCredits(&models.User{ID: uuid.New()}, uuid.New(), "bogus", "ref")
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
}

// seedUserOrgAndCredits creates a user + their personal org + a 50-credit
// "Starter"-shaped plan + an AI credits balance with the supplied amount,
// then returns the user and org.
func seedUserOrgAndCredits(t *testing.T, c *Controller, initialBalance int) (*models.User, *models.Organization) {
	t.Helper()
	ctx := context.Background()

	limits := &models.StandardUserLimits{MonthlyAICredits: 50}
	createdLimits, err := c.DB.CreateStandardUserLimits(limits)
	if err != nil {
		t.Fatalf("create limits: %v", err)
	}
	plan, err := c.DB.CreateSubscription(&models.StandardUserSubscription{
		Name: "Starter", Description: "test", LimitsID: createdLimits.ID, MonthlyPrice: 0,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	user, err := c.DB.CreateUser(&models.User{Email: "test@example.com", Name: "T"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	org := &models.Organization{
		Name: "Personal", OwnerID: user.ID, IsPersonal: true,
		SubscriptionID: plan.ID, SubscriptionStatus: "active",
	}
	if _, err := c.DB.DB.NewInsert().Model(org).Exec(ctx); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if _, err := c.DB.DB.NewInsert().Model(&models.OrganizationMember{
		OrganizationID: org.ID, UserID: user.ID, Role: "owner",
	}).Exec(ctx); err != nil {
		t.Fatalf("create org member: %v", err)
	}

	balance := &models.AICreditsBalance{
		UserID:         user.ID,
		OrganizationID: org.ID,
		Balance:        initialBalance,
		PeriodStart:    time.Now().Add(-1 * time.Hour),
		PeriodEnd:      time.Now().Add(30 * 24 * time.Hour),
	}
	if _, err := c.DB.DB.NewInsert().Model(balance).Exec(ctx); err != nil {
		t.Fatalf("create balance: %v", err)
	}

	return user, org
}
