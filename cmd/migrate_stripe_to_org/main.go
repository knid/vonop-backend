// cmd/migrate_stripe_to_org backfills:
//  1. Organization billing columns from User (DB-only, single SQL).
//  2. Stripe customer metadata on each existing Stripe customer to include
//     organization_id (so the new webhook handler can route by org).
//
// Run with --dry-run first.
//
// Usage:
//
//	go run cmd/migrate_stripe_to_org/main.go --dry-run
//	go run cmd/migrate_stripe_to_org/main.go
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/knid/wemob/internal/bootstrap"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print actions without writing")
	flag.Parse()

	ctrl, _ := bootstrap.BootstrapController()
	if ctrl == nil {
		slog.Error("bootstrap nil")
		os.Exit(1)
	}

	// BootstrapController does not set stripe.Key (only the API server does).
	// Set it here from the same SecretManager so customer.Update calls work.
	stripe.Key = ctrl.SecretManager.GetSecret("STRIPE_API_KEY")

	fmt.Println("Step 1: DB backfill (organizations <- users)")
	if *dryRun {
		fmt.Println("  (dry-run: would copy 6 billing columns from each user's row to their personal org)")
	} else {
		updated, err := ctrl.DB.BulkBackfillBillingFromUsers()
		if err != nil {
			slog.Error("backfill", "error", err)
			os.Exit(1)
		}
		fmt.Printf("  updated %d org rows\n", updated)
	}

	fmt.Println("Step 2: Stripe customer metadata update")
	orgs, err := ctrl.DB.ListPersonalOrgsWithStripeCustomer()
	if err != nil {
		slog.Error("list orgs", "error", err)
		os.Exit(1)
	}
	fmt.Printf("  %d personal orgs with Stripe customer\n", len(orgs))

	for _, org := range orgs {
		if *dryRun {
			fmt.Printf("  would set metadata.organization_id=%s on customer %s\n", org.ID, org.StripeCustomerID)
			continue
		}
		_, uErr := customer.Update(org.StripeCustomerID, &stripe.CustomerParams{
			Metadata: map[string]string{
				"organization_id": org.ID.String(),
				"user_id":         org.OwnerID.String(),
			},
		})
		if uErr != nil {
			slog.Error("stripe update", "customer_id", org.StripeCustomerID, "error", uErr)
			continue
		}
		fmt.Printf("  customer %s -> org %s\n", org.StripeCustomerID, org.ID)
	}

	fmt.Println("done.")
}
