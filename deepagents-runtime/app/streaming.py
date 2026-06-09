from __future__ import annotations

import json
import time
from typing import Any, AsyncGenerator


def ndjson_event(event_type: str, *, run_id: str, seq: int, data: dict[str, Any], job_id: str | None = None) -> bytes:
    envelope = {
        "type": event_type,
        "run_id": run_id,
        "job_id": job_id or "",
        "seq": seq,
        "time_unix_ms": int(time.time() * 1000),
        "data": data,
    }
    return json.dumps(envelope).encode("utf-8") + b"\n"


from .runtime import stream_run
from .schemas import RunStreamRequest


async def fake_run_stream(payload: RunStreamRequest | dict[str, Any]) -> AsyncGenerator[bytes, None]:
    """Compatibility shim retained for existing call sites."""
    async for event in stream_run(payload):
        yield event


async def run_stream(payload: RunStreamRequest | dict[str, Any]) -> AsyncGenerator[bytes, None]:
    """Alias for explicit naming in callers that prefer the production entrypoint."""
    async for event in stream_run(payload):
        yield event
