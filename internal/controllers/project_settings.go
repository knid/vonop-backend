package controllers

import "github.com/knid/wemob/internal/constants"

// applyDefaultAgentSettings adds default agent runtime settings that must be
// present for user projects to avoid silent behavior changes when a project is
// persisted from older rows or newer templates.
func applyDefaultAgentSettings(settings map[string]any) map[string]any {
	if settings == nil {
		settings = make(map[string]any)
	}
	if _, exists := settings[constants.AgentSettingAllowSandboxExec]; !exists {
		settings[constants.AgentSettingAllowSandboxExec] = true
	}
	return settings
}

func cloneProjectSettingsWithDefaults(settings map[string]any) map[string]any {
	cloned := make(map[string]any, len(settings))
	for key, value := range settings {
		cloned[key] = value
	}
	return applyDefaultAgentSettings(cloned)
}
