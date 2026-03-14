# Git-Based File Storage for Generated Apps

**Date:** 2026-03-14
**Status:** Approved

## Problem

Generated app files are stored as rows in the `AppGeneratedFile` Postgres table, and a legacy `AST` JSON blob (containing the entire project source as base64) is stored on `AppProject`, `AppSnapshot`, and `AppDeployment` models. This causes:

- Massive DB queries logged by Bun ORM (500KB+ single log lines)
- Slow UPDATE operations (10+ seconds for AST writes)
- Full file copies on every snapshot (no diffing)
- No real version history or diff capability

## Solution

Replace `AppGeneratedFile` table and `AST` fields with per-project git repositories on the local filesystem. Use `os/exec` to shell out to the `git` CLI.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Storage location | Local filesystem (`/data/repos/{projectId}/`) | Single droplet, fastest option, easy S3 backup later |
| Git implementation | `os/exec` shelling out to `git` CLI | Battle-tested, debuggable, no library deps |
| Commit strategy | Files written to disk during streaming, single commit after generation round completes | Preserves real-time SSE events, clean history |
| Snapshots/Undo | Git commit history (no separate snapshots table) | Commits ARE snapshots, no duplication needed |
| Theme versioning | Stored as `_wemob/theme.json` in the repo | Versioned automatically with commits, no special cases |
| Deployment reference | Commit hash on `AppDeployment` + git tag | Queryable via DB, human-readable via tag |
| `AppGeneratedFile` table | Remove entirely | Git is single source of truth |

## Architecture

### New Package: `internal/services/gitrepo/`

Wraps `os/exec` git calls behind a clean Go interface. Injected into `Controller` as a typed service (consistent with existing patterns like `*vercel.Service`, `*neon.Service`).

**Repo structure on disk:**
```
/data/repos/{projectId}/
  ├── .git/
  ├── .gitignore          # node_modules/, .next/, .env*
  ├── _wemob/
  │   └── theme.json
  ├── src/
  │   ├── app/
  │   │   ├── page.tsx
  │   │   └── layout.tsx
  │   └── components/
  │       └── Header.tsx
  ├── package.json
  └── ...
```

**Service struct:**
```go
type Service struct {
    BasePath string // /data/repos
    locks    sync.Map // per-project mutexes: map[projectID]*sync.Mutex
}

func NewService(basePath string) *Service

// Acquire per-project lock for write operations
func (s *Service) lock(projectID string) func()
```

**Repo struct and interface:**
```go
type GitRepo struct {
    Path      string // /data/repos/{projectId}
    ProjectID string
}

// Lifecycle (on Service)
func (s *Service) Init(projectID string) (*GitRepo, error)
func (s *Service) Open(projectID string) (*GitRepo, error)
func (s *Service) Delete(projectID string) error
func (s *Service) Clone(sourceID, targetID string) (*GitRepo, error)

// File operations
func (r *GitRepo) WriteFile(path string, content []byte) error
func (r *GitRepo) ReadFile(path string) ([]byte, error)
func (r *GitRepo) DeleteFile(path string) error
func (r *GitRepo) ListFiles() ([]string, error)
func (r *GitRepo) ReadAllFiles() (map[string][]byte, error)

// Git operations
func (r *GitRepo) Commit(message string) (commitHash string, err error)
func (r *GitRepo) Revert(commitHash string) error
func (r *GitRepo) Tag(name string) error
func (r *GitRepo) Log(limit int) ([]CommitInfo, error)
func (r *GitRepo) ReadFileAtCommit(path, commitHash string) ([]byte, error)
func (r *GitRepo) ReadAllFilesAtCommit(commitHash string) (map[string][]byte, error)
```

**Implementation details:**
- `Commit()` runs `git add -A && git commit -m "..."`, returns the commit hash
- `Revert()` runs `git read-tree --reset -u <hash>` for full tree replacement (deletes files added after that commit, unlike `git checkout <hash> -- .` which only restores existing files)
- `WriteFile()` uses `os.MkdirAll` + `os.WriteFile` (no git command needed)
- `ReadAllFiles()` walks the directory tree, skipping `.git/` and `_wemob/`
- `ReadAllFilesAtCommit()` uses `git archive <hash> | tar` for efficient single-command extraction
- All exec calls use `exec.CommandContext` with timeouts (30s for commits, 10s for reads)
- File paths validated via `SanitizeFilePath()` before any disk operation
- `Init()` creates a `.gitignore` with `node_modules/`, `.next/`, `.env*`
- `Clone()` creates new repo via `Init`, copies working tree files only (no `.git/`), then commits — avoids leaking source project history

**Concurrency safety:**
- Per-project mutex via `sync.Map` on the `Service` struct
- All write sequences (write + commit, revert + commit) acquire the project lock
- Read operations (ReadAllFiles, ReadFile) do not need locks (git working tree reads are safe)
- The existing `builderToolCallbacks.fileMu` continues protecting the in-memory fileMap

### Controller Integration

**Controller struct changes:**
```go
type Controller struct {
    // ... existing fields
    GitRepoService *gitrepo.Service // injected like other services
}
```

### Builder Flow (StreamBuildApp)

```
1. repo := c.GitRepoService.Open(project.ID.String())
2. During streaming: repo.WriteFile(path, content)
   - SSE file_complete events still fire (unchanged)
   - In-memory fileMap still updated (unchanged)
   - FileID in SSE events: use deterministic UUID from path hash (or drop field if frontend doesn't need it)
3. After generation completes:
   - Acquire project lock
   - repo.Commit("chat: <user message>") or repo.Commit("initial generation")
   - Release lock
```

**saveGeneratedFile changes:**
- Remove: `c.DB.CreateAppGeneratedFile()` / `c.DB.UpdateAppGeneratedFile()`
- Add: `repo.WriteFile(path, content)`
- Keep: in-memory fileMap update, SSE event emission

**deleteGeneratedFile changes:**
- Remove: `c.DB.DeleteAppGeneratedFile()`
- Add: `repo.DeleteFile(path)`
- Keep: in-memory fileMap removal

### Preview Bundle (E2B/WebContainer)

```
Currently:  c.DB.GetAppGeneratedFilesByProjectID() → map[path]content
New:        repo.ReadAllFiles() → map[path]content
```

Same JSON response shape. Frontend sees no change. `_wemob/` directory excluded from results.

### Vercel Deployment

```
Currently:  c.DB.GetAppGeneratedFilesByProjectID() → fileMap → inject .env.local + analytics → DeployFiles()
New:        repo.ReadAllFiles() → fileMap → inject .env.local + analytics → DeployFiles()
```

Same pipeline, different file source. Vercel API gets identical input.

**AppDeployment model changes:**
```go
type AppDeployment struct {
    // ... existing fields
    CommitHash string `bun:",notnull"` // NEW: replaces ASTSnapshot
    // Remove: ASTSnapshot []byte
}
```

**Deploy flow:**
1. `commitHash` = latest commit hash (from last generation/chat)
2. `repo.Tag("deploy-1710000000")`
3. Create `AppDeployment` with `CommitHash: commitHash`
4. Read files via `repo.ReadAllFiles()` for Vercel upload

### Undo / Version History

No separate snapshots table. Git commit log IS the history.

**Undo flow:**
1. Acquire project lock
2. `commits := repo.Log(2)` — get last 2 commits
3. `repo.Revert(commits[1].Hash)` — full tree replacement via `git read-tree --reset -u`
4. `repo.Commit("undo: reverted to previous version")`
5. Theme restored automatically (it's `_wemob/theme.json` in the repo)
6. Release lock

**History listing:**
- `repo.Log(limit)` returns `[]CommitInfo{Hash, Message, Timestamp}`
- Frontend shows commit messages as version history

**Commit message convention:**
```
type: description
```
Types: `init`, `chat`, `undo`, `deploy`, `clone`
Examples: `init: initial generation`, `chat: fix the navbar styling`, `undo: reverted to previous version`

### Mobile Deploy (Android/iOS)

Same as Vercel — read files from repo instead of DB:
```
repo.ReadAllFiles() → fileMap → enqueue Asynq task
```

### ZIP Export

```
Currently:  c.DB.GetAppGeneratedFilesByProjectID() → ZIP
New:        repo.ReadAllFiles() → ZIP
```

### Project Clone

```
Currently:  Copy AST + files from source project
New:        c.GitRepoService.Clone(sourceID, targetID)
            → Init new repo, copy working tree files (no .git/), commit
```

### Project Deletion

`DeleteAppProject` must call `c.GitRepoService.Delete(projectID)` to clean up the repo directory. For soft-deleted projects, a background cleanup job removes repos for projects deleted more than 30 days ago.

## Fields/Tables Removed

| Item | Location | Action |
|------|----------|--------|
| `AST []byte` | `AppProject` model | Remove field |
| `AST []byte` | `AppSnapshot` model | Remove entire model/table |
| `ASTSnapshot []byte` | `AppDeployment` model | Replace with `CommitHash string` |
| `AppGeneratedFile` | model + DB methods | Remove entire model/table |
| `app_snapshots` | database table | Drop table |
| `app_generated_files` | database table | Drop table after migration |

## New Dependencies

- `git` binary in Docker image: `apk add git` in Dockerfile (pin minimum version 2.36+)
- `/data/repos/` volume mount in docker-compose
- Git config in Dockerfile: `git config --global user.email "system@wemob.io"` and `git config --global user.name "wemob"`

## Backup & Disaster Recovery

Ship with the initial release — not deferred:
- **Cron job:** Every 6 hours, `tar -czf /backups/repos-$(date +%s).tar.gz /data/repos/` and upload to S3
- **Retention:** Keep last 7 days of backups in S3
- **Restore:** Download tarball, extract to `/data/repos/`, restart container
- **Monitoring:** Alert if `/data/repos/` volume exceeds 80% utilization
- **Git GC:** Weekly cron runs `git gc` on all repos to pack loose objects

## Migration Strategy

1. Deploy new code that reads from git but falls back to DB if repo doesn't exist
2. Migration script: for each project, read files from `AppGeneratedFile`, write to git repo, commit
3. Keep DB tables read-only for one release cycle as rollback safety net
4. After confirming stability, remove DB fallback code
5. Drop `app_generated_files` and `app_snapshots` tables

## Docker Changes

**Dockerfile:**
```dockerfile
RUN apk add --no-cache git && \
    git config --global user.email "system@wemob.io" && \
    git config --global user.name "wemob" && \
    git config --global init.defaultBranch main
```

**docker-compose.yaml:**
```yaml
services:
  api:
    volumes:
      - repo-data:/data/repos

volumes:
  repo-data:
```

Ensure `/data/repos` ownership matches the container's `appuser` UID.

## Configuration

New secret or env var: `REPO_BASE_PATH` (default: `/data/repos`)
