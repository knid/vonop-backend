package controllers

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

func TestProvisionOneSignalApp_NilProject_NoError(t *testing.T) {
	c := &Controller{}
	if err := c.provisionOneSignalApp(context.Background(), nil); err != nil {
		t.Fatalf("expected nil error when project nil, got %v", err)
	}
}

func TestProvisionOneSignalApp_NonMobileAppNative_NoError(t *testing.T) {
	c := &Controller{}
	p := &models.AppProject{ID: uuid.New(), Slug: "x", ProjectType: "app"}
	if err := c.provisionOneSignalApp(context.Background(), p); err != nil {
		t.Fatalf("expected nil error for non-native project, got %v", err)
	}
}
