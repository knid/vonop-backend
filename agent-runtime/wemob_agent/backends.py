"""Wemob backend helpers exposed to the Deep Agents graph.

These classes are intentionally small and local. Deep Agents owns graph state,
checkpoints, subagents, memory streaming, and the frontend stream contract;
Wemob only provides scoped product IO rooted in the per-run filesystem.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from wemob_agent.context import WemobContext


class BackendPermissionError(PermissionError):
    """Raised when a tool request escapes the run sandbox permissions."""


@dataclass
class StateBackend:
    """Small product state bag for non-agent runtime metadata."""

    values: dict[str, Any] = field(default_factory=dict)

    def get(self, key: str, default: Any = None) -> Any:
        return self.values.get(key, default)

    def set(self, key: str, value: Any) -> None:
        self.values[key] = value


@dataclass(frozen=True)
class WorkspaceBackend:
    workspace_root: Path
    skills_root: Path

    def resolve(self, relative_path: str) -> Path:
        path = Path(relative_path)
        if path.is_absolute():
            raise BackendPermissionError("workspace paths must be relative")
        resolved = (self.workspace_root / path).resolve()
        _assert_under_root(resolved, self.workspace_root.resolve(), "workspace")
        skills = self.skills_root.resolve()
        if resolved == skills or skills in resolved.parents:
            raise BackendPermissionError("writes to /skills are not allowed")
        return resolved

    def read_text(self, relative_path: str) -> str:
        return self.resolve(relative_path).read_text(encoding="utf-8")

    def write_text(self, relative_path: str, content: str) -> str:
        path = self.resolve(relative_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return str(path)

    def list_files(self) -> list[str]:
        root = self.workspace_root.resolve()
        if not root.exists():
            return []
        files: list[str] = []
        for path in root.rglob("*"):
            if path.is_file():
                files.append(path.relative_to(root).as_posix())
        return sorted(files)


@dataclass(frozen=True)
class StoreBackendMemory:
    memory_root: Path

    @property
    def project_root(self) -> Path:
        return self.memory_root / "project"

    def update_project_memory(self, key: str, content: str) -> str:
        relative = Path(key)
        if relative.is_absolute():
            raise BackendPermissionError("memory keys must be relative")
        if relative.suffix == "":
            relative = relative.with_suffix(".md")
        path = (self.project_root / relative).resolve()
        _assert_under_root(path, self.project_root.resolve(), "project memory")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return str(path)

    def read_project_memory(self, key: str) -> str:
        relative = Path(key)
        if relative.is_absolute():
            raise BackendPermissionError("memory keys must be relative")
        if relative.suffix == "":
            relative = relative.with_suffix(".md")
        path = (self.project_root / relative).resolve()
        _assert_under_root(path, self.project_root.resolve(), "project memory")
        return path.read_text(encoding="utf-8")


@dataclass(frozen=True)
class ArtifactBackend:
    artifacts_root: Path

    def create_text_artifact(self, name: str, content: str, content_type: str = "text/plain") -> dict[str, str]:
        relative = Path(name)
        if relative.is_absolute():
            raise BackendPermissionError("artifact names must be relative")
        path = (self.artifacts_root / relative).resolve()
        _assert_under_root(path, self.artifacts_root.resolve(), "artifacts")
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return {
            "name": relative.as_posix(),
            "path": str(path),
            "content_type": content_type,
        }


@dataclass(frozen=True)
class CompositeBackend:
    state: StateBackend
    workspace: WorkspaceBackend
    store: StoreBackendMemory
    artifacts: ArtifactBackend


def build_backend(context: WemobContext | None) -> CompositeBackend:
    if context is None:
        workspace_root = Path("/workspace")
        memory_root = Path("/memories")
        skills_root = Path("/skills")
        artifacts_root = Path("/artifacts")
    else:
        workspace_root = Path(context.workspace_root)
        memory_root = Path(context.memory_root)
        skills_root = Path(context.skills_root)
        artifacts_root = Path(context.artifacts_root)
    return CompositeBackend(
        state=StateBackend(),
        workspace=WorkspaceBackend(workspace_root=workspace_root, skills_root=skills_root),
        store=StoreBackendMemory(memory_root=memory_root),
        artifacts=ArtifactBackend(artifacts_root=artifacts_root),
    )


def _assert_under_root(path: Path, root: Path, label: str) -> None:
    if path != root and root not in path.parents:
        raise BackendPermissionError(f"path escapes {label} root")
