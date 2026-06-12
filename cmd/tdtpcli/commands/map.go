package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/mapping"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// MapOptions holds parameters for the --map command.
type MapOptions struct {
	MappingFile string // path to mapping.yaml
	InputFile   string // path to source .tdtp.xml file
	DryRun      bool   // print what would happen without writing to DB
}

// RunMap executes a cross-system field mapping: reads a TDTP packet, applies
// the field/enum remap from mappingFile, and upserts rows into the target DB.
func RunMap(ctx context.Context, opts MapOptions) error {
	// Parse mapping config
	cfg, err := mapping.ParseFile(opts.MappingFile)
	if err != nil {
		return fmt.Errorf("--map: %w", err)
	}

	// Loop guard check (Layers 2+4): blocks if min_interval hasn't elapsed
	correlationID, done, err := mapping.CheckAndRecord(cfg)
	if err != nil {
		return fmt.Errorf("--map loop guard: %w", err)
	}
	defer done(false) // will be overridden to true on success path

	fmt.Printf("Mapping: %s\n", cfg.ID)
	if opts.DryRun {
		fmt.Println("  [dry-run mode — no data will be written]")
	}
	fmt.Printf("  correlation_id: %s\n", correlationID)
	fmt.Printf("  source: %s → target: %s\n", cfg.LoopGuard.SourceSystem, cfg.LoopGuard.TargetSystem)

	// Parse input TDTP packet
	pkt, err := loadPacket(opts.InputFile)
	if err != nil {
		return fmt.Errorf("--map: load input %q: %w", opts.InputFile, err)
	}
	fmt.Printf("  input: %s (%d rows, %d fields)\n",
		pkt.Header.TableName, pkt.Header.RecordsInPart, len(pkt.Schema.Fields))

	// Execute mapping
	if err := mapping.Execute(ctx, cfg, pkt, opts.DryRun); err != nil {
		return fmt.Errorf("--map execute: %w", err)
	}

	done(true) // mark as completed in loop guard log
	return nil
}

// loadPacket reads a TDTP XML file from disk.
func loadPacket(path string) (*packet.DataPacket, error) {
	parser := packet.NewParser()
	pkt, err := parser.ParseFile(path)
	if err != nil {
		return nil, err
	}
	// Expand compact rows if needed
	if err := parser.ExpandCompactRows(pkt); err != nil {
		return nil, fmt.Errorf("expand compact rows: %w", err)
	}
	return pkt, nil
}
