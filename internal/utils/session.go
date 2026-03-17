package utils

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/knid/wemob/internal/database"
)

func CheckSessionStatusJob(DB *database.Database) {
	ticker := time.NewTicker(time.Second * 30)
	var running atomic.Bool

	for range ticker.C {
		if !running.CompareAndSwap(false, true) {
			slog.Warn("CheckSessionStatusJob: previous tick still running, skipping")
			continue
		}
		func() {
			defer running.Store(false)
			n, err := DB.TimeoutStaleSessions(30 * time.Second)
			if err != nil {
				slog.Error("CheckSessionStatusJob: failed to timeout sessions", "error", err)
				return
			}
			if n > 0 {
				slog.Info("CheckSessionStatusJob: timed out stale sessions", "count", n)
			}
		}()
	}
}
