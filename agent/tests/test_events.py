"""Tests for the event emitter."""

from __future__ import annotations

import json
from io import StringIO
from unittest.mock import patch

from mesh_agent.events import EventEmitter


def test_emit_writes_json_line() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("test-session")
        emitter.emit("test_event", key="value")
        line = mock_stdout.getvalue().strip()
        data = json.loads(line)
        assert data["event"] == "test_event"
        assert data["session_id"] == "test-session"
        assert data["key"] == "value"
        assert "ts" in data


def test_session_started() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.session_started()
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "session_started"


def test_completed_with_tokens() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.completed(turns_used=5, input_tokens=100, output_tokens=50, total_tokens=150)
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "completed"
        assert data["turns_used"] == 5
        assert data["total_tokens"] == 150


def test_turn_completed_with_tokens() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.turn_completed(turn=3, message="done phase", input_tokens=1500, output_tokens=800)
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "turn_completed"
        assert data["turn"] == 3
        assert data["message"] == "done phase"
        assert data["input_tokens"] == 1500
        assert data["output_tokens"] == 800


def test_notification_event() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.notification("Reading codebase structure...")
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "notification"
        assert data["message"] == "Reading codebase structure..."


def test_usage_event() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.usage(input_tokens=1200, output_tokens=800, total_tokens=2000)
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "usage"
        assert data["input_tokens"] == 1200
        assert data["output_tokens"] == 800
        assert data["total_tokens"] == 2000


def test_error_event() -> None:
    with patch("sys.stdout", new_callable=StringIO) as mock_stdout:
        emitter = EventEmitter("s1")
        emitter.error("test_code", "test message")
        data = json.loads(mock_stdout.getvalue().strip())
        assert data["event"] == "error"
        assert data["code"] == "test_code"
        assert data["message"] == "test message"
