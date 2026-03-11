"""Jira MCP tools for commenting and state checking."""

from __future__ import annotations

import logging
import os

from claude_agent_sdk import tool

from ..jira_client import jira_get, jira_post

logger = logging.getLogger(__name__)


@tool("jira_comment", "Post a comment on the current Jira issue", {"body": str})
async def jira_comment(args: dict) -> dict:
    """Post a comment on the current Jira issue."""
    endpoint = os.environ["JIRA_ENDPOINT"]
    issue_id = os.environ["JIRA_ISSUE_ID"]
    body_text = args.get("body", "")

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
        msg = f"Comment posted (id: {data.get('id', 'unknown')})"
    else:
        text = data.get("text", str(data))
        msg = f"Failed to post comment: HTTP {status} — {text[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool("jira_get_state", "Get current state of the Jira issue", {})
async def jira_get_state(args: dict) -> dict:
    """Get the current state of the Jira issue."""
    endpoint = os.environ["JIRA_ENDPOINT"]
    issue_key = os.environ["JIRA_ISSUE_KEY"]

    status, data = await jira_get(
        endpoint=endpoint,
        path=f"/rest/api/3/issue/{issue_key}?fields=status",
    )

    if status == 200:
        state = data.get("fields", {}).get("status", {}).get("name", "unknown")
        msg = f"Issue {issue_key} is in state: {state}"
    else:
        text = data.get("text", str(data))
        msg = f"Failed to get state: HTTP {status} — {text[:200]}"

    return {"content": [{"type": "text", "text": msg}]}
