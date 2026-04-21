package utils

// IsHiddenEditorFile reports whether a file should be excluded from user-facing
// file listings (the frontend code editor) and from the agent's list_files tool.
// These files exist in the project repo because wemob-backend or the build
// system needs them, but exposing them would either leak secrets or invite
// edits that break deploys.
//
// Note: the agent can still read these explicitly via read_file (they're
// protected from write/edit/delete separately in agent.protectedPaths); they
// just don't show up in broad listings or glob matches.
func IsHiddenEditorFile(path string) bool {
	switch path {
	case ".env.local", "Dockerfile", "next.config.mjs":
		return true
	}
	return false
}
