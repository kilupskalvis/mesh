package tracker

import (
	"fmt"
	"sync"
	"time"

	"github.com/kalvis/mesh/internal/proxy"
)

// GitHubTokenManager mints and caches GitHub App installation tokens.
// Tokens are refreshed 5 minutes before expiry.
type GitHubTokenManager struct {
	appID          string
	installationID string
	privateKeyPEM  []byte

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewGitHubTokenManager creates a token manager from GitHub App credentials.
func NewGitHubTokenManager(appID, installationID string, privateKeyPEM []byte) *GitHubTokenManager {
	return &GitHubTokenManager{
		appID:          appID,
		installationID: installationID,
		privateKeyPEM:  privateKeyPEM,
	}
}

// Token returns a valid installation token, minting a new one if needed.
func (m *GitHubTokenManager) Token() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token != "" && time.Now().Before(m.expiresAt) {
		return m.token, nil
	}

	token, err := proxy.MintInstallationToken(
		"https://api.github.com",
		m.appID,
		m.installationID,
		m.privateKeyPEM,
	)
	if err != nil {
		return "", fmt.Errorf("minting GitHub installation token: %w", err)
	}

	m.token = token
	m.expiresAt = time.Now().Add(55 * time.Minute) // tokens last 1 hour, refresh early
	return m.token, nil
}
