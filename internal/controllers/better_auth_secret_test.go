package controllers

import (
	"context"
	"testing"
)

// TestCollectBetterAuthEnvVars_NilProject verifies the nil-guard.
func TestCollectBetterAuthEnvVars_NilProject(t *testing.T) {
	c := &Controller{}
	if _, err := c.CollectBetterAuthEnvVars(context.Background(), nil); err == nil {
		t.Fatalf("expected error on nil project, got nil")
	}
}

// TestEnsureBetterAuthSecret_NilProject verifies the nil-guard on the
// underlying helper.
func TestEnsureBetterAuthSecret_NilProject(t *testing.T) {
	c := &Controller{}
	if _, err := c.EnsureBetterAuthSecret(context.Background(), nil); err == nil {
		t.Fatalf("expected error on nil project, got nil")
	}
}
