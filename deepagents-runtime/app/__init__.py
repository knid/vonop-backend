"""Wemob deepagents runtime package."""

import os

__version__ = os.getenv("RUNTIME_VERSION", "0.1.0")
__all__ = ["create_app"]

from .main import create_app
