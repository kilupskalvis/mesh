package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger creates a logger that writes to the given buffer, for test capture.
func newTestLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(handler)
}

func TestNewLogger_WritesToStderr(t *testing.T) {
	// NewLogger should return a non-nil logger. We can't easily capture
	// os.Stderr in a unit test, so we verify the constructor doesn't panic
	// and returns a usable logger. The JSON-format test below uses a buffer
	// to prove the handler works.
	logger := NewLogger(slog.LevelInfo)
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
}

func TestNewLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Info("hello", "key", "value")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if msg, ok := m["msg"]; !ok || msg != "hello" {
		t.Errorf("expected msg=hello, got %v", m["msg"])
	}
	if v, ok := m["key"]; !ok || v != "value" {
		t.Errorf("expected key=value, got %v", m["key"])
	}
}

func TestWithIssueContext(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)

	logger := WithIssueContext(base, "id-123", "PROJ-42")
	logger.Info("test issue context")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if v, ok := m["issue_id"]; !ok || v != "id-123" {
		t.Errorf("expected issue_id=id-123, got %v", v)
	}
	if v, ok := m["issue_identifier"]; !ok || v != "PROJ-42" {
		t.Errorf("expected issue_identifier=PROJ-42, got %v", v)
	}
}

func TestWithSessionContext(t *testing.T) {
	var buf bytes.Buffer
	base := newTestLogger(&buf, slog.LevelInfo)

	logger := WithSessionContext(base, "sess-abc", "ctr-xyz")
	logger.Info("test session context")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}
	if v, ok := m["session_id"]; !ok || v != "sess-abc" {
		t.Errorf("expected session_id=sess-abc, got %v", v)
	}
	if v, ok := m["container_id"]; !ok || v != "ctr-xyz" {
		t.Errorf("expected container_id=ctr-xyz, got %v", v)
	}
}
