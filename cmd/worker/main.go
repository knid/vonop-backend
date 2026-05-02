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

	srv := asynq.NewServer(redisOpts, asynq.Config{
		Concurrency: 8,
		Queues: map[string]int{
			"deploy-web-pro":  6,
			"deploy-web-free": 2,
			"cron":            1,
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
			// Backstop: if this is the final failure for a deploy:web task and
			// the in-handler maybeRetry path didn't already write 'failed'
			// (e.g. the handler panicked, the worker was killed mid-task and
			// asynq reaped it without re-entering the handler, or some other
			// path that bypasses our explicit failure marking), flip the row
			// here so the UI doesn't show 'building' forever.
			if !isFinal || t.Type() != tasks.TypeDeployWeb {
				return
			}
			var p tasks.WebDeployPayload
			if uErr := json.Unmarshal(t.Payload(), &p); uErr != nil || p.DeploymentID == "" {
				slog.Error("error_handler_unmarshal_failed", "type", t.Type(), "err", uErr)
				return
			}
			if mErr := controller.MarkDeploymentFailedIfNotTerminal(p.DeploymentID,
				"asynq retry budget exhausted: "+err.Error()); mErr != nil {
				slog.Error("error_handler_mark_failed", "deployment_id", p.DeploymentID, "err", mErr)
			}
		}),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeDeployWeb, controller.HandleWebDeployTask)
	mux.HandleFunc("cron:dns-check", func(ctx context.Context, _ *asynq.Task) error {
		return controller.CheckPendingDomainsTask(ctx)
	})
	mux.HandleFunc("cron:reap-stuck-deployments", func(ctx context.Context, _ *asynq.Task) error {
		return controller.ReapStuckDeploymentsTask(ctx)
	})
	mux.HandleFunc(tasks.TypeReapStuckAgentRuns, func(ctx context.Context, _ *asynq.Task) error {
		return controller.ReapStuckAgentRunsTask(ctx)
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
