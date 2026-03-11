"""System prompt construction for Claude Agent SDK sessions."""

from __future__ import annotations

from .types import StdinPayload


def build_system_prompt(payload: StdinPayload) -> str:
    """Build the system prompt with workspace and issue context."""
    parts = [
        f"You are working on issue {payload.issue.identifier}: {payload.issue.title}.",
        f"Workspace: {payload.workspace}",
    ]
    if payload.issue.description:
        parts.append(f"Description: {payload.issue.description}")
    return "\n".join(parts)
