// cmd/migrate_orgs is a one-shot backfill that:
//   1. Creates a personal Organization for every User that has none.
//   2. Attaches every AppProject with OrganizationID = NULL to its owner's
//      personal org.
//
// Idempotent: re-running is safe and only acts on rows that still need it.
//
// Run with --dry-run first to see counts without writing.
//
// Usage:
//   go run cmd/migrate_orgs/main.go --dry-run
//   go run cmd/migrate_orgs/main.go
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

	if err := backfillUsers(ctrl, *dryRun); err != nil {
		slog.Error("user backfill failed", "error", err)
		os.Exit(1)
	}
	if err := backfillProjects(ctrl, *dryRun); err != nil {
		slog.Error("project backfill failed", "error", err)
		os.Exit(1)
	}

	fmt.Println("done.")
}

func backfillUsers(ctrl *controllers.Controller, dry bool) error {
	users, err := ctrl.DB.ListUsersWithoutPersonalOrg()
	if err != nil {
		return fmt.Errorf("list users: %w", err)
	}
	fmt.Printf("users without personal org: %d\n", len(users))

	for i := range users {
		u := users[i]
		if dry {
			fmt.Printf("  would create personal org for user %s (%s)\n", u.ID, u.Email)
			continue
		}
		if _, err := ctrl.EnsurePersonalOrg(&u); err != nil {
			slog.Error("ensure personal org failed", "user_id", u.ID, "error", err)
			continue
		}
		fmt.Printf("  created personal org for user %s (%s)\n", u.ID, u.Email)
	}
	return nil
}

func backfillProjects(ctrl *controllers.Controller, dry bool) error {
	projects, err := ctrl.DB.ListAppProjectsWithoutOrg()
	if err != nil {
		return fmt.Errorf("list orphan projects: %w", err)
	}
	fmt.Printf("app_projects without organization_id: %d\n", len(projects))

	for _, p := range projects {
		owner, err := ctrl.DB.GetUserById(p.UserID.String())
		if err != nil {
			slog.Error("project owner lookup failed", "project_id", p.ID, "user_id", p.UserID, "error", err)
			continue
		}
		org, err := ctrl.EnsurePersonalOrg(owner)
		if err != nil {
			slog.Error("ensure personal org for project owner failed", "project_id", p.ID, "user_id", owner.ID, "error", err)
			continue
		}
		if dry {
			fmt.Printf("  would attach project %s to org %s\n", p.ID, org.ID)
			continue
		}
		if err := ctrl.DB.AssignAppProjectOrg(p.ID.String(), org.ID); err != nil {
			slog.Error("attach project failed", "project_id", p.ID, "org_id", org.ID, "error", err)
			continue
		}
		fmt.Printf("  attached project %s -> org %s\n", p.ID, org.ID)
	}
	return nil
}
