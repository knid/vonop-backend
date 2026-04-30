// cmd/migrate_resources_to_org backfills organization_id on five tables:
// apple_connections, google_play_connections, deployments, api_keys, webhook_endpoints.
// Each row's organization_id is set to its user's personal-org id. Idempotent +
// dry-run.
//
// Usage:
//   go run cmd/migrate_resources_to_org/main.go --dry-run
//   go run cmd/migrate_resources_to_org/main.go
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/knid/wemob/internal/bootstrap"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print counts without writing")
	flag.Parse()

	ctrl, _ := bootstrap.BootstrapController()
	if ctrl == nil {
		slog.Error("bootstrap returned nil controller")
		os.Exit(1)
	}

	tables := []string{
		"apple_connections",
		"google_play_connections",
		"deployments",
		"api_keys",
		"webhook_endpoints",
	}

	for _, t := range tables {
		count, err := ctrl.DB.CountRowsWithoutOrg(t)
		if err != nil {
			slog.Error("count failed", "table", t, "error", err)
			os.Exit(1)
		}
		fmt.Printf("%s without organization_id: %d\n", t, count)
		if *dryRun {
			continue
		}
		updated, err := ctrl.DB.BulkAssignOrgFromPersonalOrg(t)
		if err != nil {
			slog.Error("bulk assign failed", "table", t, "error", err)
			os.Exit(1)
		}
		fmt.Printf("  updated %d rows in %s\n", updated, t)
	}
	fmt.Println("done.")
}
