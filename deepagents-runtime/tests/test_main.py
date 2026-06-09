import os
import subprocess
import tempfile
from pathlib import Path

from fastapi.testclient import TestClient

from app.config import Settings
from app.main import app
from app.permissions import is_allowed_workspace_path
from app.repo_paths import resolve_repo_path
from app.schemas import RunStreamRequest
from app.skills import resolve_skills


def _setup_test_env():
    os.environ.setdefault("DEEPAGENTS_INTERNAL_TOKEN", "secret")
    os.environ.setdefault("GO_TOOL_BASE_URL", "http://localhost:8001")
    os.environ.setdefault("ANTHROPIC_API_KEY", "test-key")
    os.environ.setdefault("WEMOB_REPOS_BASE", "/tmp/nonexistent")
    # Reload Settings from env
    Settings.INTERNAL_TOKEN = os.environ["DEEPAGENTS_INTERNAL_TOKEN"]
    Settings.GO_TOOL_BASE_URL = os.environ["GO_TOOL_BASE_URL"]
    Settings.REPO_BASE = os.environ["WEMOB_REPOS_BASE"]


_setup_test_env()


def _init_repo_for_test(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)
    subprocess.check_call(["git", "init", "-q"], cwd=str(path))
    subprocess.check_call(
        ["git", "config", "user.email", "dev@wemob.io"], cwd=str(path)
    )
    subprocess.check_call(
        ["git", "config", "user.name", "wemob-test"], cwd=str(path)
    )
    (path / ".gitkeep").write_text("", encoding="utf-8")
    subprocess.check_call(["git", "add", ".gitkeep"], cwd=str(path))
    subprocess.check_call(["git", "commit", "-m", "init"], cwd=str(path))


def _make_stream_payload(base_path: Path, project_id: str) -> dict:
    return {
        "schema_version": "2026-06-09",
        "job_id": "job-1",
        "run_id": "run-1",
        "trace_id": "trace-1",
        "user_id": "user-1",
        "organization_id": "org-1",
        "project_id": project_id,
        "project_type": "app",
        "frontend_runtime": "nextjs",
        "is_first_generation": True,
        "read_only": False,
        "message": "Build a landing page redesign for our SaaS app.",
        "attachments": [],
        "repo": {
            "mount_base": str(base_path),
            "project_id": project_id,
            "virtual_root": "/workspace",
        },
        "model": {
            "provider": "anthropic",
            "model": "test-model",
            "langchain_model": "openrouter:test-model",
        },
        "limits": {
            "max_wallclock_ms": 2700000,
            "max_model_calls": 80,
            "max_tool_calls": 200,
            "max_output_tokens_per_call": 64000,
        },
        "tools": [],
        "skills": {"paths": [], "ids": []},
        "metadata": {},
    }


def test_health_and_ready() -> None:
    client = TestClient(app)
    assert client.get("/healthz").status_code == 200

    response = client.get("/readyz")
    assert response.status_code in (200, 503)
    if response.status_code == 200:
        data = response.json()
        assert data["status"] == "ready"
        assert "version" in data


def test_stream_rejects_invalid_auth_and_writes_test_file() -> None:
    with tempfile.TemporaryDirectory() as base:
        base_path = Path(base)
        project_dir = base_path / "11111111-1111-4111-8111-111111111111"
        _init_repo_for_test(project_dir)

        os.environ["DEEPAGENTS_INTERNAL_TOKEN"] = "secret"
        os.environ["WEMOB_REPOS_BASE"] = str(base_path)
        os.environ["DEEPAGENTS_ALLOW_SCAFFOLD_FALLBACK"] = "true"
        os.environ["ANTHROPIC_API_KEY"] = "test-key"
        Settings.INTERNAL_TOKEN = "secret"
        Settings.REPO_BASE = str(base_path)
        Settings.ALLOW_SCAFFOLD_FALLBACK = True

        # Validate path resolver for a UUID project id.
        assert resolve_repo_path("11111111-1111-4111-8111-111111111111") == project_dir.resolve()

        client = TestClient(app)
        payload = _make_stream_payload(base_path, "11111111-1111-4111-8111-111111111111")

        r = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer wrong"},
        )
        assert r.status_code == 401

        resp = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer secret"},
        )
        assert resp.status_code == 200
        assert resp.headers["content-type"].startswith("application/x-ndjson")

        lines = [line for line in resp.text.strip().splitlines() if line.strip()]
        assert any('"type": "message_delta"' in line for line in lines)
        assert any('"type": "done"' in line for line in lines)


def test_stream_rejects_invalid_repo_metadata() -> None:
    with tempfile.TemporaryDirectory() as base:
        base_path = Path(base)
        project_dir = base_path / "22222222-2222-4222-8222-222222222222"
        _init_repo_for_test(project_dir)

        os.environ["DEEPAGENTS_INTERNAL_TOKEN"] = "secret"
        os.environ["WEMOB_REPOS_BASE"] = str(base_path)
        Settings.INTERNAL_TOKEN = "secret"
        Settings.REPO_BASE = str(base_path)

        client = TestClient(app)
        payload = _make_stream_payload(base_path, "22222222-2222-4222-8222-222222222222")
        payload["repo"]["mount_base"] = "/bad/mount"

        resp = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer secret"},
        )
        assert resp.status_code == 422

        payload["repo"]["mount_base"] = str(base_path)
        payload["repo"]["project_id"] = "another-project-id"
        payload["project_id"] = "22222222-2222-4222-8222-222222222222"

        resp = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer secret"},
        )
        assert resp.status_code == 422


def test_stream_includes_runtime_events() -> None:
    with tempfile.TemporaryDirectory() as base:
        base_path = Path(base)
        project_dir = base_path / "33333333-3333-4333-8333-333333333333"
        _init_repo_for_test(project_dir)

        os.environ["DEEPAGENTS_INTERNAL_TOKEN"] = "secret"
        os.environ["WEMOB_REPOS_BASE"] = str(base_path)
        os.environ["DEEPAGENTS_ALLOW_SCAFFOLD_FALLBACK"] = "true"
        Settings.INTERNAL_TOKEN = "secret"
        Settings.REPO_BASE = str(base_path)
        Settings.ALLOW_SCAFFOLD_FALLBACK = True

        client = TestClient(app)
        payload = _make_stream_payload(base_path, "33333333-3333-4333-8333-333333333333")

        resp = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer secret"},
        )
        assert resp.status_code == 200

        lines = [line for line in resp.text.strip().splitlines() if line.strip()]
        assert any('"type": "subagent_start"' in line for line in lines)
        assert any('"type": "reasoning_delta"' in line for line in lines)
        assert any('"type": "tool_call_start"' in line for line in lines)
        assert any('"type": "tool_call_result"' in line for line in lines)
        assert any('"type": "subagent_end"' in line for line in lines)
        assert any('"type": "done"' in line for line in lines)


def test_stream_fallback_can_write_workspace_file() -> None:
    with tempfile.TemporaryDirectory() as base:
        base_path = Path(base)
        project_id = "44444444-4444-4444-8444-444444444444"
        project_dir = base_path / project_id
        _init_repo_for_test(project_dir)

        os.environ["DEEPAGENTS_INTERNAL_TOKEN"] = "secret"
        os.environ["WEMOB_REPOS_BASE"] = str(base_path)
        os.environ["DEEPAGENTS_ALLOW_SCAFFOLD_FALLBACK"] = "true"
        Settings.INTERNAL_TOKEN = "secret"
        Settings.REPO_BASE = str(base_path)
        Settings.ALLOW_SCAFFOLD_FALLBACK = True

        client = TestClient(app)
        payload = _make_stream_payload(base_path, project_id)
        payload["message"] = "Please write /workspace/test.txt with: fixture test file from fallback."

        resp = client.post(
            "/internal/v1/runs/stream",
            json=payload,
            headers={"Authorization": "Bearer secret"},
        )
        assert resp.status_code == 200
        assert (project_dir / "test.txt").exists()
        assert (project_dir / "test.txt").read_text(encoding="utf-8") == "fixture test file from fallback."


def test_permissions_deny_workspace_paths() -> None:
    assert is_allowed_workspace_path("/workspace/src/index.ts") is True
    assert is_allowed_workspace_path("/workspace/.git/config") is False
    assert is_allowed_workspace_path("/workspace/.env.prod") is False
    assert is_allowed_workspace_path("/workspace/node_modules/package.json") is False
    assert is_allowed_workspace_path("/outside") is False


def test_permissions_guardrails() -> None:
    assert is_allowed_workspace_path("/workspace/src/index.ts") is True
    assert is_allowed_workspace_path("/workspace/.git/config") is False
    assert is_allowed_workspace_path("/outside") is False


def test_resolve_skills_defaults() -> None:
    payload = RunStreamRequest(
        schema_version="2026-06-09",
        job_id="job-1",
        run_id="run-1",
        trace_id="trace-1",
        user_id="user-1",
        organization_id="org-1",
        project_id="44444444-4444-4444-8444-444444444444",
        project_type="app",
        frontend_runtime="nextjs",
        is_first_generation=True,
        message="Build a premium SaaS landing page redesign with auth and logs.",
        repo={
            "mount_base": "/mnt/wemob-repos",
            "project_id": "44444444-4444-4444-8444-444444444444",
            "virtual_root": "/workspace",
        },
        model={"provider": "anthropic", "model": "test-model"},
    )

    skills = resolve_skills(payload)
    assert "/skills/core/wemob-builder-foundation" in skills
    assert "/skills/web/web-builder-foundation" in skills
    assert "/skills/workflows/premium-saas-landing-page" in skills
    assert "/skills/core/verification-loop" in skills
