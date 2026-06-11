from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
from typing import Any

from wemob_agent.config import RuntimeConfig
from wemob_agent.context import WemobContext
from wemob_agent.daytona import (
    DaytonaWorkspaceManager,
    PreviewServerRequest,
    should_exclude,
    sync_tree,
)


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

    def execute(self, command: str, cwd: str, timeout_seconds: int) -> dict[str, Any]:
        return {"exit_code": 0, "command": command, "cwd": cwd, "timeout_seconds": timeout_seconds}

    def preview_url(self, port: int, ttl_seconds: int) -> dict[str, str]:
        return {"url": f"https://daytona.example/{port}", "token": f"token-{ttl_seconds}"}


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
            manager = DaytonaWorkspaceManager(context, sandbox=FakeSandbox(), config=config)
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


if __name__ == "__main__":
    unittest.main()
