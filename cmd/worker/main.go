package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/tasks"
)

func main() {
	// Workers only need a small pool — one connection per active task plus a
	// few for the scheduler and health checks. Ops can override via env.
	if os.Getenv("DB_MAX_OPEN_CONNS") == "" {
		os.Setenv("DB_MAX_OPEN_CONNS", "10")
	}

	controller, redisOpts := bootstrap.BootstrapController()

	// Concurrency caps total in-flight tasks across queues. The current host
	// is 1 vCPU / 2 GB and `expo export --platform all` already saturates a
	// core + ~600 MB RSS, so 8 was wildly over-budget. 6 accommodates the
	// agent-run queue added 2026-05 (LLM-call-bound, low CPU) without
	// re-introducing the lease-recovery duplicates we saw at 8. OTA publishes
	// stay serialized to 1 by expoOTAExportSemaphore in expo_ota_controller.go.
	srv := asynq.NewServer(redisOpts, asynq.Config{
		Concurrency: 6,
		Queues: map[string]int{
			"deploy-web-pro":  6,
			"deploy-web-free": 2,
			"expo-ota-pro":    4,
			"expo-ota-free":   1,
			"agent":           5, // agent runs are I/O-bound (LLM streaming);
			// weight 5 keeps them ahead of free-tier deploys
			// but behind paid web deploys.
			"cron": 1,
		},
		RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration {
			delays := []time.Duration{
				30 * time.Second,
				2 * time.Minute,
				10 * time.Minute,
				30 * time.Minute,
			}
			if n >= len(delays) {
				return delays[len(delays)-1]
			}
			return delays[n]
		},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, t *asynq.Task, err error) {
			retried, _ := asynq.GetRetryCount(ctx)
			max, _ := asynq.GetMaxRetry(ctx)
			isFinal := retried >= max
			slog.Error("asynq task failed",
				"type", t.Type(),
				"payload_bytes", len(t.Payload()),
				"retried", retried, "max_retry", max, "final", isFinal,
				"error", err)
			// Backstop: if this is the final failure for a deploy task and
			// the in-handler path didn't already write 'failed' (e.g. the
			// handler panicked, the worker was killed mid-task and asynq
			// reaped it without re-entering the handler, or some other path
			// that bypasses our explicit failure marking), flip the row here
			// so the UI doesn't show 'building' forever. Both deploy:web and
			// expo:ota_publish use the same payload shape (DeploymentID).
			if !isFinal {
				return
			}
			switch t.Type() {
			case tasks.TypeDeployWeb:
				var p tasks.WebDeployPayload
				if uErr := json.Unmarshal(t.Payload(), &p); uErr != nil || p.DeploymentID == "" {
					slog.Error("error_handler_unmarshal_failed", "type", t.Type(), "err", uErr)
					return
				}
				if mErr := controller.MarkDeploymentFailedIfNotTerminal(p.DeploymentID,
					"asynq retry budget exhausted: "+err.Error()); mErr != nil {
					slog.Error("error_handler_mark_failed", "deployment_id", p.DeploymentID, "err", mErr)
				}
			case tasks.TypeExpoOTAPublish:
				var p tasks.ExpoOTAPublishPayload
				if uErr := json.Unmarshal(t.Payload(), &p); uErr != nil || p.DeploymentID == "" {
					slog.Error("error_handler_unmarshal_failed", "type", t.Type(), "err", uErr)
					return
				}
				if mErr := controller.MarkDeploymentFailedIfNotTerminal(p.DeploymentID,
					"asynq retry budget exhausted: "+err.Error()); mErr != nil {
					slog.Error("error_handler_mark_failed", "deployment_id", p.DeploymentID, "err", mErr)
				}
			}
		}),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeDeployWeb, controller.HandleWebDeployTask)
	mux.HandleFunc(tasks.TypeExpoOTAPublish, controller.HandleExpoOTAPublishTask)
	mux.HandleFunc(tasks.TypeDeleteFlyApp, controller.HandleDeleteFlyAppTask)
	mux.HandleFunc(tasks.TypeAgentRun, controller.HandleAgentRunTask)
	// A6: 7-day grace teardown of an org's Fly + Neon resources after a
	// Stripe customer.subscription.deleted event. Re-checks the org's status
	// before destroying anything (re-subscribe wins).
	mux.HandleFunc(tasks.TypeOrgGraceTeardown, controller.HandleOrgGraceTeardownTask)
	mux.HandleFunc("cron:dns-check", func(ctx context.Context, _ *asynq.Task) error {
		return controller.CheckPendingDomainsTask(ctx)
	})
	mux.HandleFunc("cron:reap-stuck-deployments", func(ctx context.Context, _ *asynq.Task) error {
		return controller.ReapStuckDeploymentsTask(ctx)
	})
	mux.HandleFunc(tasks.TypeReapStuckAgentRuns, func(ctx context.Context, _ *asynq.Task) error {
		return controller.ReapStuckAgentRunsTask(ctx)
	})
	mux.HandleFunc(tasks.TypeReapStuckBuildTasks, func(ctx context.Context, _ *asynq.Task) error {
		return controller.ReapStuckBuildTasksTask(ctx)
	})
	mux.HandleFunc(tasks.TypeRunCredentialHealthCheck, func(ctx context.Context, _ *asynq.Task) error {
		return controller.RunCredentialHealthCheck(ctx)
	})
	// E3 Phase 7: daily Fly GraphQL pull → per-org egress tally for the
	// MonthlyBandwidthGB deploy gate.
	mux.HandleFunc(tasks.TypeFlyBandwidthSync, func(ctx context.Context, _ *asynq.Task) error {
		return controller.SyncFlyBandwidthTask(ctx)
	})

	scheduler := asynq.NewScheduler(redisOpts, &asynq.SchedulerOpts{
		Location: time.UTC,
	})
	if _, err := scheduler.Register("@every 30s", asynq.NewTask("cron:dns-check", nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule dns-check", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 1m", asynq.NewTask("cron:reap-stuck-deployments", nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule reap-stuck-deployments", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 5m", asynq.NewTask(tasks.TypeReapStuckAgentRuns, nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule reap-stuck-agent-runs", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 5m", asynq.NewTask(tasks.TypeReapStuckBuildTasks, nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule reap-stuck-build-tasks", "err", err)
		os.Exit(1)
	}
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(tasks.TypeRunCredentialHealthCheck, nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule credential-health-daily", "err", err)
		os.Exit(1)
	}
	// E3 Phase 7: daily Fly bandwidth sync. Once a day is plenty — quotas
	// are gated at deploy time (not per-request) so 24h staleness is fine.
	if _, err := scheduler.Register("@every 24h", asynq.NewTask(tasks.TypeFlyBandwidthSync, nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule fly-bandwidth-sync", "err", err)
		os.Exit(1)
	}
	go func() {
		if err := scheduler.Run(); err != nil {
			slog.Error("scheduler exit", "err", err)
		}
	}()

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		slog.Info("shutdown signal received, draining workers...")
		srv.Shutdown()
	}()

	if err := srv.Run(mux); err != nil {
		slog.Error("worker exit", "error", err)
		os.Exit(1)
	}
}
