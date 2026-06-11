"""Runtime configuration for the Wemob Deep Agents service."""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class RuntimeConfig:
    default_model: str
    runtime_version: str
    preview_domain: str
    daytona_idle_ttl_seconds: int
    daytona_preview_ttl_seconds: int
    daytona_snapshot_id: str = ""
    daytona_template_id: str = ""
    allow_local_agent_execution: bool = False
    backend_internal_url: str = ""
    internal_token: str = ""
    run_store_path: str = "/tmp/wemob-agent-runtime-runs.json"


def load_config() -> RuntimeConfig:
    return RuntimeConfig(
        default_model=os.getenv("WEMOB_AGENT_MODEL", "anthropic:claude-sonnet-4-6"),
        runtime_version=os.getenv("WEMOB_AGENT_RUNTIME_VERSION", "dev"),
        preview_domain=os.getenv("WEMOB_PREVIEW_DOMAIN", "preview.wemob.app"),
        daytona_idle_ttl_seconds=int(os.getenv("DAYTONA_SANDBOX_IDLE_TTL_SECONDS", "3600")),
        daytona_preview_ttl_seconds=int(os.getenv("DAYTONA_PREVIEW_TTL_SECONDS", "3600")),
        daytona_snapshot_id=os.getenv("DAYTONA_SNAPSHOT_ID", ""),
        daytona_template_id=os.getenv("DAYTONA_TEMPLATE_ID", ""),
        allow_local_agent_execution=os.getenv("WEMOB_ALLOW_LOCAL_AGENT_EXECUTION", "").lower()
        in {"1", "true", "yes"},
        backend_internal_url=os.getenv("WEMOB_BACKEND_INTERNAL_URL", "").rstrip("/"),
        internal_token=os.getenv("AGENT_RUNTIME_INTERNAL_TOKEN", ""),
        run_store_path=os.getenv("WEMOB_AGENT_RUN_STORE_PATH", "/tmp/wemob-agent-runtime-runs.json"),
    )
