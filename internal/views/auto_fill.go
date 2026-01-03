package views

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-playground/validator/v10"
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.NavBarAutoFillWithAIRequest](navBarAutoFillWithAIReq, validate); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	html, err := utils.FetchHTML(navBarAutoFillWithAIReq.URL)
	if err != nil {
		slog.Error("❌ Error fetching page", "error", err, "from", "AUTOFILL")
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

	outputJson, err := utils.GetJSONFromLLM(os.Getenv("DEFAULT_LLM_MODEL"), constants.NAVBAR_ITEM_GENERATOR_PROMPT, string(data))
	if err != nil {
		utils.JSONErrorResponse(w, "Error generating items. Try again please.", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(outputJson))
	// utils.JSONResponse(w, outputJson, http.StatusOK)

}
