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

// strings используется только в csvEncodingWriter.

// CSVOptions holds options for TDTP → CSV conversion.
type CSVOptions struct {
	InputFile  string
	OutputFile string // "" or "-" → stdout
	Delimiter  rune   // field separator; default ','
	Encoding   string // "utf-8" (default) | "windows-1251" | "cp1251"
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
	fmt.Printf("Delimiter: %q\n", string(delim))
	enc := opts.Encoding
	if enc == "" {
		enc = "utf-8"
	}
	fmt.Printf("Encoding:  %s\n", enc)

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

	// ── Security gate ──────────────────────────────────────────────────────────
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

	// Decompress if needed
	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
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

	out, err := csvEncodingWriter(base, opts.Encoding, opts.BOM)
	if err != nil {
		return err
	}

	// Write CSV
	w := csv.NewWriter(out)
	w.Comma = delim

	// Header: field names from schema
	headers := make([]string, len(pkt.Schema.Fields))
	for i, f := range pkt.Schema.Fields {
		headers[i] = f.Name
	}
	if err := w.Write(headers); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Data rows — use pkt.GetRows() which calls parser.GetRowValues() internally.
	// Handles pipe-delimited format, escape sequences, and rawRows fast-path.
	rows := pkt.GetRows()
	for _, values := range rows {
		if err := w.Write(values); err != nil {
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

// csvEncodingWriter wraps w with the requested output encoding.
// Supported: "utf-8" / "" (default), "windows-1251", "cp1251".
// BOM=true prepends a UTF-8 BOM (useful for Excel auto-detect on Windows).
func csvEncodingWriter(w io.Writer, encoding string, bom bool) (io.Writer, error) {
	norm := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(encoding, "-", ""), "_", ""))
	switch norm {
	case "", "utf8":
		if bom {
			if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
				return nil, fmt.Errorf("failed to write UTF-8 BOM: %w", err)
			}
		}
		return w, nil
	case "windows1251", "cp1251", "win1251":
		return transform.NewWriter(w, charmap.Windows1251.NewEncoder()), nil
	default:
		return nil, fmt.Errorf("unsupported encoding %q — supported: utf-8, windows-1251", encoding)
	}
}

