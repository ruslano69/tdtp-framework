// Package caClient is the xZMercury-side client for the TDTP CA server.
//
// On prod startup, Mercury calls Enroll (first run) or Authorize (subsequent runs)
// before accepting any key-bind/retrieve requests. Without a valid session token
// from the CA, the server refuses to start.
//
// The two-step challenge-response proves liveness: the CA sends a nonce, Mercury
// signs it with the env private key (TPM/envkey), CA verifies with env_id_pub
// embedded in the cert — proving the cert is running on the original hardware.
package caClient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ruslano69/xzmercury/internal/ca"
	"github.com/ruslano69/xzmercury/internal/envkey"
)

// Client talks to the CA server on behalf of xZMercury.
type Client struct {
	baseURL    string
	httpClient *http.Client
	identity   *envkey.Identity
}

// NewClient creates a CA client.
// baseURL example: "https://ca.tdtp.io:8443"
// identity is the env's Ed25519 keypair (TPM stub or real TPM).
func NewClient(baseURL string, identity *envkey.Identity) *Client {
	return &Client{
		baseURL:  baseURL,
		identity: identity,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// EnrollResult is returned by Enroll.
type EnrollResult struct {
	Cert         *ca.EnvCert      `json:"cert"`
	SessionToken *ca.SessionToken `json:"session_token"`
	Permissions  []string         `json:"permissions"`
}

// Enroll performs the two-step enrollment flow:
//  1. POST /api/env/enroll       → challenge nonce
//  2. POST /api/env/enroll/confirm → cert + session_token
//
// licenseKey is the raw license key (hashed on CA side, never stored by CA).
// On repeat enrollment of the same env, CA returns the existing cert (idempotent).
func (c *Client) Enroll(ctx context.Context, licenseKey string) (*EnrollResult, error) {
	envIDPub := c.identity.PublicKey()

	// Step 1: send license_key + env_id_pub, receive challenge nonce.
	step1Req := map[string]any{
		"license_key": licenseKey,
		"env_id_pub":  []byte(envIDPub),
	}
	step1Resp := struct {
		ChallengeID string `json:"challenge_id"`
		Nonce       []byte `json:"nonce"`
	}{}
	if err := c.post(ctx, "/api/env/enroll", step1Req, &step1Resp); err != nil {
		return nil, fmt.Errorf("caClient: enroll step1: %w", err)
	}

	// Step 2: sign the nonce with env private key, send signature.
	sig, err := c.identity.Sign(step1Resp.Nonce)
	if err != nil {
		return nil, fmt.Errorf("caClient: sign challenge: %w", err)
	}

	step2Req := map[string]any{
		"challenge_id": step1Resp.ChallengeID,
		"signature":    sig,
	}
	var result EnrollResult
	if err := c.post(ctx, "/api/env/enroll/confirm", step2Req, &result); err != nil {
		return nil, fmt.Errorf("caClient: enroll step2: %w", err)
	}

	return &result, nil
}

// AuthorizeResult is returned by Authorize.
type AuthorizeResult struct {
	SessionToken *ca.SessionToken `json:"session_token"`
	Permissions  []string         `json:"permissions"`
}

// Authorize performs the two-step re-authorization for an existing cert:
//  1. POST /api/env/authorize         → challenge nonce
//  2. POST /api/env/authorize/confirm → fresh session_token
//
// cert is the EnvCert issued at enrollment, stored locally by Mercury.
func (c *Client) Authorize(ctx context.Context, cert *ca.EnvCert) (*AuthorizeResult, error) {
	// Step 1: present cert, receive challenge.
	step1Req := map[string]any{"cert": cert}
	step1Resp := struct {
		ChallengeID string `json:"challenge_id"`
		Nonce       []byte `json:"nonce"`
	}{}
	if err := c.post(ctx, "/api/env/authorize", step1Req, &step1Resp); err != nil {
		return nil, fmt.Errorf("caClient: authorize step1: %w", err)
	}

	// Step 2: sign nonce, receive session token.
	sig, err := c.identity.Sign(step1Resp.Nonce)
	if err != nil {
		return nil, fmt.Errorf("caClient: sign challenge: %w", err)
	}

	step2Req := map[string]any{
		"challenge_id": step1Resp.ChallengeID,
		"signature":    sig,
	}
	var result AuthorizeResult
	if err := c.post(ctx, "/api/env/authorize/confirm", step2Req, &result); err != nil {
		return nil, fmt.Errorf("caClient: authorize step2: %w", err)
	}

	return &result, nil
}

// post is a helper for JSON POST requests.
func (c *Client) post(ctx context.Context, path string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode >= 400 {
		var e struct{ Error string `json:"error"` }
		_ = json.Unmarshal(respBody, &e)
		return fmt.Errorf("CA %s: HTTP %d: %s", path, httpResp.StatusCode, e.Error)
	}

	if resp != nil {
		if err := json.Unmarshal(respBody, resp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}
