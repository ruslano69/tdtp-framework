package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// multiPartPattern matches filenames produced by export: {base}_part_{N}_of_{total}{ext}
var multiPartPattern = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

// ImportOptions holds options for import operations
type ImportOptions struct {
	FilePath     string
	TargetTable  string // Переопределяет имя таблицы из XML (опционально)
	Strategy     adapters.ImportStrategy
	ProcessorMgr ProcessorManager
}

// ImportFile imports a TDTP XML file (or multi-part set) to database.
// If FilePath is a base name whose _part_ files exist on disk, or is itself
// a part file, all parts are collected automatically. Multiple packets are
// passed to adapter.ImportPackets — the framework handles temp table creation,
// sequential insert of all packets, and atomic swap in one transaction.
func ImportFile(ctx context.Context, config adapters.Config, opts ImportOptions) error {
	// Detect multi-part set; fall back to single file
	filePaths := discoverMultiPartFiles(opts.FilePath)
	if filePaths == nil {
		filePaths = []string{opts.FilePath}
	}

	// Read and parse all packets
	packets := make([]*packet.DataPacket, 0, len(filePaths))
	for _, fp := range filePaths {
		data, err := os.ReadFile(fp)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		fmt.Printf("Reading '%s'...\n", fp)

		parser := packet.NewParser()
		pkt, err := parser.ParseBytes(data)
		if err != nil {
			return fmt.Errorf("failed to parse TDTP packet: %w", err)
		}

		if pkt.Data.Compression != "" {
			fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
			if err := decompressPacketData(ctx, pkt); err != nil {
				return fmt.Errorf("decompression failed: %w", err)
			}
		}

		if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
			if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
				return fmt.Errorf("processor failed: %w", err)
			}
		}

		packets = append(packets, pkt)
		fmt.Printf("  ✓ %d row(s)\n", len(pkt.Data.Rows))
	}

	// Переопределяем имя таблицы если указан --table
	if opts.TargetTable != "" {
		fmt.Printf("Overriding table name: '%s' → '%s'\n", packets[0].Header.TableName, opts.TargetTable)
		for _, pkt := range packets {
			pkt.Header.TableName = opts.TargetTable
		}
	}

	// Connect adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	tableName := packets[0].Header.TableName
	canonicalSchema := packets[0].Schema
	totalRows := 0
	for _, pkt := range packets {
		if packet.SchemaEquals(canonicalSchema, pkt.Schema) {
			totalRows += len(pkt.Data.Rows)
		}
	}

	fmt.Printf("Importing table '%s': %d packet(s), %d row(s), strategy '%s'...\n",
		tableName, len(packets), totalRows, opts.Strategy)

	// 1 packet → ImportPacket; N packets → ImportPackets (atomic, via framework)
	if len(packets) == 1 {
		err = adapter.ImportPacket(ctx, packets[0], opts.Strategy)
	} else {
		err = adapter.ImportPackets(ctx, packets, opts.Strategy)
	}
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	fmt.Printf("✓ Import complete! Table '%s' — %d row(s)\n", tableName, totalRows)
	return nil
}

// discoverMultiPartFiles detects a multi-part export set on disk.
// Handles two cases:
//   - filePath IS a part file (e.g. "data.tdtp_part_1_of_9.xml")
//   - filePath is the base name (e.g. "data.tdtp.xml") and _part_ files exist
//
// Returns nil if no multi-part set is detected.
func discoverMultiPartFiles(filePath string) []string {
	var base, ext string
	var total int

	if m := multiPartPattern.FindStringSubmatch(filePath); m != nil {
		// filePath is already a part file
		base = m[1]
		ext = m[4]
		total, _ = strconv.Atoi(m[3])
	} else {
		// filePath is the base name — look for _part_1_of_N on disk
		ext = filepath.Ext(filePath)
		base = filePath[:len(filePath)-len(ext)]
		matches, _ := filepath.Glob(fmt.Sprintf("%s_part_1_of_*%s", base, ext))
		if len(matches) == 1 {
			if m := multiPartPattern.FindStringSubmatch(matches[0]); m != nil {
				total, _ = strconv.Atoi(m[3])
			}
		}
	}

	if total < 2 {
		return nil
	}

	parts := make([]string, total)
	for i := range parts {
		parts[i] = fmt.Sprintf("%s_part_%d_of_%d%s", base, i+1, total, ext)
	}
	return parts
}

// ParseImportStrategy parses import strategy string
func ParseImportStrategy(strategy string) (adapters.ImportStrategy, error) {
	switch strategy {
	case "replace":
		return adapters.StrategyReplace, nil
	case "ignore":
		return adapters.StrategyIgnore, nil
	case "fail":
		return adapters.StrategyFail, nil
	case "copy":
		return adapters.StrategyCopy, nil
	default:
		return "", fmt.Errorf("invalid import strategy: %s (valid: replace, ignore, fail, copy)", strategy)
	}
}
