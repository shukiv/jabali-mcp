// Package client is the authenticated HTTP client for the Jabali Panel REST API.
//
// Auth is a per-user Bearer token (jat_…, minted via POST /me/api-tokens). The
// token acts as its owning user, and the panel enforces ownership on every
// endpoint (claims.UserID == resource.UserID, or is_admin). The MCP server
// therefore inherits the panel's tenant isolation for free — a tenant token can
// only ever see or touch that tenant's resources.
//
// The token is read from the environment / a 0600 config file, never hardcoded
// and never logged.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config is a single panel's connection settings.
type Config struct {
	Name    string `json:"name"`    // logical panel name (fleet mode); "default" if unset
	BaseURL string `json:"url"`     // e.g. https://panel.example:8443/api/v1
	Token   string `json:"token"`   // jat_… bearer token
	CAFile  string `json:"ca_file"` // optional PEM bundle to trust (self-hosted panel CA)
}

// Client talks to one panel.
type Client struct {
	name string
	base string
	tok  string
	http *http.Client
}

// New validates cfg and builds a Client.
func New(cfg Config) (*Client, error) {
	name := cfg.Name
	if name == "" {
		name = "default"
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("panel %q: base URL is required", name)
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("panel %q: token is required", name)
	}
	if !strings.HasPrefix(cfg.BaseURL, "https://") && !strings.HasPrefix(cfg.BaseURL, "http://") {
		return nil, fmt.Errorf("panel %q: base URL must be http(s)://", name)
	}
	tr := &http.Transport{}
	// TLS verification is always on. To trust a self-hosted panel that serves a
	// private-CA or self-signed cert, point CAFile at its CA bundle — this ADDS
	// that CA to the trust set (pinning it), it never disables verification.
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("panel %q: read CA file: %w", name, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("panel %q: CA file %s contained no valid certificates", name, cfg.CAFile)
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &Client{
		name: name,
		base: strings.TrimRight(cfg.BaseURL, "/"),
		tok:  cfg.Token,
		http: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}, nil
}

// Name is the panel's logical name.
func (c *Client) Name() string { return c.name }

// Do sends an authenticated request and returns the raw response body and
// status. It does not treat a non-2xx status as a Go error — the caller decides
// how to surface an API error to the model (the body carries the error
// envelope).
func (c *Client) Do(ctx context.Context, method, path string, body any) (json.RawMessage, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request body: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.tok)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return json.RawMessage(raw), resp.StatusCode, nil
}
