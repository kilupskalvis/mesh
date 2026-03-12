"""Tests for GitHub MCP tools."""

from __future__ import annotations

import asyncio
from unittest.mock import AsyncMock, patch

from mesh_agent.tools.github_tools import (
    github_comment,
    github_create_pr,
    github_get_state,
    github_push,
)

_ENV = {
    "GITHUB_ENDPOINT": "http://proxy:9481/github",
    "GITHUB_OWNER": "owner",
    "GITHUB_REPO": "repo",
    "GITHUB_ISSUE_NUMBER": "42",
}


def _mock_session(method: str, status: int, json_data: dict) -> AsyncMock:
    # Response object: async context manager that yields itself.
    mock_resp = AsyncMock()
    mock_resp.status = status
    mock_resp.json = AsyncMock(return_value=json_data)
    mock_resp.text = AsyncMock(return_value=str(json_data))
    mock_resp.__aenter__ = AsyncMock(return_value=mock_resp)
    mock_resp.__aexit__ = AsyncMock(return_value=False)

    # Session: the HTTP method must return a sync context-manager object
    # (not a coroutine), because aiohttp uses `async with session.get(...)`.
    mock_session = AsyncMock()
    from unittest.mock import MagicMock

    setattr(mock_session, method, MagicMock(return_value=mock_resp))
    mock_session.__aenter__ = AsyncMock(return_value=mock_session)
    mock_session.__aexit__ = AsyncMock(return_value=False)
    return mock_session


def test_github_get_state_success():
    mock_session = _mock_session("get", 200, {"state": "open", "labels": ["bug"]})

    with patch("mesh_agent.tools.github_tools.aiohttp.ClientSession", return_value=mock_session):
        with patch.dict("os.environ", _ENV):
            result = asyncio.run(github_get_state.handler({}))

    assert "open" in result["content"][0]["text"]


def test_github_comment_success():
    mock_session = _mock_session("post", 201, {"id": 123})

    with patch("mesh_agent.tools.github_tools.aiohttp.ClientSession", return_value=mock_session):
        with patch.dict("os.environ", _ENV):
            result = asyncio.run(github_comment.handler({"body": "Hello"}))

    assert "Comment posted" in result["content"][0]["text"]


def test_github_create_pr_success():
    mock_session = _mock_session(
        "post",
        201,
        {
            "number": 42,
            "html_url": "https://github.com/owner/repo/pull/42",
        },
    )

    with patch("mesh_agent.tools.github_tools.aiohttp.ClientSession", return_value=mock_session):
        with patch.dict("os.environ", _ENV):
            result = asyncio.run(
                github_create_pr.handler(
                    {
                        "title": "Fix bug",
                        "body": "Fixes #42",
                        "head": "fix-branch",
                    }
                )
            )

    assert "PR #42" in result["content"][0]["text"]


def test_github_push_success():
    mock_session = _mock_session("post", 200, {"status": "ok", "output": "pushed"})

    env = {**_ENV, "GITHUB_WORKSPACE": "/host/workspaces/branch-name"}
    with patch("mesh_agent.tools.github_tools.aiohttp.ClientSession", return_value=mock_session):
        with patch.dict("os.environ", env):
            result = asyncio.run(github_push.handler({"branch": "feature-branch"}))

    assert "pushed" in result["content"][0]["text"]
