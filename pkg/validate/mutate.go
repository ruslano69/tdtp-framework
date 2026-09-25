package validate

// mutate.go — version/integrity mutations for tdtp-validate.
//
// Validation judges; these two operations fix the two things a validator
// is allowed to fix without touching user data:
//
//	--stamp-integrity: compute the three-level xxh3 hashes and raise the
//	  version to 1.4 (local stamps only — no Mercury registration; same
//	  as tdtpcli --integrity without --mercury-url). The xxh3 math itself
//	  lives in packet.ComputeIntegrity; this file only orchestrates.
//	--strip-integrity: remove all three xxh3 stamps and lower the version
//	  to the max of the REMAINING features (resolveVersion), e.g. a
//	  compressed v1.4 packet becomes 1.2, a plain one 1.0.
//
// Both refuse to touch a file that is unsound for other reasons (short
// rows, bad counters, broken structure): hashing broken data blesses it,
// and stripping will not repair shape. The one exception is the
// version-predates error itself — restoring the version is the point of
// the operation, so the pre-check runs with that blind spot and the
// result is strictly re-validated before writing.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// errMutationRefused / errMutationInvalid are sentinel errors so tests and
// callers can distinguish "input unsound, nothing done" from "bug: output
// unsound, nothing written" without parsing message text.
var (
	errMutationRefused = errors.New("refusing to mutate an invalid file — fix these first")
	errMutationInvalid = errors.New("mutation produced an invalid file (not written)")
)

// resolveVersion recomputes the spec version from the features actually
// present — the inverse of stamping. Dictionary implies 1.4 (the
// generator stamps dictionary packets 1.4); encryption is untouched by
// either operation, so a v1.5 packet stays 1.5.
func resolveVersion(pkt *packet.DataPacket) string {
	switch {
	case pkt.Schema.Encryption != "" || pkt.Data.Encryption != "" ||
		(pkt.QueryContext != nil && pkt.QueryContext.Encryption != ""):
		return "1.5"
	case pkt.Schema.Dictionary != nil && len(pkt.Schema.Dictionary.Entries) > 0:
		return "1.4"
	case pkt.Data.Compact:
		return "1.3.1"
	case pkt.Data.Compression != "":
		return "1.2"
	default:
		return "1.0"
	}
}

// stampIntegrity parses a fresh packet, stamps xxh3 + version 1.4,
// and returns the serialized result with human-readable notes.
// The validation packet is NOT reused: validation expands compact/columnar
// rows in place, while the hashes must cover the raw stored rows.
//
// Transform order mirrors the export chain (Compact → Integrity → Columnar
// → Compress): the stamp lands on plain row-major values (compacted rows
// as stored — same as the chain), so a compressed or columnar packet takes
// a round-trip: decompress/expand → stamp → re-transpose/recompress with
// the same algorithm at its default level. The blob bytes may differ from
// the input; the data does not.
func stampIntegrity(data []byte) (out []byte, notes []string, err error) {
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		return nil, nil, fmt.Errorf("unparsable as TDTP packet: %w", err)
	}
	if pkt.Data.Encryption != "" {
		return nil, nil, fmt.Errorf("refusing to stamp an encrypted packet: hashes must cover plaintext rows, which are ciphertext here")
	}

	algo := pkt.Data.Compression
	wasColumnar := pkt.Data.Layout == packet.LayoutColumns
	if algo != "" {
		if err := processors.DecompressPacket(context.Background(), pkt); err != nil {
			return nil, nil, fmt.Errorf("decompression failed: %w", err)
		}
		// DecompressPacket expands columnar layout as a side effect;
		// wasColumnar remembers to restore it after stamping.
	} else if wasColumnar {
		// Uncompressed columnar: expand to row-major BEFORE stamping —
		// the chain hashes pre-transpose values, so stamping the
		// transposed rows would mint hashes no reader reproduces.
		if err := packet.ExpandColumnarRows(pkt); err != nil {
			return nil, nil, fmt.Errorf("columnar expansion failed: %w", err)
		}
	}

	packet.BumpVersion(pkt, "1.4")
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		return nil, nil, fmt.Errorf("integrity stamp failed: %w", err)
	}

	if wasColumnar {
		packet.EnsureColumnar(pkt)
		notes = append(notes, "columnar layout restored after stamping")
	}
	if algo != "" {
		if err := recompress(pkt, algo); err != nil {
			return nil, nil, fmt.Errorf("recompression failed: %w", err)
		}
		notes = append(notes, fmt.Sprintf("recompressed with %s after stamping (blob bytes may differ, data is identical)", algo))
	}

	out, err = packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal stamped packet: %w", err)
	}
	return out, notes, nil
}

// recompress compresses plain rows back with the same algorithm at its
// default level (the level is not recorded in the packet, so the original
// one cannot be restored — same defaults the pipeline exporter uses).
func recompress(pkt *packet.DataPacket, algo string) error {
	pkt.MaterializeRows()
	rows := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		rows[i] = r.Value
	}
	level := 3
	if algo == "kanzi" {
		level = 6
	}
	blob, _, err := processors.CompressDataForTdtpAlgo(rows, algo, level)
	if err != nil {
		return err
	}
	pkt.Data.Checksum = processors.ComputeChecksum([]byte(blob))
	pkt.Data.Compression = algo
	pkt.Data.Rows = []packet.Row{{Value: blob}}
	packet.BumpVersion(pkt, "1.2")
	return nil
}

// stripIntegrity parses a fresh packet, removes all xxh3 stamps, resolves
// the version down to the remaining features, and returns the serialized
// result. No-op (besides normalization) when the packet carries no hashes.
func stripIntegrity(data []byte) ([]byte, error) {
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("unparsable as TDTP packet: %w", err)
	}
	pkt.XXH3 = ""
	pkt.Schema.XXH3 = ""
	pkt.Data.XXH3 = ""
	pkt.Version = resolveVersion(pkt)
	out, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		return nil, fmt.Errorf("marshal stripped packet: %w", err)
	}
	return out, nil
}

// readCapped reads the file, refusing absurd sizes up front: encoding/xml
// expands internal entities without a billion-laughs guard, so a validator
// handed a hostile file should not be the one to find out.
func readCapped(path string, maxMB int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if maxMB > 0 && info.Size() > int64(maxMB)<<20 {
		return nil, fmt.Errorf("file is %d bytes, over the --max-mb %d limit", info.Size(), maxMB)
	}
	return os.ReadFile(path)
}

// writeFile stores mutated output with owner-only permissions, matching
// the rest of the framework (export paths use 0o600 for packet files).
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// ProcessFile validates, optionally mutates, re-validates and writes.
// Returns the final report (of the WRITTEN bytes in mutation mode) for display.
// stamp/strip require output; without them output is ignored (pure validation).
func ProcessFile(path, output string, stamp, strip bool, maxMB int) (Report, error) {
	data, err := readCapped(path, maxMB)
	if err != nil {
		return Report{File: path, Errors: []string{err.Error()}}, err
	}

	if !stamp && !strip {
		return Validate(data, path), nil
	}

	// Pre-check: sound except possibly the version (which we are about to fix).
	if pre := validate(data, path, true); !pre.Valid() {
		return pre, errMutationRefused
	}

	var out []byte
	var notes []string
	if stamp {
		out, notes, err = stampIntegrity(data)
	} else {
		out, err = stripIntegrity(data)
	}
	if err != nil {
		return Report{File: path}, err
	}

	rep := Validate(out, output)
	rep.Notes = notes
	if !rep.Valid() {
		return rep, errMutationInvalid
	}
	if err := writeFile(output, out); err != nil {
		return rep, err
	}
	return rep, nil
}
