from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from wemob_agent.backends import BackendPermissionError, build_backend
from wemob_agent.context import WemobContext
from wemob_agent.subagents import build_subagents
from wemob_agent.tools import build_product_tools


def make_context(root: Path) -> WemobContext:
    return WemobContext(
        user_id="user-1",
        org_id="org-1",
        project_id="project-1",
        job_id="job-1",
        thread_id="thread-1",
        agent_runtime_run_id="run-1",
        mode="generate",
        project_type="app",
        frontend_runtime="vite_react",
        workspace_root=str(root / "workspace"),
        memory_root=str(root / "memories"),
        skills_root=str(root / "skills"),
        artifacts_root=str(root / "artifacts"),
    )


class BackendTests(unittest.TestCase):
    def test_workspace_write_and_project_memory_update(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            context = make_context(Path(tmp))
            backend = build_backend(context)

            written = backend.workspace.write_text("src/App.tsx", "export default null\n")
            self.assertTrue(written.endswith("workspace/src/App.tsx"))
            self.assertEqual(
                (Path(context.workspace_root) / "src/App.tsx").read_text(encoding="utf-8"),
                "export default null\n",
            )

            memory_path = backend.store.update_project_memory("requirements", "Use Deep Agents.\n")
            self.assertTrue(memory_path.endswith("memories/project/requirements.md"))
            self.assertEqual(
                backend.store.read_project_memory("requirements"),
                "Use Deep Agents.\n",
            )

    def test_workspace_rejects_absolute_skills_write(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            context = make_context(Path(tmp))
            backend = build_backend(context)

            with self.assertRaises(BackendPermissionError):
                backend.workspace.write_text(str(Path(context.skills_root) / "agent.md"), "nope")

    def test_tools_and_subagents_are_exposed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            context = make_context(Path(tmp))
            tools = {tool.__name__: tool for tool in build_product_tools(context)}

            self.assertIn("write_workspace_file", tools)
            self.assertIn("update_project_memory", tools)
            self.assertIn("get_project_context", tools)
            self.assertIn("create_artifact", tools)

            tools["write_workspace_file"]("index.tsx", "hello\n")
            self.assertEqual(
                (Path(context.workspace_root) / "index.tsx").read_text(encoding="utf-8"),
                "hello\n",
            )
            project_context = tools["get_project_context"]()
            self.assertEqual(project_context["project_id"], "project-1")

            artifact = tools["create_artifact"]("reports/summary.txt", "summary\n", "text/plain")
            self.assertTrue(artifact["path"].endswith("artifacts/reports/summary.txt"))
            self.assertEqual(
                (Path(context.artifacts_root) / "reports/summary.txt").read_text(encoding="utf-8"),
                "summary\n",
            )

            subagents = build_subagents(context)
            self.assertTrue(any(agent["name"] == "frontend-builder" for agent in subagents))


if __name__ == "__main__":
    unittest.main()
