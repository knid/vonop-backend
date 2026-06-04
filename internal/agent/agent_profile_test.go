package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestResolveAgentProfile(t *testing.T) {
	cases := []struct {
		name        string
		projectType string
		want        string
	}{
		{"web app", models.ProjectTypeApp, AgentIDWebBuilder},
		{"ecommerce", models.ProjectTypeEcommerce, AgentIDWebBuilder},
		{"mobile native", models.ProjectTypeMobileAppNative, AgentIDMobileBuilder},
		{"unknown defaults web", "legacy_unknown", AgentIDWebBuilder},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAgentProfile(&models.AppProject{ProjectType: tc.projectType})
			if got.ID != tc.want {
				t.Fatalf("ResolveAgentProfile(%q) = %q, want %q", tc.projectType, got.ID, tc.want)
			}
		})
	}
}

func TestBuildRegistryFor_ProfileToolSeparation(t *testing.T) {
	web := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeApp, Status: "ready"}, nil, "")
	webNames := web.Names()
	if slices.Contains(webNames, "read_mobile_config") {
		t.Fatal("web builder registry must not expose mobile config tool")
	}
	if !slices.Contains(webNames, "trigger_web_deploy") {
		t.Fatal("web builder registry should expose web deploy tool outside draft")
	}

	mobile := BuildRegistryFor(&models.AppProject{ProjectType: models.ProjectTypeMobileAppNative, Status: "ready"}, nil, "")
	mobileNames := mobile.Names()
	if !slices.Contains(mobileNames, "read_mobile_config") {
		t.Fatal("mobile builder registry should expose mobile config tool")
	}
	if !slices.Contains(mobileNames, "trigger_build_after_fix") {
		t.Fatal("mobile builder registry should expose build trigger outside draft")
	}
}

func TestBuildSystemMessagesForProfile_UsesProfilePrompts(t *testing.T) {
	webProject := &models.AppProject{ProjectType: models.ProjectTypeApp, Settings: map[string]any{}}
	webMsgs := BuildSystemMessagesForProfile(ResolveAgentProfile(webProject), webProject, nil, nil, false)
	if strings.Contains(concatMessages(webMsgs), "Expo Router") {
		t.Fatal("web profile should not use mobile base prompt")
	}

	mobileProject := &models.AppProject{ProjectType: models.ProjectTypeMobileAppNative, Settings: map[string]any{}}
	mobileMsgs := BuildSystemMessagesForProfile(ResolveAgentProfile(mobileProject), mobileProject, nil, nil, false)
	if !strings.Contains(concatMessages(mobileMsgs), "Expo Router") {
		t.Fatal("mobile profile should use mobile base prompt")
	}
}
