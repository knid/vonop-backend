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
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/tasks"
)

func (c *Controller) EnqueueFlyDelete(flyAppName string, runtimeAppID uuid.UUID) error {
	if flyAppName == "" {
		return nil
	}
	payload, err := json.Marshal(tasks.FlyDeleteAppPayload{
		FlyAppName:   flyAppName,
		RuntimeAppID: runtimeAppID,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeDeleteFlyApp, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue("cron"),
		asynq.MaxRetry(5),
		asynq.Timeout(2*time.Minute),
		asynq.TaskID("fly-delete-"+flyAppName),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil
		}
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

func (c *Controller) HandleDeleteFlyAppTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.FlyDeleteAppPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		slog.Error("fly_delete_unmarshal_failed", "error", err)
		return asynq.SkipRetry
	}
	if p.FlyAppName == "" {
		return asynq.SkipRetry
	}

	if err := c.FlyService.DeleteApp(p.FlyAppName); err != nil {
		retryable := fly.IsRetryable(err)
		finalAttempt := isFinalRetryAttempt(ctx)
		slog.Error("fly_delete_failed",
			"fly_app", p.FlyAppName, "runtime_app_id", p.RuntimeAppID,
			"error", err, "retryable", retryable, "final_attempt", finalAttempt)
		if retryable && !finalAttempt {
			return err
		}
		return asynq.SkipRetry
	}

	if p.RuntimeAppID != uuid.Nil {
		if _, err := c.DB.DB.NewUpdate().
			Model((*models.RuntimeApp)(nil)).
			Set("fly_app_name = ?", "").
			Set("current_image_ref = ?", "").
			Set("updated_at = ?", time.Now()).
			Where("id = ?", p.RuntimeAppID).
			WhereAllWithDeleted().
			Exec(ctx); err != nil {
			slog.Warn("fly_delete_clear_runtime_app_failed",
				"runtime_app_id", p.RuntimeAppID, "error", err)
		}
	}

	slog.Info("fly_delete_succeeded", "fly_app", p.FlyAppName, "runtime_app_id", p.RuntimeAppID)
	return nil
}
