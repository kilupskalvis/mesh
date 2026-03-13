"""Claude Agent SDK configuration — tools and options."""

from __future__ import annotations

import os

from claude_agent_sdk import ClaudeAgentOptions, create_sdk_mcp_server
from claude_agent_sdk.types import SystemPromptPreset, ThinkingConfigAdaptive

from .types import StdinPayload


def build_sdk_options(payload: StdinPayload) -> ClaudeAgentOptions:
    """Build the full ClaudeAgentOptions for a session."""
    allowed_tools = ["Read", "Edit", "Write", "Bash", "Glob", "Grep"]
    mcp_servers: dict = {}

    if os.environ.get("JIRA_ENDPOINT"):
        from .tools.jira_tools import jira_comment, jira_get_state

        jira_mcp = create_sdk_mcp_server("jira", tools=[jira_comment, jira_get_state])
        mcp_servers["jira"] = jira_mcp
        allowed_tools.extend(["mcp__jira__jira_comment", "mcp__jira__jira_get_state"])

    if os.environ.get("GITHUB_ENDPOINT"):
        from .tools.github_tools import (
            github_comment,
            github_create_pr,
            github_get_labels,
            github_get_state,
            github_push,
            github_set_labels,
        )

        github_mcp = create_sdk_mcp_server(
            "github",
            tools=[
                github_get_state,
                github_comment,
                github_create_pr,
                github_push,
                github_get_labels,
                github_set_labels,
            ],
        )
        mcp_servers["github"] = github_mcp
        allowed_tools.extend(
            [
                "mcp__github__github_get_state",
                "mcp__github__github_comment",
                "mcp__github__github_create_pr",
                "mcp__github__github_push",
                "mcp__github__github_get_labels",
                "mcp__github__github_set_labels",
            ]
        )

    system_prompt: SystemPromptPreset | str
    if payload.system_prompt:
        system_prompt = SystemPromptPreset(
            type="preset", preset="claude_code", append=payload.system_prompt
        )
    else:
        system_prompt = SystemPromptPreset(type="preset", preset="claude_code")

    opts = ClaudeAgentOptions(
        allowed_tools=allowed_tools,
        permission_mode="bypassPermissions",
        cwd=payload.workspace,
        model=payload.config.model,
        system_prompt=system_prompt,
        max_turns=payload.config.max_turns,
        thinking=ThinkingConfigAdaptive(type="adaptive"),
    )

    if mcp_servers:
        opts.mcp_servers = mcp_servers

    return opts
