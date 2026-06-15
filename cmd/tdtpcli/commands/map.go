package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/core/mapping"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// MapOptions holds parameters for the --map command.
type MapOptions struct {
	MappingFile string // path to mapping.yaml
	InputFile   string // path to source .tdtp.xml (or .tdtp.enc) file
	DryRun      bool   // print what would happen without writing to DB
	MercuryURL  string // xZMercury base URL for decrypting .enc input (burn-on-read)
}

// RunMap executes a cross-system field mapping: reads a TDTP packet, applies
// the field/enum remap from mappingFile, and upserts rows into the target DB.
func RunMap(ctx context.Context, opts MapOptions) error {
	// Parse mapping config
	cfg, err := mapping.ParseFile(opts.MappingFile)
	if err != nil {
		return fmt.Errorf("--map: %w", err)
	}

	fmt.Printf("Mapping: %s\n", cfg.ID)

	// Loop guard (Layers 2+4): skip entirely for dry-runs so validation runs
	// do not consume the min_interval cooldown and block a subsequent real sync.
	var (
		correlationID = "dry-run"
		markDone      func(bool)
	)
	if opts.DryRun {
		fmt.Println("  [dry-run mode — no data will be written]")
		markDone = func(bool) {} // no-op
	} else {
		id, done, err := mapping.CheckAndRecord(cfg)
		if err != nil {
			return fmt.Errorf("--map loop guard: %w", err)
		}
		correlationID = id
		markDone = done
	}
	success := false
	defer func() { markDone(success) }()
	fmt.Printf("  correlation_id: %s\n", correlationID)
	fmt.Printf("  source: %s → target: %s\n", cfg.LoopGuard.SourceSystem, cfg.LoopGuard.TargetSystem)

	// Parse input TDTP packet (decrypts .enc input via Mercury if needed)
	pkt, err := loadPacket(ctx, opts.InputFile, opts.MercuryURL)
	if err != nil {
		return fmt.Errorf("--map: load input %q: %w", opts.InputFile, err)
	}
	fmt.Printf("  input: %s (%d rows, %d fields)\n",
		pkt.Header.TableName, pkt.Header.RecordsInPart, len(pkt.Schema.Fields))

	// Execute mapping
	if err := mapping.Execute(ctx, cfg, pkt, opts.DryRun); err != nil {
		return fmt.Errorf("--map execute: %w", err)
	}

	success = true // deferred done(success) marks the run completed
	return nil
}

// loadPacket reads a TDTP packet from disk, transparently handling the
// encryption → compression → compact layers in that order:
//   - .tdtp.enc input is decrypted via xZMercury (burn-on-read key retrieval)
//   - a compressed Data section (zstd/kanzi) is expanded
//   - a compact v1.3.1 packet is unfolded
func loadPacket(ctx context.Context, path, mercuryURL string) (*packet.DataPacket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}

	// Decrypt first when the input is an encrypted blob. Detected by content
	// (binary header) or by the .enc extension — a pipeline may write the
	// encrypted blob to the YAML destination path (often .tdtp.xml).
	if IsEncryptedFile(path) || IsEncryptedBlob(data) {
		plaintext, derr := DecryptEncBlob(ctx, data, mercuryURL)
		if derr != nil {
			return nil, fmt.Errorf("decrypt: %w", derr)
		}
		data = plaintext
	}

	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return nil, err
	}

	// Decompress (zstd/kanzi) before anything reads the rows — a compressed
	// packet stores all data as a single blob until expanded here.
	if err := decompressPacketData(pkt); err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}
	// Expand compact rows if needed (must run after decompression).
	if err := parser.ExpandCompactRows(pkt); err != nil {
		return nil, fmt.Errorf("expand compact rows: %w", err)
	}
	return pkt, nil
}
