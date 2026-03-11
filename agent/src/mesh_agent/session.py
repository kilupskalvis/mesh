"""Claude Agent SDK session — multi-phase turn loop with issue state checking."""

from __future__ import annotations

import logging

from claude_agent_sdk import AssistantMessage, ClaudeSDKClient, ResultMessage, TextBlock

from .events import EventEmitter
from .sdk_config import build_sdk_options
from .state_checker import check_issue_state
from .types import SessionResult, StdinPayload

logger = logging.getLogger(__name__)


async def run_session(
    payload: StdinPayload, emitter: EventEmitter
) -> SessionResult:
    """Run the Claude Agent SDK session with multi-phase turn loop.

    Each phase = one client.query() → client.receive_response() cycle.
    Between phases, checks Jira issue state and provides continuation guidance.
    """
    options = build_sdk_options(payload)

    prev_input_tokens = 0
    prev_output_tokens = 0
    total_phases = 0
    max_phases = payload.config.max_turns

    result: ResultMessage | None = None

    async with ClaudeSDKClient(options=options) as client:
        emitter.session_started()
        current_prompt = payload.prompt

        gave_terminal_warning = False

        while total_phases < max_phases:
            total_phases += 1
            emitter.turn_started(turn=total_phases)

            await client.query(current_prompt)

            result = None
            last_message = ""

            async for message in client.receive_response():
                if isinstance(message, AssistantMessage):
                    for block in message.content:
                        if isinstance(block, TextBlock):
                            last_message = block.text
                elif isinstance(message, ResultMessage):
                    result = message

            # Guard: receive_response() completed without ResultMessage
            if result is None:
                emitter.error("session_error", "No ResultMessage received from SDK")
                break

            # Per-phase token deltas (ResultMessage.usage is cumulative)
            usage = result.usage or {}
            delta_in = usage.get("input_tokens", 0) - prev_input_tokens
            delta_out = usage.get("output_tokens", 0) - prev_output_tokens
            prev_input_tokens = usage.get("input_tokens", 0)
            prev_output_tokens = usage.get("output_tokens", 0)

            emitter.turn_completed(
                turn=total_phases,
                message=last_message[:200],
                input_tokens=delta_in,
                output_tokens=delta_out,
            )
            emitter.usage(
                input_tokens=prev_input_tokens,
                output_tokens=prev_output_tokens,
                total_tokens=prev_input_tokens + prev_output_tokens,
            )

            # Exit conditions
            if result.subtype == "success":
                break
            if result.subtype in ("error_max_turns", "error_max_budget_usd"):
                break
            if result.is_error:
                emitter.error("agent_error", last_message[:200])
                break

            # If we just ran the terminal wrap-up phase, exit now
            if gave_terminal_warning:
                break

            # STATE CHECK between phases
            issue_state = await check_issue_state(
                terminal_states=payload.config.terminal_states,
            )

            if issue_state.is_terminal:
                gave_terminal_warning = True
                current_prompt = (
                    payload.terminal_prompt or
                    "Issue {identifier} was moved to '{status}' (terminal). "
                    "Commit any work in progress."
                ).format(
                    identifier=payload.issue.identifier,
                    status=issue_state.status,
                )
                continue  # Exactly one graceful wrap-up phase, then break above
            if issue_state.is_blocked:
                break

            # CONTINUATION
            current_prompt = (
                payload.continuation_prompt or
                "Continue working on {identifier}. "
                "Review what you've done so far and proceed to the next step."
            ).format(identifier=payload.issue.identifier)

        emitter.completed(
            turns_used=total_phases,
            input_tokens=prev_input_tokens,
            output_tokens=prev_output_tokens,
            total_tokens=prev_input_tokens + prev_output_tokens,
        )

    return SessionResult(
        turns_used=total_phases,
        input_tokens=prev_input_tokens,
        output_tokens=prev_output_tokens,
        total_tokens=prev_input_tokens + prev_output_tokens,
        session_id=result.session_id if result else "",
    )
