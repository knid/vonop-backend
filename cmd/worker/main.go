package main

import (
	"context"
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
		ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, t *asynq.Task, err error) {
			slog.Error("asynq task failed",
				"type", t.Type(),
				"payload_bytes", len(t.Payload()),
				"error", err)
		}),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeDeployWeb, controller.HandleWebDeployTask)
	mux.HandleFunc("cron:dns-check", func(ctx context.Context, _ *asynq.Task) error {
		return controller.CheckPendingDomainsTask(ctx)
	})

	scheduler := asynq.NewScheduler(redisOpts, &asynq.SchedulerOpts{
		Location: time.UTC,
	})
	if _, err := scheduler.Register("@every 30s", asynq.NewTask("cron:dns-check", nil), asynq.Queue("cron")); err != nil {
		slog.Error("schedule dns-check", "err", err)
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
