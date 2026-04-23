// HTTP wrappers for the studio ecommerce admin endpoints. Each handler is
// the same shape: auth → app ownership check → call controller → render JSON.
// Controller errors map to 404 (ErrNoEcommerceDB / "not found") or 400.
package views

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

// authorizeApp returns the projectID once the caller is verified as owner of
// the app, or writes the failure response and returns "" on the way out.
func (v *HttpViews) authorizeApp(w http.ResponseWriter, r *http.Request) string {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return ""
	}
	appID := chi.URLParam(r, "appId")
	if _, err := v.Controller.GetAppProject(appID, user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return ""
	}
	return appID
}

// writeStoreErr maps controller errors to HTTP statuses. ErrNoEcommerceDB and
// "not found" become 404; the rest are 400.
func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, controllers.ErrNoEcommerceDB):
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
	case strings.Contains(err.Error(), "not found"):
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
	default:
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
	}
}

// ===================== STORE =====================

func (v *HttpViews) GetAppStore(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	info, err := v.Controller.GetAppStore(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, info, http.StatusOK)
}

func (v *HttpViews) UpdateAppStoreSettings(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := v.Controller.UpdateAppStoreSettings(appID, body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, info, http.StatusOK)
}

// ===================== PRODUCTS =====================

func (v *HttpViews) ListStoreProducts(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.ListStoreProducts(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) GetStoreProduct(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.GetStoreProduct(appID, chi.URLParam(r, "productId"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) CreateStoreProduct(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.CreateStoreProduct(appID, body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusCreated)
}

func (v *HttpViews) UpdateStoreProduct(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.UpdateStoreProduct(appID, chi.URLParam(r, "productId"), body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) DeleteStoreProduct(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	if err := v.Controller.DeleteStoreProduct(appID, chi.URLParam(r, "productId")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===================== CATEGORIES =====================

func (v *HttpViews) ListStoreCategories(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.ListStoreCategories(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) CreateStoreCategory(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.CreateStoreCategory(appID, body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusCreated)
}

func (v *HttpViews) UpdateStoreCategory(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.UpdateStoreCategory(appID, chi.URLParam(r, "categoryId"), body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) DeleteStoreCategory(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	if err := v.Controller.DeleteStoreCategory(appID, chi.URLParam(r, "categoryId")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===================== DISCOUNTS =====================

func (v *HttpViews) ListStoreDiscounts(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.ListStoreDiscounts(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) CreateStoreDiscount(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.CreateStoreDiscount(appID, body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusCreated)
}

func (v *HttpViews) UpdateStoreDiscount(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.UpdateStoreDiscount(appID, chi.URLParam(r, "discountId"), body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) DeleteStoreDiscount(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	if err := v.Controller.DeleteStoreDiscount(appID, chi.URLParam(r, "discountId")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ===================== ORDERS =====================

func (v *HttpViews) ListStoreOrders(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.ListStoreOrders(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) GetStoreOrder(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.GetStoreOrder(appID, chi.URLParam(r, "orderId"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

func (v *HttpViews) UpdateStoreOrderStatus(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	var body map[string]any
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := v.Controller.UpdateStoreOrderStatus(appID, chi.URLParam(r, "orderId"), body)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

// ===================== ANALYTICS =====================

func (v *HttpViews) GetStoreAnalytics(w http.ResponseWriter, r *http.Request) {
	appID := v.authorizeApp(w, r)
	if appID == "" {
		return
	}
	out, err := v.Controller.GetStoreAnalytics(appID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}
