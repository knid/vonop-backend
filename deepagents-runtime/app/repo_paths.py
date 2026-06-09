from __future__ import annotations

import re
from pathlib import Path

from .config import Settings
from .errors import (
    RepoBaseMissingError,
    RepoMissingError,
    RepoNotGitError,
    RepoSecurityError,
    RepoValidationError,
)

UUID_RE = re.compile(
    r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$"
)


class RepoValidationError(ValueError):
    pass


def resolve_repo_path(project_id: str) -> Path:
    if not UUID_RE.match(project_id):
        raise RepoValidationError("project_id must be a UUID")

    if not Settings.REPO_BASE:
        raise RepoBaseMissingError("repository base not configured")

    base = Path(Settings.REPO_BASE).resolve()

    if not base.is_dir():
        raise RepoBaseMissingError("repository base path is invalid")

    candidate = (base / project_id).resolve()

    try:
        candidate.relative_to(base)
    except ValueError as exc:
        raise RepoValidationError("project_id resolves outside configured repository base") from exc

    if not candidate.exists():
        raise RepoMissingError("project repository path does not exist")

    if not candidate.is_dir():
        raise RepoValidationError("project repository path is not a directory")

    if not (candidate / ".git").is_dir():
        raise RepoNotGitError("project repository is not a git checkout")

    if candidate.is_symlink():
        raise RepoSecurityError("project root path cannot be a symlink")
    if _path_contains_symlink(candidate):
        raise RepoSecurityError("project repository path traverses a symlink")

    return candidate


def _path_contains_symlink(path: Path) -> bool:
    current = path
    while True:
        parent = current.parent
        if parent == current:
            return False
        if parent.is_symlink():
            return True
        current = parent
