"""Test configuration — mock external SDK modules."""

from __future__ import annotations

import sys
from unittest.mock import MagicMock

# Mock claude_agent_sdk before any test file imports session.py.
# This allows tests to import from mesh_agent.session without having
# the real claude-agent-sdk package installed.
# Individual tests patch specific classes (ClaudeSDKClient, etc.) as needed.
_sdk_mock = MagicMock()
sys.modules["claude_agent_sdk"] = _sdk_mock
