package agent

import (
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

func TestBuildSystemMessages_MobileAppNativeUsesExpoPrompt(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, nil)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "Expo Router") {
		t.Error("expected 'Expo Router' to appear in mobile_app_native system messages")
	}
	if !strings.Contains(joined, "NativeWind") {
		t.Error("expected 'NativeWind' to appear in mobile_app_native system messages")
	}
	if !strings.Contains(joined, "react-native-reusables") {
		t.Error("expected 'react-native-reusables' to appear in mobile_app_native system messages")
	}
}

func TestBuildSystemMessages_AppTypeUsesWebPrompt(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, nil)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "Expo Router") {
		t.Error("expected web 'app' system messages NOT to include Expo content")
	}
}

func TestBuildSystemMessages_LegacyMobileUsesLegacyPrompt(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppLegacy,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, nil)
	joined := concatMessages(msgs)

	// Legacy WebView prompt content (BasePromptMobile) should NOT mention "Expo Router".
	if strings.Contains(joined, "Expo Router") {
		t.Error("legacy mobile_app should use BasePromptMobile, not BasePromptMobileNative")
	}
}

func concatMessages(msgs []utils.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
