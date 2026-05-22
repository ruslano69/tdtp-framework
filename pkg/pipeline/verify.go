// Package pipeline provides the TDTP v1.4 consumer pre-flight verification
// pipeline: Mercury hash registry check → local xxh3 integrity → Dictionary
// expansion, with configurable fallback when Mercury is unavailable.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
)

// FallbackPolicy controls consumer behaviour when xzMercury is unreachable.
type FallbackPolicy int

const (
	// FallbackBlock stops processing — no packet accepted without Mercury
	// confirmation. Most secure; use for financial, medical, audit pipelines.
	// Mercury down → ErrMercuryUnavailable returned to caller.
	FallbackBlock FallbackPolicy = iota

	// FallbackDegrade skips the Mercury check and relies on local xxh3 hashes
	// only. No executor control — any producer can forge a packet — but data
	// integrity (schema + rows unchanged) is still verified locally.
	// Use when availability > security (operational continuity required).
	FallbackDegrade

	// FallbackDowngrade converts the packet to v1.3.1 in-place (expands
	// Dictionary tokens, clears integrity fields, sets Version="1.3.1") and
	// processes it via the legacy path. Zero Mercury dependency after downgrade.
	// Use when the downstream system only supports v1.3.1 or Mercury SLA is low.
	FallbackDowngrade
)

// VerifyResult is returned by VerifyAndPrepare.
type VerifyResult struct {
	// Version is the effective protocol version after optional downgrade.
	// "1.4" = full verification passed; "1.3.1" = downgraded due to fallback.
	Version string

	// Degraded is true when Mercury was unavailable and a non-blocking fallback
	// was applied. Always log this; alert if it persists.
	Degraded bool

	// DegradedReason explains why degraded mode was triggered.
	DegradedReason string

	// MercuryRecord is the hash record returned by Mercury on successful
	// verification. Nil when Degraded=true or packet is pre-1.4.
	MercuryRecord *mercury.HashRecord

	// VerifiedAt is the timestamp of successful verification.
	VerifiedAt time.Time
}

// HashVerifier abstracts xzMercury so the pipeline can be tested without a
// live Mercury instance. mercury.Client satisfies this interface.
type HashVerifier interface {
	VerifyHash(ctx context.Context, uuid string, part int, xxh3, version string) (*mercury.HashRecord, error)
}

// VerifyAndPrepare is the single entry point for TDTP consumer pre-flight.
//
// For v1.4 packets it runs:
//
//  1. Mercury executor check  — UUID+part → stored_xxh3 == pkt.XXH3?
//     Attacker cannot forge: Mercury slot is SET NX by authenticated producer.
//
//  2. Local xxh3 integrity    — recompute schema/data/packet hashes locally.
//     Detects in-transit corruption even without Mercury.
//
//  3. Dictionary expansion    — expand @tokens in rows (transparent to caller).
//
// When Mercury is unreachable the policy controls behaviour:
//   - FallbackBlock     → return ErrMercuryUnavailable  (fail-closed)
//   - FallbackDegrade   → skip step 1, run steps 2+3    (degraded mode)
//   - FallbackDowngrade → Downgrade pkt to v1.3.1, skip steps 1+2+3
//
// For pre-v1.4 packets all steps are skipped (legacy pass-through).
func VerifyAndPrepare(
	ctx context.Context,
	pkt *packet.DataPacket,
	verifier HashVerifier,
	policy FallbackPolicy,
) (*VerifyResult, error) {
	result := &VerifyResult{
		Version:    pkt.Version,
		VerifiedAt: time.Now().UTC(),
	}

	// ── Pre-v1.4: legacy pass-through ────────────────────────────────────────
	if pkt.Version != "1.4" {
		return result, nil
	}

	pkt.MaterializeRows()

	// ── Step 1: Mercury executor check ───────────────────────────────────────
	mercuryErr := runMercuryCheck(ctx, pkt, verifier, result)
	if mercuryErr != nil {
		// Mercury returned a definitive BLOCK signal (tampered or not registered).
		if errors.Is(mercuryErr, mercury.ErrHashNotRegistered) ||
			errors.Is(mercuryErr, mercury.ErrHashTampered) {
			return nil, mercuryErr
		}

		// Mercury unreachable — apply fallback policy.
		if errors.Is(mercuryErr, mercury.ErrMercuryUnavailable) || errors.Is(mercuryErr, mercury.ErrMercuryError) {
			switch policy {
			case FallbackBlock:
				return nil, fmt.Errorf("%w: Mercury unavailable and policy=block", mercuryErr)

			case FallbackDegrade:
				result.Degraded = true
				result.DegradedReason = fmt.Sprintf("Mercury unavailable (%v); local integrity only", mercuryErr)
				// fall through to steps 2+3

			case FallbackDowngrade:
				packet.Downgrade(pkt)
				result.Version = "1.3.1"
				result.Degraded = true
				result.DegradedReason = fmt.Sprintf("Mercury unavailable (%v); downgraded to v1.3.1", mercuryErr)
				return result, nil // v1.3.1 needs no further v1.4 checks
			}
		} else {
			return nil, mercuryErr // unexpected error → block
		}
	}

	// ── Step 2: Local xxh3 integrity ─────────────────────────────────────────
	// Only meaningful if the packet was stamped with ComputeIntegrity.
	if packet.HasIntegrity(pkt) {
		if err := packet.VerifyIntegrity(pkt); err != nil {
			return nil, fmt.Errorf("local integrity check failed: %w", err)
		}
	}

	// ── Step 3: Dictionary expansion ─────────────────────────────────────────
	if pkt.Schema.Dictionary != nil && len(pkt.Schema.Dictionary.Entries) > 0 {
		exp := packet.NewDictExpander(pkt.Schema.Dictionary)
		for i, row := range pkt.Data.Rows {
			pkt.Data.Rows[i].Value = exp.ExpandRow(row.Value)
		}
		pkt.Schema.Dictionary = nil // clear after expansion — downstream sees plain values
	}

	return result, nil
}

// runMercuryCheck calls VerifyHash and populates result.MercuryRecord on success.
func runMercuryCheck(
	ctx context.Context,
	pkt *packet.DataPacket,
	verifier HashVerifier,
	result *VerifyResult,
) error {
	if verifier == nil {
		return nil // no verifier configured — skip (useful in unit tests)
	}
	if pkt.XXH3 == "" {
		// Packet has no integrity stamp — cannot verify with Mercury.
		// Treat as not-registered (producer should always stamp v1.4 packets).
		return mercury.ErrHashNotRegistered
	}

	rec, err := verifier.VerifyHash(ctx,
		pkt.Header.MessageID,
		pkt.Header.PartNumber,
		pkt.XXH3,
		pkt.Version,
	)
	if err != nil {
		return err
	}
	result.MercuryRecord = rec
	return nil
}
