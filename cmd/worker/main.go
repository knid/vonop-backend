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
	"github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/tasks"
)

func main() {
	// Workers only need a small pool — one connection per active task plus a
	// few for the scheduler and health checks. Ops can override via env.
	if os.Getenv("DB_MAX_OPEN_CONNS") == "" {
		os.Setenv("DB_MAX_OPEN_CONNS", "10")
	}

	controller, redisOpts := bootstrap.BootstrapController()
	shutdownLangfuse, err := observability.InitLangfuseWithSecrets(context.Background(), controller.SecretManager)
	if err != nil {
		slog.Error("failed to initialize Langfuse OpenTelemetry exporter", "error", err)
		os.Exit(1)
	}

	// Concurrency caps total in-flight tasks across queues. 4 leaves
	// headroom for parallel web/cron tasks (which are IO-bound). The
	// historical in-process Expo OTA publisher has been removed — OTA
	// publishes now run on Codemagic, not the worker.
	srv := asynq.NewServer(redisOpts, asynq.Config{
		Concurrency: 6,
		Queues: map[string]int{
			"deploy-web-pro":  6,
			"deploy-web-free": 2,
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
			// so the UI doesn't show 'building' forever.
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
			}
		}),
	})

	mux := asynq.NewServeMux()
	// PostHog lifecycle: emits job_started / job_succeeded / job_failed for
	// every task except cron heartbeats (cron:* types are skipped — they fire
	// on tight schedules and would drown out user-triggered job events).
	mux.Use(middleware.AsynqPostHogLifecycle(controller))
	mux.HandleFunc(tasks.TypeDeployWeb, controller.HandleWebDeployTask)
	mux.HandleFunc(tasks.TypeDeleteFlyApp, controller.HandleDeleteFlyAppTask)
	mux.HandleFunc(tasks.TypeAgentRun, controller.HandleAgentRunTask)
	// A6: 7-day grace teardown of an org's Fly + Neon resources after a
	// Stripe customer.subscription.deleted event. Re-checks the org's status
	// before destroying anything (re-subscribe wins).
	mux.HandleFunc(tasks.TypeOrgGraceTeardown, controller.HandleOrgGraceTeardownTask)
	// User account deletion: full purge of projects + infra (Fly/Neon/gitea/S3)
	// + owned orgs + user data. Enqueued from both self-delete and admin-delete.
	mux.HandleFunc(tasks.TypeUserAccountPurge, controller.HandleUserAccountPurgeTask)
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := shutdownLangfuse(ctx); err != nil {
			slog.Error("Langfuse OpenTelemetry shutdown error", "error", err)
		}
	}()

	if err := srv.Run(mux); err != nil {
		slog.Error("worker exit", "error", err)
		os.Exit(1)
	}
}
