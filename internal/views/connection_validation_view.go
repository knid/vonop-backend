package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/utils"
)

// GetLatestConnectionValidation returns the most recent ConnectionValidation row
// for a (kind, connectionId) pair. Used by the frontend HealthBadge to render
// "✓ valid · 3h ago" / "✗ invalid · 1d ago" pills next to each saved Apple/Google
// credential.
//
// Auth: requires a valid bearer token. The handler does NOT verify the user owns
// the connection's org — the connection-id is opaque enough that public read is
// acceptable for now (P1.1 will tighten this once we add org membership to the
// validation rows themselves or join through the connection table).
//
// Returns:
//   200 + JSON {status, validated_at, message?, app_count?}
//   200 + null when no validation exists
//   400 on bad UUID / unknown kind
//   401 on missing/invalid bearer
func (v *HttpViews) GetLatestConnectionValidation(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONError(w, "unauthorized", "unauthorized", http.StatusUnauthorized, nil)
		return
	}

	kind := chi.URLParam(r, "kind")
	if kind != "apple" && kind != "google_play" {
		utils.JSONError(w, "invalid_kind", "kind must be apple or google_play", http.StatusBadRequest, nil)
		return
	}

	connID, err := uuid.Parse(chi.URLParam(r, "connectionId"))
	if err != nil {
		utils.JSONError(w, "invalid_id", "invalid connection id", http.StatusBadRequest, nil)
		return
	}

	row, err := v.Controller.DB.LatestConnectionValidation(r.Context(), kind, connID)
	if err != nil {
		utils.JSONError(w, "lookup_failed", "lookup failed", http.StatusInternalServerError, nil)
		return
	}
	if row == nil {
		utils.JSONResponse(w, nil, http.StatusOK)
		return
	}
	utils.JSONResponse(w, map[string]any{
		"status":       row.Status,
		"validated_at": row.ValidatedAt,
		"message":      row.Message,
		"app_count":    row.AppCount,
	}, http.StatusOK)
}
