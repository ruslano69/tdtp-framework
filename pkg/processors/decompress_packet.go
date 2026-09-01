package processors

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// DecompressPacket validates the checksum (if present), decompresses
// pkt.Data via the algorithm pkt.Data.Compression names, expands a columnar
// layout, and verifies RecordsInPart — the full sequence every reader of a
// compressed TDTP packet needs. No-op if pkt isn't compressed.
//
// This is the one place that sequence should exist. Eight call sites had
// their own copy of it before this function did, two of them missing the
// columnar expansion (silent corruption: one "row" per schema column) and
// one hardcoding zstd regardless of what the packet actually declared.
func DecompressPacket(ctx context.Context, pkt *packet.DataPacket) error {
	if pkt.Data.Compression == "" {
		return nil
	}
	if len(pkt.Data.Rows) == 1 && pkt.Data.Checksum != "" {
		if err := ValidateChecksum([]byte(pkt.Data.Rows[0].Value), pkt.Data.Checksum); err != nil {
			return fmt.Errorf("checksum mismatch: %w", err)
		}
	}
	if err := packet.NewParser().DecompressData(ctx, pkt, func(ctx context.Context, compressed, algo string) ([]string, error) {
		return DecompressDataForTdtpWithAlgo(compressed, algo)
	}); err != nil {
		return err
	}
	pkt.Data.Checksum = ""
	return nil
}
