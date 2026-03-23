package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// multiPartPattern matches filenames produced by export: {base}_part_{N}_of_{total}{ext}
var multiPartPattern = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

// ImportOptions holds options for import operations
type ImportOptions struct {
	FilePath     string
	TargetTable  string   // Переопределяет имя таблицы из XML (опционально)
	Fields       []string // Column whitelist: only import these columns (nil/empty = all)
	Strategy     adapters.ImportStrategy
	ProcessorMgr ProcessorManager

	// Object storage (S3/SeaweedFS). Non-nil → download from object storage instead of local file.
	StorageCfg *storage.Config // storage driver config with bucket
	StorageKey string          // object key within the bucket
}

// ImportFile imports a TDTP XML file (or multi-part set) to database.
// If FilePath is a base name whose _part_ files exist on disk, or is itself
// a part file, all parts are collected automatically. Multiple packets are
// passed to adapter.ImportPackets — the framework handles temp table creation,
// sequential insert of all packets, and atomic swap in one transaction.
func ImportFile(ctx context.Context, config *adapters.Config, opts ImportOptions) error {
	// Collect raw data sources: either S3 objects or local files.
	type dataSource struct {
		label string
		data  []byte
	}

	var sources []dataSource

	if opts.StorageCfg != nil {
		// Download from object storage
		store, err := storage.New(*opts.StorageCfg)
		if err != nil {
			return fmt.Errorf("failed to open storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		keys := []string{opts.StorageKey}
		// Check if this is a multi-part base key by listing the bucket with prefix.
		// Parts are named {base}_part_{N}_of_{total}{ext} — same scheme as local files,
		// so strip the extension before appending "_part_" (mirrors discoverMultiPartFiles).
		if !strings.Contains(opts.StorageKey, "_part_") {
			keyExt := filepath.Ext(opts.StorageKey)
			keyBase := strings.TrimSuffix(opts.StorageKey, keyExt)
			objs, err := store.List(ctx, keyBase+"_part_")
			if err == nil && len(objs) > 0 {
				keys = make([]string, len(objs))
				for i, o := range objs {
					keys[i] = o.Key
				}
			}
		}

		for _, key := range keys {
			fmt.Printf("Downloading '%s' from s3://%s...\n", key, opts.StorageCfg.S3.Bucket)
			rc, err := store.Get(ctx, key)
			if err != nil {
				return fmt.Errorf("failed to get object %s: %w", key, err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return fmt.Errorf("failed to read object %s: %w", key, err)
			}
			sources = append(sources, dataSource{label: "s3://" + opts.StorageCfg.S3.Bucket + "/" + key, data: data})
		}
	} else {
		// Detect multi-part set; fall back to single file
		filePaths := discoverMultiPartFiles(opts.FilePath)
		if filePaths == nil {
			filePaths = []string{opts.FilePath}
		}
		for _, fp := range filePaths {
			data, err := os.ReadFile(fp)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			sources = append(sources, dataSource{label: fp, data: data})
		}
	}

	// Read and parse all packets
	packets := make([]*packet.DataPacket, 0, len(sources))
	for _, src := range sources {
		data := src.data
		fp := src.label

		fmt.Printf("Reading '%s'...\n", fp)

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

		// v1.3.1: expand compact format (carry-forward fixed fields) before any further processing
		if pkt.Data.Compact {
			fmt.Printf("  Expanding compact format (v1.3.1)...\n")
			if err := packet.ExpandCompactRows(pkt); err != nil {
				return fmt.Errorf("failed to expand compact rows: %w", err)
			}
		}

		if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
			if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
				return fmt.Errorf("processor failed: %w", err)
			}
		}

		// Apply field whitelist: keep only requested columns
		if len(opts.Fields) > 0 {
			if err := filterPacketFields(pkt, opts.Fields); err != nil {
				return fmt.Errorf("field filter failed: %w", err)
			}
		}

		packets = append(packets, pkt)
		fmt.Printf("  ✓ %d row(s)\n", len(pkt.Data.Rows))
	}

	// Validate multi-part session integrity (security: detect batch mixing)
	if len(packets) > 1 {
		if err := validateMultiPartSession(packets); err != nil {
			return fmt.Errorf("multi-part validation failed: %w", err)
		}
	}

	// Переопределяем имя таблицы если указан --table
	if opts.TargetTable != "" {
		fmt.Printf("Overriding table name: '%s' → '%s'\n", packets[0].Header.TableName, opts.TargetTable)
		for _, pkt := range packets {
			pkt.Header.TableName = opts.TargetTable
		}
	}

	// Connect adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

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
		total, _ = strconv.Atoi(m[3]) //nolint:errcheck // regex guarantees valid integer
	} else {
		// filePath is the base name — look for _part_1_of_N on disk
		ext = filepath.Ext(filePath)
		base = filePath[:len(filePath)-len(ext)]
		matches, err := filepath.Glob(fmt.Sprintf("%s_part_1_of_*%s", base, ext))
		if err == nil && len(matches) == 1 {
			if m := multiPartPattern.FindStringSubmatch(matches[0]); m != nil {
				total, _ = strconv.Atoi(m[3]) //nolint:errcheck // regex guarantees valid integer
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

// validateMultiPartSession performs security validation to ensure all packets
// belong to the same export session. Prevents data corruption from mixed batches.
//
// Checks:
//  1. All packets share the same batch ID (MessageID base before "-P")
//  2. All packets have identical schema
//  3. PartNumber sequence is complete (1..N without gaps)
//  4. TotalParts is consistent across all packets
//  5. InReplyTo matches (if present in any packet)
func validateMultiPartSession(packets []*packet.DataPacket) error {
	if len(packets) == 0 {
		return nil
	}

	// Extract expected values from first packet
	first := packets[0]
	expectedBatchID := extractBatchID(first.Header.MessageID)
	expectedSchema := first.Schema
	expectedTotalParts := first.Header.TotalParts
	expectedInReplyTo := first.Header.InReplyTo

	// Single-packet case: no validation needed if TotalParts is 0 or 1
	// (TotalParts=0 means non-multi-part export, TotalParts=1 means single part)
	if expectedTotalParts <= 1 {
		return nil
	}

	// Track seen part numbers to detect duplicates/gaps
	seenParts := make(map[int]bool)

	for i, pkt := range packets {
		// Check batch ID consistency
		batchID := extractBatchID(pkt.Header.MessageID)
		if batchID != expectedBatchID {
			return fmt.Errorf(
				"batch mismatch at packet %d: expected batch '%s', got '%s' (MessageID: %s). "+
					"Possible data corruption: parts from different export sessions mixed together",
				i+1, expectedBatchID, batchID, pkt.Header.MessageID,
			)
		}

		// Check schema consistency
		if !packet.SchemaEquals(expectedSchema, pkt.Schema) {
			return fmt.Errorf(
				"schema mismatch at packet %d (batch %s): packet has different schema. "+
					"Expected %d fields, got %d fields",
				i+1, batchID, len(expectedSchema.Fields), len(pkt.Schema.Fields),
			)
		}

		// Check TotalParts consistency
		if pkt.Header.TotalParts != expectedTotalParts {
			return fmt.Errorf(
				"TotalParts mismatch at packet %d: expected %d, got %d (MessageID: %s)",
				i+1, expectedTotalParts, pkt.Header.TotalParts, pkt.Header.MessageID,
			)
		}

		// Check InReplyTo consistency (if used)
		if expectedInReplyTo != "" || pkt.Header.InReplyTo != "" {
			if pkt.Header.InReplyTo != expectedInReplyTo {
				return fmt.Errorf(
					"InReplyTo mismatch at packet %d: expected '%s', got '%s'",
					i+1, expectedInReplyTo, pkt.Header.InReplyTo,
				)
			}
		}

		// Track part numbers
		partNum := pkt.Header.PartNumber
		if partNum < 1 || partNum > expectedTotalParts {
			return fmt.Errorf(
				"invalid PartNumber %d at packet %d: must be in range [1..%d]",
				partNum, i+1, expectedTotalParts,
			)
		}
		if seenParts[partNum] {
			return fmt.Errorf(
				"duplicate PartNumber %d detected: packet %d is a duplicate",
				partNum, i+1,
			)
		}
		seenParts[partNum] = true
	}

	// Verify we have all parts (1..TotalParts)
	if len(seenParts) != expectedTotalParts {
		missing := []int{}
		for i := 1; i <= expectedTotalParts; i++ {
			if !seenParts[i] {
				missing = append(missing, i)
			}
		}
		return fmt.Errorf(
			"incomplete part sequence: expected %d parts, got %d. Missing parts: %v",
			expectedTotalParts, len(seenParts), missing,
		)
	}

	return nil
}

// extractBatchID extracts batch ID from MessageID (part before "-P").
// Example: "MSG-2024-01-15-123456-P1" -> "MSG-2024-01-15-123456"
// This allows grouping packets from the same export session.
func extractBatchID(messageID string) string {
	// Find last occurrence of "-P" pattern
	lastPIndex := -1
	for i := len(messageID) - 2; i >= 0; i-- {
		if messageID[i:i+2] == "-P" {
			lastPIndex = i
			break
		}
	}

	if lastPIndex > 0 {
		return messageID[:lastPIndex]
	}

	// No "-P" found, return full MessageID (single-part export)
	return messageID
}

// filterPacketFields modifies a packet in-place to keep only the specified columns.
// Returns error if any requested field is not found in the packet schema.
func filterPacketFields(pkt *packet.DataPacket, fields []string) error {
	// Build case-insensitive index: field name → position in schema
	nameToIdx := make(map[string]int, len(pkt.Schema.Fields))
	for i, f := range pkt.Schema.Fields {
		nameToIdx[strings.ToLower(f.Name)] = i
	}

	// Resolve indices and build filtered schema
	indices := make([]int, 0, len(fields))
	filteredFields := make([]packet.Field, 0, len(fields))
	for _, name := range fields {
		idx, ok := nameToIdx[strings.ToLower(name)]
		if !ok {
			return fmt.Errorf("field '%s' not found in packet schema (table '%s')", name, pkt.Header.TableName)
		}
		indices = append(indices, idx)
		filteredFields = append(filteredFields, pkt.Schema.Fields[idx])
	}

	// Project each row: parse pipe-delimited values, keep only requested columns
	parser := packet.NewParser()
	projected := make([][]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		values := parser.GetRowValues(row)
		proj := make([]string, len(indices))
		for j, idx := range indices {
			if idx < len(values) {
				proj[j] = values[idx]
			}
		}
		projected[i] = proj
	}

	pkt.Schema.Fields = filteredFields
	pkt.Data = packet.RowsToData(projected)
	return nil
}
