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


# Re-exported for callers that prefer the streaming module as entrypoint.
# Avoid duplicating logic — the canonical implementation lives in runtime.py.
from .runtime import stream_run  # noqa: E402


async def run_stream(payload: Any) -> AsyncGenerator[bytes, None]:
    async for event in stream_run(payload):
        yield event
