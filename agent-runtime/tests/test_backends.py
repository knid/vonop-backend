from __future__ import annotations

import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest.mock import patch

from deepagents import create_deep_agent
from langchain_core.language_models.fake_chat_models import FakeListChatModel

from wemob_agent.context import WemobContext
from wemob_agent.subagents import build_subagents
from wemob_agent.tools import build_product_tools, runtime_context


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
            self.assertTrue(all("system_prompt" in agent for agent in subagents))
            self.assertTrue(all("prompt" not in agent for agent in subagents))

    def test_agent_graph_can_be_created_with_subagents(self) -> None:
        agent = create_deep_agent(
            model=FakeListChatModel(responses=["ok"]),
            subagents=build_subagents(),
            system_prompt="test supervisor",
        )

        self.assertIsNotNone(agent)

    def test_tools_read_context_from_langgraph_runtime_module(self) -> None:
        context = make_context(Path("/tmp/wemob-test"))
        runtime_module = types.ModuleType("langgraph.runtime")
        runtime_module.get_runtime = lambda: types.SimpleNamespace(context=context.__dict__)

        with patch.dict(sys.modules, {"langgraph.runtime": runtime_module}):
            self.assertEqual(runtime_context().project_id, "project-1")

            tools = {tool.__name__: tool for tool in build_product_tools()}
            self.assertEqual(tools["get_project_context"]()["project_id"], "project-1")


if __name__ == "__main__":
    unittest.main()
