// tdtp-svg converts SVG documents to TDTP packets and back.
//
// Usage:
//
//	tdtp-svg --from-svg icon.svg     --output icon.tdtp.xml
//	tdtp-svg --to-svg   icon.tdtp.xml --output icon.svg
//	tdtp-svg --from-svg icon.svg --compress --compress-algo kanzi --output icon.tdtp.xml
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/svg"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		fromSVG       = flag.String("from-svg", "", "SVG file to convert to TDTP")
		toSVG         = flag.String("to-svg", "", "TDTP file to convert back to SVG")
		output        = flag.String("output", "", "output file path (required)")
		compress      = flag.Bool("compress", false, "compress packet data section")
		compressAlgo  = flag.String("compress-algo", "zstd", "compression algorithm: zstd or kanzi")
		compressLevel = flag.Int("compress-level", 3, "compression level (zstd 1-19, kanzi 6-7)")
	)
	flag.Parse()

	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	if (*fromSVG == "") == (*toSVG == "") {
		return fmt.Errorf("exactly one of --from-svg or --to-svg must be set")
	}

	if *fromSVG != "" {
		return doFromSVG(*fromSVG, *output, *compress, *compressAlgo, *compressLevel)
	}
	return doToSVG(*toSVG, *output)
}

func doFromSVG(in, out string, compress bool, algo string, level int) error {
	f, err := os.Open(in)
	if err != nil {
		return fmt.Errorf("open svg: %w", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := svg.Parse(f)
	if err != nil {
		return fmt.Errorf("parse svg: %w", err)
	}

	pkts, err := svg.ToPackets(rows)
	if err != nil {
		return err
	}
	if len(pkts) != 1 {
		return fmt.Errorf("multi-part TDTP output not supported in MVP (got %d packets)", len(pkts))
	}
	pkt := pkts[0]

	if compress {
		if err := compressDataSection(pkt, algo, level); err != nil {
			return fmt.Errorf("compress: %w", err)
		}
	}

	gen := packet.NewGenerator()
	data, err := gen.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("marshal packet: %w", err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	fmt.Printf("✓ %d row(s) written to %s\n", len(rows), out)
	return nil
}

func doToSVG(in, out string) error {
	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("read tdtp: %w", err)
	}

	parser := packet.NewParser()
	pkt, err := parser.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse tdtp: %w", err)
	}
	if pkt.Header.TableName != svg.TableName {
		return fmt.Errorf("not an SVG TDTP packet (table=%q, expected %q)", pkt.Header.TableName, svg.TableName)
	}

	// Decompress if needed.
	if pkt.Data.Compression != "" {
		if err := decompressDataSection(pkt); err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
	}

	rows, err := svg.FromPacket(pkt)
	if err != nil {
		return err
	}

	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create svg: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := svg.Write(f, rows); err != nil {
		return fmt.Errorf("write svg: %w", err)
	}
	fmt.Printf("✓ %d row(s) reconstructed to %s\n", len(rows), out)
	return nil
}

// compressDataSection compresses Data.Rows into a single blob, mirroring
// the flow that cmd/tdtpcli/commands/export.go uses for DB exports.
func compressDataSection(pkt *packet.DataPacket, algo string, level int) error {
	pkt.MaterializeRows()
	if len(pkt.Data.Rows) == 0 {
		return nil
	}
	rowStrings := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		rowStrings[i] = r.Value
	}
	compressed, stats, err := processors.CompressDataForTdtpAlgo(rowStrings, algo, level)
	if err != nil {
		return err
	}
	pkt.Data.Compression = algo
	pkt.Data.Rows = []packet.Row{{Value: compressed}}
	fmt.Printf("  → Compressed (%s level %d): %d → %d bytes (%.2fx)\n",
		algo, level, stats.OriginalSize, stats.CompressedSize, stats.Ratio)
	return nil
}

// decompressDataSection reverses compressDataSection.
func decompressDataSection(pkt *packet.DataPacket) error {
	if len(pkt.Data.Rows) != 1 {
		return fmt.Errorf("compressed packet must have exactly 1 row, got %d", len(pkt.Data.Rows))
	}
	rows, err := processors.DecompressDataForTdtpWithAlgo(pkt.Data.Rows[0].Value, pkt.Data.Compression)
	if err != nil {
		return err
	}
	pkt.Data.Rows = make([]packet.Row, len(rows))
	for i, v := range rows {
		pkt.Data.Rows[i] = packet.Row{Value: v}
	}
	pkt.Data.Compression = ""
	return nil
}
