"""GitHub MCP tools for issue state, commenting, PR creation, and push."""

from __future__ import annotations

import json
import logging
import os

import aiohttp
from claude_agent_sdk import tool

logger = logging.getLogger(__name__)


def _headers() -> dict[str, str]:
    """Build request headers with issue context for the Go proxy."""
    return {
        "Content-Type": "application/json",
        "Accept": "application/json",
        "X-GitHub-Owner": os.environ.get("GITHUB_OWNER", ""),
        "X-GitHub-Repo": os.environ.get("GITHUB_REPO", ""),
        "X-GitHub-Issue": os.environ.get("GITHUB_ISSUE_NUMBER", ""),
    }


@tool("github_get_state", "Get current state of the GitHub issue", {})
async def github_get_state(args: dict) -> dict:
    """Get the current state of the GitHub issue via the proxy."""
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/state"

    async with aiohttp.ClientSession() as session:
        async with session.get(
            url, headers=_headers(), timeout=aiohttp.ClientTimeout(total=10)
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status == 200:
        state = data.get("state", "unknown")
        labels = data.get("labels", [])
        label_str = ", ".join(labels) if labels else "none"
        msg = f"Issue state: {state} (labels: {label_str})"
    else:
        msg = f"Failed to get state: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool("github_comment", "Post a comment on the current GitHub issue", {"body": str})
async def github_comment(args: dict) -> dict:
    """Post a comment on the GitHub issue via the proxy."""
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/comment"
    body_text = args.get("body", "")

    async with aiohttp.ClientSession() as session:
        async with session.post(
            url,
            headers=_headers(),
            json={"body": body_text},
            timeout=aiohttp.ClientTimeout(total=15),
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status in (200, 201):
        msg = f"Comment posted (id: {data.get('id', 'unknown')})"
    else:
        msg = f"Failed to post comment: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool(
    "github_create_pr",
    "Create a pull request for the current branch",
    {"title": str, "body": str, "head": str, "base": str},
)
async def github_create_pr(args: dict) -> dict:
    """Create a pull request via the proxy."""
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/create-pr"

    async with aiohttp.ClientSession() as session:
        async with session.post(
            url,
            headers=_headers(),
            json={
                "title": args.get("title", ""),
                "body": args.get("body", ""),
                "head": args.get("head", ""),
                "base": args.get("base", "main"),
            },
            timeout=aiohttp.ClientTimeout(total=15),
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status in (200, 201):
        number = data.get("number", "?")
        pr_url = data.get("html_url", "")
        msg = f"PR #{number} created: {pr_url}"
    else:
        msg = f"Failed to create PR: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool("github_get_labels", "Get current labels on the GitHub issue", {})
async def github_get_labels(args: dict) -> dict:
    """Get the current labels on the issue via the proxy."""
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/labels"

    async with aiohttp.ClientSession() as session:
        async with session.get(
            url, headers=_headers(), timeout=aiohttp.ClientTimeout(total=10)
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status == 200:
        labels = data.get("labels", [])
        label_str = ", ".join(labels) if labels else "none"
        msg = f"Labels: {label_str}"
    else:
        msg = f"Failed to get labels: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool(
    "github_set_labels",
    "Set mesh lifecycle labels on the GitHub issue",
    {"labels": list},
)
async def github_set_labels(args: dict) -> dict:
    """Set mesh-prefixed labels on the issue via the proxy."""
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/labels"
    labels = args.get("labels", [])
    if isinstance(labels, str):
        labels = json.loads(labels)

    async with aiohttp.ClientSession() as session:
        async with session.put(
            url,
            headers=_headers(),
            json={"labels": labels},
            timeout=aiohttp.ClientTimeout(total=10),
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status == 200:
        result_labels = data.get("labels") or labels
        msg = f"Labels set: {', '.join(str(lbl) for lbl in result_labels)}"
    else:
        msg = f"Failed to set labels: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}


@tool(
    "github_push",
    "Push the current branch to the remote repository",
    {"branch": str},
)
async def github_push(args: dict) -> dict:
    """Push a branch to origin via the host-side proxy.

    The proxy runs git push on the host with auth — the container never has a token.
    """
    endpoint = os.environ["GITHUB_ENDPOINT"]
    url = f"{endpoint}/push"

    headers = _headers()
    headers["X-GitHub-Workspace"] = os.environ.get("GITHUB_WORKSPACE", "")

    async with aiohttp.ClientSession() as session:
        async with session.post(
            url,
            headers=headers,
            json={"branch": args.get("branch", "")},
            timeout=aiohttp.ClientTimeout(total=60),
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}

    if resp.status == 200:
        msg = f"Branch pushed successfully: {data.get('output', '')}"
    else:
        msg = f"Failed to push: HTTP {resp.status} — {str(data)[:200]}"

    return {"content": [{"type": "text", "text": msg}]}
