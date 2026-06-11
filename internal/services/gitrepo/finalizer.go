package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrFinalizationConflict = errors.New("gitrepo: project current commit changed since run base")

type FinalizeRunOptions struct {
	Successful            bool
	Summary               string
	ExpectedCurrentCommit string
}

type FinalizeRunResult struct {
	Noop               bool
	RunCommit          string
	RunRef             string
	FinalCommit        string
	CheckpointCommit   string
	FinalizedToProject bool
}

func (s *BareService) FinalizeRunWorktree(
	ctx context.Context,
	wt *RunWorktree,
	opts FinalizeRunOptions,
) (*FinalizeRunResult, error) {
	if wt == nil || wt.Path == "" {
		return nil, fmt.Errorf("gitrepo: run worktree is required")
	}
	if wt.service == nil {
		wt.service = s
	}
	if opts.Summary == "" {
		opts.Summary = "workspace changes"
	}

	dirty, err := gitDirty(ctx, wt.Path)
	if err != nil {
		return nil, fmt.Errorf("gitrepo: inspect run worktree: %w", err)
	}
	if !dirty {
		return &FinalizeRunResult{Noop: true}, nil
	}

	message := fmt.Sprintf("agent run: %s - %s", wt.JobID, strings.TrimSpace(opts.Summary))
	if !opts.Successful {
		message = fmt.Sprintf("agent checkpoint: failed run %s - %s", wt.JobID, strings.TrimSpace(opts.Summary))
	}
	if err := commitAll(ctx, wt.Path, message); err != nil {
		return nil, fmt.Errorf("gitrepo: create run commit: %w", err)
	}
	runCommit, err := s.gitOutput(ctx, wt.Path, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("gitrepo: read run commit: %w", err)
	}
	runCommit = strings.TrimSpace(runCommit)
	runRef := "refs/wemob/runs/" + wt.JobID
	if err := s.git(ctx, wt.BareRepoPath, "update-ref", runRef, runCommit); err != nil {
		return nil, fmt.Errorf("gitrepo: update run ref: %w", err)
	}

	result := &FinalizeRunResult{
		RunCommit: runCommit,
		RunRef:    runRef,
	}
	if !opts.Successful {
		result.CheckpointCommit = runCommit
		return result, nil
	}

	current := strings.TrimSpace(opts.ExpectedCurrentCommit)
	if current != "" && current != wt.BaseCommit {
		result.CheckpointCommit = runCommit
		return result, ErrFinalizationConflict
	}
	if err := s.git(ctx, wt.BareRepoPath, "update-ref", "refs/heads/main", runCommit); err != nil {
		return nil, fmt.Errorf("gitrepo: update main ref: %w", err)
	}
	result.FinalCommit = runCommit
	result.FinalizedToProject = true
	return result, nil
}
