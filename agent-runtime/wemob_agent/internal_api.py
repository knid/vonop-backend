"""Internal Wemob control API for launching Deep Agents runs.

This API is intentionally separate from the LangGraph streaming API. Go calls
these endpoints to create/cancel/resume runs; the frontend still connects to
the LangGraph runtime directly with the LangChain frontend SDK.
"""

from __future__ import annotations

import asyncio
import base64
import fcntl
import hashlib
import hmac
import json
import os
import time
import uuid
from contextlib import contextmanager
from dataclasses import asdict, replace
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import httpx
from fastapi import BackgroundTasks, Depends, FastAPI, Header, HTTPException, Request, status
from pydantic import BaseModel, Field
from starlette.middleware.cors import CORSMiddleware
from starlette.responses import StreamingResponse

from wemob_agent.config import load_config
from wemob_agent.context import WemobContext
from wemob_agent.daytona import connect_runtime_sandbox, create_runtime_sandbox


class StartRunRequest(BaseModel):
    thread_id: str
    job_id: str
    project_id: str
    org_id: str
    user_id: str
    agent_runtime_run_id: str
    message: str
    mode: str
    project_type: str
    frontend_runtime: str
    worktree_path: str
    base_commit: str | None = None
    attachment_ids: list[str] = Field(default_factory=list)
    feature_flags: dict[str, Any] = Field(default_factory=dict)
    credit_budget: int = 0


class ResumeRunRequest(BaseModel):
    command: Any | None = None
    input: dict[str, Any] | None = None
    decision: str | None = None


class RuntimeRunResponse(BaseModel):
    run_id: str
    thread_id: str
    assistant_id: str
    status: str
    created_at: datetime | None = None
    runtime_api_url: str
    sandbox_id: str = ""


class RunRecord(BaseModel):
    run_id: str
    thread_id: str
    assistant_id: str
    status: str
    created_at: datetime
    runtime_api_url: str
    runtime_run_id: str = ""
    host_worktree_path: str = ""
    sandbox_id: str = ""
    context: dict[str, Any] = Field(default_factory=dict)
    finalized: bool = False


class JsonRunStore:
    def __init__(self, path: str | None = None) -> None:
        self.path = Path(path or load_config().run_store_path)
        self.lock_path = self.path.with_suffix(self.path.suffix + ".lock")
        self._lock = asyncio.Lock()

    async def load_all(self) -> dict[str, RunRecord]:
        async with self._lock:
            with self._file_lock():
                return self._load_all_unlocked()

    async def get(self, run_id: str) -> RunRecord | None:
        async with self._lock:
            with self._file_lock():
                return self._load_all_unlocked().get(run_id)

    async def put(self, record: RunRecord) -> None:
        async with self._lock:
            with self._file_lock():
                records = self._load_all_unlocked()
                records[record.run_id] = record
                self._write_all_unlocked(records)

    @contextmanager
    def _file_lock(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.lock_path.open("a+", encoding="utf-8") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            try:
                yield
            finally:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_UN)

    def _load_all_unlocked(self) -> dict[str, RunRecord]:
        if not self.path.exists():
            return {}
        try:
            raw = json.loads(self.path.read_text(encoding="utf-8"))
        except Exception:
            return {}
        records: dict[str, RunRecord] = {}
        if not isinstance(raw, dict):
            return records
        for run_id, data in raw.items():
            if not isinstance(data, dict):
                continue
            try:
                records[str(run_id)] = RunRecord(**data)
            except Exception:
                continue
        return records

    def _write_all_unlocked(self, records: dict[str, RunRecord]) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(self.path.suffix + ".tmp")
        tmp.write_text(
            json.dumps(
                {run_id: record.model_dump(mode="json") for run_id, record in records.items()},
                sort_keys=True,
            ),
            encoding="utf-8",
        )
        tmp.replace(self.path)


class LangGraphRuntimeAdapter:
    def __init__(
        self,
        runtime_api_url: str | None = None,
        assistant_id: str | None = None,
        store: JsonRunStore | None = None,
    ) -> None:
        self.runtime_api_url = (
            runtime_api_url or os.getenv("WEMOB_LANGGRAPH_API_URL") or "http://127.0.0.1:2024"
        ).rstrip("/")
        self.assistant_id = assistant_id or os.getenv("WEMOB_AGENT_ASSISTANT_ID", "wemob_agent")
        self.store = store or JsonRunStore()

    async def start_run(self, req: StartRunRequest) -> RunRecord:
        await self._ensure_thread(req.thread_id)
        context = _context_from_request(req)
        if not load_config().allow_local_agent_execution:
            sandbox = create_runtime_sandbox(context)
            context = replace(context, sandbox_id=sandbox.provider.sandbox_id)
        payload = {
            "assistant_id": self.assistant_id,
            "input": {"messages": [{"role": "user", "content": req.message}]},
            "context": asdict(context),
            "metadata": {
                "job_id": req.job_id,
                "project_id": req.project_id,
                "org_id": req.org_id,
                "user_id": req.user_id,
                "mode": req.mode,
                "base_commit": req.base_commit or "",
                "sandbox_id": context.sandbox_id,
            },
            "multitask_strategy": "enqueue",
        }
        data = await self._request("POST", f"/threads/{req.thread_id}/runs", json=payload)
        run_id = str(data.get("run_id") or data.get("id") or uuid.uuid4())
        record = RunRecord(
            run_id=run_id,
            thread_id=req.thread_id,
            assistant_id=self.assistant_id,
            status=str(data.get("status") or "running"),
            created_at=datetime.now(UTC),
            runtime_api_url=self.runtime_api_url,
            runtime_run_id=req.agent_runtime_run_id,
            host_worktree_path=req.worktree_path,
            sandbox_id=context.sandbox_id,
            context=asdict(context),
        )
        await self.store.put(record)
        return record

    async def get_run(self, run_id: str) -> RunRecord:
        record = await self.store.get(run_id)
        if record is None:
            raise KeyError(run_id)
        try:
            data = await self._request("GET", f"/threads/{record.thread_id}/runs/{run_id}")
        except HTTPException as exc:
            if exc.status_code >= 500:
                raise
            return record
        status_value = data.get("status")
        if status_value:
            record.status = str(status_value)
            await self.store.put(record)
        return record

    async def cancel_run(self, run_id: str) -> None:
        record = await self.get_run(run_id)
        await self._request("POST", f"/threads/{record.thread_id}/runs/{run_id}/cancel", json={})
        record.status = "canceled"
        await self.store.put(record)

    async def resume_run(self, run_id: str, req: ResumeRunRequest) -> RunRecord:
        record = await self.get_run(run_id)
        payload: dict[str, Any] = {
            "assistant_id": record.assistant_id,
            "input": req.input or {},
            "context": record.context,
            "multitask_strategy": "enqueue",
        }
        if req.command is not None:
            payload["command"] = req.command
        if req.decision:
            payload["input"]["decision"] = req.decision
        data = await self._request("POST", f"/threads/{record.thread_id}/runs", json=payload)
        record.run_id = str(data.get("run_id") or data.get("id") or record.run_id)
        record.status = str(data.get("status") or "running")
        await self.store.put(record)
        return record

    async def monitor_and_finalize(self, run_id: str, runtime_run_id: str) -> None:
        terminal_success = {"success", "succeeded", "completed"}
        terminal_failure = {"error", "failed", "cancelled", "canceled", "timeout", "interrupted"}
        while True:
            await asyncio.sleep(float(os.getenv("WEMOB_AGENT_RUN_POLL_SECONDS", "5")))
            try:
                record = await self.get_run(run_id)
            except KeyError:
                return
            status_value = record.status.lower()
            if status_value in terminal_success:
                await self.finalize_record(
                    record, successful=True, summary="Deep Agents run completed"
                )
                return
            if status_value in terminal_failure:
                await self.finalize_record(
                    record,
                    successful=False,
                    summary=f"Deep Agents run ended with status {record.status}",
                )
                return

    async def resume_monitors(self) -> None:
        records = await self.store.load_all()
        terminal = {
            "success",
            "succeeded",
            "completed",
            "error",
            "failed",
            "cancelled",
            "canceled",
            "timeout",
            "interrupted",
        }
        for record in records.values():
            if record.finalized:
                continue
            if record.status.lower() in terminal:
                asyncio.create_task(
                    self.finalize_record(
                        record,
                        successful=record.status.lower() in {"success", "succeeded", "completed"},
                        summary=f"Deep Agents run ended with status {record.status}",
                    )
                )
            else:
                asyncio.create_task(self.monitor_and_finalize(record.run_id, record.runtime_run_id))

    async def finalize_record(self, record: RunRecord, successful: bool, summary: str) -> None:
        if record.finalized:
            return
        if (
            record.sandbox_id
            and record.host_worktree_path
            and not load_config().allow_local_agent_execution
        ):
            sandbox = connect_runtime_sandbox(record.sandbox_id)
            sandbox.provider.download_tree(
                str(
                    record.context.get(
                        "daytona_workspace_root",
                        load_config().daytona_workspace_root,
                    )
                ),
                Path(record.host_worktree_path),
            )
        await _notify_backend_finalizer(
            record.runtime_run_id, successful=successful, summary=summary
        )
        record.finalized = True
        await self.store.put(record)

    async def _ensure_thread(self, thread_id: str) -> None:
        try:
            await self._request("POST", "/threads", json={"thread_id": thread_id})
        except HTTPException as exc:
            if exc.status_code != status.HTTP_409_CONFLICT:
                raise

    async def _request(self, method: str, path: str, **kwargs: Any) -> dict[str, Any]:
        async with httpx.AsyncClient(base_url=self.runtime_api_url, timeout=60.0) as client:
            response = await client.request(method, path, **kwargs)
        if response.status_code < 200 or response.status_code >= 300:
            raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail=response.text)
        if not response.content:
            return {}
        data = response.json()
        if isinstance(data, dict):
            return data
        return {"data": data}


def _context_from_request(req: StartRunRequest) -> WemobContext:
    runtime_config = load_config()
    return WemobContext(
        user_id=req.user_id,
        org_id=req.org_id,
        project_id=req.project_id,
        job_id=req.job_id,
        thread_id=req.thread_id,
        agent_runtime_run_id=req.agent_runtime_run_id,
        mode=req.mode,  # type: ignore[arg-type]
        project_type=req.project_type,  # type: ignore[arg-type]
        frontend_runtime=req.frontend_runtime,
        base_commit=req.base_commit,
        credit_budget=req.credit_budget,
        feature_flags=req.feature_flags,
        workspace_root="/workspace",
        daytona_workspace_root=runtime_config.daytona_workspace_root,
        host_worktree_path=req.worktree_path,
        memory_root=os.getenv("WEMOB_AGENT_MEMORY_ROOT", "/memories"),
        skills_root=os.getenv("WEMOB_AGENT_SKILLS_ROOT", "/skills"),
        artifacts_root=os.getenv("WEMOB_AGENT_ARTIFACTS_ROOT", "/artifacts"),
        preview_domain=runtime_config.preview_domain,
    )


def _runtime_response(record: RunRecord) -> RuntimeRunResponse:
    return RuntimeRunResponse(
        run_id=record.run_id,
        thread_id=record.thread_id,
        assistant_id=record.assistant_id,
        status=record.status,
        created_at=record.created_at,
        runtime_api_url=record.runtime_api_url,
        sandbox_id=record.sandbox_id,
    )


def _verify_signature(body: bytes, timestamp: str | None, signature: str | None) -> None:
    secret = os.getenv("AGENT_RUNTIME_INTERNAL_TOKEN", "")
    if not secret:
        return
    if not timestamp or not signature:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="missing signature")
    try:
        ts = int(timestamp)
    except ValueError as exc:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid timestamp"
        ) from exc
    if abs(int(time.time()) - ts) > 300:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="stale signature")
    expected = hmac.new(
        secret.encode(), timestamp.encode() + b"." + body, hashlib.sha256
    ).hexdigest()
    if not hmac.compare_digest(signature, "sha256=" + expected):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid signature")


def _verify_runtime_access_token(token: str, request_path: str) -> dict[str, Any]:
    secret = os.getenv("AGENT_RUNTIME_ACCESS_TOKEN_SECRET") or os.getenv(
        "AGENT_RUNTIME_INTERNAL_TOKEN", ""
    )
    if not secret:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="runtime access secret is not configured",
        )
    parts = token.split(".")
    if len(parts) != 2:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid runtime token"
        )
    try:
        payload_bytes = base64.urlsafe_b64decode(parts[0] + "=" * (-len(parts[0]) % 4))
        signature = base64.urlsafe_b64decode(parts[1] + "=" * (-len(parts[1]) % 4))
        payload = json.loads(payload_bytes)
    except Exception as exc:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid runtime token"
        ) from exc
    expected = hmac.new(secret.encode(), parts[0].encode(), hashlib.sha256).digest()
    if not hmac.compare_digest(signature, expected):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid runtime token"
        )
    if int(payload.get("exp", 0)) < int(time.time()):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="runtime token expired"
        )
    thread_id = str(payload.get("thread_id") or "")
    if (
        request_path.startswith("threads/")
        and thread_id
        and not request_path.startswith(f"threads/{thread_id}/")
    ):
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN, detail="runtime token thread mismatch"
        )
    return payload


async def authorize_internal_request(
    request: Request,
    authorization: str | None = Header(default=None),
    x_wemob_timestamp: str | None = Header(default=None),
    x_wemob_signature: str | None = Header(default=None),
) -> None:
    token = os.getenv("AGENT_RUNTIME_INTERNAL_TOKEN", "")
    if token and authorization != f"Bearer {token}":
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid token")
    body = await request.body()
    _verify_signature(body, x_wemob_timestamp, x_wemob_signature)


app = FastAPI(title="Wemob Deep Agents Internal API")
_runtime_config = load_config()
app.add_middleware(
    CORSMiddleware,
    allow_origins=list(_runtime_config.cors_allowed_origins),
    allow_origin_regex=_runtime_config.cors_allowed_origin_regex or None,
    allow_methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
    allow_headers=["*"],
    expose_headers=["Link", "Location", "X-Request-ID"],
    allow_credentials=False,
    max_age=300,
)
runtime_adapter: LangGraphRuntimeAdapter = LangGraphRuntimeAdapter()


@app.on_event("startup")
async def resume_runtime_monitors() -> None:
    await runtime_adapter.resume_monitors()


@app.post(
    "/internal/agent-runs",
    response_model=RuntimeRunResponse,
    dependencies=[Depends(authorize_internal_request)],
)
async def start_run(req: StartRunRequest, background_tasks: BackgroundTasks) -> RuntimeRunResponse:
    record = await runtime_adapter.start_run(req)
    background_tasks.add_task(
        runtime_adapter.monitor_and_finalize, record.run_id, req.agent_runtime_run_id
    )
    return _runtime_response(record)


@app.get(
    "/internal/agent-runs/{run_id}",
    response_model=RuntimeRunResponse,
    dependencies=[Depends(authorize_internal_request)],
)
async def get_run(run_id: str) -> RuntimeRunResponse:
    try:
        return _runtime_response(await runtime_adapter.get_run(run_id))
    except KeyError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="run not found") from exc


@app.post(
    "/internal/agent-runs/{run_id}/cancel",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(authorize_internal_request)],
)
async def cancel_run(run_id: str) -> None:
    try:
        await runtime_adapter.cancel_run(run_id)
    except KeyError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="run not found") from exc


@app.post(
    "/internal/agent-runs/{run_id}/resume",
    response_model=RuntimeRunResponse,
    dependencies=[Depends(authorize_internal_request)],
)
async def resume_run(run_id: str, req: ResumeRunRequest) -> RuntimeRunResponse:
    try:
        return _runtime_response(await runtime_adapter.resume_run(run_id, req))
    except KeyError as exc:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="run not found") from exc


@app.api_route(
    "/lg/{path:path}",
    methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"],
)
async def proxy_langgraph(
    path: str, request: Request, authorization: str | None = Header(default=None)
) -> StreamingResponse:
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED, detail="missing runtime token"
        )
    _verify_runtime_access_token(authorization.removeprefix("Bearer ").strip(), path)

    body = await request.body()
    headers = {
        key: value
        for key, value in request.headers.items()
        if key.lower() not in {"host", "content-length", "authorization"}
    }
    client = httpx.AsyncClient(base_url=runtime_adapter.runtime_api_url, timeout=None)
    upstream_request = client.build_request(
        request.method,
        "/" + path,
        params=request.query_params,
        headers=headers,
        content=body,
    )
    upstream = await client.send(upstream_request, stream=True)
    response_headers = {
        key: value
        for key, value in upstream.headers.items()
        if key.lower()
        not in {"content-encoding", "content-length", "transfer-encoding", "connection"}
    }

    async def iter_upstream():
        try:
            async for chunk in upstream.aiter_raw():
                yield chunk
        finally:
            await upstream.aclose()
            await client.aclose()

    return StreamingResponse(
        iter_upstream(),
        status_code=upstream.status_code,
        headers=response_headers,
        media_type=upstream.headers.get("content-type"),
    )


async def _notify_backend_finalizer(runtime_run_id: str, successful: bool, summary: str) -> None:
    cfg = load_config()
    backend_url = cfg.backend_internal_url
    token = cfg.internal_token
    if not backend_url or not token:
        raise RuntimeError(
            "WEMOB_BACKEND_INTERNAL_URL and AGENT_RUNTIME_INTERNAL_TOKEN are required"
        )
    last_error: Exception | None = None
    for attempt in range(5):
        try:
            async with httpx.AsyncClient(timeout=30.0) as client:
                response = await client.post(
                    f"{backend_url}/internal/deepagents/runs/{runtime_run_id}/finalize",
                    headers={"X-Internal-Token": token},
                    json={"successful": successful, "summary": summary},
                )
            response.raise_for_status()
            return
        except Exception as exc:
            last_error = exc
            await asyncio.sleep(min(2**attempt, 30))
    raise RuntimeError(f"backend finalizer callback failed: {last_error}")
