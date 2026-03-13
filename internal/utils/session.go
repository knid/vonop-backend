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
			sessions, err := DB.GetActiveSessions()
			if err != nil {
				return
			}
			for _, session := range sessions {
				lastEvent, err := DB.GetLastEventBySession(&session)
				if err == nil {
					if time.Since(lastEvent.CreatedAt).Seconds() > 30 {
						session.Duration = session.Duration + 30
						session.UpdatedAt = time.Now()
						session.Status = "inactive"
						DB.UpdateSession(&session)
					}
				} else {
					if session.CreatedAt.Add(30 * time.Second).Before(time.Now()) {
						session.Duration = session.Duration + 30
						session.UpdatedAt = time.Now()
						session.Status = "inactive"
						DB.UpdateSession(&session)
					}
				}
			}
		}()
	}
}
