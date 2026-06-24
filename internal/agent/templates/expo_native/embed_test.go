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
		"babel.config.js",
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
		// nativewind-setup registers cssInterop for Animated.* components.
		// Without it, every <Animated.View className="..."> in user code
		// silently drops styles, breaking screen padding everywhere.
		"lib/nativewind-setup.ts",
	}
	for _, p := range required {
		if _, err := fs.Stat(FS, p); err != nil {
			t.Errorf("embed.FS missing %q: %v — likely dropped from the //go:embed directive list", p, err)
		}
	}
}
