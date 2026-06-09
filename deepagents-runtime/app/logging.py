from __future__ import annotations

import json
import logging
import sys
from datetime import datetime, timezone


class JsonLogFormatter(logging.Formatter):
    """Minimal JSON formatter for container logs."""

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "ts": datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(),
            "level": record.levelname.lower(),
            "logger": record.name,
            "message": record.getMessage(),
            "path": record.pathname,
            "line": record.lineno,
        }
        for key, value in sorted(getattr(record, "__dict__", {}).items()):
            if key.startswith("_") or key in {"name", "msg", "args", "levelname", "levelno", "pathname", "lineno", "created"}:
                continue
            payload[key] = value
        return json.dumps(payload, ensure_ascii=False)


def configure() -> None:
    root = logging.getLogger()
    if root.handlers:
        return
    root.setLevel(logging.INFO)
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(JsonLogFormatter())
    root.addHandler(handler)
