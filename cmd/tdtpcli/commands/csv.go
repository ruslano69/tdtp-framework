package commands

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// CSVOptions holds options for TDTP → CSV conversion.
type CSVOptions struct {
	InputFile  string
	OutputFile string // "" or "-" → stdout
	Delimiter  rune   // field separator; default ','
	CP         string // code page: "utf8" (default), "1251", "866"
	BOM        bool   // prepend UTF-8 BOM (helps Excel auto-detect)
	Query      *packet.Query

	// MercuryURL enables full executor verification for v1.4 packets.
	// Empty → local xxh3 integrity check only (FallbackDegrade policy).
	MercuryURL string
}

// ConvertTDTPToCSV converts a TDTP packet to CSV.
//
// Security gate:
//   - v1.0 packets: pass-through, no security requirements.
//   - v1.4 packets: VerifyAndPrepare (Mercury + local xxh3).
//     MercuryURL set → full executor check, FallbackDegrade on timeout.
//     MercuryURL empty → local integrity only (FallbackDegrade).
//   - Any security failure → error returned, nothing written.
func ConvertTDTPToCSV(ctx context.Context, opts CSVOptions) error {
	delim := opts.Delimiter
	if delim == 0 {
		delim = ','
	}

	fmt.Printf("Converting TDTP to CSV...\n")
	fmt.Printf("Input:     %s\n", opts.InputFile)
	outLabel := opts.OutputFile
	if outLabel == "" || outLabel == "-" {
		outLabel = "stdout"
	}
	fmt.Printf("Output:    %s\n", outLabel)
	fmt.Printf("Delimiter: %s\n", string(delim))
	if cp := opts.CP; cp != "" && cp != "utf8" {
		fmt.Printf("CP:        %s\n", cp)
	}

	// Parse TDTP file
	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read TDTP file: %w", err)
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	// Decompress first — integrity hashes (v1.4) are computed on plain-text rows
	// BEFORE compression on the producer side. Consumer must decompress before
	// calling VerifyAndPrepare so that VerifyIntegrity hashes the same bytes.
	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
	}

	// ── Security gate (after decompression) ───────────────────────────────────
	if pkt.Version == "1.4" {
		fmt.Printf("  v1.4 packet — running security pre-flight...\n")

		var verifier pipeline.HashVerifier
		if opts.MercuryURL != "" {
			verifier = mercury.NewClient(opts.MercuryURL, 5000)
			fmt.Printf("  Mercury: %s\n", opts.MercuryURL)
		} else {
			fmt.Printf("  Mercury: not configured — local integrity only\n")
		}

		result, verErr := pipeline.VerifyAndPrepare(ctx, pkt, verifier, pipeline.FallbackDegrade)
		if verErr != nil {
			return fmt.Errorf("security check failed — export blocked: %w", verErr)
		}
		switch {
		case result.Degraded:
			fmt.Printf("  ⚠ Degraded mode: %s\n", result.DegradedReason)
		case result.MercuryRecord != nil:
			fmt.Printf("  ✓ Mercury: hash verified (sender=%s)\n", result.MercuryRecord.Sender)
		default:
			fmt.Printf("  ✓ Local integrity: OK\n")
		}
	}

	// Expand compact v1.3.1 format (carry-forward fixed fields)
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

	// Resolve column projection from --fields.
	// query.Fields drives SQL pushdown for DB exports; for in-memory packet
	// conversion we apply projection here against the packet schema.
	schemaFields := pkt.Schema.Fields
	colIndices := make([]int, len(schemaFields)) // identity: all columns
	for i := range schemaFields {
		colIndices[i] = i
	}
	if opts.Query != nil && len(opts.Query.Fields) > 0 {
		// Build a name→index map for fast lookup (case-insensitive).
		nameIdx := make(map[string]int, len(schemaFields))
		for i, f := range schemaFields {
			nameIdx[strings.ToLower(f.Name)] = i
		}
		var projIndices []int
		var projFields []packet.Field
		for _, name := range opts.Query.Fields {
			idx, ok := nameIdx[strings.ToLower(name)]
			if !ok {
				return fmt.Errorf("--fields: column %q not found in schema", name)
			}
			projIndices = append(projIndices, idx)
			projFields = append(projFields, schemaFields[idx])
		}
		colIndices = projIndices
		schemaFields = projFields
		fmt.Printf("✓ Projection: %d column(s) selected\n", len(schemaFields))
	}

	// Open output writer
	var base io.Writer
	if opts.OutputFile == "" || opts.OutputFile == "-" {
		base = os.Stdout
	} else {
		f, err := os.Create(opts.OutputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer func() { _ = f.Close() }()
		base = f
	}

	out, err := csvEncodingWriter(base, opts.CP, opts.BOM)
	if err != nil {
		return err
	}

	// Write CSV
	w := csv.NewWriter(out)
	w.Comma = delim

	// Header: projected field names
	headers := make([]string, len(schemaFields))
	for i, f := range schemaFields {
		headers[i] = f.Name
	}
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Data rows — apply column projection when needed.
	rows := pkt.GetRows()
	projected := len(colIndices) < len(pkt.Schema.Fields)
	for _, values := range rows {
		var record []string
		if projected {
			record = make([]string, len(colIndices))
			for i, idx := range colIndices {
				if idx < len(values) {
					record[i] = values[idx]
				}
			}
		} else {
			record = values
		}
		if err := w.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("CSV flush error: %w", err)
	}

	if opts.OutputFile != "" && opts.OutputFile != "-" {
		fmt.Printf("✓ CSV written: %s (%d rows)\n", opts.OutputFile, len(pkt.Data.Rows))
	}
	return nil
}

// csvEncodingWriter wraps w with the requested output code page.
//
// Accepted values (case-insensitive, hyphens/underscores ignored):
//
//	utf8, utf-8, ""  → UTF-8 (default); BOM=true prepends EF BB BF
//	1251, cp1251, windows1251, win1251  → Windows-1251 (Cyrillic Windows)
//	866,  cp866,  ibm866               → CP866 (Cyrillic DOS)
func csvEncodingWriter(w io.Writer, cp string, bom bool) (io.Writer, error) {
	norm := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(cp))
	switch norm {
	case "", "utf8":
		if bom {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return nil, fmt.Errorf("failed to write UTF-8 BOM: %w", err)
			}
		}
		return w, nil
	case "1251", "cp1251", "windows1251", "win1251":
		return transform.NewWriter(w, charmap.Windows1251.NewEncoder()), nil
	case "866", "cp866", "ibm866":
		return transform.NewWriter(w, charmap.CodePage866.NewEncoder()), nil
	default:
		return nil, fmt.Errorf("unknown code page %q — use: utf8, 1251, 866", cp)
	}
}
