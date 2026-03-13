"""Claude Agent SDK session — single query/response cycle."""

from __future__ import annotations

import logging

from claude_agent_sdk import (
    AssistantMessage,
    ClaudeSDKClient,
    ResultMessage,
    TextBlock,
    ToolResultBlock,
    ToolUseBlock,
)

from .events import EventEmitter
from .sdk_config import build_sdk_options
from .types import SessionResult, StdinPayload

logger = logging.getLogger(__name__)


async def run_session(payload: StdinPayload, emitter: EventEmitter) -> SessionResult:
    """Run the Claude Agent SDK session as a single query/response cycle.

    The SDK handles all turns internally (up to max_turns tool-use cycles).
    The agent checks issue state by calling MCP tools when the system prompt
    tells it to — no outer phase loop needed.
    """
    options = build_sdk_options(payload)

    result: ResultMessage | None = None
    last_message = ""
    turn = 0
    prev_input_tokens = 0
    prev_output_tokens = 0

    async with ClaudeSDKClient(options=options) as client:
        emitter.session_started()
        await client.query(payload.prompt)

        async for message in client.receive_response():
            if isinstance(message, AssistantMessage):
                turn += 1
                emitter.turn_started(turn=turn)
                for block in message.content:
                    if isinstance(block, TextBlock):
                        last_message = block.text
                        logger.info("[turn %d] text: %s", turn, block.text[:300])
                    elif isinstance(block, ToolUseBlock):
                        input_str = str(block.input)[:200] if block.input else ""
                        logger.info("[turn %d] tool_use: %s(%s)", turn, block.name, input_str)
                    elif isinstance(block, ToolResultBlock):
                        content_str = str(block.content)[:200] if block.content else ""
                        logger.info("[turn %d] tool_result: %s", turn, content_str)
                    else:
                        logger.info("[turn %d] block: %s", turn, type(block).__name__)
            elif isinstance(message, ResultMessage):
                result = message
                logger.info("result: is_error=%s session=%s", result.is_error, result.session_id)

        # Guard: receive_response() completed without ResultMessage.
        if result is None:
            emitter.error("session_error", "No ResultMessage received from SDK")
            emitter.completed(
                turns_used=turn,
                input_tokens=prev_input_tokens,
                output_tokens=prev_output_tokens,
                total_tokens=prev_input_tokens + prev_output_tokens,
            )
            return SessionResult(turns_used=turn, session_id="")

        # Token tracking.
        usage = result.usage or {}
        prev_input_tokens = usage.get("input_tokens", 0)
        prev_output_tokens = usage.get("output_tokens", 0)

        emitter.turn_completed(
            turn=turn,
            message=last_message[:200],
            input_tokens=prev_input_tokens,
            output_tokens=prev_output_tokens,
        )
        emitter.usage(
            input_tokens=prev_input_tokens,
            output_tokens=prev_output_tokens,
            total_tokens=prev_input_tokens + prev_output_tokens,
        )

        if result.is_error:
            emitter.error("agent_error", last_message[:200])

        emitter.completed(
            turns_used=turn,
            input_tokens=prev_input_tokens,
            output_tokens=prev_output_tokens,
            total_tokens=prev_input_tokens + prev_output_tokens,
        )

    return SessionResult(
        turns_used=turn,
        input_tokens=prev_input_tokens,
        output_tokens=prev_output_tokens,
        total_tokens=prev_input_tokens + prev_output_tokens,
        session_id=result.session_id if result else "",
    )
