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
