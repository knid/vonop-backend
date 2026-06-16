package controllers

import "testing"

func TestWebDeployMaxRetry(t *testing.T) {
	if webDeployMaxRetry != 2 {
		t.Fatalf("web deploy max retry = %d, want 2", webDeployMaxRetry)
	}
}
