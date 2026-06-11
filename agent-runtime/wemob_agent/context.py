"""Typed runtime context passed to Deep Agents runs.

Runtime context is immutable per run and is not the agent's memory. Product
identity must come from this object, not from model-provided tool arguments.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Literal

AgentMode = Literal["generate", "edit", "fix", "deploy", "research", "maintenance"]
ProjectType = Literal["app", "ecommerce", "mobile_app_native"]


@dataclass(frozen=True)
class WemobContext:
    user_id: str
    org_id: str
    project_id: str
    job_id: str
    thread_id: str
    agent_runtime_run_id: str
    mode: AgentMode
    project_type: ProjectType
    frontend_runtime: str
    base_commit: str | None = None
    plan_name: str = "free"
    credit_budget: int = 0
    feature_flags: dict[str, object] = field(default_factory=dict)
    workspace_root: str = "/workspace"
    daytona_workspace_root: str = "workspace"
    host_worktree_path: str = ""
    sandbox_id: str = ""
    memory_root: str = "/memories"
    skills_root: str = "/skills"
    artifacts_root: str = "/artifacts"
    preview_domain: str = "preview.wemob.app"
    user_timezone: str = "UTC"
    locale: str = "en"
    model_policy: dict[str, object] = field(default_factory=dict)
    permissions_policy: dict[str, object] = field(default_factory=dict)
