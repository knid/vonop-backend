# Wemob Deep Agents Production Runtime Implementation Plan

> Goal: replace Wemob’s current Go-owned agent runtime with a Deep Agents-native production agent system.
>
> Principle: Wemob adapts to Deep Agents. Deep Agents owns the agent runtime, checkpoints, memory, subagents, filesystem surface, permissions, context engineering, sandboxes, and execution lifecycle. Wemob remains the product shell: auth, projects, organizations, billing, app metadata, deployments, assets, and user-facing APIs.
>
> Non-negotiable: do not modify, wrap, normalize, or re-invent Deep Agents streaming. Wemob frontend and backend must adapt to the default Deep Agents / LangGraph stream and frontend SDK contract.

---

## 0. Source basis and current-state assumptions

### 0.1 Deep Agents facts used

Deep Agents is an agent harness built on LangGraph. It provides built-in planning, filesystem/context management, subagent spawning, long-term memory, streaming, permissions, human approval, skills, and sandbox execution.

Production Deep Agents should be invoked with:

- `thread_id`: stable conversation/checkpoint identity.
- `context`: per-run runtime data used by tools and middleware, such as `user_id`, feature flags, project IDs, credentials references, and metadata.

Deep Agents / LangGraph provide durable execution through checkpointing. If a run is interrupted by crash, timeout, or human-in-the-loop pause, it can resume from the latest checkpoint rather than replaying the whole run.

Deep Agents filesystem is backend-based. It can route different virtual paths to different backends using `CompositeBackend`. Useful built-ins include:

- `StateBackend`: thread-scoped scratch files persisted by checkpointer.
- `StoreBackend`: durable cross-thread storage for long-term memory.
- `FilesystemBackend`: local filesystem access for development or controlled environments.
- sandbox backends: isolated environments with filesystem tools plus `execute`.

Deep Agents permissions apply only to built-in filesystem tools, not arbitrary custom tools, MCP tools, or sandbox `execute`. Wemob must still authorize custom tools and sensitive product operations.

Deep Agents frontend support is also a source-of-truth constraint. Wemob-app should use the LangChain frontend SDK directly (`@langchain/react`, `useStream`) and render Deep Agents projections such as:

- `stream.messages`
- `stream.subagents`
- `stream.values`
- tool-call state
- interrupts / human-in-the-loop state

Wemob must not introduce a custom agent stream protocol when the default Deep Agents stream already carries the runtime state.

### 0.2 Wemob current-state facts used

Current Wemob backend has a Go runtime centered around `internal/agent.Run`. That runtime currently owns setup, project repo loading, template injection, run row creation, prompt/context compilation, tool registry, tool loop, validation, Git commit finalization, credit tracking, and event emission.

Current Git repo service stores project repos under a base path, opens or initializes repos by project ID, uses local process locks, and performs Git commands directly with `git add`, `git status`, and `git commit`.

Current Go agent mutates a `SafeFileMap` in memory and writes/commits files only at finalization. In the new system, Deep Agents will write to real workspace files.

Current worker calls `agent.Run(...)` from `HandleAgentRunTask`. That call boundary becomes the replacement point.

Current `AgentDeps` already exposes useful product abilities: image generation, Neon/database provisioning, AI gateway setup, local database setup, web deploy, mobile build, legacy E2B sandbox control, preview URL updates, usage tracking, MCP listing/execution/approval. The new long-term design replaces legacy E2B assumptions with Daytona.

---

## 1. North-star architecture

### 1.1 Final production architecture

```text
wemob-app
  ↓
wemob-backend Go API
  - auth
  - users/orgs/projects
  - billing/credits
  - project metadata
  - app assets
  - deployment records
  - public product API
  - creates/resolves Deep Agents threads and runs
  - provides authenticated runtime access without changing stream shape
  ↓
Deep Agents Runtime Service
  - LangGraph/Deep Agents app
  - checkpointer
  - long-term memory store
  - supervisor agent
  - subagents
  - Deep Agents filesystem backend
  - Daytona sandbox backend
  - product tools
  - permissions and human approval
  - native Deep Agents / LangGraph streaming
  ↓
Git storage + Daytona + product services
  - bare project repos
  - per-run worktrees
  - sandbox build/test/preview
  - Neon/project DB
  - asset storage
  - deploy/build systems
```

### 1.2 Hard boundary

Deep Agents owns:

```text
agent planning
agent loop
subagents
checkpoints
thread state
long-term memory
filesystem surface
context compression
skills
permissions for built-in filesystem tools
human approval interrupts
sandbox execution
tool lifecycle
native agent event stream
resume/retry within LangGraph semantics
```

Wemob owns:

```text
authentication
authorization
organizations
projects
billing
credit policies
product metadata
asset storage records
deployment records
external integration connection records
public API
authenticated runtime access
high-level job status
```

### 1.3 What must be removed from the agent domain

Long-term, these should not remain as the runtime source of truth:

```text
internal/agent.Run
internal/agent/registry.go
internal/agent/memory.go
internal/agent/run_context.go
internal/agent/agent_profile.go
internal/agent/prompt.go
internal/agent/tools_*.go
SafeFileMap as agent workspace
manual Go compaction
manual Go tool-loop
Go-owned handoff targets
Go-owned TODO tool
Go-owned skill loader
Go-owned agent memory JSON
Go-owned agent event stream
internal/views/agent_bridge.go
internal/responses.UIMessagePart as the agent stream contract
generation_jobs.stream_state as agent stream reconstruction
```

Some can remain temporarily as compatibility code during migration, but the final runtime should not depend on them.

---

## 2. Core design decisions

### Decision 1 — Self-host first, compatible with LangSmith later

Use self-hosted LangGraph / Deep Agents runtime first because Wemob needs custom project workspace, Daytona integration, product APIs, internal auth, and deep control over Git.

Design the runtime so it can later deploy to LangSmith Deployment if desired:

```text
agent-runtime/
  langgraph.json
  pyproject.toml
  wemob_agent/
    agent.py
    context.py
    ...
```

### Decision 2 — Git source stays outside checkpoints/memory

Do not store generated app source in LangGraph checkpoints, Deep Agents memory, or Postgres rows.

Source code belongs in Git:

```text
/data/git/bare/{project_id}.git
/data/worktrees/{project_id}/{job_id}/
```

Deep Agents sees the run worktree as `/workspace`.

### Decision 3 — Bare repos + per-run worktrees

Best production layout:

```text
/data/git/bare/{project_id}.git
/data/worktrees/{project_id}/{job_id}/
```

Benefits:

- isolated workspace per run
- clean failure rollback by deleting worktree
- cheap Git object sharing
- safe concurrent read/deploy operations
- easier sandbox seeding
- easier final diff validation
- better horizontal scaling story than one mutable checkout

### Decision 4 — Daytona is the production sandbox

Deep Agents should get code execution through Daytona, not host shell access.

The agent should use:

```text
/workspace filesystem tools
execute tool inside Daytona
```

No `LocalShellBackend` in production.

### Decision 5 — Deep Agents thread is the real agent conversation

Mapping:

```text
Wemob project conversation/thread   → deepagents thread_id
GenerationJob.ID                    → run/job context
AppProject.ID                       → project_id in context
User.ID / Org.ID                    → user_id/org_id in context
```

Wemob stores pointers and metadata only.

### Decision 6 — Native Deep Agents UI is the long-term target

wemob-app should render Deep Agents concepts directly:

```text
supervisor message
subagent started
subagent message
subagent tool call
file changed
approval requested
verification result
run completed
```

### Decision 7 — Typed product tools, no raw product DB access

Deep Agents tools can call Wemob APIs. They must not receive unrestricted Wemob production DB credentials.

Allowed:

```text
get_project_context
generate_image
provision_project_database
execute_project_sql
trigger_web_deploy
trigger_mobile_build
finalize_project_changes
```

Not allowed:

```text
raw SQL against Wemob product DB
direct access to users/orgs/billing tables
unscoped secrets
```


### Decision 8 — Finalization is deterministic, not prompt-dependent

Do not rely on the model to remember to call `finalize_project_changes`.

The agent may call a lightweight tool such as `mark_ready_for_finalize`, but the runtime graph must always run a deterministic post-agent finalization gate:

```text
deep_agent_supervisor
  ↓
inspect_workspace_dirty
  ↓
if dirty:
  create_run_commit
  verify_if_required
  finalize_or_checkpoint
else:
  complete_noop
```

This means:

```text
Model = creates/edits/verifies/proposes
Runtime = detects dirty workspace, creates run commit, updates refs/DB, and moves project current commit only when allowed
```

`finalize_project_changes` should become runtime-owned. It may be implemented as a custom tool/function, but it should be called by a deterministic LangGraph node after the Deep Agents run, not only by model choice.

### Decision 9 — Every meaningful run gets a durable Git commit

Every run with meaningful workspace changes should create a run commit, whether the run succeeds, fails, is canceled, or hits budget.

Only successful finalized runs update the project’s live/current commit.

Use Git refs:

```text
main                                      → latest successful project state
refs/wemob/runs/{job_id}                 → final state of every run, success/fail/cancel
refs/wemob/checkpoints/{job_id}/{index}  → optional mid-run checkpoints
```

Policy:

```text
success:
  commit run result
  update refs/wemob/runs/{job_id}
  update main/current project commit

failure/cancel/budget_exceeded:
  commit partial workspace if meaningful changes exist
  update refs/wemob/runs/{job_id}
  do not update main/current project commit
  store checkpoint_commit for continue/recovery

no-op:
  do not create commit unless explicitly requested for audit
```

This makes failed work recoverable without breaking the live app.

---

## 3. New repo and service structure

### 3.1 Add Python runtime inside monorepo

```text
agent-runtime/
  pyproject.toml
  uv.lock                       # if using uv
  langgraph.json
  README.md
  Dockerfile
  docker-compose.agent.yml       # local/dev only
  wemob_agent/
    __init__.py
    agent.py
    context.py
    config.py
    models.py
    prompts/
      supervisor.md
      subagents/
        product_researcher.md
        design_director.md
        frontend_builder.md
        mobile_builder.md
        backend_builder.md
        database_agent.md
        qa_verifier.md
        release_agent.md
    subagents/
      __init__.py
      specs.py
      product_researcher.py
      design_director.py
      frontend_builder.py
      mobile_builder.py
      backend_builder.py
      database_agent.py
      qa_verifier.py
      release_agent.py
    backends/
      __init__.py
      workspace_backend.py
      memory_backend.py
      skills_backend.py
      artifacts_backend.py
    workspace/
      __init__.py
      git_manager.py
      locks.py
      worktree.py
      diff.py
      validation.py
      cleanup.py
    sandbox/
      __init__.py
      daytona_backend.py
      preview.py
      sync.py
      commands.py
      artifacts.py
    tools/
      __init__.py
      project_context.py
      assets.py
      images.py
      database.py
      deploy.py
      mobile_build.py
      git_finalize.py
      approvals.py
      mcp.py
      billing.py
      observability.py
    streaming/
      __init__.py
      mapper.py
      events.py
      sink.py
    memory/
      __init__.py
      schemas.py
      policies.py
      bootstrap.py
    skills/
      wemob-supervisor/
        SKILL.md
      nextjs-app-router/
        SKILL.md
      tailwind-v4/
        SKILL.md
      design-quality/
        SKILL.md
      expo-router/
        SKILL.md
      database-modeling/
        SKILL.md
      verification-loop/
        SKILL.md
      deploy-debugging/
        SKILL.md
    tests/
      unit/
      integration/
      fixtures/
```

### 3.2 `langgraph.json`

```json
{
  "dependencies": ["."],
  "graphs": {
    "wemob_agent": "./wemob_agent/agent.py:create_agent"
  },
  "env": ".env"
}
```

If `create_agent` needs async Daytona setup, export an async graph factory:

```python
async def create_agent(config: RunnableConfig):
    ...
    return create_deep_agent(...)
```

### 3.3 Runtime packaging

Use `uv` or Poetry. Recommended `pyproject.toml` dependencies:

```toml
[project]
name = "wemob-agent-runtime"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
  "deepagents>=0.6.8",
  "langgraph>=1.0.0",
  "langgraph-sdk",
  "langchain",
  "langchain-openai",
  "langchain-anthropic",
  "langchain-google-genai",
  "langchain-community",
  "pydantic>=2",
  "httpx",
  "orjson",
  "tenacity",
  "structlog",
  "opentelemetry-api",
  "opentelemetry-sdk",
  "redis",
  "psycopg[binary,pool]",
  "asyncpg",
  "daytona",
  "langchain-daytona",
]
```

Pin exact versions during implementation after checking current compatible packages.

---

## 4. Data model changes in Wemob

### 4.1 New tables

#### `agent_threads`

Purpose: stable mapping from Wemob project conversation to Deep Agents thread.

```sql
CREATE TABLE agent_threads (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  app_project_id UUID NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

  deepagents_thread_id TEXT NOT NULL,
  conversation_key TEXT NOT NULL DEFAULT 'default',

  status TEXT NOT NULL DEFAULT 'active',
  metadata JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  UNIQUE (app_project_id, conversation_key),
  UNIQUE (deepagents_thread_id)
);

CREATE INDEX idx_agent_threads_project ON agent_threads(app_project_id);
CREATE INDEX idx_agent_threads_org ON agent_threads(organization_id);
```

#### `agent_runtime_runs`

Purpose: product metadata around one Deep Agents run.

```sql
CREATE TABLE agent_runtime_runs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  generation_job_id UUID NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
  agent_thread_id UUID NOT NULL REFERENCES agent_threads(id) ON DELETE CASCADE,

  app_project_id UUID NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

  deepagents_thread_id TEXT NOT NULL,
  deepagents_run_id TEXT,
  deepagents_checkpoint_id TEXT,

  runtime_version TEXT NOT NULL,
  graph_id TEXT NOT NULL DEFAULT 'wemob_agent',
  mode TEXT NOT NULL, -- generate | edit | fix | deploy | research | maintenance

  status TEXT NOT NULL DEFAULT 'queued',
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,

  base_commit TEXT,
  final_commit TEXT,
  checkpoint_commit TEXT,
  run_commit TEXT,
  run_ref TEXT,
  finalized_to_project BOOLEAN NOT NULL DEFAULT false,

  worktree_path TEXT,
  sandbox_id TEXT,

  model TEXT,
  provider TEXT,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd NUMERIC(12,6) NOT NULL DEFAULT 0,
  credits_used INTEGER NOT NULL DEFAULT 0,

  error_code TEXT,
  error_message TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_runtime_runs_job ON agent_runtime_runs(generation_job_id);
CREATE INDEX idx_agent_runtime_runs_thread ON agent_runtime_runs(agent_thread_id);
CREATE INDEX idx_agent_runtime_runs_project_created ON agent_runtime_runs(app_project_id, created_at DESC);
CREATE INDEX idx_agent_runtime_runs_status ON agent_runtime_runs(status);
```

#### `project_git_repositories`

Purpose: transition from `/data/repos/{project_id}` to bare repo + worktree architecture.

```sql
CREATE TABLE project_git_repositories (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  app_project_id UUID NOT NULL UNIQUE REFERENCES app_projects(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

  storage_mode TEXT NOT NULL DEFAULT 'bare_worktree',
  bare_repo_path TEXT NOT NULL,
  legacy_repo_path TEXT,
  default_branch TEXT NOT NULL DEFAULT 'main',

  current_commit TEXT,
  main_ref TEXT NOT NULL DEFAULT 'refs/heads/main',
  last_successful_run_ref TEXT,
  last_backup_commit TEXT,
  last_backup_at TIMESTAMPTZ,

  status TEXT NOT NULL DEFAULT 'active',
  metadata JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

#### `agent_approvals`

Purpose: product-visible human-in-the-loop approvals.

```sql
CREATE TABLE agent_approvals (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  generation_job_id UUID NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
  agent_runtime_run_id UUID REFERENCES agent_runtime_runs(id) ON DELETE CASCADE,
  app_project_id UUID NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

  deepagents_interrupt_id TEXT,
  approval_type TEXT NOT NULL, -- file_write | deploy | db_migration | mcp_tool | billing | destructive_action
  status TEXT NOT NULL DEFAULT 'pending', -- pending | approved | rejected | edited | expired

  title TEXT NOT NULL,
  description TEXT,
  risk_level TEXT NOT NULL DEFAULT 'medium',
  payload JSONB NOT NULL DEFAULT '{}',
  proposed_action JSONB NOT NULL DEFAULT '{}',
  reviewer_response JSONB NOT NULL DEFAULT '{}',

  requested_by_agent TEXT,
  reviewed_by_user_id UUID REFERENCES users(id),
  reviewed_at TIMESTAMPTZ,

  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_approvals_job ON agent_approvals(generation_job_id);
CREATE INDEX idx_agent_approvals_pending_org ON agent_approvals(organization_id, status);
```

#### `agent_workspace_artifacts`

Purpose: store build logs, screenshots, previews, generated reports, diff summaries.

```sql
CREATE TABLE agent_workspace_artifacts (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  generation_job_id UUID REFERENCES generation_jobs(id) ON DELETE SET NULL,
  agent_runtime_run_id UUID REFERENCES agent_runtime_runs(id) ON DELETE SET NULL,
  app_project_id UUID NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

  artifact_type TEXT NOT NULL, -- build_log | screenshot | diff | preview | report | generated_asset
  name TEXT NOT NULL,
  content_type TEXT,
  storage_key TEXT,
  public_url TEXT,
  metadata JSONB NOT NULL DEFAULT '{}',

  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

#### `project_preview_sessions`

Purpose: store Daytona-backed live preview sessions for `*.preview.wemob.app`.

The full schema is defined in section 7.10 because it is tightly coupled to the Daytona preview gateway design.

### 4.2 Existing table adjustments

#### `app_projects`

Add:

```sql
ALTER TABLE app_projects
ADD COLUMN IF NOT EXISTS current_commit TEXT,
ADD COLUMN IF NOT EXISTS agent_thread_id UUID REFERENCES agent_threads(id),
ADD COLUMN IF NOT EXISTS agent_runtime TEXT NOT NULL DEFAULT 'deepagents',
ADD COLUMN IF NOT EXISTS repo_storage_mode TEXT NOT NULL DEFAULT 'bare_worktree';
```

#### `generation_jobs`

Add:

```sql
ALTER TABLE generation_jobs
ADD COLUMN IF NOT EXISTS agent_runtime_run_id UUID REFERENCES agent_runtime_runs(id),
ADD COLUMN IF NOT EXISTS deepagents_thread_id TEXT,
ADD COLUMN IF NOT EXISTS deepagents_run_id TEXT;
```

#### `app_agent_runs`

Do not extend this table for the new runtime.

`app_agent_runs` belongs to the old Go-owned agent runtime. During implementation it may remain only until old code paths are deleted. The final system should either drop it or leave it as archived legacy data with no production writes.

Final target:

```text
no new app_agent_runs writes
no Deep Agents compatibility columns on app_agent_runs
agent_runtime_runs is the runtime/product run metadata table
```

---

## 5. Git workspace design

### 5.1 Storage paths

Config:

```env
WEMOB_GIT_BARE_ROOT=/data/git/bare
WEMOB_WORKTREE_ROOT=/data/worktrees
WEMOB_LEGACY_REPO_ROOT=/data/repos
```

Canonical repo path:

```text
/data/git/bare/{project_id}.git
```

Per-run worktree path:

```text
/data/worktrees/{project_id}/{job_id}
```

Optional scratch path:

```text
/data/worktrees/{project_id}/{job_id}/.wemob-runtime
```


### 5.1.1 Git refs and source-of-truth model

Canonical project state is the bare repo plus DB pointer:

```text
/data/git/bare/{project_id}.git
app_projects.current_commit
project_git_repositories.current_commit
```

Use these refs:

```text
refs/heads/main
  latest successful/live project state

refs/wemob/runs/{job_id}
  durable result of every meaningful agent run

refs/wemob/checkpoints/{job_id}/{index}
  optional mid-run checkpoint commits for long or risky runs
```

Rules:

```text
- `main` moves only when a run is successful and finalized to project.
- `refs/wemob/runs/{job_id}` is created for success, failure, cancellation, and budget-exceeded runs when there are meaningful workspace changes.
- Failed run refs never become the live project source unless the user explicitly continues/recoveries from them and a later run succeeds.
- The DB field `current_commit` always points to the latest successful/live commit, not failed run commits.
- The DB field `checkpoint_commit` or `run_commit` points to saved partial work from failed/canceled runs.
```

This separates durability from publication:

```text
durability:
  every meaningful run is saved

publication:
  only successful finalized runs update the live project commit
```

### 5.2 Migration from legacy repos

Current legacy layout:

```text
/data/repos/{project_id}
```

Migration command for each project:

```bash
mkdir -p /data/git/bare
git clone --bare /data/repos/{project_id} /data/git/bare/{project_id}.git
```

If legacy repo has uncommitted changes:

1. Check `git status --porcelain`.
2. If dirty, create migration commit:
   ```bash
   git add -A
   git commit -m "migration: persist legacy working tree before bare repo conversion"
   ```
3. Then clone bare.

Record:

```text
project_git_repositories.legacy_repo_path = /data/repos/{project_id}
project_git_repositories.bare_repo_path = /data/git/bare/{project_id}.git
project_git_repositories.current_commit = HEAD
```

### 5.3 New project initialization

When a new project is created:

1. Create bare repo:
   ```bash
   git init --bare /data/git/bare/{project_id}.git
   ```
2. Create temp init worktree:
   ```bash
   git clone /data/git/bare/{project_id}.git /tmp/wemob-init-{project_id}
   cd /tmp/wemob-init-{project_id}
   git checkout -b main
   ```
3. Add `.gitignore`:
   ```text
   node_modules/
   .next/
   .env*
   dist/
   build/
   .expo/
   ios/
   android/
   web-build/
   ```
4. Add `_wemob/.keep`.
5. Commit:
   ```bash
   git add -A
   git commit -m "chore: initialize Wemob project repository"
   git push origin main
   ```
6. Store HEAD as `current_commit`.

### 5.4 Worktree creation per agent run

Pseudo-code:

```python
async def create_run_worktree(project_id: str, job_id: str, base_commit: str) -> WorktreeInfo:
    bare = f"{GIT_BARE_ROOT}/{project_id}.git"
    path = f"{WORKTREE_ROOT}/{project_id}/{job_id}"

    ensure_clean_absent(path)

    run_branch = f"agent/{job_id}"

    git(bare, "worktree", "add", "--detach", path, base_commit)
    git(path, "checkout", "-B", run_branch)

    return WorktreeInfo(path=path, branch=run_branch, base_commit=base_commit)
```

Important: use detached or run-specific branch. Do not run agent directly on `main`.

### 5.5 Runtime finalization and run commit algorithm

`finalize_project_changes` is a Wemob custom runtime finalizer. It should not be treated as an optional model-selected tool.

The agent may call:

```text
mark_ready_for_finalize
```

but the runtime must still execute the finalization gate after the Deep Agents supervisor returns.

#### 5.5.1 Finalization graph

```text
Deep Agents supervisor/subagents
  ↓
sync Daytona /workspace → host worktree
  ↓
inspect_workspace_dirty
  ↓
if clean:
  complete_noop
else:
  create_run_commit
  ↓
  if run_success_policy_allows_publication:
    validate/rebase/finalize_to_main
  else:
    save_checkpoint_only
```

#### 5.5.2 Inputs to runtime finalizer

The runtime finalizer receives structured state, not just model text:

```json
{
  "job_id": "uuid",
  "project_id": "uuid",
  "agent_runtime_run_id": "uuid",
  "base_commit": "abc123",
  "run_status": "completed | failed | canceled | budget_exceeded",
  "agent_summary": "Implemented redesigned dashboard analytics page",
  "validation": {
    "build_passed": true,
    "tests_passed": true,
    "manual_notes": "..."
  },
  "requested_publication": true
}
```

#### 5.5.3 Algorithm

```text
1. Assert runtime context has project_id/job_id/user_id/org_id.
2. Sync Daytona /workspace back to /data/worktrees/{project_id}/{job_id}.
3. Verify worktree path belongs to /data/worktrees/{project_id}/{job_id}.
4. Generate diff summary:
   git status --porcelain
   git diff --stat
   git diff --name-status
5. If no meaningful changes:
   - mark run completed/no-op or failed/no-op depending terminal status
   - do not create commit
   - do not update main/current_commit
   - schedule cleanup
   - return
6. Create a run commit in the run worktree:
   git add -A
   git commit -m "agent run: {job_id} - {short summary}"
7. Update or create:
   refs/wemob/runs/{job_id} → run_commit
8. Store:
   agent_runtime_runs.run_commit = run_commit
   agent_runtime_runs.run_ref = refs/wemob/runs/{job_id}
9. If terminal status is not successful:
   - set checkpoint_commit = run_commit
   - finalized_to_project = false
   - do not update app_projects.current_commit
   - do not move refs/heads/main
   - emit run_checkpoint_saved
   - return
10. If terminal status is successful:
   - run required validation if not already passed
   - acquire distributed project finalization lock
   - read project current_commit from DB
   - compare current_commit with base_commit
11. If current_commit == base_commit:
   - fast-forward/update refs/heads/main to run_commit
12. If current_commit != base_commit:
   - attempt rebase/merge run_commit changes onto current_commit
   - if clean, create new final_commit
   - if conflict, emit workspace_conflict and either route back to agent or fail gracefully
13. Update:
   project_git_repositories.current_commit = final_commit
   project_git_repositories.last_successful_run_ref = refs/wemob/runs/{job_id}
   app_projects.current_commit = final_commit
   agent_runtime_runs.final_commit = final_commit
   agent_runtime_runs.finalized_to_project = true
14. Create assistant chat summary.
15. Emit workspace_finalized.
16. Trigger repository backup hook.
17. Release lock.
18. Schedule worktree cleanup.
```

#### 5.5.4 Commit message policy

Run commit:

```text
agent run: {job_id} - {mode} - {summary}
```

Successful final commit, if separate from run commit after rebase:

```text
feat: {user-facing summary}
```

Failed checkpoint commit:

```text
agent checkpoint: failed run {job_id} - {reason}
```

#### 5.5.5 Continue after failed run

When user says “continue” after a failed/canceled/budget-exceeded run:

```text
if previous_run.checkpoint_commit exists:
  create new worktree from checkpoint_commit
else:
  create new worktree from app_projects.current_commit
```

If the continuation succeeds, only the new successful continuation run updates `main/current_commit`.


### 5.6 Distributed lock

Do not use only Go `sync.Mutex`. Use Postgres advisory lock or Redis lock.

Recommended: Postgres advisory lock for finalization.

```sql
SELECT pg_advisory_lock(hashtext('project-finalize:' || $1));
SELECT pg_advisory_unlock(hashtext('project-finalize:' || $1));
```

Lock scope:

```text
project_id
```

Use it only around final Git commit + DB current_commit update, not entire agent run.

### 5.7 Conflict handling

If current commit changed while run was executing:

```text
base_commit != project.current_commit
```

Attempt to replay the run changes onto the latest successful project commit. Because every run has a run commit/ref, the runtime can use merge-base, cherry-pick, rebase, or patch application depending on implementation.

Example:

```bash
git fetch /data/git/bare/{project_id}.git main
git rebase {current_commit}
```

On conflict:

1. Capture `git status`, conflicted file list, conflict markers.
2. Emit `workspace_conflict`.
3. Ask Deep Agents supervisor to resolve conflict in `/workspace`.
4. Re-run validation.
5. Retry finalization once.

If conflict remains:

```text
status = failed_conflict
message = "Project changed while the agent was working and automatic conflict resolution failed."
```

### 5.8 Worktree cleanup policy

Worktrees are disposable but should be retained briefly for debugging.

Config:

```env
WEMOB_WORKTREE_TTL_SUCCESS_HOURS=24
WEMOB_WORKTREE_TTL_FAILED_HOURS=72
WEMOB_WORKTREE_MAX_PER_PROJECT=20
```

Cleanup worker:

```text
- remove successful worktrees older than TTL
- keep failed worktrees longer
- prune broken git worktrees: git worktree prune
- delete orphaned directories not referenced by active runs
```

### 5.9 Backup strategy

Current Go has an async commit hook. New production version should:

- push bare repos to object storage snapshots, or
- mirror to private Git remote, or
- create periodic compressed repo bundles.

Recommended:

```bash
git bundle create /tmp/{project_id}-{commit}.bundle --all
upload to s3://wemob-repo-backups/{org_id}/{project_id}/{commit}.bundle
```

Record in `project_git_repositories.last_backup_commit`.

---

## 6. Deep Agents filesystem design

### 6.1 Virtual filesystem

Deep Agents should see:

```text
/workspace/                 project files from worktree
/memories/user/             durable user memory
/memories/project/          durable project memory
/memories/org/              read-only org policy/brand memory
/skills/                    read-only skill packs
/artifacts/                 generated artifacts
/internal/                  agent scratch, StateBackend
```

### 6.2 Backend routing

Initial production routing:

```python
backend = CompositeBackend(
    default=StateBackend(),
    routes={
        "/workspace/": DaytonaWorkspaceBackend(...),
        "/memories/user/": StoreBackend(namespace=user_memory_namespace),
        "/memories/project/": StoreBackend(namespace=project_memory_namespace),
        "/memories/org/": StoreBackend(namespace=org_memory_namespace),
        "/skills/": SkillsBackend(...),
        "/artifacts/": ArtifactsBackend(...),
    },
)
```

Prefer the sandbox backend to own both file operations and execution so the agent sees one environment.

### 6.3 Path policy

Agent must never see host absolute paths. It should only see:

```text
/workspace/...
/memories/...
/skills/...
/artifacts/...
```

Backend maps to physical locations internally.

Reject:

```text
../
/etc
/proc
/root
/data/git
/data/worktrees not matching current project/job
.env
.env.local
*.pem
*.key
```

### 6.4 Built-in filesystem permissions

Recommended first-match rules:

```python
permissions = [
    # Deny secrets
    FilesystemPermission(
        operations=["read", "write"],
        paths=[
            "/workspace/.env",
            "/workspace/.env.*",
            "/workspace/**/*.pem",
            "/workspace/**/*.key",
            "/workspace/**/secrets.*",
        ],
        mode="deny",
    ),

    # Skills are read-only
    FilesystemPermission(
        operations=["write"],
        paths=["/skills/**"],
        mode="deny",
    ),

    # Org memory is read-only
    FilesystemPermission(
        operations=["write"],
        paths=["/memories/org/**"],
        mode="deny",
    ),

    # Allow project workspace
    FilesystemPermission(
        operations=["read", "write"],
        paths=["/workspace/**"],
        mode="allow",
    ),

    # Allow user/project memories
    FilesystemPermission(
        operations=["read", "write"],
        paths=["/memories/user/**", "/memories/project/**"],
        mode="allow",
    ),

    # Allow artifacts write
    FilesystemPermission(
        operations=["read", "write"],
        paths=["/artifacts/**"],
        mode="allow",
    ),

    # Deny everything else by default
    FilesystemPermission(
        operations=["read", "write"],
        paths=["/**"],
        mode="deny",
    ),
]
```

Note: also enforce these in backend policy hooks because permissions do not cover custom tools, MCP tools, or sandbox command execution.

---

## 7. Daytona sandbox and preview design

### 7.1 Daytona role

Daytona is the long-term production sandbox platform for Wemob.

Daytona should provide:

```text
isolated sandbox filesystem
Deep Agents filesystem backend through langchain-daytona
execute tool
package install
build/test/lint commands
preview server
preview URL generation
custom preview proxy support
WebSocket proxy support for hot reload
log streaming and process/session management
artifact retrieval
snapshot-based warm starts
TTL lifecycle
```

Deep Agents should use the **sandbox-as-tool pattern**: the agent runtime runs in the Python `agent-runtime` service, while filesystem and execution calls are sent to Daytona through `DaytonaSandbox`. This keeps model/provider keys and Wemob product credentials outside the sandbox while giving the agent a safe environment for code execution.

### 7.2 Daytona + Deep Agents integration

Install:

```toml
dependencies = [
  "daytona",
  "langchain-daytona",
]
```

Core runtime pattern:

```python
from daytona import CreateSandboxFromSnapshotParams, Daytona
from deepagents import create_deep_agent
from langchain_core.runnables import RunnableConfig
from langchain_daytona import DaytonaSandbox

daytona = Daytona()

async def create_agent(config: RunnableConfig):
    thread_id = config["configurable"]["thread_id"]
    context = config["configurable"].get("wemob_context", {})
    project_id = context["project_id"]
    job_id = context["job_id"]

    labels = {
        "thread_id": thread_id,
        "project_id": project_id,
        "job_id": job_id,
        "runtime": "wemob-deepagents",
    }

    sandbox = await resolve_or_create_daytona_sandbox(labels=labels)

    return create_deep_agent(
        model=resolve_model(context),
        system_prompt=load_supervisor_prompt(),
        backend=DaytonaSandbox(sandbox=sandbox),
        subagents=build_subagents(context),
        tools=build_product_tools(context),
        context_schema=WemobContext,
        permissions=build_permissions(context),
    )
```

Use thread/project labels for lookup, TTL, observability, and cleanup. For Wemob, prefer **project-thread scoped sandboxes**:

```text
one active sandbox per project thread
new run reuses sandbox if still alive and compatible
sandbox reset/reseed when base commit changes unexpectedly
sandbox auto-deletes after idle TTL
```

Run-scoped sandboxes are acceptable for high-risk jobs, but project-thread scoped sandboxes give better package install reuse and preview continuity.

### 7.3 Sandbox lifecycle

Lifecycle:

```text
1. Resolve Deep Agents thread_id.
2. Resolve project current_commit.
3. Create host worktree from bare repo.
4. Resolve or create Daytona sandbox with labels:
   - org_id
   - user_id
   - project_id
   - job_id
   - thread_id
   - base_commit
   - runtime_version
5. Seed /workspace from host worktree.
6. Deep Agents operates in Daytona via DaytonaSandbox.
7. Agent may run install/build/test/preview through execute.
8. On finalize, download/sync /workspace back to host worktree.
9. Commit host worktree into bare repo.
10. Keep sandbox alive for preview TTL.
11. Cleanup after TTL or explicit stop.
```

Recommended TTLs:

```env
DAYTONA_SANDBOX_IDLE_TTL_SECONDS=3600
DAYTONA_PREVIEW_TTL_SECONDS=3600
DAYTONA_FAILED_RUN_TTL_SECONDS=10800
DAYTONA_MAX_SANDBOXES_PER_PROJECT=3
```

### 7.4 Snapshot strategy

Use Daytona snapshots from day zero.

Initial snapshots:

```text
wemob-nextjs-base
  - Node LTS
  - pnpm/npm/yarn
  - common Next.js tooling
  - Playwright optional
  - TypeScript
  - git
  - ripgrep

wemob-expo-base
  - Node LTS
  - Expo CLI
  - React Native/NativeWind basics
  - mobile build prerequisites where practical

wemob-python-tools
  - Python 3.12
  - common utility packages for analysis/tools
```

Snapshot selection:

```text
project_type=app/ecommerce       → wemob-nextjs-base
project_type=mobile_app_native   → wemob-expo-base
```

Long-term optimization:

```text
project dependency snapshot
  - after dependency install stabilizes
  - keyed by lockfile hash
  - reused by later project-thread sandboxes
  - invalidated when package manager lockfile changes
```

### 7.5 File synchronization model

Deep Agents filesystem tools operate inside Daytona.

Host runtime still owns canonical Git finalization.

Flow:

```text
1. Host creates worktree from /data/git/bare/{project_id}.git.
2. Runtime uploads worktree files into Daytona /workspace.
3. Deep Agents reads/writes/runs commands inside Daytona /workspace.
4. Runtime downloads changed source files and artifacts from Daytona.
5. Runtime writes them into host worktree.
6. finalize_project_changes commits host worktree.
```

Do not commit directly inside Daytona as the only source of truth. Daytona is the execution environment; canonical commits happen in controlled runtime infrastructure.

### 7.6 Sync rules

Exclude:

```text
node_modules/
.next/
.expo/
dist/
dist-ios/
dist-android/
build/
web-build/
ios/
android/
.env*
.git/
_wemob/runtime-cache/
coverage/
.playwright/
```

Use hashing to avoid uploading/downloading unchanged large files.

### 7.7 Controlled command helpers

Expose these as product-level tools or verifier utilities:

```text
detect_project_runtime
install_dependencies
run_typecheck
run_lint
run_tests
run_web_build
run_mobile_checks
start_preview_server
stop_preview_server
restart_preview_server
read_preview_logs
capture_preview_screenshot
```

The Deep Agents `execute` tool remains available, but skills should instruct agents to prefer these helpers for common workflows because they enforce timeout, cwd, logging, artifact storage, and output truncation policies.

### 7.8 Preview architecture

Use Daytona previews, but expose them through Wemob-branded preview domains.

Public user-facing preview URL:

```text
https://{preview_slug}.preview.wemob.app
```

Examples:

```text
crm-x7k2.preview.wemob.app
p-8f3a-r-92ks.preview.wemob.app
```

DNS:

```text
*.preview.wemob.app → Wemob Preview Gateway
```

TLS:

```text
wildcard certificate for *.preview.wemob.app
```

Daytona provides two useful preview URL modes:

```text
standard preview URL
  - URL includes sandbox ID
  - token sent with x-daytona-preview-token header
  - best for Wemob preview gateway/proxy

signed preview URL
  - token embedded in URL
  - useful for direct user links or email links
  - expiry should be explicit, recommended around 3600 seconds
```

Wemob should use **standard Daytona preview URLs behind the Wemob Preview Gateway** so raw Daytona URLs and tokens are not exposed to the browser.

### 7.9 Preview gateway

Add a dedicated preview gateway inside Go backend or as a small separate service.

Responsibilities:

```text
receive requests for *.preview.wemob.app
extract preview_slug from Host
lookup project_preview_sessions by preview_slug
authenticate if preview is private
ensure sandbox is running or restart if allowed
forward request to Daytona preview URL
attach x-daytona-preview-token header
set X-Daytona-Skip-Preview-Warning: true
support WebSocket upgrade for Vite/Next hot reload
support SSE/streaming responses
handle custom branded error pages
track access/last activity
```

Gateway must proxy:

```text
HTTP
WebSocket upgrade
static JS chunks
source maps if enabled
HMR endpoints
SSE endpoints
large asset responses
```

### 7.10 Preview session table

Add:

```sql
CREATE TABLE project_preview_sessions (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  preview_slug TEXT NOT NULL UNIQUE,
  app_project_id UUID NOT NULL REFERENCES app_projects(id) ON DELETE CASCADE,
  generation_job_id UUID REFERENCES generation_jobs(id) ON DELETE SET NULL,
  agent_runtime_run_id UUID REFERENCES agent_runtime_runs(id) ON DELETE SET NULL,
  organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

  sandbox_provider TEXT NOT NULL DEFAULT 'daytona',
  sandbox_id TEXT NOT NULL,
  sandbox_status TEXT NOT NULL DEFAULT 'running',

  port INTEGER NOT NULL,
  command TEXT NOT NULL,
  cwd TEXT NOT NULL DEFAULT '/workspace',

  daytona_preview_url TEXT NOT NULL,
  daytona_preview_token TEXT,
  daytona_signed_preview_url TEXT,
  daytona_signed_preview_token TEXT,

  public_preview_url TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'starting', -- starting | ready | stopped | expired | failed

  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ready_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL,
  last_healthcheck_at TIMESTAMPTZ,
  last_accessed_at TIMESTAMPTZ,

  metadata JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_project_preview_sessions_project ON project_preview_sessions(app_project_id);
CREATE INDEX idx_project_preview_sessions_job ON project_preview_sessions(generation_job_id);
CREATE INDEX idx_project_preview_sessions_status ON project_preview_sessions(status);
```

### 7.11 `start_preview_server` tool

Tool input:

```json
{
  "framework": "nextjs",
  "command": "npm run dev -- --hostname 0.0.0.0 --port 3000",
  "port": 3000,
  "cwd": "/workspace",
  "ttl_seconds": 3600
}
```

Tool behavior:

```text
1. Validate cwd is inside /workspace.
2. Detect package manager if not provided.
3. Install dependencies if needed or if lockfile hash changed.
4. Start command as long-running Daytona process/session.
5. Poll localhost:{port} inside sandbox until healthy.
6. Request Daytona standard preview link for port.
7. Create preview_slug.
8. Store project_preview_sessions row.
9. Emit preview.ready with public_preview_url.
10. Return public_preview_url to agent.
```

Preferred framework commands:

```text
Next.js:
  npm run dev -- --hostname 0.0.0.0 --port 3000

Vite:
  npm run dev -- --host 0.0.0.0 --port 5173

Expo web:
  npx expo start --web --host 0.0.0.0 --port 8081
```

### 7.12 Security

Rules:

```text
No Wemob product DB credentials in Daytona.
No raw model provider keys in Daytona.
No unrestricted organization secrets in Daytona.
Project env vars are mounted only through explicit scoped secret injection.
.env files are denied through filesystem permissions and backend checks.
Preview URLs expire.
Preview gateway authenticates private previews.
All Daytona API calls are server-side.
Preview gateway never exposes Daytona preview tokens to frontend.
Long logs are stored as artifacts, not fed into model context.
```

### 7.13 Cost and cleanup

Track:

```text
sandbox creation count
sandbox active seconds
preview active seconds
command execution seconds
snapshot usage
storage usage
egress if available
```

Cleanup:

```text
expire preview sessions
stop idle sandboxes
delete old sandboxes
remove orphan sessions
prune worktrees
archive build logs/artifacts
```

---


---

## 8. Deep Agents context schema

### 8.1 `WemobContext`

```python
from dataclasses import dataclass, field
from typing import Literal, Optional

@dataclass
class WemobContext:
    user_id: str
    org_id: str
    project_id: str
    job_id: str

    mode: Literal["generate", "edit", "fix", "deploy", "research", "maintenance"]
    project_type: Literal["app", "ecommerce", "mobile_app_native"]
    frontend_runtime: str

    thread_id: str
    agent_runtime_run_id: str
    base_commit: Optional[str]

    plan_name: str
    credit_budget: int
    feature_flags: dict = field(default_factory=dict)

    workspace_root: str = "/workspace"
    memory_root: str = "/memories"
    skills_root: str = "/skills"
    artifacts_root: str = "/artifacts"

    user_timezone: str = "Europe/Istanbul"
    locale: str = "en"

    model_policy: dict = field(default_factory=dict)
    permissions_policy: dict = field(default_factory=dict)
```

### 8.2 Runtime context usage

Every custom tool must read from runtime context, not from model-provided IDs.

Bad:

```python
@tool
async def execute_sql(project_id: str, sql: str): ...
```

Good:

```python
@tool
async def execute_project_sql(sql: str, runtime: ToolRuntime): 
    project_id = runtime.context.project_id
```

Never trust model-supplied project IDs, user IDs, org IDs, job IDs, or paths.

---

## 9. Agent hierarchy

### 9.1 Supervisor

Name:

```text
wemob-supervisor
```

Responsibilities:

```text
understand user intent
read project memory when needed
inspect workspace summaries
write high-level TODOs
delegate to specialized subagents
enforce no direct implementation for complex tasks
ask for human approval when needed
coordinate final verification
call finalize_project_changes
return concise user-facing summary
```

Supervisor should not:

```text
read dozens of files directly
perform broad implementation itself
run raw SQL itself unless tiny inspection
ignore verifier
finalize without verification
write to org memory
```

### 9.2 Subagents

#### `product-researcher`

Use for:

```text
market research
competitor analysis
design references
feature analysis
copy/content research
```

Tools:

```text
web search
read/write /memories/project/research.md
read /workspace/DESIGN.md
```

Return:

```text
research brief
key recommendations
citations/links if web used
risks/unknowns
```

#### `design-director`

Use for:

```text
new app visual direction
redesign
DESIGN.md creation/update
design system decisions
component style rules
```

Tools:

```text
read/write /workspace/DESIGN.md
read/write relevant design files
generate_image maybe only for design assets
```

Return:

```text
design direction
token system
layout rules
component rules
forbidden patterns
files changed
```

#### `frontend-builder`

Use for:

```text
Next.js App Router implementation
Tailwind/shadcn UI
web app pages/components
UI states
responsive behavior
```

Tools:

```text
workspace file tools
execute for typecheck/build only if needed
skills: nextjs-app-router, tailwind-v4, design-quality
```

Return:

```text
changed files
implementation summary
remaining concerns
validation commands run
```

#### `mobile-builder`

Use for:

```text
Expo Router native app implementation
NativeWind
mobile UI/navigation
local device storage
```

Tools:

```text
workspace file tools
execute mobile checks
trigger_mobile_build only through release/verifier if needed
```

#### `backend-builder`

Use for:

```text
API routes
server actions
auth integration
AI gateway setup
external services
```

Tools:

```text
workspace file tools
project context
safe env var references
```

#### `database-agent`

Use for:

```text
data modeling
project DB schema
migrations
seed data
query debugging
```

Tools:

```text
provision_project_database
execute_project_sql
read schema
migration preview
approval request for destructive changes
```

Must not access Wemob product DB.

#### `qa-verifier`

Use for:

```text
build/test/lint/typecheck
file consistency
route checks
visual quality checks
security checks
regression checks
```

Tools:

```text
execute
read build logs
capture screenshot
read files as needed
```

Return structured verdict:

```json
{
  "passed": true,
  "checks": [
    {"name": "typecheck", "status": "passed"},
    {"name": "build", "status": "passed"}
  ],
  "blocking_issues": [],
  "warnings": []
}
```

#### `release-agent`

Use for:

```text
web deploy
mobile build
deployment failure debugging
preview URL updates
release notes
```

Tools:

```text
trigger_web_deploy
read_web_deploy_logs
trigger_mobile_build
read_mobile_build_logs
set_preview_url
```

### 9.3 Subagent selection rules

Supervisor prompt should say:

```text
For any task touching more than 2 files, delegate implementation.
For any visual design task, call design-director before frontend-builder.
For any database/schema task, call database-agent.
For any release/deploy task, call release-agent.
For any finished implementation, call qa-verifier before finalization.
For any broad research/design request, use product-researcher/design-director first.
```

---

## 10. Skills system

### 10.1 Skill directory format

Each skill:

```text
skills/{skill-name}/SKILL.md
```

With frontmatter:

```markdown
---
name: nextjs-app-router
description: Use for implementing and debugging Next.js App Router projects in Wemob-generated apps.
---
```

### 10.2 Initial skills

```text
wemob-supervisor
project-workspace-git
nextjs-app-router
tailwind-v4
design-quality
expo-router
nativewind-ui
database-modeling
verification-loop
deployment-debugging
mcp-safety
```

### 10.3 Skill permissions

`/skills/**` read-only.

Skills can include scripts, but scripts must run only in sandbox, never host.

### 10.4 Migration from existing skills

Map current `internal/agent/skills` to new Deep Agents skills.

Process:

1. Inventory all current skills.
2. Remove Go-specific tool names.
3. Replace `write_files/edit_file` references with Deep Agents built-in filesystem tools.
4. Add explicit `/workspace` paths.
5. Add verifier checklist.
6. Add "when to activate" in description.

---

## 11. Product tools

### 11.1 Tool implementation rules

All custom tools must:

```text
read identity from runtime.context
validate org/project ownership through Wemob API
be async
use timeouts
return structured short outputs
store large logs/artifacts outside context
emit observability events
audit sensitive actions
```

### 11.2 Tool list

#### `get_project_context`

Returns:

```json
{
  "project": {
    "id": "...",
    "name": "...",
    "type": "app",
    "frontend_runtime": "...",
    "current_commit": "...",
    "status": "..."
  },
  "features": {...},
  "design_brief_exists": true,
  "database": {"enabled": true, "provider": "neon"},
  "deployments": {...},
  "assets": [...]
}
```

#### `generate_image`

Input:

```json
{
  "prompt": "...",
  "width": 1200,
  "height": 800,
  "style": "..."
}
```

Behavior:

```text
call Wemob image generation endpoint
store result in asset storage
return public asset URL and asset ID
```

#### `provision_project_database`

Behavior:

```text
create/ensure Neon database for project
return schema status, connection reference, not raw product credentials
```

#### `execute_project_sql`

Rules:

```text
only project database
only project schema
timeout
row limit
audit SQL
approval for destructive statements
no Wemob product DB
```

#### `trigger_web_deploy`

Rules:

```text
requires finalized commit or explicit draft preview mode
approval may be required for production deploy
returns deployment ID
```

#### `read_web_deploy_logs`

Return concise latest failure summary. Store full logs as artifact.

#### `trigger_mobile_build`

Rules:

```text
project_type must be mobile_app_native
platforms allowlist
approval for production/TestFlight submissions
```

#### `read_mobile_build_logs`

Same as web logs.

#### `mark_ready_for_finalize`

Optional agent-facing tool.

The agent can call this to provide:

```json
{
  "summary": "...",
  "changed_files": ["..."],
  "validation_notes": "...",
  "ready": true
}
```

This tool does **not** commit by itself. It only records the agent's declared readiness and summary. The deterministic runtime finalizer still decides what to commit and whether to publish to the project current commit.

#### `finalize_project_changes`

Runtime-owned custom finalizer.

Covered in Git section. This should be executed by the deterministic post-agent graph step, not left as a normal optional model tool.


#### `request_human_approval`

Creates `agent_approvals` row and triggers LangGraph interrupt.

#### `record_usage`

Records tokens/credits/cost from LangSmith/LangGraph metadata.

#### MCP tools

Long-term use Deep Agents/LangChain MCP support where possible. Wemob should adapt connection records to MCP tool loading, not wrap every MCP tool manually. However, product authorization and approval remain Wemob-owned.

---

## 12. Streaming design

** IMPORTANT MESSAGE TO LLM: You should talk this section with me  **

### 12.1 Non-negotiable stream rule

Do **not** introduce a Wemob stream protocol.

Do **not** introduce:

```text
deepagents_v1
internal/views/deepagents_bridge.go
Wemob-specific supervisor.message.delta events
Wemob-specific subagent/tool/workspace event names
UIMessagePart compatibility as the long-term stream contract
generation_jobs.stream_state as Deep Agents stream reconstruction
```

The stream contract is Deep Agents / LangGraph's default contract.

For new applications, Deep Agents documentation recommends event streaming. Wemob should consume Deep Agents streaming as-is, including:

```text
stream.messages
stream.subagents
stream.values
tool-call state
interrupts
subgraph/subagent streams
```

If the runtime is served through LangGraph Platform / LangGraph dev server / LangGraph SDK endpoints, Wemob should use those endpoints directly where possible. If Go sits in front of the runtime, it may authenticate and authorize access, but it must forward the stream without semantic transformation.

### 12.2 Backend stream role

Wemob backend may:

```text
authenticate user/org/project access
resolve or create deepagents_thread_id
create a product job/run metadata row
mint a frontend-safe runtime access token
proxy bytes if deployment requires same-origin access
record coarse product status from terminal callbacks/webhooks
```

Wemob backend must not:

```text
rename Deep Agents events
flatten subagents into one assistant stream
convert Deep Agents projections into UIMessagePart
rebuild agent state from generation_jobs.stream_state
persist every stream frame as product truth
make current Wemob SSE events the agent API
```

### 12.3 Frontend stream role

Wemob-app should use the LangChain frontend SDK directly:

```ts
import { useStream } from "@langchain/react";

const stream = useStream<typeof agent>({
  apiUrl: runtimeApiUrl,
  assistantId: "wemob_agent",
  threadId: deepagentsThreadId,
});

const messages = stream.messages;
const subagents = [...stream.subagents.values()];
const todos = stream.values?.todos;
```

The frontend should render the SDK projections directly instead of consuming a Wemob-normalized event stream.

### 12.4 Native frontend target

Update wemob-app to render:

```text
left: conversation
right: live workspace/build/preview
timeline:
  supervisor
  design-director
  frontend-builder
  qa-verifier
  release-agent
```

Native UI should understand subagent handles, not pretend all events are one assistant stream.

### 12.5 Product persistence boundary

Deep Agents / LangGraph checkpoints and stream state are runtime-owned.

Wemob stores product metadata only:

```text
deepagents_thread_id
deepagents_run_id
app_project_id
generation/product job id if still used
status summary
base_commit / final_commit / checkpoint_commit
credits/cost summary
created_at / completed_at
error summary
```

Wemob should not use `app_chat_messages`, `generation_jobs.events`, or `generation_jobs.stream_state` to reconstruct Deep Agents state.


---

## 13. Checkpoints, memory, and state

### 13.1 Checkpoints

Use LangGraph checkpointer as source of truth.

Wemob stores only:

```text
deepagents_thread_id
deepagents_run_id
latest_checkpoint_id if available
status
metadata
```

Do not reconstruct Deep Agents history from `app_chat_messages`.

### 13.2 Memory paths

** IMPORTANT MESSAGE TO LLM: You should talk this section with me  **

These Markdown files are Wemob conventions, not Deep Agents defaults. Deep Agents provides memory and filesystem backends; it does not require this exact taxonomy.

Start with the smallest useful memory surface:

```text
/memories/user.md

/memories/project.md

/memories/org.md
```

Only split into multiple files after there is a clear reason, such as file size, different write permissions, or different retrieval policies.

Optional future split:

```text
/memories/project/summary.md
/memories/project/decisions.md
/memories/project/known-issues.md
/memories/project/deployment-notes.md
/memories/user/preferences.md
/memories/org/brand.md
```

Avoid creating many memory files up front. Too many files make memory stale, fragmented, and harder for agents to maintain.

### 13.3 Memory write policy

Allowed:

```text
user memory: agent can write
project memory: agent can write
org memory: agent cannot write
global/system memory: agent cannot write
```

### 13.4 Memory hygiene

Add memory review tool:

```text
summarize_project_memory
compact_project_memory
remove_stale_memory
```

Memory must not include:

```text
secrets
raw logs
full source files
temporary tool outputs
third-party credentials
personal data beyond needed preferences
```

---

## 14. Billing and usage

### 14.1 Usage source

Preferred:

```text
LangSmith/LangGraph run metadata + provider callbacks
```

Wemob should record:

```text
tokens
cache tokens
model
provider
cost
credits
tool counts
duration
subagent counts
sandbox runtime seconds
Daytona cost
image generation cost
deploy/build cost
```

### 14.2 Credit enforcement

Two layers:

1. Pre-run Wemob budget check.
2. Runtime budget middleware inside Deep Agents.

Budget policies:

```text
max wallclock
max model tokens
max number of tool calls
max subagent calls
max sandbox seconds
max generated image count
max deploy/build triggers
```

### 14.3 Mid-run budget stop

If budget exceeded:

```text
Deep Agents should checkpoint
emit budget_exceeded
write partial summary to project memory
create checkpoint run commit if meaningful changes exist, but do not update project current_commit
user can continue after purchasing/allocating credits
```

---

## 15. Security and authorization

### 15.1 Identity

Every runtime request includes:

```text
user_id
org_id
project_id
job_id
thread_id
```

Wemob validates before starting run.

Runtime tools validate again through Wemob APIs.

### 15.2 Project isolation

All physical paths must include project/job and be checked:

```text
/data/worktrees/{project_id}/{job_id}
```

Reject symlinks escaping root.

Reject path traversal.

### 15.3 Secret handling

Rules:

```text
Never expose Wemob product DB URL to agent.
Never expose global provider keys to sandbox.
Project env vars are mounted only for build/deploy when needed.
No reading .env files through filesystem tools.
Use secret references, not secret values.
```

### 15.4 Approval requirements

Approval required for:

```text
production deploy
mobile app submission
destructive SQL
external MCP side effects
billing/payment actions
deleting large file sets
writing config that changes auth/payment/secrets
connecting third-party services
```

### 15.5 Audit

Audit log should include:

```text
tool name
user_id
org_id
project_id
job_id
subagent name
input preview
approval ID if any
result summary
duration
error
```

---

## 16. Observability

### 16.1 LangSmith

Use LangSmith tracing for:

```text
supervisor runs
subagent runs
tool calls
model latency
token usage
errors
human interrupts
```

Tag every run:

```text
wemob_org_id
wemob_user_id
wemob_project_id
wemob_job_id
agent_runtime_version
project_type
mode
```

### 16.2 PostHog

Keep PostHog for product analytics:

```text
agent_run_started
agent_run_completed
agent_run_failed
agent_subagent_used
agent_approval_requested
agent_approval_approved
agent_finalized_commit
agent_deploy_triggered
```

### 16.3 Metrics

Track:

```text
success rate
build pass rate
validation failure rate
average time to first useful event
average total runtime
tokens per successful run
cost per successful run
subagent usage distribution
conflict rate
approval rate
retry/resume rate
Daytona sandbox failures
worktree cleanup failures
```

### 16.4 Logs

Use structured logs in Go and Python.

Every log line includes:

```text
job_id
project_id
org_id
thread_id
run_id
subagent
```

---

## 17. API design

### 17.1 Runtime API principle

Prefer the standard LangGraph / Deep Agents runtime API and frontend SDK contract.

Wemob should not design a parallel agent API unless required for product authorization. If a custom Go endpoint exists, it should be a thin product shell around Deep Agents runtime operations, not a second agent protocol.

Frontend access should be:

```text
wemob-app
  -> authenticated runtime URL/token from Wemob
  -> LangChain frontend SDK useStream(...)
  -> Deep Agents / LangGraph runtime stream unchanged
```

Go may sit in the path for auth/same-origin/proxy reasons, but it must preserve the Deep Agents stream payload.

### 17.2 Go to Python runtime API

Options:

1. LangGraph SDK HTTP API.
2. Custom internal FastAPI wrapper around LangGraph app.
3. Direct queue worker in Python.

Recommended: use LangGraph SDK / LangGraph Platform-compatible HTTP semantics first. Add a small internal FastAPI wrapper only when Wemob needs product-specific preflight, signed runtime access, or server-side callbacks.

Endpoints:

```http
POST /internal/agent-runs
GET  /internal/agent-runs/{run_id}
POST /internal/agent-runs/{run_id}/cancel
POST /internal/agent-runs/{run_id}/resume
```

Do not add `GET /internal/agent-runs/{run_id}/stream` if the frontend can connect directly to the LangGraph / Deep Agents stream endpoint through the SDK. If a stream proxy is required, it must be byte/protocol preserving.

### 17.3 Start request

```json
{
  "thread_id": "uuid-or-langgraph-thread",
  "job_id": "uuid",
  "project_id": "uuid",
  "org_id": "uuid",
  "user_id": "uuid",
  "message": "user prompt",
  "mode": "edit",
  "project_type": "app",
  "frontend_runtime": "nextjs",
  "attachment_ids": [],
  "feature_flags": {},
  "credit_budget": 1000
}
```

### 17.4 Stream response

Use the default Deep Agents / LangGraph stream transport and payload shape.

Rules:

```text
no Wemob event renaming
no UIMessagePart conversion
no generation_jobs.stream_state reconstruction
no old SSE milestone protocol for agent state
```

### 17.5 Auth between Go and Python

Use internal service token:

```env
AGENT_RUNTIME_INTERNAL_TOKEN
```

Each request signed with HMAC:

```text
X-Wemob-Signature
X-Wemob-Timestamp
```

Python validates timestamp and signature.

---

## 18. Go backend changes

### 18.1 New packages

```text
internal/agentruntime/
  client.go
  types.go
  errors.go

internal/controllers/deepagents_run_handler.go
internal/views/runtime_access.go
internal/database/agent_threads.go
internal/database/agent_runtime_runs.go
internal/database/project_git_repositories.go
```

Do not create `internal/views/deepagents_bridge.go`. That would reintroduce a Wemob-owned stream dialect.

### 18.2 Worker replacement

Current:

```go
runErr := agent.Run(runCtx, deps, project, user, p.Message, turnAttachments, emitter)
```

New:

```go
runtimeRun, err := c.DeepAgentsClient.StartRun(ctx, agentruntime.StartRunRequest{...})
```

For the frontend stream, return enough runtime access information for wemob-app to call `useStream` against the Deep Agents / LangGraph runtime:

```json
{
  "thread_id": "...",
  "assistant_id": "wemob_agent",
  "runtime_api_url": "...",
  "runtime_access_token": "..."
}
```

If Go must proxy the stream, proxy the native stream unchanged.

### 18.3 Job finalization

Go no longer decides agent success by `agent.Run` return only.

Runtime completion should be recorded through one of:

```text
LangGraph / Deep Agents run status API
runtime callback/webhook
runtime-owned finalizer result row
```

Then Go updates product metadata only. It does not parse stream frames to infer canonical agent state.

### 18.4 Cancellation

DELETE `/v1/jobs/{id}` should call:

```text
POST /internal/agent-runs/{run_id}/cancel
```

Deep Agents should receive cancellation and checkpoint/stop cleanly.

### 18.5 Approval response

When user approves/rejects:

```text
POST /v1/agent-approvals/{id}/respond
```

Go updates DB and calls Deep Agents resume endpoint with approval payload.

### 18.6 Deletion targets

Because Deep Agents is the one true agent system, these Go pieces are deletion targets after cutover:

```text
internal/agent/**
internal/views/agent_bridge.go
internal/views/agent_run_stream.go
internal/controllers/agent_run_handler.go
internal/controllers/agent_deps.go
internal/jobstate stream reconstruction for agent runs
internal/responses.UIMessagePart for agent runs
generation_jobs.stream_state for agent runs
legacy app_agent_runs as runtime source of truth
```

Some product job rows may remain, but they should point to Deep Agents thread/run IDs rather than contain agent state.

---

## 19. Frontend changes

** IMPORTANT MESSAGE TO LLM: You should talk this section with me  **

### 19.1 Principle

Use Deep Agents frontend directly.

Do not map Deep Agents events to the current Wemob `UIMessagePart` stream. The frontend should adapt to the LangChain frontend SDK, not the other way around.

Canonical integration:

```ts
import { useStream } from "@langchain/react";

const stream = useStream<typeof agent>({
  apiUrl: runtimeApiUrl,
  assistantId: "wemob_agent",
  threadId: deepagentsThreadId,
});
```

Render:

```text
stream.messages
stream.subagents
stream.values
tool-call state
interrupts / approval state
```

### 19.2 First implementation

** IMPORTANT MESSAGE TO LLM: You should talk this section with me  **

Build the native Deep Agents timeline immediately. Do not spend a migration phase preserving the old chat stream UI.

```text
Agent Timeline
  Supervisor
    message stream
    todo list
  Product Researcher
    research summary
  Design Director
    design decisions
  Frontend Builder
    file changes
  QA Verifier
    checks
  Release Agent
    deploy status
```

The first frontend can be minimal, but it must be native:

```text
conversation from stream.messages
subagent list/cards from stream.subagents
todo/progress state from stream.values
tool cards from SDK tool-call state
approval UI from interrupts
workspace/preview panels from sandbox/runtime values
```

### 19.3 UI components

```text
AgentRunTimeline
SubagentCard
ToolCallCard
WorkspaceDiffPanel
ApprovalCard
VerificationResultCard
MemoryUpdateCard
ArtifactCard
```

### 19.4 Approval UX

Approval card shows:

```text
action
risk level
file/sql/deploy diff
agent reason
approve/reject/edit buttons
```

---

## 20. Deployment architecture

### 20.1 Services

```text
wemob-api          Go API
wemob-worker       Go worker for product jobs and stream proxy
agent-runtime      Python Deep Agents runtime
postgres           product DB + optionally LangGraph checkpointer/store
redis              queues/pubsub/cache
daytona             external sandbox provider
object-storage     assets/artifacts/repo backups
```

### 20.2 Volumes

```text
/data/git/bare      mounted to agent-runtime
/data/worktrees     mounted to agent-runtime
```

Go does not need write access long-term, except migration/admin tools.

### 20.3 Environment variables

```env
AGENT_RUNTIME_URL=
AGENT_RUNTIME_INTERNAL_TOKEN=
WEMOB_API_INTERNAL_URL=
WEMOB_API_INTERNAL_TOKEN=

WEMOB_GIT_BARE_ROOT=/data/git/bare
WEMOB_WORKTREE_ROOT=/data/worktrees

LANGSMITH_API_KEY=
LANGSMITH_PROJECT=wemob-agent-runtime
LANGCHAIN_TRACING_V2=true

Daytona_API_KEY=
Daytona_TEMPLATE_ID=
Daytona_SANDBOX_TTL_SECONDS=7200

OPENAI_API_KEY=
ANTHROPIC_API_KEY=
GOOGLE_API_KEY=
```

### 20.4 Runtime scaling

Run Python runtime horizontally.

Requirements:

```text
shared Postgres checkpointer/store
shared Git volume or network filesystem
distributed finalization lock
Daytona external sandbox
idempotent run start
```

Long-term, move Git bare repos to network storage or Git server if container local volume becomes limiting.

---

## 21. Testing strategy

### 21.1 Unit tests

Python:

```text
context validation
path policy
workspace backend path mapping
Git worktree manager
finalize conflict handling
tool input validation
memory namespace generation
Daytona sync exclude/include rules
```

Go:

```text
DeepAgentsClient request signing
DB run/thread methods
runtime access token issuance
job finalization from runtime status/callbacks
approval response handling
feature flag routing
```

Frontend:

```text
useStream connection setup
stream.messages rendering
stream.subagents rendering
stream.values rendering
tool-call state rendering
interrupt / approval rendering
```

### 21.2 Integration tests

```text
create project repo
start Deep Agents run
agent writes file
sync to Daytona
execute build command
start preview server
open preview through preview_slug.preview.wemob.app
finalize commit
DB current_commit updated
Deep Agents stream visible through useStream
worktree cleanup scheduled
```

### 21.3 Failure tests

```text
Python runtime crash mid-run
Daytona unavailable
Daytona preview URL failure
Preview gateway WebSocket failure
Git conflict during finalize
approval timeout
budget exceeded
sandbox command timeout
invalid path traversal
attempt to read .env
attempt to write /skills
destructive SQL without approval
```

### 21.4 Golden project tests

Maintain fixtures:

```text
basic Next.js app
ecommerce app
mobile Expo app
app with database
app with failing build
app with existing DESIGN.md
app requiring conflict resolution
```

Each fixture has expected:

```text
build pass
diff shape
minimum UI quality rubric
no forbidden files changed
```

---

## 22. Rollout plan

### Stage A — Internal shadow

Deep Agents writes to worktree and streams through the native LangChain frontend SDK. It does not finalize. Compare output manually.

### Stage B — Internal finalize

Internal projects only. Deep Agents can finalize commits. Wemob-app uses native Deep Agents UI.

### Stage C — Default for all new projects

All new projects use Deep Agents. The old Go agent path is no longer used for new work.

### Stage D — Remove Go runtime

Delete old `internal/agent` runtime and old stream compatibility code.

---

## 23. PR-by-PR implementation plan

### PR 1 — Architecture and skeleton

Deliver:

```text
docs/architecture/deepagents-production-runtime.md
agent-runtime skeleton
langgraph.json
pyproject.toml
Dockerfile
README
```

Acceptance:

```text
agent-runtime imports
langgraph dev starts
no product behavior yet
```

### PR 2 — DB schema and feature flags

Deliver migrations for:

```text
agent_threads
agent_runtime_runs
project_git_repositories
agent_approvals
agent_workspace_artifacts
app_projects additions
generation_jobs additions
```

Add remote config:

```text
agent_runtime_default
deepagents_shadow_mode
```

Acceptance:

```text
migrations pass
models/database methods compile
new project/runtime records point to Deep Agents thread/run IDs
```

### PR 3 — Git bare repo manager

Deliver Python:

```text
GitManager
BareRepo
WorktreeManager
finalization lock abstraction
legacy repo migration command
```

Deliver Go admin endpoint/CLI if needed:

```text
migrate-project-repo-to-bare
```

Acceptance:

```text
legacy /data/repos project migrates to /data/git/bare
worktree created from current commit
worktree deleted safely
```

### PR 4 — Deep Agents base agent

Deliver:

```text
WemobContext
create_deep_agent graph factory
CompositeBackend with StateBackend + workspace backend placeholder + StoreBackend memory
supervisor prompt
basic subagent specs
```

Acceptance:

```text
agent can write /workspace
agent can call task subagent
agent can update /memories/project
permissions deny /skills writes
```

### PR 5 — Daytona workspace, execution, and preview backend

Deliver:

```text
Daytona sandbox lifecycle
DaytonaSandbox integration via langchain-daytona
thread/project/job labels
TTL and cleanup policy
worktree-to-Daytona sync
Daytona-to-worktree sync
execute support
artifact retrieval
start_preview_server tool
project_preview_sessions table/methods
preview_slug generation
*.preview.wemob.app gateway
WebSocket proxy support
```

Acceptance:

```text
agent can run npm command inside Daytona
sandbox cannot access host files
files written in Daytona sync back to worktree
Next.js/Vite dev server can start in Daytona
frontend receives https://{preview_slug}.preview.wemob.app
preview gateway proxies HTTP and WebSocket traffic
preview expires and cleans up correctly
```

### PR 6 — Go DeepAgentsClient

Deliver:

```text
internal/agentruntime/client.go
StartRun
CancelRun
ResumeRun
HMAC request signing
runtime access token issuance
runtime status lookup
```

Acceptance:

```text
Go can start local Python runtime
cancel request reaches runtime
frontend receives runtime API URL, assistant ID, thread ID, and access token
```

### PR 7 — Runtime launch integration

Deliver:

```text
agent thread lookup/create
agent_runtime_run row creation
generation job integration
runtime access endpoint for wemob-app
runtime status/callback finalization
```

Acceptance:

```text
new agent job starts Deep Agents instead of agent.Run
wemob-app connects to the default Deep Agents stream
Go stores product metadata, not stream state
```

### PR 8 — Native Deep Agents frontend v1

Deliver:

```text
@langchain/react useStream integration
stream.messages rendering
stream.subagents rendering
stream.values/todos rendering
tool-call cards from SDK state
interrupt/approval rendering
```

Acceptance:

```text
frontend renders Deep Agents stream without a Wemob event mapper
subagents are visible as first-class runtime handles
approvals use Deep Agents interrupts/resume flow
```

### PR 9 — Deterministic finalizer and run commits

Deliver:

```text
mark_ready_for_finalize optional tool
runtime-owned finalize_project_changes node/function
workspace dirty inspection
success/failure/cancel/budget run commit creation
refs/wemob/runs/{job_id}
optional refs/wemob/checkpoints/{job_id}/{index}
checkpoint_commit persistence
finalized_to_project flag
validation checks
conflict handling
current_commit DB update only on success
repo backup hook
continue-from-checkpoint support
```

Acceptance:

```text
successful run creates run commit and updates main/current_commit
failed run creates checkpoint run commit but does not update main/current_commit
canceled/budget-exceeded run saves meaningful partial work
no-op run creates no commit
continue after failure starts from checkpoint_commit
conflict test handled
current_commit updated atomically only on success
```

### PR 10 — Product context and asset/image tools

Deliver Python tools:

```text
get_project_context
list_project_assets
generate_image
create_artifact
```

Add internal Go API endpoints as needed.

Acceptance:

```text
agent can inspect project metadata
agent can generate image and receive asset URL
large artifacts stored outside context
```

### PR 11 — Database tools

Deliver:

```text
provision_project_database
execute_project_sql
schema inspection
destructive SQL approval gate
audit logs
```

Acceptance:

```text
agent can create project DB tables
cannot access Wemob product DB
destructive SQL pauses for approval
```

### PR 12 — Deploy/mobile tools

Deliver:

```text
trigger_web_deploy
read_web_deploy_logs
trigger_mobile_build
read_mobile_build_logs
set_preview_url
```

Acceptance:

```text
release-agent can deploy after finalized commit
logs stored as artifacts and summarized
```

### PR 13 — Memory and skills migration

Deliver:

```text
skills migrated to agent-runtime
memory bootstrap files
memory write policies
memory compaction tool
```

Acceptance:

```text
skills load
subagents have correct skills
org memory read-only
project memory updates after run
```

### PR 14 — Permissions and approvals

Deliver:

```text
filesystem permissions
backend policy hooks
agent_approvals flow
frontend approval cards
Deep Agents resume after approval
```

Acceptance:

```text
write to /skills denied
write to /memories/org denied
production deploy approval interrupts and resumes
```

### PR 15 — Native Deep Agents timeline UI

Deliver frontend:

```text
AgentRunTimeline
SubagentCard
ToolCallCard
VerificationResultCard
WorkspaceDiffPanel
ApprovalCard
ArtifactCard
```

Acceptance:

```text
Deep Agents subagents render as separate tracks/cards
frontend remains on the default Deep Agents stream contract
```

### PR 16 — Observability and billing

Deliver:

```text
LangSmith trace tags
PostHog events
usage/cost ingestion
credit budget middleware
Daytona cost tracking
dashboards
```

Acceptance:

```text
every run trace linked to Wemob job/project
credits deducted correctly
budget exceeded stops safely
```

### PR 17 — Cleanup and production hardening

Deliver:

```text
worktree cleanup worker
sandbox cleanup worker
repo backup worker
orphan detector
run reaper
dead-letter handling
rate limiting
load tests
```

Acceptance:

```text
no orphan worktrees after TTL
stuck runs become failed/resumable
Daytona cleanup confirmed
```

### PR 18 — Internal rollout

Deliver:

```text
shadow/full mode controls
quality dashboards
run comparison report
```

Acceptance:

```text
internal projects can use Deep Agents end-to-end
build success and cost tracked
```

### PR 19 — Legacy runtime removal

Deliver:

```text
remove old runtime entrypoint
delete internal/agent runtime code
delete old Go-owned tools/prompts/skills
delete agent_bridge/UIMessagePart agent stream compatibility
delete generation_jobs.stream_state usage for agent runs
delete app_agent_runs as runtime source of truth
```

Acceptance:

```text
all agent jobs use Deep Agents
no production path calls internal/agent.Run
wemob-app uses @langchain/react useStream directly
```

### PR 20 — Production readiness

Deliver:

```text
support/admin controls
rollback at service/deployment level, not old Go agent fallback
operator docs
load tests
```

Acceptance:

```text
Deep Agents is the only agent runtime
runtime can be redeployed/rolled back without re-enabling old Wemob agent
```

---

## 24. Definition of done

The migration is complete when:

```text
Deep Agents owns the agent loop.
Deep Agents checkpoints are the real runtime state.
Deep Agents memory is the real long-term memory.
Deep Agents subagents are visible in product UI.
Wemob-app uses the LangChain frontend SDK / `useStream` directly.
Wemob does not modify, rename, flatten, or normalize Deep Agents stream events.
Deep Agents filesystem maps /workspace to Daytona-backed per-run/project-thread workspace seeded from Git worktree.
Daytona is the only production execution and preview environment.
Runtime finalizer creates run commits for success/failure and updates project current commit only on successful finalized runs.
Wemob stores pointers/status/metadata, not agent internals.
Go no longer owns tools/prompts/compaction/subagents.
Go no longer owns agent streaming or UI message normalization.
Old internal/agent runtime is removed or fully inactive.
```

---

## 25. Open decisions to confirm before implementation

These are not blockers for the plan, but they should be decided before PR 3–5:

1. Should the canonical bare repos stay on a shared mounted volume, or should we introduce an internal Git server/Gitea-like service earlier?
2. Should each project have one Deep Agents thread forever, or should there be multiple threads per project, such as `default`, `release`, `research`, and `maintenance`?
3. Should production deploy always require approval, or only when the project is already public/paid/live?
4. Should mobile builds require approval for all platforms, or only TestFlight/Play Store submission?
5. Should `execute_project_sql` allow writes by default on dev project DBs, or require approval for all non-SELECT statements?
6. Should previews be public by default during active generation, or require authenticated Wemob sessions from day one?
7. Should preview slugs be run-scoped (`{project}-{run}`) or stable project-scoped (`{project}` with latest active preview)?
8. Should Daytona sandbox scope be project-thread scoped by default, with run-scoped sandbox only for high-risk operations?
9. Should Wemob use Daytona signed preview URLs anywhere directly, or always hide Daytona URLs behind the Wemob preview gateway?
10. How long should failed run refs/checkpoint commits be retained before pruning or archival?
11. Should wemob-app connect directly to the LangGraph / Deep Agents runtime endpoint with a Wemob-minted access token, or should Go provide a same-origin byte-preserving proxy for browser/network constraints?

---

## 26. Documentation references checked for Daytona update

- Deep Agents frontend docs: use the LangChain frontend SDK (`@langchain/react`, `useStream`) and consume stream projections such as `messages`, `subagents`, `values`, tool-call state, and interrupts directly.
- Deep Agents streaming docs: for new applications, prefer event streaming and keep the runtime stream shape native.
- Deep Agents sandbox docs: Daytona is an available sandbox backend through `langchain-daytona`; sandbox backends provide filesystem tools plus `execute`.
- Deep Agents sandbox lifecycle docs: thread-scoped sandboxes can be resolved using labels and TTL.
- Deep Agents sandbox integration docs: the sandbox-as-tool pattern keeps API keys outside the sandbox and separates agent state from execution.
- Deep Agents sandbox file docs: use file transfer APIs to seed sandbox files and retrieve artifacts.
- Daytona docs: Daytona provides isolated sandboxes with filesystem, network, lifecycle, SDK/API/CLI, snapshots, and process execution.
- Daytona preview docs: any HTTP service on ports 1–65535 can be previewed; standard preview URLs use `x-daytona-preview-token`; signed URLs embed the token and are useful for iframe/share links.
- Daytona custom preview proxy docs: custom proxies support custom domains, authentication, starting stopped sandboxes, branded errors, preview warning control, and WebSocket proxying.
