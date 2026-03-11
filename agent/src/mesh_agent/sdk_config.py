"""Claude Agent SDK configuration — tools and options."""

from __future__ import annotations

import os

from claude_agent_sdk import ClaudeAgentOptions, create_sdk_mcp_server

from .prompt import build_system_prompt
from .types import StdinPayload


def build_sdk_options(payload: StdinPayload) -> ClaudeAgentOptions:
    """Build the full ClaudeAgentOptions for a session."""
    allowed_tools = ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
    mcp_servers: dict = {}

    # Jira tracker: register Jira MCP tools
    if os.environ.get("JIRA_ENDPOINT"):
        from .tools.jira_tools import jira_comment, jira_get_state

        jira_mcp = create_sdk_mcp_server("jira", tools=[jira_comment, jira_get_state])
        mcp_servers["jira"] = jira_mcp
        allowed_tools.extend(["mcp__jira__jira_comment", "mcp__jira__jira_get_state"])

    opts = ClaudeAgentOptions(
        allowed_tools=allowed_tools,
        permission_mode="bypassPermissions",
        cwd=payload.workspace,
        model=payload.config.model,
        system_prompt=build_system_prompt(payload),
        max_turns=200,
    )

    if mcp_servers:
        opts.mcp_servers = mcp_servers

    return opts
