"""Type definitions for the Mesh agent."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Optional


@dataclass
class AgentConfig:
    """Configuration for the agent session."""

    turn_timeout_ms: int = 3600000
    max_turns: int = 20
    model: str = "claude-sonnet-4-6"
    terminal_states: list[str] = field(default_factory=lambda: ["done", "cancelled"])


@dataclass
class IssueState:
    """Normalized issue data from the orchestrator."""

    id: str
    identifier: str
    title: str
    state: str
    description: Optional[str] = None
    priority: Optional[int] = None
    url: Optional[str] = None
    branch_name: Optional[str] = None
    labels: list[str] = field(default_factory=list)


@dataclass
class StdinPayload:
    """The JSON payload received on stdin from the Go orchestrator."""

    issue: IssueState
    prompt: str
    workspace: str
    config: AgentConfig
    attempt: Optional[int] = None
    system_prompt: str = ""


@dataclass
class IssueStateResult:
    """Result from checking Jira issue state."""

    status: str
    is_terminal: bool
    is_blocked: bool = False


@dataclass
class SessionResult:
    """Result from running the agent session."""

    turns_used: int = 0
    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0
    session_id: str = ""
