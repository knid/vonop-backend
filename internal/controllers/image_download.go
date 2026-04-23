package controllers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// imageDownloadClient is used to fetch generated images from remote CDNs.
var imageDownloadClient = &http.Client{Timeout: 30 * time.Second}

// downloadImage fetches the image bytes at the given URL.
func downloadImage(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := imageDownloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
