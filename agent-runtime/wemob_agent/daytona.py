"""Daytona workspace lifecycle helpers for the Wemob Deep Agents runtime.

The concrete Daytona SDK is loaded lazily. File synchronization and preview
metadata are deterministic and testable without live Daytona credentials.
"""

from __future__ import annotations

import fnmatch
import os
import secrets
import shutil
import subprocess
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol

from wemob_agent.config import RuntimeConfig, load_config
from wemob_agent.context import WemobContext


SYNC_EXCLUDES = (
    "node_modules/",
    ".next/",
    ".expo/",
    "dist/",
    "dist-ios/",
    "dist-android/",
    "build/",
    "web-build/",
    "ios/",
    "android/",
    ".env*",
    ".git/",
    "_wemob/runtime-cache/",
    "coverage/",
    ".playwright/",
)


class DaytonaUnavailable(RuntimeError):
    """Raised when Daytona is required but not configured or importable."""


class SandboxProvider(Protocol):
    sandbox_id: str

    def execute(self, command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]: ...

    def preview_url(self, port: int, ttl_seconds: int) -> dict[str, str]: ...


@dataclass(frozen=True)
class PreviewServerRequest:
    framework: str
    command: str
    port: int
    cwd: str = "/workspace"
    ttl_seconds: int | None = None


@dataclass(frozen=True)
class PreviewServerResult:
    preview_slug: str
    public_preview_url: str
    provider_preview_url: str
    provider_preview_token: str
    sandbox_id: str
    port: int
    command: str
    cwd: str


class DaytonaWorkspaceManager:
    def __init__(
        self,
        context: WemobContext,
        sandbox: SandboxProvider | None = None,
        config: RuntimeConfig | None = None,
    ) -> None:
        self.context = context
        self.config = config or load_config()
        self.sandbox = sandbox

    @property
    def workspace_root(self) -> Path:
        return Path(self.context.workspace_root)

    def labels(self) -> dict[str, str]:
        return {
            "org_id": self.context.org_id,
            "user_id": self.context.user_id,
            "project_id": self.context.project_id,
            "job_id": self.context.job_id,
            "thread_id": self.context.thread_id,
            "base_commit": self.context.base_commit or "",
            "runtime_version": self.config.runtime_version,
            "runtime": "wemob-deepagents",
        }

    def seed_workspace_from_worktree(self, host_worktree: str | Path) -> None:
        sync_tree(Path(host_worktree), self.workspace_root)

    def sync_workspace_to_worktree(self, host_worktree: str | Path) -> None:
        sync_tree(self.workspace_root, Path(host_worktree))

    def execute(self, command: str, cwd: str = "/workspace", timeout_seconds: int = 120) -> dict[str, Any]:
        if self.sandbox is not None:
            return self.sandbox.execute(command, cwd, timeout_seconds)
        if os.getenv("WEMOB_ALLOW_LOCAL_AGENT_EXECUTION", "").lower() not in {"1", "true", "yes"}:
            raise DaytonaUnavailable("Daytona sandbox is required for agent execution")
        return _local_execute(command, cwd, timeout_seconds)

    def start_preview_server(self, request: PreviewServerRequest) -> PreviewServerResult:
        ttl = request.ttl_seconds or self.config.daytona_preview_ttl_seconds
        self.execute(request.command, cwd=request.cwd, timeout_seconds=10)
        preview = self._provider_preview(request.port, ttl)
        slug = preview_slug(self.context.project_id, self.context.job_id)
        domain = self.context.preview_domain or self.config.preview_domain
        return PreviewServerResult(
            preview_slug=slug,
            public_preview_url=f"https://{slug}.{domain.lstrip('.')}",
            provider_preview_url=preview.get("url", ""),
            provider_preview_token=preview.get("token", ""),
            sandbox_id=getattr(self.sandbox, "sandbox_id", ""),
            port=request.port,
            command=request.command,
            cwd=request.cwd,
        )

    def _provider_preview(self, port: int, ttl_seconds: int) -> dict[str, str]:
        if self.sandbox is None:
            return {"url": f"http://127.0.0.1:{port}", "token": ""}
        return self.sandbox.preview_url(port, ttl_seconds)


@dataclass(frozen=True)
class DaytonaRuntimeSandbox:
    sandbox: Any
    provider: DaytonaSDKProvider


def create_runtime_sandbox(context: WemobContext, config: RuntimeConfig | None = None) -> DaytonaRuntimeSandbox:
    """Create the per-run Daytona sandbox used by Deep Agents in production."""

    cfg = config or load_config()
    if context.sandbox_id:
        return connect_runtime_sandbox(context.sandbox_id, cfg)
    if not os.getenv("DAYTONA_API_KEY"):
        raise DaytonaUnavailable("DAYTONA_API_KEY is required for production Deep Agents execution")

    try:
        from daytona import Daytona
    except Exception as exc:  # pragma: no cover - depends on optional production package
        raise DaytonaUnavailable("daytona package is not installed") from exc

    client = Daytona()
    labels = DaytonaWorkspaceManager(context, config=cfg).labels()
    sandbox = _create_daytona_sandbox(client, cfg, labels)
    sandbox_id = str(
        getattr(sandbox, "id", "")
        or getattr(sandbox, "sandbox_id", "")
        or getattr(sandbox, "name", "")
        or context.sandbox_id
    )
    provider = DaytonaSDKProvider(sandbox, sandbox_id)
    if context.host_worktree_path:
        provider.upload_tree(Path(context.host_worktree_path), "/workspace")
    return DaytonaRuntimeSandbox(sandbox=sandbox, provider=provider)


def connect_runtime_sandbox(sandbox_id: str, config: RuntimeConfig | None = None) -> DaytonaRuntimeSandbox:
    cfg = config or load_config()
    if not os.getenv("DAYTONA_API_KEY"):
        raise DaytonaUnavailable("DAYTONA_API_KEY is required for production Deep Agents execution")
    try:
        from daytona import Daytona
    except Exception as exc:  # pragma: no cover - depends on optional production package
        raise DaytonaUnavailable("daytona package is not installed") from exc
    client = Daytona()
    last_error: Exception | None = None
    for method_name in ("get", "get_sandbox", "sandbox"):
        method = getattr(client, method_name, None)
        if method is None:
            continue
        try:
            sandbox = method(sandbox_id)
            return DaytonaRuntimeSandbox(
                sandbox=sandbox,
                provider=DaytonaSDKProvider(sandbox, sandbox_id),
            )
        except Exception as exc:  # pragma: no cover - SDK-version dependent
            last_error = exc
    raise DaytonaUnavailable(f"unable to connect to Daytona sandbox {sandbox_id}: {last_error}")


def _create_daytona_sandbox(client: Any, config: RuntimeConfig, labels: dict[str, str]) -> Any:
    create_methods = ("create", "create_sandbox", "start")
    params_candidates: list[Any] = []
    if config.daytona_snapshot_id:
        params_candidates.extend(
            _construct_daytona_params(
                "CreateSandboxFromSnapshotParams",
                snapshot=config.daytona_snapshot_id,
                snapshot_id=config.daytona_snapshot_id,
                labels=labels,
                auto_stop_interval=config.daytona_idle_ttl_seconds,
            )
        )
    if config.daytona_template_id:
        params_candidates.extend(
            _construct_daytona_params(
                "CreateSandboxParams",
                template=config.daytona_template_id,
                template_id=config.daytona_template_id,
                labels=labels,
                auto_stop_interval=config.daytona_idle_ttl_seconds,
            )
        )
    params_candidates.append({"labels": labels})
    params_candidates.append(None)

    last_error: Exception | None = None
    for method_name in create_methods:
        method = getattr(client, method_name, None)
        if method is None:
            continue
        for params in params_candidates:
            try:
                if params is None:
                    return method()
                if isinstance(params, dict):
                    try:
                        return method(**params)
                    except TypeError:
                        return method(params)
                return method(params)
            except Exception as exc:  # pragma: no cover - SDK-version dependent
                last_error = exc
    raise DaytonaUnavailable(f"unable to create Daytona sandbox: {last_error}")


def _construct_daytona_params(class_name: str, **values: Any) -> list[Any]:
    try:
        import daytona
    except Exception:
        return []
    cls = getattr(daytona, class_name, None)
    if cls is None:
        return []
    candidates = []
    for key_name in ("snapshot", "snapshot_id", "template", "template_id"):
        value = values.get(key_name)
        if not value:
            continue
        payload = {"labels": values["labels"], key_name: value}
        if values.get("auto_stop_interval"):
            payload["auto_stop_interval"] = values["auto_stop_interval"]
        try:
            candidates.append(cls(**payload))
        except TypeError:
            continue
    return candidates


class DaytonaSDKProvider:
    """Thin adapter for the Daytona SDK object.

    The SDK method names have changed across releases. Keep the probing here,
    not spread through tools or graph construction.
    """

    def __init__(self, sandbox: Any, sandbox_id: str) -> None:
        self.sandbox = sandbox
        self.sandbox_id = sandbox_id

    def execute(self, command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]:
        candidates = (
            ("process", "exec"),
            ("commands", "run"),
            (None, "execute"),
            (None, "exec"),
        )
        for owner_name, method_name in candidates:
            owner = getattr(self.sandbox, owner_name) if owner_name else self.sandbox
            method = getattr(owner, method_name, None)
            if method is None:
                continue
            result = method(command, cwd=cwd, timeout=timeout_seconds)
            return _result_to_dict(result)
        raise DaytonaUnavailable("configured Daytona sandbox does not expose an execute method")

    def preview_url(self, port: int, ttl_seconds: int) -> dict[str, str]:
        candidates = (
            ("previews", "create"),
            ("preview", "url"),
            (None, "get_preview_url"),
        )
        for owner_name, method_name in candidates:
            owner = getattr(self.sandbox, owner_name) if owner_name else self.sandbox
            method = getattr(owner, method_name, None)
            if method is None:
                continue
            result = method(port=port, ttl=ttl_seconds)
            data = _result_to_dict(result)
            return {
                "url": str(data.get("url") or data.get("preview_url") or ""),
                "token": str(data.get("token") or data.get("preview_token") or ""),
            }
        raise DaytonaUnavailable("configured Daytona sandbox does not expose preview URL creation")

    def upload_tree(self, source: Path, target_root: str = "/workspace") -> None:
        source = source.resolve()
        if not source.exists():
            raise FileNotFoundError(source)
        for path in source.rglob("*"):
            if not path.is_file():
                continue
            rel = path.relative_to(source).as_posix()
            if should_exclude(rel):
                continue
            self._upload_file(path, f"{target_root.rstrip('/')}/{rel}")

    def download_tree(self, source_root: str, target: Path) -> None:
        target.mkdir(parents=True, exist_ok=True)
        files = self._list_files(source_root)
        source_files: set[str] = set()
        for remote_path in files:
            rel = remote_path.removeprefix(source_root.rstrip("/") + "/").strip("/")
            if not rel or should_exclude(rel):
                continue
            source_files.add(rel)
            content = self._read_file(remote_path)
            dest = target / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_bytes(content)
        for path in sorted(target.rglob("*"), reverse=True):
            rel = path.relative_to(target).as_posix()
            if should_exclude(rel):
                continue
            if path.is_file() and rel not in source_files:
                path.unlink()
            elif path.is_dir():
                try:
                    path.rmdir()
                except OSError:
                    pass

    def _upload_file(self, source: Path, remote_path: str) -> None:
        data = source.read_bytes()
        candidates = (
            ("fs", "upload_file"),
            ("fs", "write_file"),
            ("filesystem", "upload_file"),
            ("filesystem", "write_file"),
            (None, "upload_file"),
            (None, "write_file"),
        )
        for owner_name, method_name in candidates:
            owner = getattr(self.sandbox, owner_name) if owner_name else self.sandbox
            method = getattr(owner, method_name, None)
            if method is None:
                continue
            if method_name == "upload_file":
                method(data, remote_path)
                return
            try:
                method(remote_path, data)
                return
            except TypeError:
                method(remote_path, data.decode("utf-8"))
                return
        raise DaytonaUnavailable("configured Daytona sandbox does not expose file upload")

    def _list_files(self, source_root: str) -> list[str]:
        candidates = (
            ("fs", "find_files"),
            ("fs", "list_files"),
            ("filesystem", "find_files"),
            ("filesystem", "list_files"),
            (None, "find_files"),
            (None, "list_files"),
        )
        for owner_name, method_name in candidates:
            owner = getattr(self.sandbox, owner_name) if owner_name else self.sandbox
            method = getattr(owner, method_name, None)
            if method is None:
                continue
            result = method(source_root)
            data = _result_to_dict(result)
            values = data.get("files") or data.get("paths") or result
            if isinstance(values, list):
                out: list[str] = []
                for item in values:
                    if isinstance(item, str):
                        out.append(item)
                    elif isinstance(item, dict) and item.get("path"):
                        out.append(str(item["path"]))
                return out
        raise DaytonaUnavailable("configured Daytona sandbox does not expose file listing")

    def _read_file(self, remote_path: str) -> bytes:
        candidates = (
            ("fs", "read_file"),
            ("filesystem", "read_file"),
            (None, "read_file"),
        )
        for owner_name, method_name in candidates:
            owner = getattr(self.sandbox, owner_name) if owner_name else self.sandbox
            method = getattr(owner, method_name, None)
            if method is None:
                continue
            result = method(remote_path)
            if isinstance(result, bytes):
                return result
            if isinstance(result, str):
                return result.encode("utf-8")
            data = _result_to_dict(result)
            content = data.get("content") or data.get("data") or b""
            if isinstance(content, bytes):
                return content
            return str(content).encode("utf-8")
        raise DaytonaUnavailable("configured Daytona sandbox does not expose file reads")


def sync_tree(source: Path, target: Path) -> None:
    source = source.resolve()
    target = target.resolve()
    if not source.exists():
        raise FileNotFoundError(source)
    target.mkdir(parents=True, exist_ok=True)

    source_files: set[str] = set()
    for path in source.rglob("*"):
        if not path.is_file():
            continue
        rel = path.relative_to(source).as_posix()
        if should_exclude(rel):
            continue
        source_files.add(rel)
        dest = target / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        if not dest.exists() or path.read_bytes() != dest.read_bytes():
            shutil.copy2(path, dest)

    for path in sorted(target.rglob("*"), reverse=True):
        rel = path.relative_to(target).as_posix()
        if should_exclude(rel):
            continue
        if path.is_file() and rel not in source_files:
            path.unlink()
        elif path.is_dir():
            try:
                path.rmdir()
            except OSError:
                pass


def should_exclude(relative_path: str) -> bool:
    rel = relative_path.strip("/")
    parts = rel.split("/")
    for pattern in SYNC_EXCLUDES:
        directory_pattern = pattern.endswith("/")
        normalized = pattern.strip("/")
        if directory_pattern:
            if rel == normalized or rel.startswith(normalized + "/") or normalized in parts:
                return True
            continue
        if fnmatch.fnmatch(rel, normalized) or any(fnmatch.fnmatch(part, normalized) for part in parts):
            return True
    return False


def preview_slug(project_id: str, job_id: str) -> str:
    project = project_id.replace("-", "")[:6].lower() or "project"
    job = job_id.replace("-", "")[:6].lower() or "run"
    return f"p-{project}-r-{job}-{secrets.token_hex(2)}"


def _local_execute(command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]:
    completed = subprocess.run(
        command,
        cwd=cwd,
        shell=True,
        check=False,
        capture_output=True,
        text=True,
        timeout=timeout_seconds,
        env=_safe_local_env(),
    )
    return {
        "exit_code": completed.returncode,
        "stdout": completed.stdout,
        "stderr": completed.stderr,
    }


def _safe_local_env() -> Mapping[str, str]:
    allowed = ("PATH", "HOME", "TMPDIR", "LANG", "LC_ALL")
    return {key: value for key, value in os.environ.items() if key in allowed}


def _result_to_dict(result: Any) -> dict[str, Any]:
    if isinstance(result, dict):
        return result
    if hasattr(result, "model_dump"):
        return dict(result.model_dump())
    if hasattr(result, "__dict__"):
        return dict(result.__dict__)
    return {"result": result}
