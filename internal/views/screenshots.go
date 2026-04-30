package views

import (
	"context"
	"encoding/json"
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
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.NavBarAutoFillWithAIRequest](autoGenerateScreenshotsReq, validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
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
		slog.Error("error fetching page for screenshots", "error", err, "from", "screenshots")
		utils.JSONError(w, constants.ErrValidationBody, "Error fetching website. Try again please.", http.StatusBadRequest, nil)
		return
	}

	urls := utils.ExtractAllURLs(html, autoGenerateScreenshotsReq.URL)
	urls = append(urls, autoGenerateScreenshotsReq.URL)
	if len(urls) == 0 {
		utils.JSONError(w, constants.ErrValidationBody, "No navigation URLs found on the page.", http.StatusBadRequest, nil)
		return
	}
	output := map[string]any{"urls": urls}

	data, _ := json.MarshalIndent(output, "", "  ")

	// Token tracking for post-hoc billing
	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	ctx := utils.WithTokenTracker(r.Context(), tracker)

	outputJson, err := utils.GetJSONFromLLM(ctx, "claude-haiku-4-5", constants.LINK_SELECTION_FOR_SCREENSHOTS_PROMPT, string(data), v.Controller.AnthropicProvider())
	if err != nil {
		utils.JSONError(w, constants.ErrValidationBody, "Could not generate screenshots. Try again please.", http.StatusBadRequest, nil)
		return
	}

	var screenshots Screenshots
	if err := json.Unmarshal([]byte(outputJson), &screenshots); err != nil {
		slog.Error("screenshot JSON unmarshal failed", "error", err, "raw_output", outputJson)
		utils.JSONError(w, constants.ErrValidationBody, "Could not generate screenshots. Try again please.", http.StatusBadRequest, nil)
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

	v.Controller.RecordTokenUsage(user, "screenshot_generate",
		"claude-haiku-4-5",
		"anthropic", "", "standard", tracker)

	// Post-hoc token-based billing — no project context, charge personal org.
	prompt, completion, total, _ := tracker.Totals()
	if total > 0 {
		credits := models.TokensToCredits(prompt, completion)
		orgID, orgErr := v.Controller.RequireOrgIDForUser(user, nil)
		if orgErr != nil {
			slog.Warn("screenshot_generate: resolve personal org failed", "error", orgErr, "user_id", user.ID)
		} else if err := v.Controller.DeductAICreditsUnchecked(user, orgID, credits, "screenshot_generate", ""); err != nil {
			slog.Warn("failed to deduct AI credits from tokens", "error", err,
				"user_id", user.ID, "credits", credits, "tokens", total)
		}
	}

	utils.JSONResponse(w, screenshotLinks, http.StatusOK)
}
