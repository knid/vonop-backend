"""Internal Wemob control API for launching Deep Agents runs.

This API is intentionally separate from the LangGraph streaming API. Go calls
these endpoints to create/cancel/resume runs; the frontend still connects to
the LangGraph runtime directly with the LangChain frontend SDK.
"""

from __future__ import annotations

import hashlib
import hmac
import os
import time
import uuid
from dataclasses import asdict
from datetime import UTC, datetime
from typing import Any

import httpx
from fastapi import Depends, FastAPI, Header, HTTPException, Request, status
from pydantic import BaseModel, Field

from wemob_agent.config import load_config
from wemob_agent.context import WemobContext


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


class RunRecord(BaseModel):
    run_id: str
    thread_id: str
    assistant_id: str
    status: str
    created_at: datetime
    runtime_api_url: str


class LangGraphRuntimeAdapter:
    def __init__(self, runtime_api_url: str | None = None, assistant_id: str | None = None) -> None:
        self.runtime_api_url = (runtime_api_url or os.getenv("WEMOB_LANGGRAPH_API_URL") or "http://127.0.0.1:2024").rstrip("/")
        self.assistant_id = assistant_id or os.getenv("WEMOB_AGENT_ASSISTANT_ID", "wemob_agent")
        self._runs: dict[str, RunRecord] = {}

    async def start_run(self, req: StartRunRequest) -> RunRecord:
        await self._ensure_thread(req.thread_id)
        payload = {
            "assistant_id": self.assistant_id,
            "input": {"messages": [{"role": "user", "content": req.message}]},
            "config": {"configurable": {"wemob_context": asdict(_context_from_request(req))}},
            "metadata": {
                "job_id": req.job_id,
                "project_id": req.project_id,
                "org_id": req.org_id,
                "user_id": req.user_id,
                "mode": req.mode,
                "base_commit": req.base_commit or "",
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
        )
        self._runs[run_id] = record
        return record

    async def get_run(self, run_id: str) -> RunRecord:
        record = self._runs.get(run_id)
        if record is None:
            raise KeyError(run_id)
        try:
            data = await self._request("GET", f"/threads/{record.thread_id}/runs/{run_id}")
        except HTTPException:
            return record
        status_value = data.get("status")
        if status_value:
            record.status = str(status_value)
            self._runs[run_id] = record
        return record

    async def cancel_run(self, run_id: str) -> None:
        record = await self.get_run(run_id)
        await self._request("POST", f"/threads/{record.thread_id}/runs/{run_id}/cancel", json={})
        record.status = "canceled"
        self._runs[run_id] = record

    async def resume_run(self, run_id: str, req: ResumeRunRequest) -> RunRecord:
        record = await self.get_run(run_id)
        payload: dict[str, Any] = {
            "assistant_id": record.assistant_id,
            "input": req.input or {},
            "config": {"configurable": {}},
            "multitask_strategy": "enqueue",
        }
        if req.command is not None:
            payload["command"] = req.command
        if req.decision:
            payload["input"]["decision"] = req.decision
        data = await self._request("POST", f"/threads/{record.thread_id}/runs", json=payload)
        record.run_id = str(data.get("run_id") or data.get("id") or record.run_id)
        record.status = str(data.get("status") or "running")
        self._runs[record.run_id] = record
        return record

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
        workspace_root=req.worktree_path,
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
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid timestamp") from exc
    if abs(int(time.time()) - ts) > 300:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="stale signature")
    expected = hmac.new(secret.encode(), timestamp.encode() + b"." + body, hashlib.sha256).hexdigest()
    if not hmac.compare_digest(signature, "sha256=" + expected):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="invalid signature")


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
runtime_adapter: LangGraphRuntimeAdapter = LangGraphRuntimeAdapter()


@app.post(
    "/internal/agent-runs",
    response_model=RuntimeRunResponse,
    dependencies=[Depends(authorize_internal_request)],
)
async def start_run(req: StartRunRequest) -> RuntimeRunResponse:
    return _runtime_response(await runtime_adapter.start_run(req))


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
