package views

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/utils/external"
)

type Screenshots struct {
	URLs []string `json:"selected_urls"`
}

func (v *HttpViews) AutoGenerateScreenshots(w http.ResponseWriter, r *http.Request) {
	var autoGenerateScreenshotsReq requests.NavBarAutoFillWithAIRequest
	if err := utils.ParseJSONBody(r, &autoGenerateScreenshotsReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.NavBarAutoFillWithAIRequest](autoGenerateScreenshotsReq, validate); err != nil {
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

	html, err := utils.FetchHTML(autoGenerateScreenshotsReq.URL, v.Controller.SecretManager.GetSecret("PROXY_HOST"), v.Controller.SecretManager.GetSecret("PROXY_AUTH"))
	if err != nil {
		log.Printf(" Error fetching page: %v", err)
		utils.JSONErrorResponse(w, "Error fetching website. Try again please.", http.StatusBadRequest)
		return
	}

	urls := utils.ExtractAllURLs(html, autoGenerateScreenshotsReq.URL)
	urls = append(urls, autoGenerateScreenshotsReq.URL)
	if len(urls) == 0 {
		utils.JSONErrorResponse(w, "No navigation URLs found on the page.", http.StatusBadRequest)
		return
	}
	output := map[string]any{"urls": urls}

	data, _ := json.MarshalIndent(output, "", "  ")

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	outputJson, err := utils.GetJSONFromLLM(ctx, v.Controller.SecretManager.GetSecret("DEFAULT_LLM_MODEL"), constants.LINK_SELECTION_FOR_SCREENSHOTS_PROMPT, string(data), v.Controller.LLMProvider())
	if err != nil {
		utils.JSONErrorResponse(w, "Could not generate screenshots. Try again please.", http.StatusBadRequest)
		return
	}

	var screenshots Screenshots
	if err := json.Unmarshal([]byte(outputJson), &screenshots); err != nil {
		utils.JSONErrorResponse(w, "Could not generate screenshots. Try again please.", http.StatusBadRequest)
		return
	}

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(len(screenshots.URLs))

	screenshotLinks := struct {
		Screenshots []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"screenshots"`
	}{}

	var mu sync.Mutex
	var homePageScreenshotId string
	uploadCtx, uploadCancel := context.WithCancel(r.Context())
	defer uploadCancel()
	for _, screenshotUrl := range screenshots.URLs {
		go func() {
			defer waitGroup.Done()
			screenshot, err := external.GetScreenshotOfURL(screenshotUrl, v.Controller.SecretManager.GetSecret("SITESHOT_API_KEY"))
			if err != nil {
				slog.Error("screenshot fetch error", "url", screenshotUrl, "error", err)
				return
			}
			mockupBytes, err := utils.ComposeMockup(screenshot)
			if err != nil {
				slog.Error("mockup compose error", "url", screenshotUrl, "error", err)
				return
			}

			id := uuid.New().String()
			err = v.Controller.AssetsStorage.UploadFileByte(uploadCtx, "screenshots/"+id+".png", mockupBytes)
			if err != nil {
				slog.Error("screenshot upload error", "url", screenshotUrl, "error", err)
				return
			}

			req, err := v.Controller.AssetsStorage.GetObject(uploadCtx, "screenshots/"+id+".png", 60*60*24*7)
			if err != nil {
				slog.Error("presigned URL error", "url", screenshotUrl, "error", err)
				return
			}

			mu.Lock()
			screenshotLinks.Screenshots = append(screenshotLinks.Screenshots,
				struct {
					ID  string `json:"id"`
					URL string `json:"url"`
				}{
					ID:  id,
					URL: req.URL,
				},
			)
			if screenshotUrl == autoGenerateScreenshotsReq.URL {
				homePageScreenshotId = id
			}
			mu.Unlock()
		}()
	}

	waitGroup.Wait()

	// Reorder to have homepage screenshot first
	if homePageScreenshotId != "" && len(screenshotLinks.Screenshots) > 1 {
		for i, screenshot := range screenshotLinks.Screenshots {
			if screenshot.ID == homePageScreenshotId {
				// Move to front
				screenshotLinks.Screenshots[0], screenshotLinks.Screenshots[i] = screenshotLinks.Screenshots[i], screenshotLinks.Screenshots[0]
				break
			}
		}
	}

	// Post-hoc token-based billing
	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		if err := v.Controller.DeductAICreditsUnchecked(user, credits, "screenshot_generate", ""); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	utils.JSONResponse(w, screenshotLinks, http.StatusOK)
}
