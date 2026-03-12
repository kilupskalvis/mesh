"""Tests for stdin parsing."""

from __future__ import annotations

import json
from io import StringIO
from unittest.mock import MagicMock, patch

from mesh_agent.main import parse_stdin


def test_parse_stdin_valid() -> None:
    payload = {
        "issue": {
            "id": "10042",
            "identifier": "PROJ-123",
            "title": "Fix login",
            "state": "To Do",
        },
        "prompt": "Fix this issue",
        "workspace": "/workspaces/issue-123-fix-login",
        "config": {
            "turn_timeout_ms": 60000,
            "max_turns": 10,
        },
    }
    with patch("sys.stdin", StringIO(json.dumps(payload))):
        result = parse_stdin()
        assert result.issue.identifier == "PROJ-123"
        assert result.prompt == "Fix this issue"
        assert result.config.max_turns == 10
        assert result.attempt is None


def test_parse_stdin_with_attempt() -> None:
    payload = {
        "issue": {
            "id": "1",
            "identifier": "X-1",
            "title": "T",
            "state": "S",
        },
        "prompt": "p",
        "workspace": "/w",
        "attempt": 3,
    }
    with patch("sys.stdin", StringIO(json.dumps(payload))):
        result = parse_stdin()
        assert result.attempt == 3


def test_parse_stdin_invalid_json() -> None:
    with patch("sys.stdin", StringIO("not json")):
        try:
            parse_stdin()
            assert False, "Should have raised"
        except json.JSONDecodeError:
            pass


def test_parse_stdin_with_model_and_terminal_states() -> None:
    payload = {
        "issue": {
            "id": "10042",
            "identifier": "PROJ-123",
            "title": "Fix login",
            "state": "To Do",
        },
        "prompt": "Fix this issue",
        "workspace": "/workspaces/issue-123-fix-login",
        "config": {
            "turn_timeout_ms": 60000,
            "max_turns": 10,
            "model": "claude-opus-4-6",
            "terminal_states": ["done", "wontfix"],
        },
    }
    with patch("sys.stdin", StringIO(json.dumps(payload))):
        result = parse_stdin()
        assert result.config.model == "claude-opus-4-6"
        assert result.config.terminal_states == ["done", "wontfix"]


def test_parse_stdin_config_defaults() -> None:
    payload = {
        "issue": {
            "id": "1",
            "identifier": "X-1",
            "title": "T",
            "state": "S",
        },
        "prompt": "p",
        "workspace": "/w",
        "config": {},
    }
    with patch("sys.stdin", StringIO(json.dumps(payload))):
        result = parse_stdin()
        assert result.config.model == "claude-sonnet-4-6"
        assert result.config.terminal_states == ["done", "cancelled"]


def test_run_calls_session() -> None:
    """run() should call asyncio.run(run_session(...)) and return 0 on success."""
    payload = {
        "issue": {
            "id": "1",
            "identifier": "PROJ-1",
            "title": "Fix",
            "state": "To Do",
        },
        "prompt": "Fix it",
        "workspace": "/w",
        "config": {
            "model": "claude-sonnet-4-6",
            "terminal_states": ["done"],
        },
    }

    mock_result = MagicMock(turns_used=2)

    with patch("sys.stdin", StringIO(json.dumps(payload))):
        with patch("sys.stdout", new_callable=StringIO):
            with patch("mesh_agent.main.asyncio") as mock_asyncio:
                with patch("mesh_agent.main.run_session"):
                    mock_asyncio.run.return_value = mock_result
                    from mesh_agent.main import run

                    exit_code = run()

    assert exit_code == 0
    mock_asyncio.run.assert_called_once()
