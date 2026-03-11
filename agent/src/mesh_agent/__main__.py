"""Allow running as `python -m mesh_agent`."""

from __future__ import annotations

import sys

from .main import run

sys.exit(run())
