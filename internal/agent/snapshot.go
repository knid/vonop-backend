package agent

import (
	"fmt"

	"github.com/knid/wemob/internal/services/gitrepo"
)

// PreRunCommit captures a baseline commit before the agent makes changes.
// Returns the commit SHA (or the HEAD SHA if there were no pending changes).
func PreRunCommit(repo *gitrepo.Repo, runID string) (string, error) {
	msg := fmt.Sprintf("agent: pre-run %s", runID)
	hash, err := repo.Commit(msg)
	if err != nil {
		// If there's nothing to commit, HEAD is already the pre-run snapshot.
		head, headErr := repo.Head()
		if headErr != nil {
			return "", fmt.Errorf("pre-commit: %w (head: %v)", err, headErr)
		}
		return head, nil
	}
	return hash, nil
}

// FinaliseRun writes the fileMap to disk and commits with the given message.
// Returns the final commit SHA. An informational turn that mutates no files
// lands here with nothing staged; treat that the same way PreRunCommit does —
// no commit means HEAD is the correct final SHA — so the run finalises as
// success instead of being flipped to CodeGitError for doing nothing.
func FinaliseRun(repo *gitrepo.Repo, fileMap *SafeFileMap, userMessage string) (string, error) {
	snap := fileMap.Snapshot()
	if err := repo.WriteFileMap(snap); err != nil {
		return "", fmt.Errorf("write file map: %w", err)
	}
	msg := fmt.Sprintf("agent: %s", firstLine(userMessage, 80))
	hash, err := repo.Commit(msg)
	if err != nil {
		head, headErr := repo.Head()
		if headErr != nil {
			return "", fmt.Errorf("post-commit: %w (head: %v)", err, headErr)
		}
		return head, nil
	}
	return hash, nil
}

// RollbackTo hard-resets to the given commit. Called on run failure as a
// defensive measure; in practice tool mutations only touch the in-memory
// fileMap so there's usually nothing on disk to undo.
func RollbackTo(repo *gitrepo.Repo, preCommit string) error {
	if preCommit == "" {
		return nil
	}
	return repo.Reset(preCommit)
}

// firstLine returns the first non-empty line of s, truncated to max characters.
func firstLine(s string, max int) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			s = s[:i]
			break
		}
	}
	if len(s) > max {
		s = s[:max]
	}
	if s == "" {
		return "chat turn"
	}
	return s
}
