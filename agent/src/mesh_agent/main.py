"""Main entry point for the Mesh agent."""

from __future__ import annotations

import asyncio
import json
import os
import sys
import uuid
from typing import Any

import sentry_sdk

from .events import EventEmitter
from .session import run_session
from .types import AgentConfig, IssueState, StdinPayload


def parse_stdin() -> StdinPayload:
    """Read and parse JSON payload from stdin."""
    raw = sys.stdin.read()
    data: dict[str, Any] = json.loads(raw)

    issue_data = data["issue"]
    issue = IssueState(
        id=issue_data["id"],
        identifier=issue_data["identifier"],
        title=issue_data["title"],
        state=issue_data["state"],
        description=issue_data.get("description"),
        priority=issue_data.get("priority"),
        url=issue_data.get("url"),
        branch_name=issue_data.get("branch_name"),
        labels=issue_data.get("labels", []),
    )

    config_data = data.get("config", {})
    agent_config = AgentConfig(
        turn_timeout_ms=config_data.get("turn_timeout_ms", 3600000),
        max_turns=config_data.get("max_turns", 20),
        model=config_data.get("model", "claude-sonnet-4-6"),
        terminal_states=config_data.get("terminal_states", ["done", "cancelled"]),
    )

    return StdinPayload(
        issue=issue,
        prompt=data["prompt"],
        workspace=data["workspace"],
        config=agent_config,
        attempt=data.get("attempt"),
        system_prompt=data.get("system_prompt", ""),
    )


def run() -> int:
    """Main agent execution loop. Returns exit code."""
    # Initialize Sentry (no-op if SENTRY_DSN is empty/absent).
    dsn = os.environ.get("SENTRY_DSN", "")
    if dsn:
        sentry_sdk.init(dsn=dsn)

    session_id = str(uuid.uuid4())
    emitter = EventEmitter(session_id)

    try:
        payload = parse_stdin()
    except (json.JSONDecodeError, KeyError) as e:
        emitter.error("stdin_parse_error", str(e))
        return 1

    try:
        asyncio.run(run_session(payload, emitter))
        return 0
    except Exception as e:
        sentry_sdk.capture_exception(e)
        emitter.error("session_error", str(e))
        return 1


if __name__ == "__main__":
    sys.exit(run())
