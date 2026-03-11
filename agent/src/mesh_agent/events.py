"""JSON line event emitter for stdout communication with Go orchestrator."""

from __future__ import annotations

import json
import sys
from datetime import datetime, timezone


class EventEmitter:
    """Emits JSON line events to stdout."""

    def __init__(self, session_id: str) -> None:
        self.session_id = session_id

    def emit(self, event: str, **kwargs: object) -> None:
        """Emit a single JSON line event to stdout."""
        payload = {
            "event": event,
            "ts": datetime.now(timezone.utc).isoformat(),
            "session_id": self.session_id,
            **kwargs,
        }
        line = json.dumps(payload, default=str)
        sys.stdout.write(line + "\n")
        sys.stdout.flush()

    def session_started(self) -> None:
        self.emit("session_started")

    def turn_started(self, turn: int) -> None:
        self.emit("turn_started", turn=turn)

    def turn_completed(
        self, turn: int, message: str = "", input_tokens: int = 0, output_tokens: int = 0
    ) -> None:
        self.emit(
            "turn_completed",
            turn=turn,
            message=message,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
        )

    def completed(
        self, turns_used: int, input_tokens: int = 0, output_tokens: int = 0, total_tokens: int = 0
    ) -> None:
        self.emit(
            "completed",
            turns_used=turns_used,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            total_tokens=total_tokens,
        )

    def notification(self, message: str) -> None:
        """Emit a human-readable status update."""
        self.emit("notification", message=message)

    def usage(self, input_tokens: int, output_tokens: int, total_tokens: int) -> None:
        """Emit a token consumption update."""
        self.emit(
            "usage",
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            total_tokens=total_tokens,
        )

    def error(self, code: str, message: str) -> None:
        self.emit("error", code=code, message=message)
