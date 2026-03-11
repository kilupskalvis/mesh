"""Jira MCP tools for commenting and state checking."""

from __future__ import annotations

import logging
import os

from ..jira_client import jira_get, jira_post

logger = logging.getLogger(__name__)


async def jira_comment(args: dict) -> str:  # type: ignore[type-arg]
    """Post a comment on the current Jira issue.

    Args dict must contain:
        body (str): The comment text.
    """
    endpoint = os.environ["JIRA_ENDPOINT"]
    issue_id = os.environ["JIRA_ISSUE_ID"]
    body_text = args.get("body", "")

    # Jira ADF (Atlassian Document Format) body
    payload = {
        "body": {
            "version": 1,
            "type": "doc",
            "content": [
                {
                    "type": "paragraph",
                    "content": [{"type": "text", "text": body_text}],
                }
            ],
        }
    }

    status, data = await jira_post(
        endpoint=endpoint,
        path=f"/rest/api/3/issue/{issue_id}/comment",
        body=payload,
    )

    if status in (200, 201):
        return f"Comment posted (id: {data.get('id', 'unknown')})"
    text = data.get("text", str(data))
    return f"Failed to post comment: HTTP {status} — {text[:200]}"


async def jira_get_state(args: dict) -> str:  # type: ignore[type-arg]
    """Get the current state of the Jira issue."""
    endpoint = os.environ["JIRA_ENDPOINT"]
    issue_key = os.environ["JIRA_ISSUE_KEY"]

    status, data = await jira_get(
        endpoint=endpoint,
        path=f"/rest/api/3/issue/{issue_key}?fields=status",
    )

    if status == 200:
        state = data.get("fields", {}).get("status", {}).get("name", "unknown")
        return f"Issue {issue_key} is in state: {state}"
    text = data.get("text", str(data))
    return f"Failed to get state: HTTP {status} — {text[:200]}"
