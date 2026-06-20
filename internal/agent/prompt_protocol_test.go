package agent

import (
	"strings"
	"testing"
)

func TestAgentPromptsDoNotAdvertiseRemovedRawFileBlockProtocol(t *testing.T) {
	prompts := map[string]string{
		"planningDiscipline":             planningDiscipline,
		"BasePromptWebFirstGen":          BasePromptWebFirstGen,
		"BasePromptWebChat":              BasePromptWebChat,
		"BasePromptMobileNativeFirstGen": BasePromptMobileNativeFirstGen,
		"BasePromptMobileNativeChat":     BasePromptMobileNativeChat,
		"CompactBasePromptWeb":           CompactBasePromptWeb,
		"CompactBasePromptMobileNative":  CompactBasePromptMobileNative,
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			for _, forbidden := range []string{
				"BEGIN_WEMOB_FILE",
				"END_WEMOB_FILE",
				"raw file block",
				"raw file blocks",
				"raw blocks",
			} {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("%s still mentions removed raw file block protocol %q", name, forbidden)
				}
			}
		})
	}
}

func TestAgentBasePromptsDescribeDaytonaSandboxRuntime(t *testing.T) {
	prompts := map[string]string{
		"CompactBasePromptWeb":          CompactBasePromptWeb,
		"CompactBasePromptMobileNative": CompactBasePromptMobileNative,
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			for _, forbidden := range []string{
				"E2B",
				"/home/user/project",
				"/home/user/backend",
				"/home/user/artifacts",
			} {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("%s still contains stale sandbox guidance %q", name, forbidden)
				}
			}
			for _, required := range []string{
				"Daytona-backed sandbox",
				"/workspace/project",
				"/workspace/backend",
				"/workspace/artifacts",
				"sandbox_start",
				"sandbox_sync",
				"process_restart",
				"Git-backed",
			} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("%s is missing sandbox guidance %q", name, required)
				}
			}
		})
	}
}
