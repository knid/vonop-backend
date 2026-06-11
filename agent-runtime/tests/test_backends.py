from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

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
    def test_tools_and_subagents_are_exposed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            context = make_context(Path(tmp))
            tools = {tool.__name__: tool for tool in build_product_tools(context)}

            self.assertIn("get_project_context", tools)
            self.assertIn("list_project_assets", tools)
            self.assertIn("generate_image", tools)
            self.assertIn("start_preview_server", tools)
            self.assertNotIn("write_workspace_file", tools)
            project_context = tools["get_project_context"]()
            self.assertEqual(project_context["project_id"], "project-1")

            subagents = build_subagents(context)
            self.assertTrue(any(agent["name"] == "frontend-builder" for agent in subagents))


if __name__ == "__main__":
    unittest.main()
