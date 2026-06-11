from __future__ import annotations

import asyncio
import importlib.util
import tempfile
import unittest
from datetime import UTC, datetime
from pathlib import Path
from unittest.mock import patch

FASTAPI_AVAILABLE = importlib.util.find_spec("fastapi") is not None


@unittest.skipUnless(FASTAPI_AVAILABLE, "FastAPI runtime dependencies are not installed")
class InternalAPITests(unittest.TestCase):
    def test_start_run_endpoint_uses_adapter_without_stream_projection(self) -> None:
        from fastapi.testclient import TestClient

        from wemob_agent import internal_api

        class FakeAdapter:
            def __init__(self) -> None:
                self.last_request = None

            async def start_run(self, req):
                self.last_request = req
                return internal_api.RunRecord(
                    run_id="run-1",
                    thread_id=req.thread_id,
                    assistant_id="wemob_agent",
                    status="running",
                    created_at=datetime.now(UTC),
                    runtime_api_url="http://langgraph:2024",
                )

            async def get_run(self, run_id):
                raise KeyError(run_id)

            async def cancel_run(self, run_id):
                raise KeyError(run_id)

            async def resume_run(self, run_id, req):
                raise KeyError(run_id)

            async def monitor_and_finalize(self, run_id, runtime_run_id):
                return None

            async def resume_monitors(self):
                return None

        original = internal_api.runtime_adapter
        fake = FakeAdapter()
        internal_api.runtime_adapter = fake
        try:
            client = TestClient(internal_api.app)
            response = client.post(
                "/internal/agent-runs",
                json={
                    "thread_id": "thread-1",
                    "job_id": "job-1",
                    "project_id": "project-1",
                    "org_id": "org-1",
                    "user_id": "user-1",
                    "agent_runtime_run_id": "runtime-run-1",
                    "message": "build",
                    "mode": "generate",
                    "project_type": "app",
                    "frontend_runtime": "vite_react",
                    "worktree_path": "/worktrees/project-1/job-1",
                    "base_commit": "abc123",
                },
            )
        finally:
            internal_api.runtime_adapter = original

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json()["runtime_api_url"], "http://langgraph:2024")
        self.assertEqual(response.json()["sandbox_id"], "")
        self.assertEqual(fake.last_request.worktree_path, "/worktrees/project-1/job-1")
        self.assertEqual(fake.last_request.agent_runtime_run_id, "runtime-run-1")

    def test_start_run_sends_langgraph_context_without_configurable_context(self) -> None:
        from wemob_agent import internal_api

        class CapturingAdapter(internal_api.LangGraphRuntimeAdapter):
            def __init__(self, store):
                super().__init__(runtime_api_url="http://langgraph:2024", store=store)
                self.requests = []

            async def _ensure_thread(self, thread_id):
                return None

            async def _request(self, method, path, **kwargs):
                self.requests.append((method, path, kwargs))
                return {"run_id": "run-1", "status": "running"}

        with tempfile.TemporaryDirectory() as tmp:
            store = internal_api.JsonRunStore(str(Path(tmp) / "runs.json"))
            adapter = CapturingAdapter(store)
            req = internal_api.StartRunRequest(
                thread_id="thread-1",
                job_id="job-1",
                project_id="project-1",
                org_id="org-1",
                user_id="user-1",
                agent_runtime_run_id="runtime-run-1",
                message="build",
                mode="generate",
                project_type="app",
                frontend_runtime="vite_react",
                worktree_path="/worktrees/project-1/job-1",
                base_commit="abc123",
            )
            with patch.dict("os.environ", {"WEMOB_ALLOW_LOCAL_AGENT_EXECUTION": "true"}):
                record = asyncio.run(adapter.start_run(req))

        self.assertEqual(record.run_id, "run-1")
        payload = adapter.requests[-1][2]["json"]
        self.assertIn("context", payload)
        self.assertNotIn("config", payload)
        self.assertEqual(payload["context"]["project_id"], "project-1")
        self.assertEqual(payload["context"]["workspace_root"], "/workspace")

    def test_ensure_thread_allows_existing_thread_conflict(self) -> None:
        from fastapi import status

        from wemob_agent import internal_api

        class CapturingAdapter(internal_api.LangGraphRuntimeAdapter):
            def __init__(self, store):
                super().__init__(runtime_api_url="http://langgraph:2024", store=store)
                self.requests = []

            async def _request(self, method, path, allowed_statuses=None, **kwargs):
                self.requests.append((method, path, allowed_statuses, kwargs))
                return {}

        with tempfile.TemporaryDirectory() as tmp:
            store = internal_api.JsonRunStore(str(Path(tmp) / "runs.json"))
            adapter = CapturingAdapter(store)
            asyncio.run(adapter._ensure_thread("thread-1"))

        self.assertEqual(adapter.requests[0][0], "POST")
        self.assertEqual(adapter.requests[0][1], "/threads")
        self.assertEqual(adapter.requests[0][2], {status.HTTP_409_CONFLICT})
        self.assertEqual(adapter.requests[0][3]["json"], {"thread_id": "thread-1"})

    def test_langgraph_proxy_preflight_allows_wemob_app_origin_without_auth(self) -> None:
        from fastapi.testclient import TestClient

        from wemob_agent import internal_api

        client = TestClient(internal_api.app)
        response = client.options(
            "/lg/threads/c041bfe1-63f5-4950-9309-88731bd7db97/state",
            headers={
                "Origin": "https://app.wemob.io",
                "Access-Control-Request-Method": "GET",
                "Access-Control-Request-Headers": "authorization,content-type",
            },
        )

        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.headers["access-control-allow-origin"], "https://app.wemob.io")
        self.assertIn("authorization", response.headers["access-control-allow-headers"].lower())


class ConfigTests(unittest.TestCase):
    def test_load_config_parses_agent_cors_allowed_origins(self) -> None:
        from wemob_agent.config import load_config

        with patch.dict(
            "os.environ",
            {"WEMOB_AGENT_CORS_ALLOWED_ORIGINS": "https://app.wemob.io, http://localhost:5173"},
        ):
            cfg = load_config()

        self.assertEqual(
            cfg.cors_allowed_origins,
            ("https://app.wemob.io", "http://localhost:5173"),
        )


if __name__ == "__main__":
    unittest.main()
