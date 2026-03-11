// Package sentry provides error tracking via Sentry.
package sentry

import (
	"time"

	sentrygo "github.com/getsentry/sentry-go"
)

// Client is a thin wrapper around the Sentry Go SDK.
// When DSN is empty, all methods are no-ops.
type Client struct {
	enabled bool
}

// Init initializes the Sentry SDK with the given DSN.
// Returns a Client. If dsn is empty, returns a no-op client.
func Init(dsn string) (*Client, error) {
	if dsn == "" {
		return &Client{enabled: false}, nil
	}

	err := sentrygo.Init(sentrygo.ClientOptions{
		Dsn: dsn,
	})
	if err != nil {
		return nil, err
	}

	return &Client{enabled: true}, nil
}

// CaptureError sends an error to Sentry with optional tags.
// No-op if the client is not enabled.
func (c *Client) CaptureError(err error, tags map[string]string) {
	if !c.enabled || err == nil {
		return
	}

	sentrygo.WithScope(func(scope *sentrygo.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		sentrygo.CaptureException(err)
	})
}

// Flush waits for buffered events to be sent. Call before shutdown.
func (c *Client) Flush(timeout time.Duration) {
	if !c.enabled {
		return
	}
	sentrygo.Flush(timeout)
}
