"""Runtime configuration for the Wemob Deep Agents service."""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class RuntimeConfig:
    default_model: str
    runtime_version: str


def load_config() -> RuntimeConfig:
    return RuntimeConfig(
        default_model=os.getenv("WEMOB_AGENT_MODEL", "anthropic:claude-sonnet-4-6"),
        runtime_version=os.getenv("WEMOB_AGENT_RUNTIME_VERSION", "dev"),
    )
