package mercury

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ─── Hash registry client methods ────────────────────────────────────────────
//
// These methods extend the existing Client to support the xzMercury hash
// registry introduced for TDTP v1.4 packet integrity verification.
//
// Key difference from key operations:
//   - BindKey / RetrieveKey: burn-on-read — key is destroyed after retrieval
//   - RegisterHash / VerifyHash: read-only — hash persists for HashTTL (24h)
//     and survives any number of VerifyHash calls
//
// Version guard: both methods check PacketVersion and are no-ops for pre-1.4
// packets — legacy behaviour is unchanged.

// RegisterHash registers the packet's xxh3_128 fingerprint in xzMercury.
// Must be called by the PRODUCER after ComputeIntegrity and before sending.
//
// caller is the service account name (X-Caller header) — same identity that
// would call BindKey for key registration.
//
// Returns nil if already registered (idempotent).
// Silently skips if packetVersion != "1.4" (pre-1.4 packets are not affected).
func (c *Client) RegisterHash(ctx context.Context, hash, tableName, sender, packetVersion, caller string) error {
	if packetVersion != "1.4" {
		return nil // pre-1.4: no hash registry, legacy checksum only
	}
	if hash == "" {
		return fmt.Errorf("mercury: RegisterHash: hash is empty (call ComputeIntegrity first)")
	}

	body := RegisterHashRequest{
		Hash:          hash,
		TableName:     tableName,
		Sender:        sender,
		PacketVersion: packetVersion,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mercury: RegisterHash marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/hashes/", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mercury: RegisterHash create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Caller", caller)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: RegisterHash: %s", ErrMercuryUnavailable, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	// 201 Created = new registration; 200 OK = already registered (idempotent).
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}
	body2, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, body2)
	}
	return fmt.Errorf("%w: HTTP %d: %s", ErrHashRegisterFailed, resp.StatusCode, body2)
}

// VerifyHash checks whether the packet's xxh3_128 fingerprint is registered
// in xzMercury. Must be called by the CONSUMER as a pre-flight check before
// decompression, decryption, or any DB write.
//
// Returns:
//   - (record, nil) if registered — consumer may proceed
//   - (nil, ErrHashNotRegistered) if not registered — consumer must BLOCK and LOG
//   - (nil, ErrMercuryUnavailable) if Mercury is unreachable — fail-closed: BLOCK
//
// Silently returns (nil, nil) for pre-1.4 packets (packetVersion != "1.4").
func (c *Client) VerifyHash(ctx context.Context, hash, packetVersion string) (*HashRecord, error) {
	if packetVersion != "1.4" {
		return nil, nil // pre-1.4: no hash verification, pass through
	}
	if hash == "" {
		return nil, fmt.Errorf("mercury: VerifyHash: hash is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/hashes/"+hash, nil)
	if err != nil {
		return nil, fmt.Errorf("mercury: VerifyHash create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Mercury unreachable → fail-closed: block the consumer
		return nil, fmt.Errorf("%w: VerifyHash: %s", ErrMercuryUnavailable, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrMercuryError, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, body)
	}

	var result struct {
		Registered       bool      `json:"registered"`
		Hash             string    `json:"hash"`
		TableName        string    `json:"table"`
		Sender           string    `json:"sender"`
		PacketVersion    string    `json:"packet_version"`
		ExpiresInSeconds int64     `json:"expires_in_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mercury: VerifyHash decode: %w", err)
	}

	if !result.Registered {
		// Hash not in Mercury — packet was either tampered or never registered.
		// Consumer must BLOCK and LOG.
		return nil, ErrHashNotRegistered
	}

	return &HashRecord{
		Hash:             result.Hash,
		TableName:        result.TableName,
		Sender:           result.Sender,
		PacketVersion:    result.PacketVersion,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}
