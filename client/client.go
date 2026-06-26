package client

import (
	"net/http"
	"time"
)

// Client is a tern API client.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new Client for the given server URL.
func New(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ClientOption configures the Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// WithNoTimeout disables the HTTP client timeout.
// This is required for SSE streaming connections that may run for
// extended periods. Without this, the default 30s timeout will
// terminate long-running SSE streams.
func WithNoTimeout() ClientOption {
	return func(c *Client) { c.httpClient.Timeout = 0 }
}
