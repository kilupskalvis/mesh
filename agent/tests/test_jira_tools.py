"""Tests for Jira MCP tools."""

from __future__ import annotations

import asyncio
import json
import os
from http.server import HTTPServer, BaseHTTPRequestHandler
from threading import Thread

from mesh_agent.tools.jira_tools import jira_comment, jira_get_state


class MockJiraToolHandler(BaseHTTPRequestHandler):
    """Mock Jira for tool tests."""

    last_comment_body: str | None = None

    def do_POST(self) -> None:
        if "/comment" in self.path:
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            content = body.get("body", {}).get("content", [{}])
            if content:
                inner = content[0].get("content", [{}])
                if inner:
                    MockJiraToolHandler.last_comment_body = inner[0].get("text")
            self.send_response(201)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"id": "1"}')
        else:
            self.send_error(404)

    def do_GET(self) -> None:
        if "/rest/api/3/issue/" in self.path:
            body = json.dumps({"fields": {"status": {"name": "In Progress"}}})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body.encode())
        else:
            self.send_error(404)

    def log_message(self, format: str, *args: object) -> None:
        pass


def _run_async(coro):  # type: ignore[no-untyped-def]
    loop = asyncio.new_event_loop()
    try:
        return loop.run_until_complete(coro)
    finally:
        loop.close()


def test_jira_comment() -> None:
    server = HTTPServer(("127.0.0.1", 0), MockJiraToolHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    os.environ["JIRA_ENDPOINT"] = f"http://127.0.0.1:{port}"
    os.environ["JIRA_ISSUE_ID"] = "10042"
    os.environ["JIRA_ISSUE_KEY"] = "PROJ-1"

    try:
        result = _run_async(jira_comment({"body": "Test comment"}))
        assert "1" in result  # comment id
    finally:
        for k in ["JIRA_ENDPOINT", "JIRA_ISSUE_ID", "JIRA_ISSUE_KEY"]:
            os.environ.pop(k, None)
    server.server_close()


def test_jira_get_state() -> None:
    server = HTTPServer(("127.0.0.1", 0), MockJiraToolHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    os.environ["JIRA_ENDPOINT"] = f"http://127.0.0.1:{port}"
    os.environ["JIRA_ISSUE_KEY"] = "PROJ-1"

    try:
        result = _run_async(jira_get_state({}))
        assert "In Progress" in result
    finally:
        for k in ["JIRA_ENDPOINT", "JIRA_ISSUE_KEY"]:
            os.environ.pop(k, None)
    server.server_close()
