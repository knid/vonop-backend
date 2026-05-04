package controllers

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/knid/wemob/internal/agent/templates/expo_native"
	"github.com/knid/wemob/internal/services/gitrepo"
)

// scaffoldExpoTemplate copies every file in the embedded wemob-expo-template
// FS into the project's git repo under mobile/<path>. Called from
// CreateAppProject for mobile_app_native projects so the AI agent has a
// real Expo project to read from + edit, and the preview sandbox can run
// `npx expo start --web` against a complete source tree.
//
// Existing files in mobile/ are NOT overwritten — this is a one-shot
// scaffold meant to run on a fresh project. Callers should only invoke
// when the repo's mobile/ subtree is empty (or only contains .gitkeep).
//
// Binary assets (PNG icons under assets/) are written as-is. Text files
// are written verbatim — there's no project-name templating because the
// build pipeline (codemagic.yaml on the build side, app.json's expo CLI
// substitution at expo prebuild time) already substitutes per-project
// values like __APP_TITLE__, __APP_SLUG__, etc.
func scaffoldExpoTemplate(repo *gitrepo.Repo) error {
	if repo == nil {
		return fmt.Errorf("scaffoldExpoTemplate: nil repo")
	}

	return fs.WalkDir(expo_native.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip the embed.go directive file itself — it's pulled in by `all:`
		// otherwise but doesn't belong in the user's repo.
		if p == "embed.go" || strings.HasSuffix(p, "/embed.go") {
			return nil
		}
		data, err := expo_native.FS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("scaffoldExpoTemplate: read %s: %w", p, err)
		}
		dest := path.Join("mobile", p)
		if err := repo.WriteFile(dest, data); err != nil {
			return fmt.Errorf("scaffoldExpoTemplate: write %s: %w", dest, err)
		}
		return nil
	})
}
