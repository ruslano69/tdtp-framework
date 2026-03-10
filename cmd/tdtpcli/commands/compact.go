package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ConvertCompactOptions holds options for the --to-compact conversion command.
type ConvertCompactOptions struct {
	InputFile   string
	OutputFile  string
	FixedFields []string // explicit list; nil = auto-detect
	Tail        bool     // write tail row with all fixed fields explicit (stream validation / carry handoff)
}

// ConvertToCompact reads a TDTP v1.x file and rewrites it in v1.3.1 compact format.
//
// Auto-detection priority (applied to each packet independently):
//  1. Explicit --fixed-fields list → mark those fields as fixed
//  2. Fields with "_" prefix in name → strip prefix, mark fixed
//  3. Data analysis → any column where all values are identical → mark fixed
//
// If the input file is compressed, it is decompressed before processing and
// NOT re-compressed in the output (user can chain with --compress if needed).
func ConvertToCompact(opts ConvertCompactOptions) error {
	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	fmt.Printf("Reading '%s'...\n", opts.InputFile)

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
	}

	if pkt.Data.Compact {
		// Already compact — expand first so we can re-detect and re-encode cleanly
		if err := packet.ExpandCompactRows(pkt); err != nil {
			return fmt.Errorf("failed to expand existing compact data: %w", err)
		}
	}

	fmt.Printf("  %d row(s), %d field(s)\n", len(pkt.Data.Rows), len(pkt.Schema.Fields))

	// Determine fixed fields
	fixedNames := resolveFixedFields(pkt, opts.FixedFields)
	if len(fixedNames) == 0 {
		return fmt.Errorf("no fixed fields detected; use --fixed-fields to specify them explicitly")
	}
	fmt.Printf("  Fixed fields: %s\n", strings.Join(fixedNames, ", "))

	if err := applyCompactToPacket(pkt, fixedNames, opts.Tail); err != nil {
		return fmt.Errorf("failed to apply compact format: %w", err)
	}
	if opts.Tail {
		fmt.Println("  tail=true: last row will repeat all fixed fields explicitly")
	}

	// Determine output path
	out := opts.OutputFile
	if out == "" {
		out = opts.InputFile // overwrite in place
	}

	generator := packet.NewGenerator()
	xmlData, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	if err := os.WriteFile(out, xmlData, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("✓ Compact v1.3.1 written to: %s\n", out)
	return nil
}

// applyCompactToPacket delegates to packet.ApplyCompact.
func applyCompactToPacket(pkt *packet.DataPacket, fixedFieldNames []string, tail bool) error {
	return packet.ApplyCompact(pkt, fixedFieldNames, tail)
}

// resolveFixedFields determines which field names should be marked as fixed.
// Priority: explicit list > _prefix detection > data analysis.
func resolveFixedFields(pkt *packet.DataPacket, explicit []string) []string {
	if len(explicit) > 0 {
		return explicit
	}

	// Detect from _ prefix
	var prefixed []string
	for _, f := range pkt.Schema.Fields {
		if strings.HasPrefix(f.Name, "_") {
			prefixed = append(prefixed, f.Name)
		}
	}
	if len(prefixed) > 0 {
		return prefixed
	}

	// Analyze data: find columns where all values are identical
	return detectFixedFieldsByData(pkt)
}

// detectFixedFieldsByData returns field names where every row has the same value.
// This is the "aggressive" auto-detect mode described in the v1.3.1 spec.
func detectFixedFieldsByData(pkt *packet.DataPacket) []string {
	if len(pkt.Data.Rows) < 2 {
		return nil // Need at least 2 rows to call something "fixed"
	}

	parser := packet.NewParser()
	nFields := len(pkt.Schema.Fields)

	// Parse all rows
	parsed := make([][]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		parsed[i] = parser.GetRowValues(row)
	}

	// First row values as baseline
	baseline := parsed[0]

	var fixed []string
	for col := 0; col < nFields; col++ {
		var baseVal string
		if col < len(baseline) {
			baseVal = baseline[col]
		}

		allSame := true
		for _, row := range parsed[1:] {
			var v string
			if col < len(row) {
				v = row[col]
			}
			if v != baseVal {
				allSame = false
				break
			}
		}

		if allSame {
			fixed = append(fixed, pkt.Schema.Fields[col].Name)
		}
	}

	return fixed
}

// BuildFixedFieldsForExport delegates to packet.ResolveFixedFields.
// Data-analysis fallback is not performed here (no rows yet at schema build time).
func BuildFixedFieldsForExport(schema packet.Schema, fixedFields []string) []string {
	return packet.ResolveFixedFields(schema, fixedFields)
}
