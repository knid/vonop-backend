package fal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGenerateImagesSendsBatchRequest(t *testing.T) {
	oldBaseURL := baseURL
	oldClient := httpClient
	defer func() {
		baseURL = oldBaseURL
		httpClient = oldClient
	}()

	var gotPath string
	var gotAuth string
	var gotBody generateRequest
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"images":[{"url":"https://cdn.example/1.png"},{"url":"https://cdn.example/2.png"}]}`)),
		}, nil
	})}
	baseURL = "https://fal.test"

	urls, err := GenerateImages(context.Background(), "fal-key", "fal-ai/flux/schnell", "a hero image", "", 2)
	if err != nil {
		t.Fatalf("GenerateImages returned error: %v", err)
	}
	if gotPath != "/fal-ai/flux/schnell" {
		t.Fatalf("path = %q, want /fal-ai/flux/schnell", gotPath)
	}
	if gotAuth != "Key fal-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBody.Prompt != "a hero image" || gotBody.ImageSize != "square_hd" || gotBody.NumImages != 2 {
		t.Fatalf("unexpected request body: %#v", gotBody)
	}
	if len(urls) != 2 || urls[0] != "https://cdn.example/1.png" || urls[1] != "https://cdn.example/2.png" {
		t.Fatalf("unexpected urls: %#v", urls)
	}
}

func TestGenerateImageReturnsFirstURL(t *testing.T) {
	oldBaseURL := baseURL
	oldClient := httpClient
	defer func() {
		baseURL = oldBaseURL
		httpClient = oldClient
	}()

	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"images":[{"url":"https://cdn.example/one.png"}]}`)),
		}, nil
	})}
	baseURL = "https://fal.test"

	url, err := GenerateImage(context.Background(), "fal-key", "fal-ai/flux/schnell", "prompt", "portrait_4_3")
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if url != "https://cdn.example/one.png" {
		t.Fatalf("url = %q", url)
	}
}
