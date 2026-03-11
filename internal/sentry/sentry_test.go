package sentry

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_EmptyDSN_ReturnsNoOpClient(t *testing.T) {
	c, err := Init("")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.False(t, c.enabled, "client should be disabled when DSN is empty")
}

func TestInit_InvalidDSN_ReturnsError(t *testing.T) {
	_, err := Init("not-a-valid-dsn")
	assert.Error(t, err, "invalid DSN should produce an error")
}

func TestCaptureError_NoOp_NilClient(t *testing.T) {
	c, err := Init("")
	require.NoError(t, err)

	// Should not panic on a no-op client.
	assert.NotPanics(t, func() {
		c.CaptureError(errors.New("test"), map[string]string{"key": "val"})
	})
}

func TestCaptureError_NilError(t *testing.T) {
	c, err := Init("")
	require.NoError(t, err)

	// Passing nil error should be a safe no-op.
	assert.NotPanics(t, func() {
		c.CaptureError(nil, nil)
	})
}

func TestFlush_NoOp(t *testing.T) {
	c, err := Init("")
	require.NoError(t, err)

	// Flush on a no-op client should not panic.
	assert.NotPanics(t, func() {
		c.Flush(2 * time.Second)
	})
}
