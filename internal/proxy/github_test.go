package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestBuildAppJWT(t *testing.T) {
	t.Parallel()
	pemBytes := generateTestKey(t)

	jwt, err := BuildAppJWT("123456", pemBytes)
	require.NoError(t, err)
	assert.NotEmpty(t, jwt)

	// JWT has 3 dot-separated parts.
	parts := strings.Split(jwt, ".")
	assert.Len(t, parts, 3, "JWT should have header.payload.signature")

	// Decode payload and verify claims.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var claims map[string]any
	err = json.Unmarshal(payload, &claims)
	require.NoError(t, err)

	assert.Equal(t, "123456", claims["iss"])
	assert.Contains(t, claims, "iat")
	assert.Contains(t, claims, "exp")
}

func TestBuildAppJWT_BadPEM(t *testing.T) {
	t.Parallel()
	_, err := BuildAppJWT("123", []byte("not a pem"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PEM")
}

func TestMintInstallationToken(t *testing.T) {
	t.Parallel()
	pemBytes := generateTestKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/app/installations/789/access_tokens", r.URL.Path)
		assert.True(t, strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "))
		assert.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))

		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      "ghs_test_token_abc123",
			"expires_at": "2026-01-01T00:00:00Z",
		})
	}))
	defer server.Close()

	token, err := MintInstallationToken(server.URL, "123456", "789", pemBytes)
	require.NoError(t, err)
	assert.Equal(t, "ghs_test_token_abc123", token)
}

func TestMintInstallationToken_APIError(t *testing.T) {
	t.Parallel()
	pemBytes := generateTestKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	_, err := MintInstallationToken(server.URL, "123456", "789", pemBytes)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
