from __future__ import annotations

import os


class Settings:
    """Runtime settings loaded from environment variables."""

    REPO_BASE = os.getenv("WEMOB_REPOS_BASE", "/mnt/wemob-repos")
    INTERNAL_TOKEN = os.getenv("DEEPAGENTS_INTERNAL_TOKEN", "")
    SCHEMA_VERSION = "2026-06-09"
    MAX_CONCURRENT_RUNS = int(os.getenv("MAX_CONCURRENT_RUNS", "6") or 6)
    RUN_DEFAULT_TIMEOUT_SECONDS = int(os.getenv("RUN_DEFAULT_TIMEOUT_SECONDS", "2700") or 2700)

    # Go-side tool bridge (PR 8). GO_TOOL_BASE_URL points at the wemob-backend
    # internal endpoint. The Python service POSTs every Wemob tool call here
    # so Go continues to enforce guardrails and emit MCP approval events.
    GO_TOOL_BASE_URL = os.getenv("GO_TOOL_BASE_URL", "").strip()
    GO_TOOL_TIMEOUT_S = float(os.getenv("GO_TOOL_TIMEOUT_S", "60") or 60)

    # PR 9: LangSmith tracing. When LANGSMITH_TRACING=true and the
    # `langsmith` package is installed, the runtime attaches a LangChain
    # tracer so model calls land in the configured LangSmith project.
    LANGSMITH_TRACING = (os.getenv("LANGSMITH_TRACING", "false").lower() in {"1", "true", "yes"})
    LANGSMITH_PROJECT = os.getenv("LANGSMITH_PROJECT", "wemob-deepagents-prod")
    LANGSMITH_API_KEY = os.getenv("LANGSMITH_API_KEY", "").strip()


def require_internal_token() -> str:
    token = Settings.INTERNAL_TOKEN.strip()
    if not token:
        raise RuntimeError("DEEPAGENTS_INTERNAL_TOKEN is not configured")
    return token
