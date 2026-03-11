"""Claude Agent SDK configuration — MCP servers, plugins, options."""

from __future__ import annotations

import os

from claude_agent_sdk import ClaudeAgentOptions, create_sdk_mcp_server

from .prompt import build_system_prompt
from .tools.jira_tools import jira_comment, jira_get_state
from .types import StdinPayload


def build_mcp_server():  # type: ignore[no-untyped-def]
    """Create the Jira MCP server from tool functions."""
    tools = {
        "comment": {
            "description": "Post a comment on the current Jira issue",
            "parameters": {"body": {"type": "string", "description": "Comment text"}},
            "handler": jira_comment,
        },
        "get_state": {
            "description": "Get current state of the Jira issue",
            "parameters": {},
            "handler": jira_get_state,
        },
    }
    return create_sdk_mcp_server("jira", tools=tools)  # pyright: ignore[reportArgumentType]


def build_plugins() -> list[dict[str, str]]:
    """Build LSP plugin configs if plugin dirs exist."""
    plugins: list[dict[str, str]] = []
    for plugin_name in ("pyright-lsp", "typescript-lsp"):
        path = f"/app/plugins/{plugin_name}"
        if os.path.isdir(path):
            plugins.append({"type": "local", "path": path})
    return plugins


def build_sdk_options(payload: StdinPayload) -> ClaudeAgentOptions:
    """Build the full ClaudeAgentOptions for a session."""
    jira_mcp = build_mcp_server()
    plugins = build_plugins()

    allowed_tools = [
        "Read", "Edit", "Write", "Bash", "Glob", "Grep",
        "mcp__jira__comment", "mcp__jira__get_state",
    ]
    if plugins:
        allowed_tools.append("LSP")

    return ClaudeAgentOptions(
        tools={"type": "preset", "preset": "claude_code"},
        allowed_tools=allowed_tools,
        mcp_servers={"jira": jira_mcp},
        plugins=plugins,  # pyright: ignore[reportArgumentType]
        permission_mode="bypassPermissions",
        cwd=payload.workspace,
        model=payload.config.model,
        system_prompt=build_system_prompt(payload),
        max_turns=200,  # Per-phase SDK safety net
    )
