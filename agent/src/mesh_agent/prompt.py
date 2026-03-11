"""System prompt assembly — three-layer architecture."""

from __future__ import annotations

import os
from pathlib import Path

from .types import StdinPayload

_BASE_PROMPT_PATH = os.environ.get(
    "MESH_BASE_PROMPT_PATH", "/app/prompts/base_system.md"
)


def _read_base_prompt() -> str:
    """Read the base system prompt from the Docker image."""
    path = Path(_BASE_PROMPT_PATH)
    if path.exists():
        return path.read_text().strip()
    return ""


def build_system_prompt(payload: StdinPayload) -> str:
    """Assemble the system prompt from three layers.

    Layer 1: Base agent identity (from file in Docker image)
    Layer 2: Workflow instructions (from orchestrator, tracker-kind default)
    Layer 3: Current task context (issue metadata)
    """
    parts: list[str] = []

    base = _read_base_prompt()
    if base:
        parts.append(base)

    if payload.system_prompt:
        parts.append(payload.system_prompt)

    context_lines = [
        "## Current Task",
        "",
        f"Issue: {payload.issue.identifier} — {payload.issue.title}",
        f"Workspace: {payload.workspace}",
    ]
    if payload.issue.description:
        context_lines.append(f"Description: {payload.issue.description}")
    parts.append("\n".join(context_lines))

    return "\n\n---\n\n".join(parts)
