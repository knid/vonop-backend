package views

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ListReleases handles GET /v2/apps/{appId}/releases.
//
// Optional query: ?platform=ios|android — filters server-side. The unfiltered
// list returns both platforms; the frontend uses this to render a single
// "Releases" timeline.
//
// Auth: caller must own the project OR be an active member of project.OrganizationID.
func (v *HttpViews) ListReleases(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	releases, err := v.Controller.GetReleasesForApp(r.Context(), project.ID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Optional ?platform=ios|android filter. We filter in-handler so we don't
	// have to invent a second controller method just for one query param.
	if want := r.URL.Query().Get("platform"); want != "" {
		switch want {
		case models.ReleasePlatformIOS, models.ReleasePlatformAndroid:
			out := make([]models.Release, 0, len(releases))
			for _, rel := range releases {
				if rel.Platform == want {
					out = append(out, rel)
				}
			}
			releases = out
		default:
			utils.JSONError(w, constants.ErrValidationBody,
				"platform must be ios or android",
				http.StatusBadRequest, nil)
			return
		}
	}

	utils.JSONResponse(w, releases, http.StatusOK)
}

// GetCurrentLiveRelease handles GET /v2/apps/{appId}/releases/current?platform=ios|android.
//
// Returns 404 when nothing is live yet for that (project, platform). The
// platform query param is required — there's no sensible default since iOS
// and Android can be live independently.
func (v *HttpViews) GetCurrentLiveRelease(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	platform := r.URL.Query().Get("platform")
	switch platform {
	case models.ReleasePlatformIOS, models.ReleasePlatformAndroid:
	default:
		utils.JSONError(w, constants.ErrValidationBody,
			"platform query param is required and must be ios or android",
			http.StatusBadRequest, nil)
		return
	}

	rel, err := v.Controller.GetCurrentLiveRelease(r.Context(), project.ID, platform)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	if rel == nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, rel, http.StatusOK)
}

// HaltPhasedRollout handles POST /v2/apps/{appId}/releases/{releaseID}/halt.
//
// iOS: pauses the phased rollout in App Store Connect. Returns 200 with the
// updated release. Android: returns 422 with ErrReleaseNotImplemented (Play's
// halt API requires opening an edit + writing a halted track release; tracked
// as follow-up).
//
// Cross-project guard: we load the release, then verify it belongs to the
// AppProject identified in the URL. This prevents `appId=A` + `releaseId=B`
// where B is in a different org from leaking through.
func (v *HttpViews) HaltPhasedRollout(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	releaseID, err := uuid.Parse(chi.URLParam(r, "releaseID"))
	if err != nil {
		utils.JSONError(w, constants.ErrValidationBody,
			"release id must be a uuid", http.StatusBadRequest, nil)
		return
	}

	// Cross-project guard — release must belong to this app.
	rel, err := v.Controller.DB.GetRelease(r.Context(), releaseID)
	if err != nil || rel == nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if rel.AppProjectID != project.ID {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	updated, haltErr := v.Controller.HaltPhasedRollout(r.Context(), releaseID)
	if haltErr != nil {
		switch {
		case errors.Is(haltErr, controllers.ErrReleaseNotFound):
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		case errors.Is(haltErr, controllers.ErrReleaseNotImplemented):
			utils.JSONError(w, constants.ErrValidationBody,
				"halt not implemented for android",
				http.StatusUnprocessableEntity, nil)
		default:
			slog.Error("HaltPhasedRollout: unexpected error",
				"app_id", project.ID, "release_id", releaseID, "err", haltErr)
			utils.JSONInternalError(w, haltErr)
		}
		return
	}

	utils.JSONResponse(w, updated, http.StatusOK)
}

// RollbackRelease handles POST /v2/apps/{appId}/releases/rollback.
//
// Body: {from_release_id, target_release_id}. Both must be Releases on the
// same AppProject (cross-project guard verifies this; otherwise a 404 is
// returned).
func (v *HttpViews) RollbackRelease(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.RollbackReleaseRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	// Cross-project guard — both releases must belong to this app.
	from, err := v.Controller.DB.GetRelease(r.Context(), req.FromReleaseID)
	if err != nil || from == nil || from.AppProjectID != project.ID {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	target, err := v.Controller.DB.GetRelease(r.Context(), req.TargetReleaseID)
	if err != nil || target == nil || target.AppProjectID != project.ID {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	updated, rbErr := v.Controller.RollbackRelease(r.Context(), req.FromReleaseID, req.TargetReleaseID)
	if rbErr != nil {
		switch {
		case errors.Is(rbErr, controllers.ErrReleaseNotFound):
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		default:
			slog.Error("RollbackRelease: unexpected error",
				"app_id", project.ID, "from", req.FromReleaseID, "target", req.TargetReleaseID, "err", rbErr)
			utils.JSONInternalError(w, rbErr)
		}
		return
	}

	utils.JSONResponse(w, updated, http.StatusOK)
}
