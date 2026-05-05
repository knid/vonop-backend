package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) Health(w http.ResponseWriter, r *http.Request) {
	if err := v.Controller.DB.DB.DB.PingContext(r.Context()); err != nil {
		utils.JSONResponse(w, map[string]string{"status": "error", "reason": "database unreachable"}, http.StatusServiceUnavailable)
		return
	}

	body := map[string]any{
		"status": "ok",
	}

	if v.Controller.FlyService != nil && v.Controller.FlyService.StatusClient != nil {
		fs := v.Controller.FlyService.StatusClient.GetStatus()
		body["host_status"] = map[string]string{
			"indicator": fs.Indicator,
			"message":   hostStatusMessage(fs.Indicator),
		}
	}

	utils.JSONResponse(w, body, http.StatusOK)
}

func hostStatusMessage(indicator string) string {
	switch indicator {
	case "none":
		return "All systems operational"
	case "minor":
		return "Our hosting provider is experiencing minor issues. Deployments may be slightly delayed."
	case "major", "critical":
		return "Our hosting provider is experiencing issues. Deployments may fail or be significantly delayed."
	default:
		return "Hosting provider status unknown."
	}
}
