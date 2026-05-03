package controllers

import (
	"context"
	"testing"
)

// TestProvisionRevenueCat_NilService_NoError verifies the graceful-degrade
// contract: when REVENUECAT_API_KEY is unset (Controller.RevenueCatService
// is nil), the provisioning call returns nil even with a nil project. This
// keeps the create-time goroutine + the build-time lazy call from blowing
// up on dev/staging boxes that don't have an RC API key.
func TestProvisionRevenueCat_NilService_NoError(t *testing.T) {
	c := &Controller{RevenueCatService: nil}
	if err := c.provisionRevenueCat(context.Background(), nil); err != nil {
		t.Fatalf("expected nil error when RevenueCatService is nil, got %v", err)
	}
}
