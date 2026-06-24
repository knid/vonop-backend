package agent

import (
	"os"
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
				"process_restart",
				"Git-backed",
			} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("%s is missing sandbox guidance %q", name, required)
				}
			}
			for _, forbidden := range []string{
				"sandbox_start",
				"sandbox_sync",
			} {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("%s still advertises removed sandbox tool %q", name, forbidden)
				}
			}
		})
	}
}

func TestWebFirstGenPromptEmphasizesBatchedUiScaffold(t *testing.T) {
	required := []string{
		"Default phase is UI scaffold only",
		"Plan the file set before writing",
		"Batch many small files in each write_files call",
		"Do not use read_files as the default inspection tool",
		"Do not re-read files you just wrote",
	}
	for _, text := range required {
		if !strings.Contains(BasePromptWebFirstGen, text) {
			t.Fatalf("BasePromptWebFirstGen missing batching/scope guidance %q", text)
		}
	}

	for _, forbidden := range []string{
		"build production-grade web applications",
		"finish the entire visible UI/product surface BEFORE any",
		"Keep tool arguments valid JSON by writing smaller files",
	} {
		if strings.Contains(BasePromptWebFirstGen, forbidden) {
			t.Fatalf("BasePromptWebFirstGen still carries over-broad first-gen guidance %q", forbidden)
		}
	}
}

func TestMobileFirstGenPromptDefaultsToSmallUiScaffold(t *testing.T) {
	for _, required := range []string{
		"Default phase is UI scaffold only",
		"create DESIGN.md",
		"one coherent primary mobile screen",
		"Do not add backend APIs",
		"Stop after one focused verification pass",
	} {
		if !strings.Contains(BasePromptMobileNativeFirstGen, required) {
			t.Fatalf("BasePromptMobileNativeFirstGen missing lean scaffold guidance %q", required)
		}
	}

	for _, forbidden := range []string{
		"home screen first",
		"finish the entire visible UI surface",
		"Every other screen + component with stubs",
		"Images Last",
	} {
		if strings.Contains(BasePromptMobileNativeFirstGen, forbidden) {
			t.Fatalf("BasePromptMobileNativeFirstGen still carries over-broad first-gen guidance %q", forbidden)
		}
	}
}

func TestWebBuilderFoundationBatchesSmallFiles(t *testing.T) {
	if strings.Count(webBuilderFoundationSkillForTests(), "NEVER write files larger than ~15KB") != 0 {
		t.Fatal("web-builder foundation must not repeat the old single-file-biased 15KB warning")
	}
	if !strings.Contains(webBuilderFoundationSkillForTests(), "Keep each individual file under ~15KB when practical, but batch many small files in one write_files call") {
		t.Fatal("web-builder foundation must tell the model to batch many small files")
	}
}

func webBuilderFoundationSkillForTests() string {
	b, err := os.ReadFile("skills/web/web-builder-foundation/SKILL.md")
	if err != nil {
		panic(err)
	}
	return string(b)
}
