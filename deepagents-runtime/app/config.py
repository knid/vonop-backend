from __future__ import annotations

import os


class Settings:
    """Runtime settings loaded from environment variables."""

    REPO_BASE = os.getenv("WEMOB_REPOS_BASE", "/mnt/wemob-repos")
    INTERNAL_TOKEN = os.getenv("DEEPAGENTS_INTERNAL_TOKEN", "")
    SCHEMA_VERSION = "2026-06-09"
    MAX_CONCURRENT_RUNS = int(os.getenv("MAX_CONCURRENT_RUNS", "6") or 6)
    RUN_DEFAULT_TIMEOUT_SECONDS = int(os.getenv("RUN_DEFAULT_TIMEOUT_SECONDS", "2700") or 2700)


def require_internal_token() -> str:
    token = Settings.INTERNAL_TOKEN.strip()
    if not token:
        raise RuntimeError("DEEPAGENTS_INTERNAL_TOKEN is not configured")
    return token
