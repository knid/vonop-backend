from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import os


WORKSPACE_DENY_PREFIXES = (
    "/workspace/.git/",
    "/workspace/.env",
    "/workspace/.env.",
    "/workspace/_wemob/",
    "/workspace/node_modules/",
    "/workspace/.next/",
    "/workspace/dist/",
    "/workspace/build/",
    "/workspace/web-build/",
    "/workspace/ios/",
    "/workspace/android/",
)


@dataclass(frozen=True)
class RepoPolicy:
    workspace_root: str = "/workspace"


def _normalize_path(path: str) -> str:
    if not path:
        return "/"
    if not path.startswith("/"):
        path = "/" + path
    return path


def is_workspace_path(path: str) -> bool:
    normalized = _normalize_path(path)
    return normalized == "/workspace" or normalized.startswith("/workspace/")


def is_denied_workspace_path(path: str) -> bool:
    normalized = _normalize_path(path)
    for prefix in WORKSPACE_DENY_PREFIXES:
        if normalized == prefix[:-1] or normalized.startswith(prefix):
            return True
    return False


def is_allowed_workspace_path(path: str) -> bool:
    if not is_workspace_path(path):
        return False
    return not is_denied_workspace_path(path)


def workspace_relative_path(path: str) -> Path:
    normalized = _normalize_path(path)
    if normalized == "/workspace":
        return Path(".")
    return Path(normalized.removeprefix("/workspace/")).resolve()


def is_skills_path(path: str) -> bool:
    return _normalize_path(path).startswith("/skills/")


def is_read_write_allowed(path: str, policy: RepoPolicy | None = None) -> bool:
    _ = policy
    normalized = _normalize_path(path)
    if is_skills_path(normalized):
        return False
    return is_allowed_workspace_path(normalized)


def resolve_workspace_path(repo_root: str | Path, path: str) -> Path:
    normalized = _normalize_path(path)
    if not is_allowed_workspace_path(normalized):
        raise ValueError(f"path denied: {path}")

    repo_root = Path(repo_root).resolve()
    suffix = normalized.removeprefix("/workspace/")
    candidate = (repo_root / suffix).resolve()
    if not str(candidate).startswith(os.fspath(repo_root) + os.sep) and candidate != repo_root:
        raise ValueError(f"path escapes workspace root: {path}")
    return candidate
