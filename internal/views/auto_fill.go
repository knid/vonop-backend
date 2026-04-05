package views

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.NavBarAutoFillWithAIRequest](navBarAutoFillWithAIReq, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	html, err := utils.FetchHTML(navBarAutoFillWithAIReq.URL, v.Controller.SecretManager.GetSecret("PROXY_HOST"), v.Controller.SecretManager.GetSecret("PROXY_AUTH"))
	if err != nil {
		slog.Error("error fetching page", "error", err, "from", "autofill")
		utils.JSONErrorResponse(w, "Error fetching website. Try again please.", http.StatusBadRequest)
		return
	}

	urls := utils.ExtractAllURLs(html, navBarAutoFillWithAIReq.URL)
	if len(urls) == 0 {
		utils.JSONErrorResponse(w, "No navigation URLs found on the page.", http.StatusBadRequest)
		return
	}
	output := map[string]any{"urls": urls}

	data, _ := json.MarshalIndent(output, "", "  ")

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	outputJson, err := utils.GetJSONFromLLM(ctx, "claude-haiku-4-5", constants.NAVBAR_ITEM_GENERATOR_PROMPT, string(data), v.Controller.AnthropicProvider())
	if err != nil {
		utils.JSONErrorResponse(w, "Error generating items. Try again please.", http.StatusBadRequest)
		return
	}

	v.Controller.RecordTokenUsage(user, "navbar_autofill",
		"claude-haiku-4-5",
		"anthropic", "", "standard", tracker)

	// Post-hoc token-based billing
	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "navbar_autofill", ""); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(outputJson))
}
