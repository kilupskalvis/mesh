"""Tests for the simplified session (single query/response, no phase loop)."""

from __future__ import annotations

import asyncio
from io import StringIO
from typing import Any
from unittest.mock import MagicMock, patch


from mesh_agent.events import EventEmitter
from mesh_agent.session import run_session
from mesh_agent.types import AgentConfig, IssueState, SessionResult, StdinPayload


def _make_payload(**overrides: Any) -> StdinPayload:
    defaults: dict[str, Any] = {
        "issue": IssueState(id="1", identifier="PROJ-1", title="Fix login", state="To Do"),
        "prompt": "Fix the login bug",
        "workspace": "/workspaces/issue-10042-fix-login",
        "config": AgentConfig(max_turns=20),
        "system_prompt": "You are Mesh Agent.",
    }
    defaults.update(overrides)
    return StdinPayload(**defaults)


def _make_emitter() -> EventEmitter:
    emitter = EventEmitter("test-session-id")
    emitter.session_started = MagicMock()
    emitter.turn_started = MagicMock()
    emitter.turn_completed = MagicMock()
    emitter.usage = MagicMock()
    emitter.completed = MagicMock()
    emitter.error = MagicMock()
    return emitter


class FakeTextBlock:
    def __init__(self, text: str) -> None:
        self.text = text
        self.type = "text"


class FakeAssistantMessage:
    def __init__(self, text: str = "Done") -> None:
        self.content = [FakeTextBlock(text)]


class FakeResultMessage:
    def __init__(
        self,
        subtype: str = "success",
        session_id: str = "sid-1",
    ) -> None:
        self.subtype = subtype
        self.is_error = subtype.startswith("error")
        self.session_id = session_id
        self.usage = {"input_tokens": 100, "output_tokens": 50}


class FakeClient:
    """Simulates ClaudeSDKClient for testing."""

    def __init__(self, responses: list[Any] | None = None) -> None:
        self._responses = responses or [
            FakeAssistantMessage("I fixed it"),
            FakeResultMessage("success", "sid-1"),
        ]
        self.queries: list[str] = []

    async def query(self, prompt: str) -> None:
        self.queries.append(prompt)

    async def receive_response(self):  # type: ignore[no-untyped-def]
        for msg in self._responses:
            yield msg

    async def __aenter__(self) -> "FakeClient":
        return self

    async def __aexit__(self, *args: Any) -> None:
        pass


def _run(payload: StdinPayload, emitter: EventEmitter, client: FakeClient) -> SessionResult:
    with patch("sys.stdout", new_callable=StringIO):
        with patch("mesh_agent.session.ClaudeSDKClient", return_value=client):
            with patch("mesh_agent.session.AssistantMessage", FakeAssistantMessage):
                with patch("mesh_agent.session.ResultMessage", FakeResultMessage):
                    with patch("mesh_agent.session.TextBlock", FakeTextBlock):
                        with patch(
                            "mesh_agent.session.build_sdk_options", return_value=MagicMock()
                        ):
                            return asyncio.run(run_session(payload, emitter))


def test_session_single_query_response() -> None:
    """Session should call query once, iterate receive_response, and return."""
    payload = _make_payload()
    emitter = _make_emitter()
    client = FakeClient()

    result = _run(payload, emitter, client)

    assert client.queries == ["Fix the login bug"]
    emitter.session_started.assert_called_once()
    emitter.turn_started.assert_called_once_with(turn=1)
    emitter.turn_completed.assert_called_once()
    emitter.completed.assert_called_once()
    assert result.session_id == "sid-1"
    assert result.input_tokens == 100
    assert result.output_tokens == 50


def test_session_no_result_message() -> None:
    """Session should emit error if no ResultMessage is received."""
    payload = _make_payload()
    emitter = _make_emitter()
    client = FakeClient([FakeAssistantMessage("Working...")])

    result = _run(payload, emitter, client)

    emitter.error.assert_called_once_with("session_error", "No ResultMessage received from SDK")
    assert result.turns_used == 1


def test_session_error_result() -> None:
    """Session should emit error event on error result."""
    payload = _make_payload()
    emitter = _make_emitter()
    client = FakeClient(
        [
            FakeAssistantMessage("Something went wrong"),
            FakeResultMessage("error", "sid-1"),
        ]
    )

    result = _run(payload, emitter, client)

    emitter.error.assert_called_once()
    assert result.turns_used == 1
