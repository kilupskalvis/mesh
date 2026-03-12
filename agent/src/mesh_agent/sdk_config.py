"""Claude Agent SDK configuration — tools and options."""

from __future__ import annotations

import os

from claude_agent_sdk import ClaudeAgentOptions, create_sdk_mcp_server

from .types import StdinPayload


def build_sdk_options(payload: StdinPayload) -> ClaudeAgentOptions:
    """Build the full ClaudeAgentOptions for a session."""
    allowed_tools = ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
    mcp_servers: dict = {}

    # Jira tracker: register Jira MCP tools.
    if os.environ.get("JIRA_ENDPOINT"):
        from .tools.jira_tools import jira_comment, jira_get_state

        jira_mcp = create_sdk_mcp_server("jira", tools=[jira_comment, jira_get_state])
        mcp_servers["jira"] = jira_mcp
        allowed_tools.extend(["mcp__jira__jira_comment", "mcp__jira__jira_get_state"])

    # GitHub tracker: register GitHub MCP tools.
    if os.environ.get("GITHUB_ENDPOINT"):
        from .tools.github_tools import (
            github_comment,
            github_create_pr,
            github_get_state,
            github_push,
        )

        github_mcp = create_sdk_mcp_server(
            "github", tools=[github_get_state, github_comment, github_create_pr, github_push]
        )
        mcp_servers["github"] = github_mcp
        allowed_tools.extend([
            "mcp__github__github_get_state",
            "mcp__github__github_comment",
            "mcp__github__github_create_pr",
            "mcp__github__github_push",
        ])

    opts = ClaudeAgentOptions(
        allowed_tools=allowed_tools,
        permission_mode="bypassPermissions",
        cwd=payload.workspace,
        model=payload.config.model,
        system_prompt=payload.system_prompt,
        max_turns=payload.config.max_turns,
    )

    if mcp_servers:
        opts.mcp_servers = mcp_servers

    return opts
