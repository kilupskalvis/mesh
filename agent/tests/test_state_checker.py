"""Tests for the Jira state checker."""

from __future__ import annotations

import asyncio
import json
from http.server import HTTPServer, BaseHTTPRequestHandler
from threading import Thread

from mesh_agent.state_checker import check_jira_state


class MockJiraHandler(BaseHTTPRequestHandler):
    """Minimal mock Jira for state checker tests."""

    response_status: str = "In Progress"
    should_fail: bool = False

    def do_GET(self) -> None:
        if self.should_fail:
            self.send_error(500, "Server Error")
            return
        if "/rest/api/3/issue/" in self.path:
            body = json.dumps({"fields": {"status": {"name": self.response_status}}})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body.encode())
        else:
            self.send_error(404)

    def log_message(self, format: str, *args: object) -> None:
        pass  # Suppress request logs in tests


def _run_async(coro):  # type: ignore[no-untyped-def]
    loop = asyncio.new_event_loop()
    try:
        return loop.run_until_complete(coro)
    finally:
        loop.close()


def test_check_jira_state_active() -> None:
    MockJiraHandler.response_status = "In Progress"
    MockJiraHandler.should_fail = False
    server = HTTPServer(("127.0.0.1", 0), MockJiraHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    result = _run_async(
        check_jira_state(
            endpoint=f"http://127.0.0.1:{port}",
            issue_key="PROJ-1",
            terminal_states=["done", "cancelled"],
        )
    )
    assert result.status == "In Progress"
    assert result.is_terminal is False
    server.server_close()


def test_check_jira_state_terminal() -> None:
    MockJiraHandler.response_status = "Done"
    MockJiraHandler.should_fail = False
    server = HTTPServer(("127.0.0.1", 0), MockJiraHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    result = _run_async(
        check_jira_state(
            endpoint=f"http://127.0.0.1:{port}",
            issue_key="PROJ-1",
            terminal_states=["done", "cancelled"],
        )
    )
    assert result.status == "Done"
    assert result.is_terminal is True
    server.server_close()


def test_check_jira_state_failure_non_fatal() -> None:
    MockJiraHandler.should_fail = True
    server = HTTPServer(("127.0.0.1", 0), MockJiraHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    result = _run_async(
        check_jira_state(
            endpoint=f"http://127.0.0.1:{port}",
            issue_key="PROJ-1",
            terminal_states=["done", "cancelled"],
        )
    )
    # Failure is non-fatal — returns active status
    assert result.is_terminal is False
    assert result.is_blocked is False
    server.server_close()


def test_check_jira_state_unreachable_non_fatal() -> None:
    result = _run_async(
        check_jira_state(
            endpoint="http://127.0.0.1:1",  # nothing listening
            issue_key="PROJ-1",
            terminal_states=["done"],
        )
    )
    assert result.is_terminal is False
