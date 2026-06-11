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


def load_config() -> RuntimeConfig:
    return RuntimeConfig(
        default_model=os.getenv("WEMOB_AGENT_MODEL", "anthropic:claude-sonnet-4-6"),
        runtime_version=os.getenv("WEMOB_AGENT_RUNTIME_VERSION", "dev"),
        preview_domain=os.getenv("WEMOB_PREVIEW_DOMAIN", "preview.wemob.app"),
        daytona_idle_ttl_seconds=int(os.getenv("DAYTONA_SANDBOX_IDLE_TTL_SECONDS", "3600")),
        daytona_preview_ttl_seconds=int(os.getenv("DAYTONA_PREVIEW_TTL_SECONDS", "3600")),
    )
