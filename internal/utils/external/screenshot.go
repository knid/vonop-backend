package external

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func GetScreenshotOfURL(url string) ([]byte, error) {
	resp, err := http.Get(
		fmt.Sprintf(
			"https://api.site-shot.com/?url=%s&userkey=%s&width=1290&height=2790&zoom=300&format=png&no_cookie_popup=1&no_ads=1&country=us", url, os.Getenv("SITESHOT_API_KEY")))
	if err != nil {
		fmt.Printf("Error making HTTP request: %v\n", err)
		return nil, err
	}

	fmt.Println(resp.Status)

	defer resp.Body.Close()

	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response body: %v\n", err)
		return nil, err
	}

	return imageData, nil
}
