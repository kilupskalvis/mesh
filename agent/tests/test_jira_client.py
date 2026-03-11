"""Tests for the shared Jira HTTP client."""

from __future__ import annotations

import asyncio
import json
from http.server import HTTPServer, BaseHTTPRequestHandler
from threading import Thread

from mesh_agent.jira_client import jira_get, jira_post


class MockHandler(BaseHTTPRequestHandler):
    """Minimal mock for Jira HTTP client tests."""

    def do_GET(self) -> None:
        body = json.dumps({"result": "ok"})
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body.encode())

    def do_POST(self) -> None:
        self.send_response(201)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"id": "1"}')

    def log_message(self, format: str, *args: object) -> None:
        pass


def _run_async(coro):  # type: ignore[no-untyped-def]
    loop = asyncio.new_event_loop()
    try:
        return loop.run_until_complete(coro)
    finally:
        loop.close()


def test_jira_get_returns_json() -> None:
    server = HTTPServer(("127.0.0.1", 0), MockHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    status, data = _run_async(
        jira_get(
            endpoint=f"http://127.0.0.1:{port}",
            path="/rest/api/3/issue/PROJ-1",
        )
    )
    assert status == 200
    assert data == {"result": "ok"}
    server.server_close()


def test_jira_get_no_auth_header() -> None:
    """No auth header is sent — proxy handles credentials."""

    class AuthCheckHandler(BaseHTTPRequestHandler):
        received_auth: str | None = None

        def do_GET(self) -> None:
            AuthCheckHandler.received_auth = self.headers.get("Authorization")
            body = json.dumps({"result": "proxy_ok"})
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(body.encode())

        def log_message(self, format: str, *args: object) -> None:
            pass

    server = HTTPServer(("127.0.0.1", 0), AuthCheckHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    status, data = _run_async(
        jira_get(
            endpoint=f"http://127.0.0.1:{port}",
            path="/rest/api/3/issue/PROJ-1",
        )
    )
    assert status == 200
    assert data == {"result": "proxy_ok"}
    assert AuthCheckHandler.received_auth is None, "No auth header should be sent"
    server.server_close()


def test_jira_post_no_auth_header() -> None:
    """No auth header on POST — proxy handles credentials."""

    class AuthCheckHandler(BaseHTTPRequestHandler):
        received_auth: str | None = None

        def do_POST(self) -> None:
            AuthCheckHandler.received_auth = self.headers.get("Authorization")
            self.send_response(201)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b'{"id": "1"}')

        def log_message(self, format: str, *args: object) -> None:
            pass

    server = HTTPServer(("127.0.0.1", 0), AuthCheckHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    status, data = _run_async(
        jira_post(
            endpoint=f"http://127.0.0.1:{port}",
            path="/rest/api/3/issue/10042/comment",
            body={"content": "hello"},
        )
    )
    assert status == 201
    assert AuthCheckHandler.received_auth is None, "No auth header should be sent"
    server.server_close()


def test_jira_post_returns_json() -> None:
    server = HTTPServer(("127.0.0.1", 0), MockHandler)
    port = server.server_address[1]
    t = Thread(target=server.handle_request, daemon=True)
    t.start()

    status, data = _run_async(
        jira_post(
            endpoint=f"http://127.0.0.1:{port}",
            path="/rest/api/3/issue/10042/comment",
            body={"content": "hello"},
        )
    )
    assert status == 201
    assert data == {"id": "1"}
    server.server_close()
