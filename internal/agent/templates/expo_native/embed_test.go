package expo_native

import (
	"io/fs"
	"testing"
)

// TestEmbedIncludesTemplateBootFiles guards against a regression where the
// //go:embed directive list in embed.go drifts behind the on-disk template.
// Past omissions of tailwind.config.js / babel.config.js silently shipped
// scaffolded projects that crashed Metro at startup.
//
// Files listed here are the minimum set Metro + Expo CLI need at boot.
// When refreshing the template, add any new boot-required files here so
// the build fails loudly instead of breaking users at scaffold time.
func TestEmbedIncludesTemplateBootFiles(t *testing.T) {
	required := []string{
		"app.json",
		// app.config.js patches the resolved Expo config based on
		// EXPO_GO_MODE — strips EAS / fingerprint when the Expo Go-targeted
		// Metro starts. Drop it and the Expo Go preview hits a non-
		// interactive auth wall on manifest fetch.
		"app.config.js",
		"babel.config.js",
		"eas.json",
		"global.css",
		"metro.config.js",
		"nativewind-env.d.ts",
		"package.json",
		"postcss.config.mjs",
		"tailwind.config.js",
		"tsconfig.json",
		".npmrc",
		".gitignore",
		"store/index.ts",
		"app/_layout.tsx",
		"lib/api.ts",
		// push.ts and iap.ts carry the Expo Go runtime guards. Dropping
		// them would let the agent's generated code crash at module-eval
		// time the moment a project is opened in Expo Go.
		"lib/push.ts",
		"lib/iap.ts",
	}
	for _, p := range required {
		if _, err := fs.Stat(FS, p); err != nil {
			t.Errorf("embed.FS missing %q: %v — likely dropped from the //go:embed directive list", p, err)
		}
	}
}
