"""Async issue state checker for between-phase polling."""

from __future__ import annotations

import asyncio
import json
import logging
import os

from .jira_client import jira_get
from .types import IssueStateResult

logger = logging.getLogger(__name__)


async def check_issue_state(
    terminal_states: list[str] | None = None,
) -> IssueStateResult:
    """Check the current issue state using the appropriate tracker.

    Detects tracker type from env vars:
    - GITHUB_ISSUE_NUMBER set → GitHub (via gh CLI)
    - JIRA_ENDPOINT set → Jira (via REST API)

    Failure is non-fatal — returns active (non-terminal) result so the agent continues.
    """
    gh_issue_number = os.environ.get("GITHUB_ISSUE_NUMBER")
    if gh_issue_number:
        return await _check_github_state(gh_issue_number, terminal_states)

    jira_endpoint = os.environ.get("JIRA_ENDPOINT")
    jira_issue_key = os.environ.get("JIRA_ISSUE_KEY", "")
    if jira_endpoint:
        return await check_jira_state(
            endpoint=jira_endpoint,
            issue_key=jira_issue_key,
            terminal_states=terminal_states,
        )

    logger.warning("No tracker env vars found (GITHUB_ISSUE_NUMBER or JIRA_ENDPOINT)")
    return IssueStateResult(status="unknown", is_terminal=False)


async def _check_github_state(
    issue_number: str,
    terminal_states: list[str] | None = None,
) -> IssueStateResult:
    """Check GitHub issue state via gh CLI."""
    repo = os.environ.get("GITHUB_REPO", "")
    try:
        proc = await asyncio.create_subprocess_exec(
            "gh", "issue", "view", issue_number,
            "--repo", repo,
            "--json", "state",
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await proc.communicate()

        if proc.returncode != 0:
            logger.warning(
                "gh issue view failed (exit %d): %s",
                proc.returncode,
                stderr.decode().strip(),
            )
            return IssueStateResult(status="unknown", is_terminal=False)

        data = json.loads(stdout.decode())
        state = data.get("state", "unknown").lower()
        states = terminal_states or []
        is_terminal = state in [s.lower() for s in states]

        return IssueStateResult(status=state, is_terminal=is_terminal)

    except Exception as e:
        logger.warning("GitHub state check failed for #%s: %s", issue_number, e)
        return IssueStateResult(status="unknown", is_terminal=False)


async def check_jira_state(
    endpoint: str,
    issue_key: str = "",
    terminal_states: list[str] | None = None,
) -> IssueStateResult:
    """Check the current state of a Jira issue.

    Failure is non-fatal — logs the error and returns an active (non-terminal)
    result so the agent continues. The Go orchestrator's reconciliation loop
    is the safety net for state changes.
    """
    try:
        status, data = await jira_get(
            endpoint=endpoint,
            path=f"/rest/api/3/issue/{issue_key}?fields=status",
        )
        if status != 200:
            logger.warning("Jira state check failed: HTTP %d for %s", status, issue_key)
            return IssueStateResult(status="unknown", is_terminal=False)
    except Exception as e:
        logger.warning("Jira state check failed for %s: %s", issue_key, e)
        return IssueStateResult(status="unknown", is_terminal=False)

    status_name = data.get("fields", {}).get("status", {}).get("name", "unknown")
    states = terminal_states or []
    is_terminal = status_name.lower() in [s.lower() for s in states]

    return IssueStateResult(status=status_name, is_terminal=is_terminal)
