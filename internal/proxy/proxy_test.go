package proxy

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderConfig_ContainsAllUpstreams(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ListenPort:   9480,
		ClaudeAPIKey: "sk-ant-test-key",
		JiraEndpoint: "https://test.atlassian.net",
		JiraEmail:    "user@test.com",
		JiraAPIToken: "jira-secret",
		PidFile:      "/tmp/mesh-proxy.pid",
		ErrorLog:     "/tmp/mesh-proxy-error.log",
	}

	result, err := RenderConfig(cfg)
	require.NoError(t, err)
	assert.Contains(t, result, "listen 9480")
	assert.Contains(t, result, "sk-ant-test-key")
	assert.Contains(t, result, "proxy_pass https://api.anthropic.com/v1/")
	assert.Contains(t, result, "proxy_pass https://test.atlassian.net/")
	// Basic auth should be base64(email:token)
	assert.Contains(t, result, "Basic ")
	assert.NotContains(t, result, "jira-secret") // raw token should not appear — only base64
}

func TestRenderConfig_OmitsSentryWhenEmpty(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ListenPort:   9480,
		ClaudeAPIKey: "sk-ant-test-key",
		JiraEndpoint: "https://test.atlassian.net",
		JiraEmail:    "user@test.com",
		JiraAPIToken: "jira-secret",
		PidFile:      "/tmp/mesh-proxy.pid",
		ErrorLog:     "/tmp/mesh-proxy-error.log",
		SentryDSN:    "",
	}

	result, err := RenderConfig(cfg)
	require.NoError(t, err)
	assert.NotContains(t, result, "/sentry/")
}

func TestRenderConfig_IncludesSentryWhenSet(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		ListenPort:      9480,
		ClaudeAPIKey:    "sk-ant-test-key",
		JiraEndpoint:    "https://test.atlassian.net",
		JiraEmail:       "user@test.com",
		JiraAPIToken:    "jira-secret",
		PidFile:         "/tmp/mesh-proxy.pid",
		ErrorLog:        "/tmp/mesh-proxy-error.log",
		SentryDSN:       "https://abc@o123.ingest.sentry.io/456",
		SentryIngestURL: "https://o123.ingest.sentry.io",
	}

	result, err := RenderConfig(cfg)
	require.NoError(t, err)
	assert.Contains(t, result, "/sentry/")
	assert.Contains(t, result, "https://o123.ingest.sentry.io")
}

func TestProxy_StartStop(t *testing.T) {
	// Skip if nginx is not installed.
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("nginx not installed")
	}

	tmpDir := t.TempDir()
	cfg := &Config{
		ListenPort:   9481, // Use a non-standard port to avoid conflicts
		ClaudeAPIKey: "sk-ant-test",
		JiraEndpoint: "https://test.atlassian.net",
		JiraEmail:    "user@test.com",
		JiraAPIToken: "token",
		PidFile:      filepath.Join(tmpDir, "proxy.pid"),
		ErrorLog:     filepath.Join(tmpDir, "error.log"),
	}

	p, err := New(cfg)
	require.NoError(t, err)

	err = p.Start()
	require.NoError(t, err)

	// Health check should respond.
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", p.Port()))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	err = p.Stop()
	assert.NoError(t, err)
}
