// Package client is the authenticated HTTP client for the Jabali Panel
// automation API.
//
// It signs every request with HMAC-SHA256 over METHOD || PATH || ts ||
// sha256(BODY) and sends it as X-Jabali-Signature, matching ADR-0093 and the
// panel's middleware/automation_hmac.go verifier (5-minute skew window). The
// automation_token and its secret are read from the environment / a 0600 config
// file — never hardcoded, never logged.
package client
