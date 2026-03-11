package proxy

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"text/template"
	"time"

	_ "embed"
)

//go:embed config.tmpl
var configTemplate string

// Config holds the parameters for generating the nginx config.
type Config struct {
	ListenPort      int
	ClaudeAPIKey    string
	JiraEndpoint    string
	JiraEmail       string
	JiraAPIToken    string
	SentryDSN       string
	SentryIngestURL string
	PidFile         string
	ErrorLog        string
}

// jiraAuthBase64 returns the base64-encoded Basic auth value for Jira.
func (c *Config) jiraAuthBase64() string {
	raw := fmt.Sprintf("%s:%s", c.JiraEmail, c.JiraAPIToken)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// RenderConfig renders the nginx config template with the given parameters.
func RenderConfig(cfg *Config) (string, error) {
	tmpl, err := template.New("nginx").Parse(configTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing nginx config template: %w", err)
	}

	data := struct {
		*Config
		JiraAuthBase64 string
	}{
		Config:         cfg,
		JiraAuthBase64: cfg.jiraAuthBase64(),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering nginx config: %w", err)
	}

	return buf.String(), nil
}

// Proxy manages the nginx reverse proxy lifecycle.
type Proxy struct {
	cfg        *Config
	cmd        *exec.Cmd
	configPath string
	port       int
}

// New creates a Proxy instance and writes the nginx config to a temp file.
func New(cfg *Config) (*Proxy, error) {
	rendered, err := RenderConfig(cfg)
	if err != nil {
		return nil, err
	}

	f, err := os.CreateTemp("", "mesh-proxy-*.conf")
	if err != nil {
		return nil, fmt.Errorf("creating temp config: %w", err)
	}
	if _, err := f.WriteString(rendered); err != nil {
		f.Close()
		return nil, fmt.Errorf("writing config: %w", err)
	}
	f.Close()

	return &Proxy{
		cfg:        cfg,
		configPath: f.Name(),
		port:       cfg.ListenPort,
	}, nil
}

// Start launches the nginx process.
func (p *Proxy) Start() error {
	p.cmd = exec.Command("nginx", "-c", p.configPath)
	p.cmd.Stdout = os.Stderr
	p.cmd.Stderr = os.Stderr

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("starting nginx: %w", err)
	}

	// Wait for health check.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", p.port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	return fmt.Errorf("nginx did not become healthy within 5s")
}

// Stop sends SIGINT to nginx for graceful shutdown.
func (p *Proxy) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		return p.cmd.Process.Kill()
	}
	_ = p.cmd.Wait()
	os.Remove(p.configPath)
	return nil
}

// Port returns the port the proxy is listening on.
func (p *Proxy) Port() int {
	return p.port
}
