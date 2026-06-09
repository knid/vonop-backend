from __future__ import annotations

import asyncio
import os
import uuid
from time import time
from typing import AsyncGenerator

from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse, StreamingResponse
from starlette.background import BackgroundTask
from starlette.status import HTTP_404_NOT_FOUND

from . import __version__ as service_version
from . import logging as app_logging
from .config import Settings
from .errors import RepoValidationError
from .repo_paths import resolve_repo_path
from .schemas import RunStreamRequest
from .security import InternalAuth
from .streaming import ndjson_event
from .streaming import run_stream as _stream_run_engine

app_logging.configure()

auth = InternalAuth()
app = FastAPI(title="Wemob deepagents runtime", version="0.1.0")
active_runs: dict[str, asyncio.Event] = {}
run_guard = asyncio.Semaphore(max(Settings.MAX_CONCURRENT_RUNS, 1))


async def _cleanup_run(run_id: str) -> None:
    await asyncio.sleep(0)
    active_runs.pop(run_id, None)


@app.middleware("http")
async def add_request_context(request: Request, call_next):  # type: ignore[no-untyped-def]
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    request.state.request_id = request_id
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    response.headers["X-Service"] = "deepagents-runtime"
    response.headers["X-Service-Version"] = service_version
    response.headers["Cache-Control"] = "no-store"
    return response


def _validate_project_id(project_id: str) -> None:
    try:
        # Validation uses uuid parsing in repo_paths.
        from uuid import UUID

        UUID(project_id)
    except ValueError as exc:
        raise HTTPException(status_code=422, detail="invalid project_id") from exc


def _read_project_path(payload: RunStreamRequest) -> str:
    _validate_project_id(payload.project_id)
    if payload.repo.project_id != payload.project_id:
        raise HTTPException(status_code=422, detail="repo.project_id mismatch")
    if payload.repo.mount_base != Settings.REPO_BASE:
        # Keep host path derivation strict; never trust caller-provided mount_base.
        raise HTTPException(status_code=422, detail="invalid repo.mount_base")

    try:
        resolved = resolve_repo_path(payload.project_id)
    except RepoValidationError:
        # Never expose filesystem internals in request-facing error payload.
        raise HTTPException(status_code=400, detail="invalid repository path") from None

    return str(resolved)


@app.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/readyz")
async def readyz() -> dict[str, str | list[str]]:
    reasons: list[str] = []

    if not Settings.REPO_BASE:
        reasons.append("WEMOB_REPOS_BASE is not configured")
    else:
        from pathlib import Path
        base = Path(Settings.REPO_BASE)
        if not base.exists():
            reasons.append(f"WEMOB_REPOS_BASE does not exist: {Settings.REPO_BASE}")
        elif not base.is_dir():
            reasons.append(f"WEMOB_REPOS_BASE is not a directory: {Settings.REPO_BASE}")
        elif not os.access(str(base), os.W_OK):
            reasons.append(f"WEMOB_REPOS_BASE is not writable: {Settings.REPO_BASE}")

    if not Settings.INTERNAL_TOKEN:
        reasons.append("DEEPAGENTS_INTERNAL_TOKEN is not configured")

    if not Settings.GO_TOOL_BASE_URL:
        reasons.append("GO_TOOL_BASE_URL is not configured")

    model_providers = os.getenv("ANTHROPIC_API_KEY") or os.getenv("OPENROUTER_API_KEY") or os.getenv("OPENAI_API_KEY") or os.getenv("MOONSHOT_API_KEY")
    if not model_providers:
        reasons.append("no model provider API key configured (ANTHROPIC_API_KEY, OPENROUTER_API_KEY, OPENAI_API_KEY, or MOONSHOT_API_KEY)")

    if Settings.LANGSMITH_TRACING and not Settings.LANGSMITH_API_KEY:
        reasons.append("LANGSMITH_TRACING is enabled but LANGSMITH_API_KEY is not configured")

    if reasons:
        return JSONResponse(status_code=503, content={"status": "not_ready", "reasons": reasons})
    return {"status": "ready", "version": service_version}


@app.post("/internal/v1/runs/stream", dependencies=[Depends(auth)], response_class=StreamingResponse)
async def run_stream(payload: RunStreamRequest):
    if payload.run_id in active_runs:
        raise HTTPException(status_code=409, detail="run already in progress")

    _ = _read_project_path(payload)
    await run_guard.acquire()

    cancel_evt = asyncio.Event()
    active_runs[payload.run_id] = cancel_evt

    async def stream() -> AsyncGenerator[bytes, None]:
        started_at = time()
        max_ms = int(payload.limits.max_wallclock_ms or 0)
        if max_ms <= 0:
            max_ms = Settings.RUN_DEFAULT_TIMEOUT_SECONDS * 1000

        try:
            async for event in _stream_run_engine(payload.model_dump()):
                if cancel_evt.is_set():
                    yield ndjson_event(
                        "error",
                        run_id=payload.run_id,
                        seq=9999,
                        job_id=payload.job_id,
                        data={
                            "code": "cancelled",
                            "message": "run cancelled",
                            "phase": "runtime",
                            "recoverable": True,
                        },
                    )
                    return

                yield event

                elapsed_ms = int((time() - started_at) * 1000)
                if elapsed_ms >= max_ms:
                    yield ndjson_event(
                        "error",
                        run_id=payload.run_id,
                        seq=9998,
                        job_id=payload.job_id,
                        data={
                            "code": "timeout",
                            "message": "runtime timeout",
                            "phase": "runtime",
                            "recoverable": True,
                        },
                    )
                    return
        finally:
            run_guard.release()
            active_runs.pop(payload.run_id, None)

    return StreamingResponse(
        stream(),
        media_type="application/x-ndjson",
        background=BackgroundTask(_cleanup_run, payload.run_id),
    )


@app.post("/internal/v1/runs/{run_id}/cancel")
async def cancel_run(run_id: str, _=Depends(auth)):
    evt = active_runs.get(run_id)
    if not evt:
        return JSONResponse(status_code=HTTP_404_NOT_FOUND, content={"status": "not_found"})
    evt.set()
    return {"status": "cancelled", "run_id": run_id}


@app.post("/internal/v1/runs/{run_id}/heartbeat")
async def heartbeat(run_id: str, _=Depends(auth)) -> dict[str, str]:
    if run_id not in active_runs:
        return {"status": "stale"}
    return {"status": "active", "run_id": run_id}


def create_app() -> FastAPI:
    return app
