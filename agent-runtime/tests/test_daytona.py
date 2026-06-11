from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from typing import Any

from wemob_agent.config import RuntimeConfig
from wemob_agent.context import WemobContext
from wemob_agent.daytona import (
    DaytonaSDKProvider,
    DaytonaWorkspaceManager,
    PreviewServerRequest,
    _create_daytona_sandbox,
    daytona_workspace_path,
    should_exclude,
    sync_tree,
)
from wemob_agent.daytona_backend import DaytonaWorkspaceBackend


def make_context(root: Path) -> WemobContext:
    return WemobContext(
        user_id="user-1",
        org_id="org-1",
        project_id="abcdef12-0000-0000-0000-000000000000",
        job_id="98765432-0000-0000-0000-000000000000",
        thread_id="thread-1",
        agent_runtime_run_id="run-1",
        mode="generate",
        project_type="app",
        frontend_runtime="vite_react",
        workspace_root=str(root / "workspace"),
    )


class FakeSandbox:
    sandbox_id = "sandbox-1"

    def __init__(self) -> None:
        self.executions: list[tuple[str, str, int]] = []

    def execute(self, command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]:
        self.executions.append((command, cwd, timeout_seconds))
        return {"exit_code": 0, "command": command, "cwd": cwd, "timeout_seconds": timeout_seconds}

    def preview_url(self, port: int, ttl_seconds: int) -> dict[str, str]:
        return {"url": f"https://daytona.example/{port}", "token": f"token-{ttl_seconds}"}


class FakeFilesystem:
    def __init__(self) -> None:
        self.uploads: list[tuple[bytes, str]] = []
        self.files: dict[str, bytes] = {}

    def upload_file(self, src: bytes | str, dst: str, timeout: int = 1800) -> None:
        del timeout
        if isinstance(src, str):
            raise AssertionError("upload_file source should be bytes, not a local path")
        self.uploads.append((src, dst))
        self.files[dst] = src

    def read_file(self, path: str) -> bytes:
        return self.files[path]


class FakePathEntry:
    def __init__(self, path: str) -> None:
        self.path = path


class FakeDaytonaSandbox:
    def __init__(self) -> None:
        self.fs = FakeFilesystem()


class FakeFindFilesFilesystem(FakeFilesystem):
    def __init__(self) -> None:
        super().__init__()
        self.find_calls: list[tuple[str, str]] = []

    def find_files(self, source_root: str, pattern: str) -> list[FakePathEntry]:
        self.find_calls.append((source_root, pattern))
        return [FakePathEntry("workspace/src/App.tsx")]


class FakeWrappedFindFilesFilesystem(FakeFilesystem):
    def __init__(self) -> None:
        super().__init__()
        self.find_calls: list[tuple[str, str]] = []

    def find_files(self, source_root: str, pattern: str | None = None) -> list[FakePathEntry]:
        if pattern is None:
            raise RuntimeError("FileSystem.find_files() missing 1 required positional argument: 'pattern'")
        self.find_calls.append((source_root, pattern))
        return [FakePathEntry("workspace/src/App.tsx")]


class FakeFindFilesSandbox:
    def __init__(self) -> None:
        self.fs = FakeFindFilesFilesystem()
        self.fs.files["workspace/src/App.tsx"] = b"app\n"


class FakeWrappedFindFilesSandbox:
    def __init__(self) -> None:
        self.fs = FakeWrappedFindFilesFilesystem()
        self.fs.files["workspace/src/App.tsx"] = b"app\n"


class FakeCreateClient:
    def __init__(self) -> None:
        self.created_with: list[Any] = []
        self.start_called = False

    def create(self, params: Any = None, **kwargs: Any) -> str:
        del kwargs
        self.created_with.append(params)
        return "sandbox-created"

    def start(self, sandbox: Any) -> None:
        del sandbox
        self.start_called = True


class FakeDaytonaProvider:
    sandbox_id = "sandbox-1"

    def __init__(self) -> None:
        self.executions: list[tuple[str, str, int]] = []
        self.uploads: list[tuple[str, bytes]] = []

    def execute(self, command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]:
        self.executions.append((command, cwd, timeout_seconds))
        return {
            "exit_code": 0,
            "stdout": '{"path": "workspace/src", "is_dir": true}\n',
        }

    def upload_bytes(self, remote_path: str, data: bytes) -> None:
        self.uploads.append((remote_path, data))

    def read_bytes(self, remote_path: str) -> bytes:
        return b"app\n"


class DaytonaTests(unittest.TestCase):
    def test_sync_tree_excludes_build_secret_and_git_paths(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            target = root / "target"
            (source / "src").mkdir(parents=True)
            (source / "src/App.tsx").write_text("app\n", encoding="utf-8")
            (source / ".env.local").write_text("secret\n", encoding="utf-8")
            (source / ".git").mkdir()
            (source / ".git/HEAD").write_text("ref\n", encoding="utf-8")
            (source / "node_modules/pkg").mkdir(parents=True)
            (source / "node_modules/pkg/index.js").write_text("pkg\n", encoding="utf-8")

            sync_tree(source, target)

            self.assertTrue((target / "src/App.tsx").exists())
            self.assertFalse((target / ".env.local").exists())
            self.assertFalse((target / ".git/HEAD").exists())
            self.assertFalse((target / "node_modules/pkg/index.js").exists())

    def test_should_exclude_known_runtime_paths(self) -> None:
        self.assertTrue(should_exclude("apps/web/.next/server.js"))
        self.assertTrue(should_exclude(".env.production"))
        self.assertTrue(should_exclude("_wemob/runtime-cache/log.txt"))
        self.assertFalse(should_exclude("src/App.tsx"))

    def test_daytona_provider_uploads_bytes_to_remote_destination(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp) / "source"
            (source / "src").mkdir(parents=True)
            (source / "src/App.tsx").write_text("app\n", encoding="utf-8")

            sandbox = FakeDaytonaSandbox()
            provider = DaytonaSDKProvider(sandbox, "sandbox-1")
            provider.upload_tree(source, "workspace")

            self.assertEqual(sandbox.fs.uploads, [(b"app\n", "workspace/src/App.tsx")])

    def test_daytona_provider_downloads_with_find_files_pattern_api(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp) / "target"
            sandbox = FakeFindFilesSandbox()
            provider = DaytonaSDKProvider(sandbox, "sandbox-1")

            provider.download_tree("workspace", target)

            self.assertEqual(sandbox.fs.find_calls, [("workspace", "**/*")])
            self.assertEqual((target / "src/App.tsx").read_text(encoding="utf-8"), "app\n")

    def test_daytona_provider_downloads_when_sdk_wraps_find_files_type_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            target = Path(tmp) / "target"
            sandbox = FakeWrappedFindFilesSandbox()
            provider = DaytonaSDKProvider(sandbox, "sandbox-1")

            provider.download_tree("workspace", target)

            self.assertEqual(sandbox.fs.find_calls, [("workspace", "**/*")])
            self.assertEqual((target / "src/App.tsx").read_text(encoding="utf-8"), "app\n")

    def test_create_daytona_sandbox_uses_create_not_start_for_default_config(self) -> None:
        client = FakeCreateClient()
        config = RuntimeConfig(
            default_model="test:model",
            runtime_version="test",
            preview_domain="preview.wemob.app",
            daytona_idle_ttl_seconds=3600,
            daytona_preview_ttl_seconds=600,
        )

        sandbox = _create_daytona_sandbox(client, config, {"project_id": "project-1"})

        self.assertEqual(sandbox, "sandbox-created")
        self.assertEqual(len(client.created_with), 1)
        self.assertFalse(client.start_called)

    def test_daytona_workspace_path_maps_deepagents_path_to_daytona_path(self) -> None:
        self.assertEqual(daytona_workspace_path("/workspace"), "workspace")
        self.assertEqual(daytona_workspace_path("/workspace/src/App.tsx"), "workspace/src/App.tsx")
        self.assertEqual(daytona_workspace_path("/src/App.tsx"), "workspace/src/App.tsx")
        self.assertEqual(
            daytona_workspace_path("/workspace/src/App.tsx", "/home/daytona/workspace"),
            "/home/daytona/workspace/src/App.tsx",
        )
        self.assertEqual(daytona_workspace_path("/tmp/edit-buffer"), "/tmp/edit-buffer")

    def test_daytona_workspace_backend_maps_routed_paths_for_uploads_and_downloads(self) -> None:
        sandbox = FakeDaytonaSandbox()
        provider = DaytonaSDKProvider(sandbox, "sandbox-1")
        backend = DaytonaWorkspaceBackend(provider)

        upload = backend.upload_files([("/src/App.tsx", b"app\n")])
        download = backend.download_files(["/src/App.tsx"])

        self.assertIsNone(upload[0].error)
        self.assertEqual(sandbox.fs.uploads, [(b"app\n", "workspace/src/App.tsx")])
        self.assertEqual(download[0].content, b"app\n")

    def test_daytona_workspace_backend_runs_helpers_from_sandbox_base(self) -> None:
        provider = FakeDaytonaProvider()
        backend = DaytonaWorkspaceBackend(provider)

        result = backend.ls("/")
        execute_result = backend.execute("test -f package.json")

        self.assertEqual(provider.executions[0][1], ".")
        self.assertEqual(provider.executions[1][1], "workspace")
        self.assertEqual(result.entries[0]["path"], "/src")
        self.assertEqual(execute_result.exit_code, 0)

    def test_daytona_workspace_backend_write_does_not_double_prefix_uploads(self) -> None:
        provider = FakeDaytonaProvider()
        backend = DaytonaWorkspaceBackend(provider)

        result = backend.write("/src/New.tsx", "new\n")

        self.assertEqual(provider.executions[0][1], ".")
        self.assertIsNone(result.error)
        self.assertEqual(provider.uploads[0], ("workspace/src/New.tsx", b"new\n"))

    def test_daytona_workspace_backend_preserves_absolute_daytona_root(self) -> None:
        provider = FakeDaytonaProvider()
        backend = DaytonaWorkspaceBackend(provider, "/home/daytona/workspace")

        upload = backend.upload_files([("/src/App.tsx", b"app\n")])
        backend.ls("/")

        self.assertIsNone(upload[0].error)
        self.assertEqual(provider.uploads[0], ("/home/daytona/workspace/src/App.tsx", b"app\n"))
        self.assertEqual(provider.executions[0][1], "/")

    def test_start_preview_server_returns_public_url_without_provider_token(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            context = make_context(Path(tmp))
            config = RuntimeConfig(
                default_model="test:model",
                runtime_version="test",
                preview_domain="preview.wemob.app",
                daytona_idle_ttl_seconds=3600,
                daytona_preview_ttl_seconds=600,
            )
            sandbox = FakeSandbox()
            manager = DaytonaWorkspaceManager(context, sandbox=sandbox, config=config)
            result = manager.start_preview_server(
                PreviewServerRequest(
                    framework="vite",
                    command="npm run dev -- --host 0.0.0.0 --port 5173",
                    port=5173,
                )
            )

            self.assertTrue(result.public_preview_url.endswith(".preview.wemob.app"))
            self.assertEqual(result.provider_preview_url, "https://daytona.example/5173")
            self.assertEqual(result.provider_preview_token, "token-600")
            self.assertNotIn("token-600", result.public_preview_url)
            self.assertEqual(sandbox.executions[0][1], "workspace")


if __name__ == "__main__":
    unittest.main()
