"""Tests for SDK configuration and tool registration."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

from mesh_agent.sdk_config import build_sdk_options
from mesh_agent.types import AgentConfig, IssueState, StdinPayload


def _make_payload(**overrides) -> StdinPayload:
    defaults = dict(
        issue=IssueState(id="1", identifier="X-1", title="T", state="S"),
        prompt="test",
        workspace="/w",
        config=AgentConfig(),
        system_prompt="You are a test agent.",
    )
    defaults.update(overrides)
    return StdinPayload(**defaults)


def test_build_sdk_options_base_tools():
    """Should include base tools and pass system_prompt through."""
    with patch.dict("os.environ", {}, clear=True):
        opts = build_sdk_options(_make_payload())

    assert "Read" in opts.allowed_tools
    assert "Bash" in opts.allowed_tools
    assert opts.system_prompt == "You are a test agent."
    assert opts.permission_mode == "bypassPermissions"


def test_build_sdk_options_jira_tools():
    """Should register Jira MCP tools when JIRA_ENDPOINT is set."""
    env = {"JIRA_ENDPOINT": "http://proxy/jira", "JIRA_ISSUE_ID": "1", "JIRA_ISSUE_KEY": "X-1"}
    with patch.dict("os.environ", env, clear=True):
        with patch("mesh_agent.sdk_config.create_sdk_mcp_server") as mock_mcp:
            mock_mcp.return_value = MagicMock()
            opts = build_sdk_options(_make_payload())

    assert "mcp__jira__jira_comment" in opts.allowed_tools
    assert "mcp__jira__jira_get_state" in opts.allowed_tools
    mock_mcp.assert_called_once()


def test_build_sdk_options_github_tools():
    """Should register GitHub MCP tools when GITHUB_ENDPOINT is set."""
    env = {
        "GITHUB_ENDPOINT": "http://proxy/github",
        "GITHUB_OWNER": "o",
        "GITHUB_REPO": "r",
        "GITHUB_ISSUE_NUMBER": "1",
    }
    with patch.dict("os.environ", env, clear=True):
        with patch("mesh_agent.sdk_config.create_sdk_mcp_server") as mock_mcp:
            mock_mcp.return_value = MagicMock()
            opts = build_sdk_options(_make_payload())

    assert "mcp__github__github_get_state" in opts.allowed_tools
    assert "mcp__github__github_comment" in opts.allowed_tools
    assert "mcp__github__github_create_pr" in opts.allowed_tools
    assert "mcp__github__github_push" in opts.allowed_tools
    mock_mcp.assert_called_once()


def test_build_sdk_options_max_turns_from_config():
    """max_turns should come from payload config, not be hardcoded."""
    with patch.dict("os.environ", {}, clear=True):
        opts = build_sdk_options(_make_payload(config=AgentConfig(max_turns=50)))

    assert opts.max_turns == 50
