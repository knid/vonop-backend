package external

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

var screenshotHTTPClient = &http.Client{Timeout: 30 * time.Second}

func GetScreenshotOfURL(url string, apiKey string) ([]byte, error) {
	resp, err := screenshotHTTPClient.Get(
		fmt.Sprintf(
			"https://api.site-shot.com/?url=%s&userkey=%s&width=1290&height=2790&zoom=300&format=png&no_cookie_popup=1&no_ads=1&country=us", url, apiKey))
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return imageData, nil
}
