package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/tasks"
)

// EnqueueUserAccountPurge schedules a best-effort full purge of the user's
// projects (AppProject + legacy), owned organizations (including personal),
// user-scoped data, and the user record itself. Runs asynchronously via Asynq
// so the HTTP delete can return immediately and container restarts don't lose
// the cleanup work. Matches the pattern of billing:org-grace-teardown.
func (c *Controller) EnqueueUserAccountPurge(userID uuid.UUID) error {
	if userID == uuid.Nil {
		return nil
	}
	payload, err := json.Marshal(tasks.UserAccountPurgePayload{UserID: userID})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeUserAccountPurge, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue("cron"),
		asynq.MaxRetry(5),
		asynq.Timeout(30*time.Minute),
		asynq.TaskID("user-purge-"+userID.String()),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil
		}
		return fmt.Errorf("enqueue: %w", err)
	}
	slog.Info("user_account_purge_scheduled", "user_id", userID)
	return nil
}

// HandleUserAccountPurgeTask is the Asynq handler. It performs the actual
// destructive work: infra teardowns + row deletions. Idempotent / best-effort
// (individual failures are logged and do not fail the whole task). Re-runnable.
func (c *Controller) HandleUserAccountPurgeTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.UserAccountPurgePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		slog.Error("user_purge_unmarshal_failed", "error", err)
		return asynq.SkipRetry
	}
	userID := p.UserID
	userIDStr := userID.String()

	slog.Info("user_account_purge_starting", "user_id", userID)

	// 1. AppProjects owned by this user (via user_id on the project row).
	//    This catches everything the user ever created, whether in personal org
	//    or shared orgs. We teardown infra first, then hard-delete the rows.
	appProjects, err := c.DB.GetAppProjectsByUserID(userIDStr)
	if err != nil {
		slog.Warn("user_purge_list_app_projects_failed", "user_id", userID, "error", err)
	} else {
		for i := range appProjects {
			proj := &appProjects[i]
			c.tearDownProjectInfra(ctx, proj)
			if _, delErr := c.DB.DB.NewDelete().
				Model((*models.AppProject)(nil)).
				Where("id = ?", proj.ID).
				ForceDelete().
				Exec(ctx); delErr != nil {
				slog.Warn("user_purge_delete_app_project_failed", "project_id", proj.ID, "error", delErr)
			}
		}
		slog.Info("user_purge_app_projects_done", "user_id", userID, "count", len(appProjects))
	}

	// 2. Legacy (v1) Projects owned by user. No heavy infra teardown (different
	//    hosting era), just remove the DB rows.
	legacyProjects, err := c.DB.GetProjectsByUserID(userIDStr)
	if err != nil {
		slog.Warn("user_purge_list_legacy_projects_failed", "user_id", userID, "error", err)
	} else {
		for i := range legacyProjects {
			proj := &legacyProjects[i]
			if _, delErr := c.DB.DB.NewDelete().
				Model((*models.Project)(nil)).
				Where("id = ?", proj.ID).
				ForceDelete().
				Exec(ctx); delErr != nil {
				slog.Warn("user_purge_delete_legacy_project_failed", "project_id", proj.ID, "error", delErr)
			}
		}
		slog.Info("user_purge_legacy_projects_done", "user_id", userID, "count", len(legacyProjects))
	}

	// 3. Organizations owned by this user. For each:
	//    - Hard delete members and invites (clean association data)
	//    - Hard delete other org-scoped user data (api keys, webhooks, etc.)
	//    - Hard delete the org row.
	//    Projects under these orgs were already handled in step 1 via user_id.
	ownedOrgs, err := c.DB.GetOrganizationsOwnedByUserID(userIDStr)
	if err != nil {
		slog.Warn("user_purge_list_owned_orgs_failed", "user_id", userID, "error", err)
	} else {
		for _, org := range ownedOrgs {
			orgID := org.ID

			// Hard-delete membership + invite rows for this org.
			c.DB.DB.NewDelete().Model((*models.OrganizationMember)(nil)).
				Where("organization_id = ?", orgID).ForceDelete().Exec(ctx)
			c.DB.DB.NewDelete().Model((*models.OrganizationInvite)(nil)).
				Where("organization_id = ?", orgID).ForceDelete().Exec(ctx)

			// Hard-delete other org-owned resources the user may have created.
			c.DB.DB.NewDelete().Model((*models.APIKey)(nil)).
				Where("organization_id = ?", orgID).ForceDelete().Exec(ctx)
			c.DB.DB.NewDelete().Model((*models.WebhookEndpoint)(nil)).
				Where("organization_id = ?", orgID).ForceDelete().Exec(ctx)

			// Hard-delete the org row itself (this also covers personal orgs,
			// which are normally protected in the user-facing DeleteOrganization).
			if _, delErr := c.DB.DB.NewDelete().
				Model((*models.Organization)(nil)).
				Where("id = ?", orgID).
				ForceDelete().
				Exec(ctx); delErr != nil {
				slog.Warn("user_purge_delete_org_failed", "org_id", orgID, "error", delErr)
			}
		}
		slog.Info("user_purge_orgs_done", "user_id", userID, "count", len(ownedOrgs))
	}

	// 4. User-scoped supporting data (tokens, notifications, action tokens).
	_ = c.DB.DeleteTokensByUser(ctx, userID)
	c.DB.DB.NewDelete().Model((*models.UserNotifications)(nil)).
		Where("user_id = ?", userID).ForceDelete().Exec(ctx)
	c.DB.DB.NewDelete().Model((*models.UserActionToken)(nil)).
		Where("user_id = ?", userID).ForceDelete().Exec(ctx)

	// 5. Hard-delete the user row itself. This is the final step of "remove everything".
	//    We do this last so that any lookups during the purge (if any) could still see it.
	if _, delErr := c.DB.DB.NewDelete().Model((*models.User)(nil)).
		Where("id = ?", userID).
		ForceDelete().
		Exec(ctx); delErr != nil {
		slog.Warn("user_purge_hard_delete_user_failed", "user_id", userID, "error", delErr)
	}

	slog.Info("user_account_purge_completed", "user_id", userID)
	return nil
}
