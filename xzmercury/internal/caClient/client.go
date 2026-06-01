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

// Ensure ca.RenewalThreshold is accessible — imported above.

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

// hello calls GET /hello (waits 2s on server) and returns the single-use token.
// Must be called before every step-1 request (enroll or authorize).
func (c *Client) hello(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/hello", nil)
	if err != nil {
		return "", fmt.Errorf("caClient: hello request: %w", err)
	}
	// Timeout must be > HelloDelay (2s) + network overhead.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("caClient: hello: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("caClient: hello: HTTP %d", resp.StatusCode)
	}
	var body struct {
		HelloToken string `json:"hello_token"`
	}
	if err := jsonDecode(resp, &body); err != nil {
		return "", fmt.Errorf("caClient: hello decode: %w", err)
	}
	return body.HelloToken, nil
}

// Enroll performs the two-step enrollment flow:
//  0. GET  /hello                  → 2s wait → single-use token
//  1. POST /api/env/enroll         → challenge nonce
//  2. POST /api/env/enroll/confirm → cert + session_token
//
// licenseKey is the raw license key (hashed on CA side, never stored by CA).
// On repeat enrollment of the same env, CA returns the existing cert (idempotent).
func (c *Client) Enroll(ctx context.Context, licenseKey string) (*EnrollResult, error) {
	envIDPub := c.identity.PublicKey()

	// Step 0: get hello token (2s gate).
	helloToken, err := c.hello(ctx)
	if err != nil {
		return nil, fmt.Errorf("caClient: enroll hello: %w", err)
	}

	// Step 1: send license_key + env_id_pub, receive challenge nonce.
	step1Req := map[string]any{
		"license_key": licenseKey,
		"env_id_pub":  []byte(envIDPub),
	}
	step1Resp := struct {
		ChallengeID string `json:"challenge_id"`
		Nonce       []byte `json:"nonce"`
	}{}
	if err := c.postWithToken(ctx, "/api/env/enroll", helloToken, step1Req, &step1Resp); err != nil {
		return nil, fmt.Errorf("caClient: enroll step1: %w", err)
	}

	// Step 2: sign the nonce with env private key, send signature.
	// No hello token needed for confirm — it's keyed to challenge_id from step 1.
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
	// CertNotAfter is the new cert expiry after implicit renewal.
	// Schedule next renewal at CertNotAfter - ca.RenewalThreshold.
	CertNotAfter time.Time `json:"cert_not_after"`
}

// Authorize performs the two-step re-authorization for an existing cert:
//  0. GET  /hello                      → 2s wait → single-use token
//  1. POST /api/env/authorize           → challenge nonce
//  2. POST /api/env/authorize/confirm   → fresh session_token
//
// cert is the EnvCert issued at enrollment, stored locally by Mercury.
func (c *Client) Authorize(ctx context.Context, cert *ca.EnvCert) (*AuthorizeResult, error) {
	// Step 0: get hello token (2s gate).
	helloToken, err := c.hello(ctx)
	if err != nil {
		return nil, fmt.Errorf("caClient: authorize hello: %w", err)
	}

	// Step 1: present cert, receive challenge.
	step1Req := map[string]any{"cert": cert}
	step1Resp := struct {
		ChallengeID string `json:"challenge_id"`
		Nonce       []byte `json:"nonce"`
	}{}
	if err := c.postWithToken(ctx, "/api/env/authorize", helloToken, step1Req, &step1Resp); err != nil {
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

// AutoRenew starts a background goroutine that keeps the cert alive by calling
// Authorize before it expires. Each successful Authorize implicitly extends
// cert.not_after by CertTTL (24h) on the CA side.
//
// Renewal fires ca.RenewalThreshold (12h) before cert expiry, giving a 12h
// retry window if CA is temporarily unavailable. On failure, retries every hour.
//
// onRenew is called after each successful renewal with the fresh result —
// use it to update session_token and permissions in the running Mercury instance.
//
// The goroutine exits when ctx is cancelled (typically on server shutdown).
func (c *Client) AutoRenew(ctx context.Context, cert *ca.EnvCert, initialNotAfter time.Time,
	onRenew func(*AuthorizeResult)) {

	go func() {
		certNotAfter := initialNotAfter
		for {
			// Schedule renewal RenewalThreshold before current expiry.
			renewAt := certNotAfter.Add(-ca.RenewalThreshold)
			wait := time.Until(renewAt)
			if wait < 0 {
				wait = 0 // already past threshold — renew immediately
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}

			// Retry loop: keep trying every hour until success or ctx cancelled.
			for {
				result, err := c.Authorize(ctx, cert)
				if err == nil {
					certNotAfter = result.CertNotAfter
					onRenew(result)
					break // success — go back to outer loop to reschedule
				}

				// Log and retry.
				// Don't log the full error chain to avoid noise; caller sees 503.
				retryIn := time.Hour
				select {
				case <-ctx.Done():
					return
				case <-time.After(retryIn):
				}
			}
		}
	}()
}

// postWithToken sends a JSON POST with the hello token in X-Hello-Token header.
func (c *Client) postWithToken(ctx context.Context, path, helloToken string, req, resp any) error {
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
	httpReq.Header.Set("X-Hello-Token", helloToken)
	return c.do(httpReq, resp)
}

// post sends a plain JSON POST (no hello token — used for step-2 confirm).
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
	return c.do(httpReq, resp)
}

func (c *Client) do(httpReq *http.Request, resp any) error {
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode >= 400 {
		var e struct{ Error string `json:"error"` }
		_ = json.Unmarshal(respBody, &e)
		return fmt.Errorf("CA %s: HTTP %d: %s", httpReq.URL.Path, httpResp.StatusCode, e.Error)
	}

	if resp != nil {
		if err := json.Unmarshal(respBody, resp); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}

func jsonDecode(resp *http.Response, v any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
