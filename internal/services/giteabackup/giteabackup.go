// Package giteabackup pushes per-project local git repos to Gitea after every
// commit so the API container's volume isn't the only copy of user code.
//
// Design notes:
//   - Fire-and-forget. Never blocks the commit caller.
//   - Per-project serialization: concurrent commits coalesce — a backup that's
//     already running absorbs subsequent triggers and pushes HEAD once it
//     finishes. We don't queue separate runs per commit; force-pushing HEAD
//     after the last commit is equivalent to pushing each one for backup.
//   - Errors are logged and swallowed. Backup is best-effort; a failed push
//     is not a user-visible problem and the next commit will retry.
//   - Idempotent repo provisioning via gitea.EnsureRepo (HEAD then maybe POST).
package giteabackup

import (
	"log/slog"
	"sync"

	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/gitrepo"
)

const repoNamePrefix = "wemob-backup-"

type Service struct {
	gitea  *gitea.Service
	repos  *gitrepo.Service
	state  sync.Map // projectID -> *projectState
}

type projectState struct {
	mu      sync.Mutex
	pending bool // another commit landed while a backup was in progress
}

func New(g *gitea.Service, r *gitrepo.Service) *Service {
	return &Service{gitea: g, repos: r}
}

// OnCommit is the gitrepo.CommitHook entry point. Schedules a backup for the
// project, coalescing with any in-flight backup.
func (s *Service) OnCommit(projectID string) {
	if s == nil || s.gitea == nil {
		return
	}
	v, _ := s.state.LoadOrStore(projectID, &projectState{})
	st := v.(*projectState)

	// If the per-project mutex is already held, mark pending and return —
	// the in-flight runner will see `pending` and re-push HEAD before exiting.
	if !st.mu.TryLock() {
		st.pending = true
		return
	}
	go s.run(projectID, st)
}

// run holds st.mu for the duration. After the push, if more commits triggered
// during the run (st.pending == true), it loops to push HEAD again.
func (s *Service) run(projectID string, st *projectState) {
	defer st.mu.Unlock()
	for {
		st.pending = false
		s.pushOnce(projectID)
		if !st.pending {
			return
		}
		// Another commit landed while we were pushing — push the new HEAD too.
	}
}

func (s *Service) pushOnce(projectID string) {
	repo, err := s.repos.Open(projectID)
	if err != nil {
		slog.Warn("gitea_backup_open_failed", "project", projectID, "err", err)
		return
	}
	branch, err := repo.CurrentBranch()
	if err != nil || branch == "" {
		branch = "main"
	}
	cloneURL, err := s.gitea.EnsureRepo(repoNamePrefix + projectID)
	if err != nil {
		slog.Warn("gitea_backup_ensure_failed", "project", projectID, "err", err)
		return
	}
	if err := repo.ForcePushToURL(cloneURL, branch); err != nil {
		slog.Warn("gitea_backup_push_failed", "project", projectID, "err", err)
		return
	}
	slog.Debug("gitea_backup_ok", "project", projectID, "branch", branch)
}
