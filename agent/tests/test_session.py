"""Tests for session.py — mocks the Claude Agent SDK."""

from __future__ import annotations

import asyncio
import os
from io import StringIO
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from mesh_agent.events import EventEmitter
from mesh_agent.types import AgentConfig, IssueState, SessionResult, StdinPayload
from mesh_agent.session import run_session


def _make_payload(**overrides: Any) -> StdinPayload:
    defaults: dict[str, Any] = {
        "issue": IssueState(
            id="10042",
            identifier="PROJ-1",
            title="Fix login",
            state="To Do",
        ),
        "prompt": "Fix the login bug",
        "workspace": "/workspace",
        "config": AgentConfig(max_turns=3, terminal_states=["done", "cancelled"]),
        "attempt": 1,
    }
    defaults.update(overrides)
    return StdinPayload(**defaults)


def _make_emitter() -> EventEmitter:
    return EventEmitter("test-session")


class FakeTextBlock:
    def __init__(self, text: str) -> None:
        self.text = text


class FakeAssistantMessage:
    def __init__(self, text: str) -> None:
        self.content = [FakeTextBlock(text)]


class FakeResultMessage:
    def __init__(
        self,
        subtype: str = "success",
        is_error: bool = False,
        usage: dict[str, int] | None = None,
        session_id: str = "sdk-session-1",
    ) -> None:
        self.subtype = subtype
        self.is_error = is_error
        self.usage = usage or {"input_tokens": 1000, "output_tokens": 500}
        self.session_id = session_id
        self.num_turns = 5
        self.total_cost_usd = 0.01


class FakeClient:
    """Simulates ClaudeSDKClient for testing."""

    def __init__(self, responses: list[list[Any]] | None = None) -> None:
        self._responses = responses or [
            [FakeAssistantMessage("I fixed it"), FakeResultMessage()],
        ]
        self._phase = 0
        self.queries: list[str] = []

    async def query(self, prompt: str) -> None:
        self.queries.append(prompt)

    async def receive_response(self):  # type: ignore[no-untyped-def]
        if self._phase < len(self._responses):
            for msg in self._responses[self._phase]:
                yield msg
            self._phase += 1
        else:
            yield FakeResultMessage()
            self._phase += 1

    async def __aenter__(self) -> "FakeClient":
        return self

    async def __aexit__(self, *args: Any) -> None:
        pass


@pytest.fixture()
def jira_env():  # type: ignore[no-untyped-def]
    """Set required Jira env vars for tests."""
    env_vars = {
        "JIRA_ENDPOINT": "http://localhost:9090",
        "JIRA_EMAIL": "test@test.com",
        "JIRA_API_TOKEN": "tok",
        "JIRA_PROJECT_KEY": "PROJ",
        "JIRA_ISSUE_ID": "10042",
        "JIRA_ISSUE_KEY": "PROJ-1",
    }
    for k, v in env_vars.items():
        os.environ[k] = v
    yield
    for k in env_vars:
        os.environ.pop(k, None)


def _run_session_with_client(
    payload: StdinPayload,
    emitter: EventEmitter,
    client: FakeClient,
    check_state: Any = None,
) -> SessionResult:
    """Helper to run session with mocked SDK types and optional state checker."""
    with patch("sys.stdout", new_callable=StringIO):
        with patch("mesh_agent.session.ClaudeSDKClient", return_value=client):
            with patch("mesh_agent.session.AssistantMessage", FakeAssistantMessage):
                with patch("mesh_agent.session.ResultMessage", FakeResultMessage):
                    with patch("mesh_agent.session.TextBlock", FakeTextBlock):
                        with patch("mesh_agent.session.build_sdk_options", return_value=MagicMock()):
                            if check_state is not None:
                                with patch(
                                    "mesh_agent.session.check_jira_state",
                                    return_value=check_state,
                                ):
                                    return asyncio.run(run_session(payload, emitter))
                            else:
                                return asyncio.run(run_session(payload, emitter))


def test_session_success_single_phase(jira_env: None) -> None:
    """Agent completes in one phase with subtype=success."""
    payload = _make_payload()
    emitter = _make_emitter()

    client = FakeClient(
        [
            [
                FakeAssistantMessage("Done fixing"),
                FakeResultMessage(
                    subtype="success", usage={"input_tokens": 1000, "output_tokens": 500}
                ),
            ],
        ]
    )

    result = _run_session_with_client(payload, emitter, client)

    assert isinstance(result, SessionResult)
    assert result.turns_used == 1
    assert result.input_tokens == 1000
    assert result.output_tokens == 500
    assert client.queries[0] == "Fix the login bug"


def test_session_multi_phase_continuation(jira_env: None) -> None:
    """Agent needs multiple phases before completing."""
    payload = _make_payload()
    emitter = _make_emitter()

    client = FakeClient(
        [
            [
                FakeAssistantMessage("Phase 1"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 500, "output_tokens": 200}
                ),
            ],
            [
                FakeAssistantMessage("Phase 2"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 1200, "output_tokens": 600}
                ),
            ],
            [
                FakeAssistantMessage("Done"),
                FakeResultMessage(
                    subtype="success", usage={"input_tokens": 2000, "output_tokens": 1000}
                ),
            ],
        ]
    )

    active_state = MagicMock(is_terminal=False, is_blocked=False, status="In Progress")
    result = _run_session_with_client(payload, emitter, client, check_state=active_state)

    assert result.turns_used == 3
    assert result.input_tokens == 2000
    assert result.output_tokens == 1000
    assert client.queries[0] == "Fix the login bug"
    assert "Continue" in client.queries[1]


def test_session_terminal_state_graceful_exit(jira_env: None) -> None:
    """When Jira state becomes terminal, agent gets one wrap-up phase then exits."""
    payload = _make_payload()
    emitter = _make_emitter()

    client = FakeClient(
        [
            [
                FakeAssistantMessage("Phase 1"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 500, "output_tokens": 200}
                ),
            ],
            [
                FakeAssistantMessage("Committing"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 1000, "output_tokens": 400}
                ),
            ],
        ]
    )

    terminal_state = MagicMock(is_terminal=True, is_blocked=False, status="Done")
    result = _run_session_with_client(payload, emitter, client, check_state=terminal_state)

    assert result.turns_used == 2
    assert "terminal" in client.queries[1].lower() or "Done" in client.queries[1]


def test_session_max_phases_cap(jira_env: None) -> None:
    """Session stops when max_turns (phases) is reached."""
    payload = _make_payload(config=AgentConfig(max_turns=2, terminal_states=["done"]))
    emitter = _make_emitter()

    client = FakeClient(
        [
            [
                FakeAssistantMessage("Phase 1"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 500, "output_tokens": 200}
                ),
            ],
            [
                FakeAssistantMessage("Phase 2"),
                FakeResultMessage(
                    subtype="end_turn", usage={"input_tokens": 1000, "output_tokens": 400}
                ),
            ],
            [
                FakeAssistantMessage("Phase 3"),
                FakeResultMessage(
                    subtype="success", usage={"input_tokens": 1500, "output_tokens": 600}
                ),
            ],
        ]
    )

    active_state = MagicMock(is_terminal=False, is_blocked=False, status="In Progress")
    result = _run_session_with_client(payload, emitter, client, check_state=active_state)

    assert result.turns_used == 2


def test_session_no_result_message(jira_env: None) -> None:
    """When receive_response() yields no ResultMessage (connection drop)."""
    payload = _make_payload()
    emitter = _make_emitter()

    client = FakeClient(
        [
            [FakeAssistantMessage("Started...")],  # No ResultMessage
        ]
    )

    result = _run_session_with_client(payload, emitter, client)

    assert result.turns_used == 1
    assert result.session_id == ""
