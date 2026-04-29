// cmd/backfill_twenty walks the users table and upserts a Twenty Person
// for every non-soft-deleted user. Run ONCE post-deploy. Re-runnable
// without harm: skips users that already have a non-empty twenty_person_id.
//
// Reuses bootstrap.BootstrapController() for DB + secrets wiring, then
// constructs its own TwentyService directly — bypassing the multicast so
// the backfill does NOT also fire Resend events (which would email
// thousands of users a "backfill" notification).
//
// Usage:
//
//	go run ./cmd/backfill_twenty
package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/knid/wemob/internal/bootstrap"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/events"
)

func main() {
	controller, _ := bootstrap.BootstrapController()
	db := controller.DB
	sm := controller.SecretManager

	twenty := events.NewTwentyService(db,
		sm.GetSecret("TWENTY_API_KEY"),
		sm.GetSecret("TWENTY_BASE_URL"))

	twenty.EnsureSchema(context.Background())

	const pageSize = 1000
	const concurrency = 3

	offset := 0
	total := 0
	failed := 0
	skipped := 0
	for {
		var users []models.User
		err := db.DB.NewSelect().
			Model(&users).
			Where("deleted_at IS NULL").
			Order("created_at ASC").
			Limit(pageSize).
			Offset(offset).
			Scan(context.Background())
		if err != nil {
			log.Fatalf("fetch users: %v", err)
		}
		if len(users) == 0 {
			break
		}

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		var batchFailed int
		var failMu sync.Mutex
		for i := range users {
			u := users[i]
			if u.TwentyPersonID != "" {
				skipped++
				continue
			}
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := twenty.SendEvent(&u, "backfill", nil); err != nil {
					failMu.Lock()
					batchFailed++
					failMu.Unlock()
					log.Printf("FAIL %s: %v", u.ID, err)
				}
			}()
		}
		wg.Wait()
		failed += batchFailed

		total += len(users)
		log.Printf("Progress: total=%d skipped=%d failed=%d", total, skipped, failed)
		offset += pageSize
		time.Sleep(200 * time.Millisecond) // gentle on Twenty rate limit
	}
	log.Printf("Done. total=%d skipped=%d failed=%d", total, skipped, failed)
}
