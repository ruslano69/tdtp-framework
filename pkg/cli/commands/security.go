package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// rejectForgedCompression is the trust-layer pre-flight that runs BEFORE
// decompression, unlike applyV14SecurityGate (which needs the plain rows and
// so runs after). The Mercury integrity audit hashes decompressed rows, so it
// cannot see a stream whose header alone is hostile: a genuine payload
// re-wrapped to declare a 1 GiB block size decompresses to the same rows, its
// checksum matches and its integrity hash matches, yet it drives the decoder
// to gigabytes of allocation and crashes the importer on a tight host.
//
// This gate decodes the compressed stream header without decompressing and
// rejects anything tdtp's own writer would never emit — a forgery — before a
// single block buffer is allocated. It inspects kanzi payloads only; other
// algorithms fall through unchanged.
func rejectForgedCompression(pkt *packet.DataPacket) error {
	if pkt.Data.Compression != processors.AlgoKanzi {
		return nil
	}
	if len(pkt.Data.Rows) != 1 {
		return nil // not the single-blob shape a compressed packet carries
	}
	if err := processors.VerifyKanziStreamStrict(pkt.Data.Rows[0].Value); err != nil {
		return fmt.Errorf("rejected as forged kanzi packet: %w", err)
	}
	return nil
}

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
	if packet.NeedsRowCountCheck(pkt.Version) {
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
		// Not "export blocked": this gate guards import, the converters, the
		// broker and listen — every reader — and never the export path.
		return fmt.Errorf("security check failed — packet refused: %w", err)
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
