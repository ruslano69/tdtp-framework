package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
)

// applyV14SecurityGate runs the TDTP v1.4 consumer pre-flight for pkt.
//
// Must be called AFTER decompression (integrity hashes are computed on
// plain-text rows before compression on the producer side).
//
// Behaviour:
//   - Pre-v1.4 packets: no-op (returns nil immediately).
//   - v1.4 packets: runs VerifyAndPrepare with FallbackDegrade policy.
//     mercuryURL non-empty → full Mercury executor check first.
//     mercuryURL empty    → local xxh3 integrity only (degraded mode).
//   - Any verification failure → error; caller must not proceed with export.
//
// On success the function prints a one-line status to stdout so the user
// can see whether Mercury or local integrity was used.
func applyV14SecurityGate(ctx context.Context, pkt *packet.DataPacket, mercuryURL string) error {
	if pkt.Version != "1.4" {
		return nil
	}

	fmt.Printf("  v1.4 packet — running security pre-flight...\n")

	var verifier pipeline.HashVerifier
	if mercuryURL != "" {
		verifier = mercury.NewClient(mercuryURL, 5000)
		fmt.Printf("  Mercury: %s\n", mercuryURL)
	} else {
		fmt.Printf("  Mercury: not configured — local integrity only\n")
	}

	result, err := pipeline.VerifyAndPrepare(ctx, pkt, verifier, pipeline.FallbackDegrade)
	if err != nil {
		return fmt.Errorf("security check failed — export blocked: %w", err)
	}

	switch {
	case result.Degraded:
		fmt.Printf("  ⚠ Degraded mode: %s\n", result.DegradedReason)
	case result.MercuryRecord != nil:
		fmt.Printf("  ✓ Mercury: hash verified (sender=%s)\n", result.MercuryRecord.Sender)
	default:
		fmt.Printf("  ✓ Local integrity: OK\n")
	}

	return nil
}
