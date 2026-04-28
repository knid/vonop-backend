package views

import (
	"context"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/utils"
)

// HandleDeployPipelineHealth returns a JSON aggregate of the deploy pipeline's
// recent operational state. Mounted under /internal/health behind the InternalOnly middleware.
func (v *HttpViews) HandleDeployPipelineHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	since := time.Now().Add(-5 * time.Minute)

	body := map[string]any{
		"queue_pro":  queueInfoMap(v.Controller.AsynqInspector, "deploy-web-pro"),
		"queue_free": queueInfoMap(v.Controller.AsynqInspector, "deploy-web-free"),
		"queue_cron": queueInfoMap(v.Controller.AsynqInspector, "cron"),
	}

	succeeded, _ := v.Controller.DB.CountAppDeploymentsByStatusSince(ctx, "deployed", since)
	failed, _ := v.Controller.DB.CountAppDeploymentsByStatusSince(ctx, "failed", since)
	body["deploys_5min_succeeded"] = succeeded
	body["deploys_5min_failed"] = failed

	body["redis_responsive"] = redisResponsive(v.Controller.AsynqInspector)

	utils.JSONResponse(w, body, http.StatusOK)
}

func queueInfoMap(inspector *asynq.Inspector, name string) map[string]int {
	if inspector == nil {
		return map[string]int{"pending": -1, "active": -1}
	}
	qi, _ := inspector.GetQueueInfo(name)
	if qi == nil {
		return map[string]int{"pending": 0, "active": 0}
	}
	return map[string]int{
		"pending":   qi.Pending,
		"active":    qi.Active,
		"scheduled": qi.Scheduled,
		"retry":     qi.Retry,
		"dead":      qi.Archived,
	}
}

func redisResponsive(inspector *asynq.Inspector) bool {
	if inspector == nil {
		return false
	}
	_, err := inspector.Queues()
	return err == nil
}
