// migrate-to-fly walks every runtime_apps row that still holds a Coolify app
// UUID and redeploys the underlying AppProject onto Fly Machines, then deletes
// the Coolify resource.
//
// Idempotent: re-running the binary picks up where it left off.
package main

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/fly"
)

func main() {
	controller, _ := bootstrap.BootstrapController()

	rows, err := controller.DB.GetAllRuntimeAppsOnCoolify()
	if err != nil {
		slog.Error("list rows failed", "err", err)
		os.Exit(1)
	}
	slog.Info("starting migration", "rows", len(rows))

	sem := make(chan struct{}, 4) // max 4 concurrent migrations
	var wg sync.WaitGroup
	var mu sync.Mutex
	var successCount, failureCount int

	for i := range rows {
		ra := &rows[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			if err := migrateOneApp(controller, ra); err != nil {
				slog.Error("migration failed",
					"app_project_id", ra.AppProjectID,
					"fly_app_name", "wemob-"+ra.GiteaRepoName,
					"err", err)
				mu.Lock()
				failureCount++
				mu.Unlock()
				return
			}
			// Tear down Coolify resource AFTER Fly is healthy.
			if err := controller.CoolifyService.DeleteApp(ra.CoolifyAppUUID); err != nil {
				slog.Warn("coolify delete failed (non-fatal — runtime row already moved)",
					"coolify_app_uuid", ra.CoolifyAppUUID, "err", err)
			}
			if err := controller.DB.ClearCoolifyAppUUID(ra.ID.String()); err != nil {
				slog.Warn("clear coolify uuid failed",
					"runtime_app_id", ra.ID, "err", err)
			}
			mu.Lock()
			successCount++
			mu.Unlock()
		}()
	}
	wg.Wait()
	slog.Info("migration complete", "success", successCount, "failure", failureCount)
}

func migrateOneApp(c *controllers.Controller, ra *models.RuntimeApp) error {
	project, err := c.DB.GetAppProjectByID(ra.AppProjectID.String())
	if err != nil {
		return err
	}
	user, err := c.DB.GetUserById(project.UserID.String())
	if err != nil {
		return err
	}

	flyAppName := "wemob-" + project.Slug
	if err := c.FlyService.EnsureApp(flyAppName); err != nil {
		return err
	}
	if err := c.FlyService.SyncSecrets(flyAppName, c.CollectAllEnvVars(project)); err != nil {
		return err
	}

	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return err
	}

	imageRef, _, err := c.FlyService.BuildAndPush(flyAppName, repo.Path, "migration")
	if err != nil {
		return err
	}

	limits, _ := c.GetUserLimits(user)
	spec := fly.PlanMachineSpec(limits, c.FlyService.Region)
	spec.Image = imageRef
	if err := c.FlyService.DeployMachines(flyAppName, spec); err != nil {
		return err
	}
	if err := c.FlyService.WaitForHealthy(flyAppName, 5*time.Minute); err != nil {
		return err
	}

	ra.FlyAppName = flyAppName
	ra.CurrentImageRef = imageRef
	ra.LastDeployedAt = time.Now()
	if _, err := c.DB.UpdateRuntimeApp(ra); err != nil {
		return err
	}
	return nil
}
