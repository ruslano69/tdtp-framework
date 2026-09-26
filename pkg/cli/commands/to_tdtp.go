package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// ToTDTPOptions holds options for the --to-tdtp conversion command.
type ToTDTPOptions struct {
	InputFile  string
	OutputFile string
	Query      *packet.Query // --where/--order-by/--limit/--offset/--fields, same as --to-csv

	// Version is the target protocol version to write: "1.0", "1.3.1", or
	// "1.4" (default). See applyTargetVersion for what each strips/adds.
	Version string

	// MercuryURL enables full executor verification for v1.4 packets.
	// Empty → local xxh3 integrity check only (FallbackDegrade policy).
	MercuryURL string
}

// ConvertTDTPToTDTP re-filters and/or re-versions an existing TDTP file into
// a new TDTP file, without a database round-trip. Shares its decompress/
// security-gate/filter pipeline with ConvertTDTPToCSV — the differences are
// that the last step writes TDTP XML instead of CSV, and that encrypted input
// is refused rather than decrypted (see rejectEncryptedFile).
//
// The explicit, mandatory version choice (--v1/--v13/--v14) is deliberate:
// a file produced by --to-tdtp always declares exactly which feature set
// it carries (integrity hashes, Dictionary) rather than leaving that
// ambiguous, the way a hand-assembled or third-party-generated file might.
func ConvertTDTPToTDTP(ctx context.Context, opts ToTDTPOptions) error {
	version := opts.Version
	if version == "" {
		version = "1.4"
	}
	if version != "1.0" && version != "1.3.1" && version != "1.4" {
		return fmt.Errorf("--to-tdtp: unsupported version %q (expected 1.0, 1.3.1, or 1.4)", version)
	}

	fmt.Printf("Converting TDTP to TDTP (target version %s)...\n", version)
	fmt.Printf("Input:  %s\n", opts.InputFile)
	outLabel := opts.OutputFile
	if outLabel == "" {
		outLabel = opts.InputFile + " (overwrite)"
	}
	fmt.Printf("Output: %s\n", outLabel)

	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read TDTP file: %w", err)
	}

	// Encrypted input is refused outright — see rejectEncrypted. Checked before
	// anything else touches the bytes, so the xZMercury key is never burned.
	if err := rejectEncryptedFile(opts.InputFile, data); err != nil {
		return err
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	// v1.5 keeps ciphertext inside an otherwise readable packet, so it parses
	// fine and only shows up here.
	if err := rejectEncryptedSections(pkt); err != nil {
		return err
	}

	// Decompress first — integrity hashes (v1.4) are computed on plain-text
	// rows BEFORE compression on the producer side, and the target-version
	// transform below needs plain rows either way.
	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
	}

	// ── Security gate (after decompression) ───────────────────────────────────
	if err := applyV14SecurityGate(ctx, pkt, opts.MercuryURL); err != nil {
		return err
	}

	// Expand compact v1.3.1 format (carry-forward fixed fields) — always
	// normalize to plain rows first, same as ConvertToCompact does before
	// re-encoding, so filtering/projection below sees real values.
	if pkt.Data.Compact {
		fmt.Printf("  Expanding compact format (v1.3.1)...\n")
		if err := packet.ExpandCompactRows(pkt); err != nil {
			return fmt.Errorf("compact expansion failed: %w", err)
		}
	}

	fmt.Printf("✓ Packet: table=%q version=%s fields=%d rows=%d\n",
		pkt.Header.TableName, pkt.Version, len(pkt.Schema.Fields), len(pkt.Data.Rows))

	// Apply TDTQL filters (--where, --order-by, --limit, --offset)
	if opts.Query != nil {
		executor := tdtql.NewExecutor()
		execResult, err := executor.Execute(opts.Query, pkt.GetRows(), pkt.Schema)
		if err != nil {
			return fmt.Errorf("failed to apply query filters: %w", err)
		}
		pkt.SetRows(execResult.FilteredRows)
		fmt.Printf("✓ Filtered: %d row(s) matched\n", len(execResult.FilteredRows))
	}

	// Column projection (--fields) — unlike --to-csv, which only projects at
	// render time, the packet itself must stay self-consistent: Schema and
	// Data have to agree on exactly which fields exist and in what order.
	if opts.Query != nil && len(opts.Query.Fields) > 0 {
		if err := projectPacketFields(pkt, opts.Query.Fields); err != nil {
			return err
		}
		fmt.Printf("✓ Projection: %d column(s) selected\n", len(pkt.Schema.Fields))
	}

	if err := applyTargetVersion(pkt, version); err != nil {
		return fmt.Errorf("failed to apply target version: %w", err)
	}

	out := opts.OutputFile
	if out == "" {
		out = opts.InputFile
	}

	generator := packet.NewGenerator()
	xmlData, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}
	if err := os.WriteFile(out, xmlData, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("✓ TDTP v%s written to: %s (%d rows)\n", version, out, len(pkt.Data.Rows))
	return nil
}

// rejectEncryptedFile refuses a whole-file encrypted input (.tdtp.enc,
// AES-256-GCM + xZMercury).
//
// --to-tdtp deliberately has no decryption path at all. Decrypting here would
// make it a second, quieter way out of the envelope: reading an .enc burns the
// xZMercury key, and the command's own output is plaintext, so a single
// "convert" would consume the key and leave the ciphertext replaced by a
// readable file still named .enc. Decryption stays where it is auditable —
// --decrypt — and this command only ever sees material that is already plain.
//
// Detection is by content as well as by name: IsEncryptedBlob reads the
// encryption header, so renaming an .enc to .tdtp.xml is not a way around it.
func rejectEncryptedFile(path string, data []byte) error {
	if !IsEncryptedFile(path) && !IsEncryptedBlob(data) {
		return nil
	}

	return fmt.Errorf(
		"--to-tdtp: %s is encrypted and this command does not decrypt: "+
			"decrypt it explicitly first (--decrypt), then convert the plaintext", path)
}

// rejectEncryptedSections refuses a v1.5 packet whose Schema or Data is still
// ciphertext. Same rule as rejectEncryptedFile, one level in: the packet parses,
// but its rows are one opaque blob, so filtering and projection below would
// silently operate on ciphertext and write out a packet that is neither
// decryptable nor meaningful.
func rejectEncryptedSections(pkt *packet.DataPacket) error {
	switch {
	case pkt.Data.Encryption != "" && pkt.Schema.Encryption != "":
		return fmt.Errorf("--to-tdtp: packet Schema and Data are encrypted (%s), decrypt it first",
			pkt.Data.Encryption)
	case pkt.Data.Encryption != "":
		return fmt.Errorf("--to-tdtp: packet Data is encrypted (%s), decrypt it first",
			pkt.Data.Encryption)
	case pkt.Schema.Encryption != "":
		return fmt.Errorf("--to-tdtp: packet Schema is encrypted (%s), decrypt it first",
			pkt.Schema.Encryption)
	}

	return nil
}

// projectPacketFields prunes pkt.Schema.Fields and every row's values down
// to fieldNames, in that order. Column names are matched case-insensitively,
// same as ConvertTDTPToCSV's projection.
func projectPacketFields(pkt *packet.DataPacket, fieldNames []string) error {
	schemaFields := pkt.Schema.Fields
	nameIdx := make(map[string]int, len(schemaFields))
	for i, f := range schemaFields {
		nameIdx[strings.ToLower(f.Name)] = i
	}

	indices := make([]int, 0, len(fieldNames))
	newFields := make([]packet.Field, 0, len(fieldNames))
	for _, name := range fieldNames {
		idx, ok := nameIdx[strings.ToLower(name)]
		if !ok {
			return fmt.Errorf("--fields: column %q not found in schema", name)
		}
		indices = append(indices, idx)
		newFields = append(newFields, schemaFields[idx])
	}

	rows := pkt.GetRows()
	newRows := make([][]string, len(rows))
	for i, values := range rows {
		rec := make([]string, len(indices))
		for j, idx := range indices {
			if idx < len(values) {
				rec[j] = values[idx]
			}
		}
		newRows[i] = rec
	}

	pkt.Schema.Fields = newFields
	pkt.SetRows(newRows)
	return nil
}

// applyTargetVersion rewrites pkt's structural attributes to match version.
//
//   - "1.0":   packet.Downgrade first (expands any Dictionary tokens back
//     into row values, since a v1.0 reader has no Dictionary to resolve
//     them against), then also strips the v1.4 integrity hashes.
//   - "1.3.1": packet.Downgrade — expands and clears Dictionary. Does not
//     touch integrity hashes: this matches the framework's own existing
//     v1.4→v1.3.1 downgrade semantics (pkg/core/packet/dictionary.go)
//     rather than inventing stricter behavior here. Downgrade is a no-op
//     when there's no Dictionary, so Version is set explicitly regardless.
//   - "1.4":   recomputes xxh3 integrity fresh via packet.ComputeIntegrity
//     (the packet may have been filtered/projected/re-versioned above,
//     which would make any pre-existing hash stale).
func applyTargetVersion(pkt *packet.DataPacket, version string) error {
	switch version {
	case "1.0":
		packet.Downgrade(pkt)
		pkt.XXH3 = ""
		pkt.Schema.XXH3 = ""
		pkt.Data.XXH3 = ""
		pkt.Version = "1.0"
	case "1.3.1":
		packet.Downgrade(pkt)
		pkt.Version = "1.3.1"
	case "1.4":
		if _, err := packet.ComputeIntegrity(pkt); err != nil {
			return err
		}
		pkt.Version = "1.4"
	default:
		return fmt.Errorf("unsupported version %q", version)
	}
	return nil
}
