package views

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Response structure for parsing the LLM output
type Choice struct {
	Message Message `json:"message"`
}

type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

func (v *HttpViews) NavBarAutoFillWithAI(w http.ResponseWriter, r *http.Request) {
	var navBarAutoFillWithAIReq requests.NavBarAutoFillWithAIRequest
	if err := utils.ParseJSONBody(r, &navBarAutoFillWithAIReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.NavBarAutoFillWithAIRequest](navBarAutoFillWithAIReq, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, v.Controller, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	html, err := utils.FetchHTML(navBarAutoFillWithAIReq.URL, v.Controller.SecretManager.GetSecret("PROXY_HOST"), v.Controller.SecretManager.GetSecret("PROXY_AUTH"))
	if err != nil {
		slog.Error("error fetching page", "error", err, "from", "autofill")
		utils.JSONError(w, constants.ErrValidationBody, "Error fetching website. Try again please.", http.StatusBadRequest, nil)
		return
	}

	urls := utils.ExtractAllURLs(html, navBarAutoFillWithAIReq.URL)
	if len(urls) == 0 {
		utils.JSONError(w, constants.ErrValidationBody, "No navigation URLs found on the page.", http.StatusBadRequest, nil)
		return
	}
	output := map[string]any{"urls": urls}

	data, _ := json.MarshalIndent(output, "", "  ")

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.TraceID = uuid.New().String()
	tracker.TrackContent = true
	tracker.TraceInput = string(data)
	if orgID, orgErr := v.Controller.RequireOrgIDForUser(user, nil); orgErr == nil {
		tracker.OrganizationID = orgID.String()
	}
	ctx := utils.WithTokenTracker(r.Context(), tracker)
	provider := v.Controller.LLMProvider()
	model := provider.FastModel()

	outputJson, err := utils.GetJSONFromLLM(ctx, model, constants.NAVBAR_ITEM_GENERATOR_PROMPT, string(data), provider)
	if err != nil {
		utils.JSONError(w, constants.ErrValidationBody, "Error generating items. Try again please.", http.StatusBadRequest, nil)
		return
	}
	tracker.TraceOutput = outputJson

	v.Controller.RecordTokenUsage(user, "navbar_autofill",
		model,
		provider.Name, "", "standard", tracker)

	// Post-hoc token-based billing — no project context, charge personal org.
	_, _, total, _ := tracker.Totals()
	if total > 0 {
		credits := v.Controller.CreditsForTracker(tracker)
		orgID, orgErr := v.Controller.RequireOrgIDForUser(user, nil)
		if orgErr != nil {
			slog.Warn("navbar_autofill: resolve personal org failed", "error", orgErr, "user_id", user.ID)
		} else if err := v.Controller.DeductAICreditsUnchecked(user, orgID, credits, "navbar_autofill", ""); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(outputJson))
}
