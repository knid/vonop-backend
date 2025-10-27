package utils

import (
	"time"

	"github.com/knid/wemob/internal/database"
)

func CheckSessionStatusJob(DB *database.Database) {
	ticker := time.NewTicker(time.Minute * 10)

	for range ticker.C {
		sessions, err := DB.GetActiveSessions()
		if err == nil {
			for _, session := range sessions {
				lastEvent, err := DB.GetLastEventBySession(&session)
				if err == nil {
					if time.Since(lastEvent.CreatedAt).Seconds() > 30 {
						session.Duration = session.Duration + 30
						session.UpdatedAt = time.Now()
						session.Status = "inactive"
						DB.UpdateSession(&session)
					}
				}
			}
		}
	}
}
