package views

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

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

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Println("Please set OPENROUTER_API_KEY in your environment.")
		return
	}

	html, err := utils.FetchHTML(navBarAutoFillWithAIReq.URL)
	if err != nil {
		log.Fatalf("❌ Error fetching page: %v", err)
	}

	urls := utils.ExtractAllURLs(html, navBarAutoFillWithAIReq.URL)
	output := map[string]any{"urls": urls}

	data, _ := json.MarshalIndent(output, "", "  ")

	// Prepare request
	reqBody := ChatRequest{
		Model: "openai/gpt-oss-20b:free", // or "claude-3.5-sonnet", "mistral-large-latest", etc.
		Messages: []Message{
			{Role: "system", Content: constants.NAVBAR_ITEM_GENERATOR_PROMPT},
			{Role: "user", Content: string(data)},
		},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://wemob.io") // Optional branding
	req.Header.Set("X-Title", "wemob-nav-analyzer")    // Optional project name

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var parsed ChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	outputJson := utils.CleanLLMOutput(parsed.Choices[0].Message.Content)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(outputJson))
	// utils.JSONResponse(w, outputJson, http.StatusOK)

}
