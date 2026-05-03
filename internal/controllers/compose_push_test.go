package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
)

func TestSendComposePush_NilProject_Error(t *testing.T) {
	c := &Controller{}
	_, err := c.SendComposePush(context.Background(), nil, &models.User{}, requests.ComposePushRequest{})
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("expected project-required error, got %v", err)
	}
}

func TestSendComposePush_NonNativeProject_Error(t *testing.T) {
	c := &Controller{}
	p := &models.AppProject{ID: uuid.New(), Slug: "x", ProjectType: "app"}
	u := &models.User{ID: uuid.New()}
	_, err := c.SendComposePush(context.Background(), p, u, requests.ComposePushRequest{})
	if err == nil || !strings.Contains(err.Error(), "mobile_app_native") {
		t.Fatalf("expected mobile_app_native error, got %v", err)
	}
}

func TestFirstLocaleString_Empty(t *testing.T) {
	if got := firstLocaleString(nil); got != "" {
		t.Errorf("nil map: got %q", got)
	}
	if got := firstLocaleString(map[string]string{}); got != "" {
		t.Errorf("empty map: got %q", got)
	}
}

func TestFirstLocaleString_Alphabetical(t *testing.T) {
	m := map[string]string{"tr": "Merhaba", "en": "Hello"}
	if got := firstLocaleString(m); got != "Hello" {
		t.Errorf("expected Hello, got %q", got)
	}
}
