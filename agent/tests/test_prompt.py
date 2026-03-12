"""Tests for prompt building."""

from __future__ import annotations

from mesh_agent.prompt import build_system_prompt
from mesh_agent.types import AgentConfig, IssueState, StdinPayload


def test_build_system_prompt_basic() -> None:
    payload = StdinPayload(
        issue=IssueState(id="1", identifier="PROJ-1", title="Fix login", state="To Do"),
        prompt="Fix it",
        workspace="/workspaces/issue-1-fix-login",
        config=AgentConfig(),
    )
    result = build_system_prompt(payload)
    assert "PROJ-1" in result
    assert "Fix login" in result
    assert "/workspaces/issue-1-fix-login" in result


def test_build_system_prompt_with_description() -> None:
    payload = StdinPayload(
        issue=IssueState(
            id="1",
            identifier="PROJ-2",
            title="Add feature",
            state="In Progress",
            description="Detailed description here",
        ),
        prompt="Do it",
        workspace="/ws",
        config=AgentConfig(),
    )
    result = build_system_prompt(payload)
    assert "Detailed description here" in result


def test_build_system_prompt_no_description() -> None:
    payload = StdinPayload(
        issue=IssueState(id="1", identifier="PROJ-3", title="Bug", state="To Do"),
        prompt="Fix",
        workspace="/ws",
        config=AgentConfig(),
    )
    result = build_system_prompt(payload)
    assert "Description" not in result
