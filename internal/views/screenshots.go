package views

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
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

	html, err := utils.FetchHTML(autoGenerateScreenshotsReq.URL)
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

	outputJson, err := utils.GetJSONFromLLM(constants.DEFAULT_LLM_MODEL, constants.LINK_SELECTION_FOR_SCREENSHOTS_PROMPT, string(data))
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var screenshots Screenshots
	if err := json.Unmarshal([]byte(outputJson), &screenshots); err != nil {
		utils.JSONErrorResponse(w, "Error parsing generated items. Try again please.", http.StatusBadRequest)
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

	var homePageScreenshotId string
	for _, screenshotUrl := range screenshots.URLs {
		go func() {
			defer waitGroup.Done()
			screenshot, err := external.GetScreenshotOfURL(screenshotUrl)
			if err != nil {
				log.Printf("Error getting screenshot for %s: %v", screenshotUrl, err)
				return
			}
			mockupBytes, err := utils.ComposeMockup(screenshot)
			if err != nil {
				log.Printf("Error composing mockup for %s: %v", screenshotUrl, err)
				return
			}

			id := uuid.New().String()
			err = v.Controller.AssetsStorage.UploadFileByte(context.TODO(), "screenshots/"+id+".png", mockupBytes)
			if err != nil {
				log.Printf("Error uploading screenshot for %s: %v", screenshotUrl, err)
				return
			}

			req, err := v.Controller.AssetsStorage.GetObject(context.TODO(), "screenshots/"+id+".png", 60*60*24*7)
			if err != nil {
				log.Printf("Error getting presigned URL for %s: %v", screenshotUrl, err)
				return
			}
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
		}()
	}

	waitGroup.Wait()

	// Reorder to have homepage screenshot first
	if homePageScreenshotId != "" {
		for i, screenshot := range screenshotLinks.Screenshots {
			if screenshot.ID == homePageScreenshotId {
				// Move to front
				screenshotLinks.Screenshots[0], screenshotLinks.Screenshots[i] = screenshotLinks.Screenshots[i], screenshotLinks.Screenshots[0]
				break
			}
		}
	}

	utils.JSONResponse(w, screenshotLinks, http.StatusOK)
}
