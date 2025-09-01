package views

import (
	"net/http"

	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) Health(w http.ResponseWriter, r *http.Request) {
	utils.JSONResponse(w, map[string]string{"status": "ok"}, http.StatusOK)
}
