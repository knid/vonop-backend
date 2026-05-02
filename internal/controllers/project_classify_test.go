package controllers

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestLooksLikeMobileNative(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"habit tracker mobile app", "Build me a habit tracker mobile app where users sign up, log daily habits, get streak push notifications.", true},
		{"native iOS fitness app", "I want a native iOS fitness tracker with HealthKit integration and biometric login.", true},
		{"mobile-first social", "Mobile-first social network for runners with push, IAP, and offline mode.", true},
		{"react native chat", "Build a chat app in react native with rooms and message history.", true},
		{"expo to-do app", "Simple to-do app on Expo with push notifications.", true},
		{"app store ecommerce", "I want an app store mobile app for selling artisan candles.", true},

		// Negative cases — should NOT match
		{"web SaaS dashboard", "Build me a project management dashboard with team workspaces and reporting.", false},
		{"online store", "Online store selling artisan candles with stripe checkout.", false},
		{"marketing site", "Marketing site for a SaaS that sells time-tracking software with a blog.", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeMobileNative(tc.prompt)
			if got != tc.want {
				t.Errorf("looksLikeMobileNative(%q) = %v, want %v", tc.prompt, got, tc.want)
			}
		})
	}
}

// TestProjectTypeConstantsMatchSpec is a guard that catches accidental
// drift in the ProjectType constants used by the classifier.
func TestProjectTypeConstantsMatchSpec(t *testing.T) {
	if models.ProjectTypeMobileAppNative != "mobile_app_native" {
		t.Errorf("ProjectTypeMobileAppNative = %q, want mobile_app_native", models.ProjectTypeMobileAppNative)
	}
	if models.ProjectTypeApp != "app" {
		t.Errorf("ProjectTypeApp = %q, want app", models.ProjectTypeApp)
	}
	if models.ProjectTypeEcommerce != "ecommerce" {
		t.Errorf("ProjectTypeEcommerce = %q, want ecommerce", models.ProjectTypeEcommerce)
	}
}
