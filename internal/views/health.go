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
	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}
