// cmd/migrate_credits_to_org backfills organization_id on:
//   1. ai_credits_balances rows (set to user's personal org)
//   2. ai_credit_transactions rows (set to user's personal org)
//
// Idempotent: only updates rows where organization_id IS NULL.
// Run with --dry-run first to preview counts.
//
// Usage:
//   go run cmd/migrate_credits_to_org/main.go --dry-run
//   go run cmd/migrate_credits_to_org/main.go
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/controllers"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print actions without writing")
	flag.Parse()

	ctrl, _ := bootstrap.BootstrapController()
	if ctrl == nil {
		slog.Error("bootstrap returned nil controller")
		os.Exit(1)
	}

	if err := backfillBalances(ctrl, *dryRun); err != nil {
		slog.Error("balance backfill failed", "error", err)
		os.Exit(1)
	}
	if err := backfillTransactions(ctrl, *dryRun); err != nil {
		slog.Error("transaction backfill failed", "error", err)
		os.Exit(1)
	}

	fmt.Println("done.")
}

func backfillBalances(ctrl *controllers.Controller, dry bool) error {
	rows, err := ctrl.DB.ListBalancesWithoutOrg()
	if err != nil {
		return fmt.Errorf("list balances: %w", err)
	}
	fmt.Printf("ai_credits_balances without organization_id: %d\n", len(rows))
	for _, b := range rows {
		owner, err := ctrl.DB.GetUserById(b.UserID.String())
		if err != nil {
			slog.Error("balance owner lookup failed", "balance_id", b.ID, "user_id", b.UserID, "error", err)
			continue
		}
		org, err := ctrl.EnsurePersonalOrg(owner)
		if err != nil {
			slog.Error("ensure personal org failed", "user_id", owner.ID, "error", err)
			continue
		}
		if dry {
			fmt.Printf("  would attach balance %s -> org %s\n", b.ID, org.ID)
			continue
		}
		if err := ctrl.DB.AssignBalanceOrg(b.ID.String(), org.ID); err != nil {
			slog.Error("attach balance failed", "balance_id", b.ID, "error", err)
			continue
		}
		fmt.Printf("  attached balance %s -> org %s\n", b.ID, org.ID)
	}
	return nil
}

func backfillTransactions(ctrl *controllers.Controller, dry bool) error {
	count, err := ctrl.DB.CountTransactionsWithoutOrg()
	if err != nil {
		return fmt.Errorf("count txns: %w", err)
	}
	fmt.Printf("ai_credit_transactions without organization_id: %d\n", count)
	if dry {
		fmt.Println("  (dry-run: bulk update would set organization_id from each row's owner's personal org)")
		return nil
	}
	updated, err := ctrl.DB.BulkAssignTransactionOrgsFromPersonalOrg()
	if err != nil {
		return fmt.Errorf("bulk update txns: %w", err)
	}
	fmt.Printf("  updated %d transactions\n", updated)
	return nil
}
