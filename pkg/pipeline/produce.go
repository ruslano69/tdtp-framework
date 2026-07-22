package pipeline

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// HashRegistrar is satisfied by *mercury.Client (production) and by any
// dev/test substitute — mirrors the same optional-override shape
// processors.MercuryBinder already provides for BindKey, so callers that
// support a MercuryBinder override for key operations (e.g. pkg/etl's
// Exporter) can support one here too without a parallel mechanism.
type HashRegistrar interface {
	RegisterHash(ctx context.Context, uuid string, part int, xxh3, tableName, sender, packetVersion string) error
}

// ComputeAndRegisterIntegrity stamps pkt with TDTP v1.4 xxh3_128 integrity
// hashes (packet.ComputeIntegrity) and, when client is non-nil, registers
// the packet fingerprint with xZMercury's hash registry so a consumer's
// VerifyAndPrepare can later confirm nothing was tampered with in transit.
//
// Producer-side counterpart to VerifyAndPrepare in this same package.
//
// Mandatory for TDTP v1.5 encryption, not just an opt-in v1.4 feature: once
// a packet's Version is >= "1.4" (checked via packet.NeedsRowCountCheck),
// VerifyAndPrepare's Mercury pre-flight ALWAYS runs on the consumer side —
// runMercuryCheck treats an empty pkt.XXH3 as ErrHashNotRegistered (a hard
// block), not "integrity wasn't requested, skip". A v1.5-encrypted packet
// that skipped this call would therefore be unimportable the moment
// --mercury-url is set — which v1.5 decryption itself always requires. See
// docs/tdtp-protocol-schema.md → "v1.5" for the full design; this function
// exists specifically to close that gap, not as a general-purpose
// convenience — every TDTP v1.5 encryption call site must call this before
// compression, or produce packets that cannot be imported.
//
// Order matters and is fixed, same as compression/encryption: this must
// run BEFORE compression (hashes cover plaintext row values) and BEFORE
// encryption (packet.EncryptSections later bumps Version to "1.5",
// overwriting the "1.4" this function sets — the final packet ends up
// correctly versioned either way).
func ComputeAndRegisterIntegrity(ctx context.Context, pkt *packet.DataPacket, client HashRegistrar, sender string) error {
	pkt.Version = "1.4"
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		return fmt.Errorf("compute integrity: %w", err)
	}
	if client != nil {
		if err := client.RegisterHash(ctx,
			pkt.Header.MessageID, pkt.Header.PartNumber,
			pkt.XXH3, pkt.Header.TableName, sender, pkt.Version,
		); err != nil {
			return fmt.Errorf("register hash: %w", err)
		}
	}
	return nil
}
