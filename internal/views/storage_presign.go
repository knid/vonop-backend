package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) authorizeBuildArtifactDownload(r *http.Request) (string, bool, string, int) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		return "", false, constants.ErrAuthInvalidToken, http.StatusUnauthorized
	}

	id := chi.URLParam(r, "buildId")
	if id == "" {
		return "", false, constants.ErrValidationRequired, http.StatusBadRequest
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", false, constants.ErrValidationInvalidFormat, http.StatusBadRequest
	}

	build, err := v.Controller.DB.GetProjectBuildById(id)
	if err != nil || build == nil || build.AppProjectID == uuid.Nil {
		return "", false, constants.ErrNotFound, http.StatusNotFound
	}
	appProject, err := v.Controller.DB.GetAppProjectByID(build.AppProjectID.String())
	if err != nil || appProject == nil || appProject.ID == uuid.Nil {
		return "", false, constants.ErrNotFound, http.StatusNotFound
	}
	if appProject.UserID.String() != user.ID.String() {
		return "", false, constants.ErrForbidden, http.StatusForbidden
	}

	return id, true, "", 0
}

func (v *HttpViews) PutP8KeyToS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := uuid.New().String()
	objectKey := "p8_keys/" + id + ".p8"

	presignedRequest, err := v.Controller.UserApiKeysStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) PutServiceAccountToS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := uuid.New().String()
	objectKey := "service_accounts/" + id + ".json"

	presignedRequest, err := v.Controller.UserApiKeysStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) GetProfilePhotoFromS3Presign(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := user.ID.String()
	objectKey := "profile_photos/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) PutProfilePhotoToS3Presign(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := user.ID.String()
	objectKey := "profile_photos/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}
func (v *HttpViews) GetIconFromS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := chi.URLParam(r, "iconId")
	if id == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "icon_id"})
		return
	}
	objectKey := "icons/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) PutIconToS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := uuid.New().String()
	objectKey := "icons/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) GetScreenshotFromS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := chi.URLParam(r, "screenshotId")
	if id == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "screenshot_id"})
		return
	}

	objectKey := "screenshots/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)

}
func (v *HttpViews) PutScreenshotToS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := uuid.New().String()
	objectKey := "screenshots/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) DownloadAABFromS3Presign(w http.ResponseWriter, r *http.Request) {
	id, ok, code, status := v.authorizeBuildArtifactDownload(r)
	if !ok {
		utils.JSONErrorCode(w, code, status, nil)
		return
	}

	objectKey := "aabs/" + id + ".aab"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) DownloadIPAFromS3Presign(w http.ResponseWriter, r *http.Request) {
	id, ok, code, status := v.authorizeBuildArtifactDownload(r)
	if !ok {
		utils.JSONErrorCode(w, code, status, nil)
		return
	}

	objectKey := "ipas/" + id + ".ipa"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) GetNotificationImageFromS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := chi.URLParam(r, "notificationId")
	if id == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "notification_id"})
		return
	}

	objectKey := "notifications/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.GetObject(r.Context(), objectKey, 60*60*24)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}

func (v *HttpViews) PutNotificationImageToS3Presign(w http.ResponseWriter, r *http.Request) {
	if _, err := v.Controller.GetUserFromReq(r); err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	id := uuid.New().String()
	objectKey := "notifications/" + id + ".png"

	presignedRequest, err := v.Controller.AssetsStorage.PutObject(r.Context(), objectKey, 300)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]interface{}{
		"id":                id,
		"presigned_request": presignedRequest,
	}, http.StatusOK)
}
