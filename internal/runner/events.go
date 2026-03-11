// Package runner implements the Docker container runner for agent execution.
package runner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kalvis/mesh/internal/model"
)

// HumanizeEvent returns a short human-readable summary of an agent event.
// This is observability-only output and must not affect orchestrator logic.
func HumanizeEvent(ev model.AgentEvent) string {
	switch ev.Event {
	case "session_started":
		return "Session started"
	case "turn_started":
		if ev.Turn > 0 {
			return fmt.Sprintf("Turn %d started", ev.Turn)
		}
		return "Turn started"
	case "turn_completed":
		if ev.Turn > 0 {
			return fmt.Sprintf("Turn %d completed", ev.Turn)
		}
		return "Turn completed"
	case "completed":
		if ev.TurnsUsed > 0 {
			return fmt.Sprintf("Session completed (%d turns)", ev.TurnsUsed)
		}
		return "Session completed"
	case "notification":
		if ev.Message != "" {
			msg := ev.Message
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			return fmt.Sprintf("Agent: %s", msg)
		}
		return "Agent notification"
	case "usage":
		return fmt.Sprintf("Usage: %d in / %d out tokens", ev.InputTokens, ev.OutputTokens)
	case "error":
		if ev.Message != "" {
			msg := ev.Message
			if len(msg) > 80 {
				msg = msg[:77] + "..."
			}
			return fmt.Sprintf("Error: %s", msg)
		}
		return "Agent error"
	case "malformed":
		return "Malformed agent output"
	default:
		return fmt.Sprintf("Event: %s", ev.Event)
	}
}

// ParseEvent parses a single JSON line from the agent's stdout into an AgentEvent.
// Returns an error if the line is empty, not valid JSON, or missing the required "event" field.
func ParseEvent(line string) (model.AgentEvent, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return model.AgentEvent{}, fmt.Errorf("empty line")
	}

	var ev model.AgentEvent
	if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
		return model.AgentEvent{}, fmt.Errorf("invalid JSON: %w", err)
	}

	if ev.Event == "" {
		return model.AgentEvent{}, fmt.Errorf("missing required field: event")
	}

	return ev, nil
}
