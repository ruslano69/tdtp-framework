package mercury

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ─── Hash registry client ─────────────────────────────────────────────────────
//
// RegisterHash and VerifyHash extend the existing Client for xzMercury hash
// registry (TDTP v1.4 packet integrity verification).
//
// Key difference from key operations:
//   - BindKey / RetrieveKey  → burn-on-read: key destroyed after retrieval
//   - RegisterHash / VerifyHash → read-only: hash persists for HashTTL (24h)
//
// Redis key: mercury:hash:{uuid}:{part}  (SET NX — registered exactly once)
// Security property: attacker cannot re-register a forged hash for the same
// UUID+part; the slot is occupied by the producer's registration and NX blocks
// any subsequent writes.
//
// Version guard: both methods are no-ops for pre-v1.4 packets (no hash registry).

// RegisterHash registers the packet's xxh3_128 fingerprint in xzMercury.
// Must be called by the PRODUCER after ComputeIntegrity, before queueing.
//
// Parameters:
//   - uuid         : pkt.Header.MessageID
//   - part         : pkt.Header.PartNumber (0 for single-part packets)
//   - xxh3         : pkt.XXH3  (32-char hex from ComputeIntegrity)
//   - tableName    : pkt.Header.TableName
//   - sender       : service account / pipeline name (X-Caller header)
//   - packetVersion: pkt.Version — must be "1.4", else no-op
//
// Returns nil on success or if already registered (idempotent retry).
// Returns ErrHashRegisterFailed with status 409 if the slot is taken by a
// DIFFERENT registration (attacker tried to pre-empt the slot).
func (c *Client) RegisterHash(
	ctx context.Context,
	uuid string, part int,
	xxh3, tableName, sender, packetVersion string,
) error {
	if packet.NeedsRowCountCheck(packetVersion) {
		return nil // pre-1.4: no hash registry
	}
	if xxh3 == "" {
		return fmt.Errorf("mercury: RegisterHash: xxh3 is empty (call ComputeIntegrity first)")
	}

	body := RegisterHashRequest{
		UUID:          uuid,
		Part:          part,
		XXH3:          xxh3,
		TableName:     tableName,
		Sender:        sender,
		PacketVersion: packetVersion,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("mercury: RegisterHash marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/hashes/", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("mercury: RegisterHash create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Caller", sender)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: RegisterHash: %s", ErrMercuryUnavailable, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil // newly registered
	case http.StatusConflict:
		// Slot already taken (NX blocked). Could be the producer retrying, or
		// an attacker who pre-registered. Either way the stored hash wins.
		return fmt.Errorf("%w: slot %s:%d already registered (SET NX blocked)",
			ErrHashRegisterFailed, uuid, part)
	}
	body2, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, body2)
	}
	return fmt.Errorf("%w: HTTP %d: %s", ErrHashRegisterFailed, resp.StatusCode, body2)
}

// VerifyHash checks the packet hash against what xzMercury has registered for
// this UUID+part. Must be called by the CONSUMER as a pre-flight check.
//
// Parameters:
//   - uuid         : pkt.Header.MessageID
//   - part         : pkt.Header.PartNumber
//   - xxh3         : pkt.XXH3  (what the packet claims its fingerprint is)
//   - packetVersion: pkt.Version — pre-1.4 returns (nil, nil) = pass-through
//
// Returns:
//   - (record, nil)                  — registered and hash matches  → proceed
//   - (nil, ErrHashNotRegistered)    — slot unknown                 → BLOCK + LOG
//   - (nil, ErrHashTampered)         — slot found, hash mismatch    → BLOCK + LOG
//   - (nil, ErrMercuryUnavailable)   — Mercury unreachable          → BLOCK (fail-closed)
func (c *Client) VerifyHash(
	ctx context.Context,
	uuid string, part int,
	xxh3, packetVersion string,
) (*HashRecord, error) {
	if packet.NeedsRowCountCheck(packetVersion) {
		return nil, nil // pre-1.4: pass-through
	}
	if xxh3 == "" {
		return nil, fmt.Errorf("mercury: VerifyHash: xxh3 is empty")
	}

	url := fmt.Sprintf("%s/api/hashes/%s/%s?xxh3=%s",
		c.baseURL, uuid, strconv.Itoa(part), xxh3)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("mercury: VerifyHash create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
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
		Registered       bool   `json:"registered"`
		Match            bool   `json:"match"`
		UUID             string `json:"uuid"`
		Part             int    `json:"part"`
		StoredXXH3       string `json:"stored_xxh3"`
		TableName        string `json:"table"`
		Sender           string `json:"sender"`
		PacketVersion    string `json:"packet_version"`
		ExpiresInSeconds int64  `json:"expires_in_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mercury: VerifyHash decode: %w", err)
	}

	if !result.Registered {
		return nil, ErrHashNotRegistered
	}
	if !result.Match {
		return nil, fmt.Errorf("%w: uuid=%s part=%d stored=%s presented=%s",
			ErrHashTampered, uuid, part, result.StoredXXH3, xxh3)
	}

	return &HashRecord{
		UUID:             result.UUID,
		Part:             result.Part,
		StoredXXH3:       result.StoredXXH3,
		TableName:        result.TableName,
		Sender:           result.Sender,
		PacketVersion:    result.PacketVersion,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}
