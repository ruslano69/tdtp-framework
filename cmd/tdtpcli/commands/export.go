package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// ExportOptions holds options for export operations
type ExportOptions struct {
	TableName        string
	OutputFile       string
	Query            *packet.Query
	Fields           []string // Column projection: nil/empty = all columns
	ProcessorMgr     ProcessorManager
	Compress         bool
	CompressLevel    int
	CompressAlgo     string // Алгоритм сжатия: "zstd" (по умолчанию) или "kanzi"
	EnableChecksum   bool   // Add XXH3 checksum for data integrity verification
	ReadOnlyFields   bool   // Include read-only fields (timestamp, computed, identity)
	Fast             bool   // Skip SpecialValues detection for maximum export speed
	FallbackRowLimit int64  // Max rows for in-memory fallback when SQL pushdown fails (0 = unlimited)

	// v1.3.1 compact format
	Compact     bool     // Enable compact format output
	FixedFields []string // Explicit fixed field names; nil = auto-detect from _prefix
	CompactTail bool     // Write tail row with all fixed fields explicit

	// v1.4 integrity — xxh3_128 hashes (Schema + Data + Packet fingerprint).
	// Computed BEFORE compression so hashes cover plain-text rows.
	// Consumer must decompress first, then call pipeline.VerifyAndPrepare.
	IntegrityV14  bool   // Stamp packet with v1.4 xxh3_128 hashes
	MercuryURL    string // Optional: register hash in xzMercury (empty = local integrity only)
	MercuryCaller string // X-Caller header for Mercury registration (default: "tdtpcli")

	// Object storage (S3/SeaweedFS). Non-nil → stream to object storage instead of local file.
	StorageCfg *storage.Config // storage driver config with bucket
	StorageKey string          // object key within the bucket
}

// ProcessorManager interface for applying data processors.
// Embeds processors.PacketProcessor so it can participate in PacketChain directly.
type ProcessorManager interface {
	processors.PacketProcessor // Name() + ProcessPacket()
	HasProcessors() bool
}

// compactProc адаптирует applyCompactToPacket в PacketProcessor.
type compactProc struct {
	fixedNames []string
	writeTail  bool
}

func (p *compactProc) Name() string { return "compact" }
func (p *compactProc) ProcessPacket(_ context.Context, pkt *packet.DataPacket) error {
	return applyCompactToPacket(pkt, p.fixedNames, p.writeTail)
}

// compressProc адаптирует compressPacketData в PacketProcessor.
type compressProc struct {
	algo     string
	level    int
	checksum bool
}

func (p *compressProc) Name() string { return "compress" }
func (p *compressProc) ProcessPacket(_ context.Context, pkt *packet.DataPacket) error {
	return compressPacketData(pkt, p.level, p.algo, p.checksum)
}

// integrityProc computes TDTP v1.4 xxh3_128 integrity hashes and optionally
// registers the packet fingerprint in xzMercury as the authoritative hash record.
//
// MUST run BEFORE compressProc — hashes cover plain-text rows.
// The compressed packet carries the integrity attributes intact; the consumer
// decompresses first, then calls pipeline.VerifyAndPrepare to verify.
type integrityProc struct {
	mercuryClient *mercury.Client // nil = local integrity only (no Mercury registration)
	mercuryURL    string          // embedded in Dictionary as @MRC for consumer pre-flight
	caller        string
}

func (p *integrityProc) Name() string { return "integrity" }

func (p *integrityProc) ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error {
	// Integrity stamping is a v1.4 feature — upgrade packet version so the consumer
	// pipeline (VerifyAndPrepare) recognises this packet as v1.4 and runs the
	// 3-step pre-flight (Mercury → local xxh3 → Dictionary expansion).
	// Without this, consumer treats packet as pre-v1.4 and skips all integrity checks.
	pkt.Version = "1.4"

	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		return fmt.Errorf("integrity: %w", err)
	}
	// Print abbreviated fingerprints (first 8 hex chars = 32 bits) for operator visibility.
	schemaShort, dataShort, pktShort := pkt.Schema.XXH3, pkt.Data.XXH3, pkt.XXH3
	if len(schemaShort) > 8 {
		schemaShort = schemaShort[:8]
	}
	if len(dataShort) > 8 {
		dataShort = dataShort[:8]
	}
	if len(pktShort) > 8 {
		pktShort = pktShort[:8]
	}
	fmt.Printf("  → Integrity: schema=%s… data=%s… packet=%s…\n", schemaShort, dataShort, pktShort)

	if p.mercuryClient == nil {
		return nil
	}

	// Embed Mercury base URL in Dictionary as @MRC so the consumer knows
	// where to call GET /api/hashes/{uuid}/{part}?xxh3=... for pre-flight.
	// Only added when Mercury registration is active — no URL = no entry.
	if p.mercuryURL != "" {
		if pkt.Schema.Dictionary == nil {
			pkt.Schema.Dictionary = &packet.Dictionary{}
		}
		// Avoid duplicate @MRC if packet already carries one (e.g. from data export).
		hasMRC := false
		for _, e := range pkt.Schema.Dictionary.Entries {
			if e.Short == "@MRC" {
				hasMRC = true
				break
			}
		}
		if !hasMRC {
			pkt.Schema.Dictionary.Entries = append(pkt.Schema.Dictionary.Entries,
				packet.DictEntry{Short: "@MRC", Full: p.mercuryURL},
			)
		}
	}

	caller := p.caller
	if caller == "" {
		caller = "tdtpcli"
	}
	if err := p.mercuryClient.RegisterHash(ctx,
		pkt.Header.MessageID, pkt.Header.PartNumber,
		pkt.XXH3, pkt.Header.TableName, caller, pkt.Version,
	); err != nil {
		return fmt.Errorf("mercury RegisterHash: %w", err)
	}
	fmt.Printf("  → Registered in Mercury: uuid=%s part=%d\n",
		pkt.Header.MessageID, pkt.Header.PartNumber)
	return nil
}

// ExportTable exports a table to TDTP XML file
func ExportTable(ctx context.Context, config *adapters.Config, opts ExportOptions) error {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	fmt.Printf("Exporting table '%s'...\n", opts.TableName)

	// Add includeReadOnly flag to context for MS SQL adapter
	// (other adapters will ignore it)
	ctx = mssql.WithIncludeReadOnlyFields(ctx, opts.ReadOnlyFields)

	// --fast: skip SpecialValues detection for maximum throughput
	if opts.Fast {
		type specialValueSkipper interface{ SetSkipSpecialValues(bool) }
		if sv, ok := adapter.(specialValueSkipper); ok {
			sv.SetSkipSpecialValues(true)
		}
	}

	// --fallback-row-limit: safety-net против обвала на in-memory сканах
	if opts.FallbackRowLimit > 0 {
		type fallbackLimiter interface{ SetMaxFallbackRows(int64) }
		if fl, ok := adapter.(fallbackLimiter); ok {
			fl.SetMaxFallbackRows(opts.FallbackRowLimit)
		}
	}

	// If fields projection is requested, ensure we go through ExportTableWithQuery
	// (even if no other query params are set) so the adapter can build SELECT f1,f2,...
	if len(opts.Fields) > 0 {
		if opts.Query == nil {
			opts.Query = packet.NewQuery()
		}
		opts.Query.Fields = opts.Fields
	}

	// Export with or without query
	var packets []*packet.DataPacket
	if opts.Query != nil {
		fmt.Printf("Applying filters...\n")
		packets, err = adapter.ExportTableWithQuery(ctx, opts.TableName, opts.Query, "tdtpcli", "")
	} else {
		packets, err = adapter.ExportTable(ctx, opts.TableName)
	}

	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("⚠ No data to export")
		return nil
	}

	fmt.Printf("✓ Exported %d packet(s)\n", len(packets))

	// Count total rows BEFORE processing:
	// compact меняет RecordsInPart, compress заменяет все строки одним блобом.
	totalRows := 0
	for _, pkt := range packets {
		totalRows += pkt.Header.RecordsInPart
	}
	fmt.Printf("✓ Total rows: %d\n", totalRows)

	// Build packet processing chain.
	// Порядок: mask/normalize/validate → compact → compress → (encrypt) → (hash)
	chain := processors.NewPacketChain()

	if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
		chain.Add(opts.ProcessorMgr)
	}

	if opts.Compact {
		fixedNames := BuildFixedFieldsForExport(packets[0].Schema, opts.FixedFields)
		if len(fixedNames) == 0 {
			fmt.Println("⚠ compact requested but no fixed fields found (use --fixed-fields or add _ prefix to view columns)")
		} else {
			fmt.Printf("Applying compact format (fixed: %s)...\n", strings.Join(fixedNames, ", "))
			chain.Add(&compactProc{fixedNames: fixedNames, writeTail: opts.CompactTail})
		}
	}

	// v1.4 integrity: runs BEFORE compression so hashes cover plain-text rows.
	if opts.IntegrityV14 {
		caller := opts.MercuryCaller
		if caller == "" {
			caller = "tdtpcli"
		}
		var mclient *mercury.Client
		if opts.MercuryURL != "" {
			mclient = mercury.NewClient(opts.MercuryURL, 5000)
			fmt.Printf("v1.4 integrity + Mercury registration (%s, caller=%s)...\n",
				opts.MercuryURL, caller)
		} else {
			fmt.Printf("v1.4 integrity (local hashes only, no Mercury registration)...\n")
		}
		chain.Add(&integrityProc{mercuryClient: mclient, mercuryURL: opts.MercuryURL, caller: caller})
	}

	if opts.Compress {
		fmt.Printf("Compressing data (algo: %s, level %d)...\n", opts.CompressAlgo, opts.CompressLevel)
		chain.Add(&compressProc{algo: opts.CompressAlgo, level: opts.CompressLevel, checksum: opts.EnableChecksum})
	}

	// Open object storage once outside the loop (if needed).
	var store storage.ObjectStorage
	if opts.StorageCfg != nil {
		store, err = storage.New(*opts.StorageCfg)
		if err != nil {
			return fmt.Errorf("failed to open storage: %w", err)
		}
		defer func() { _ = store.Close() }()
	}

	total := len(packets)

	// stdout требует строгого порядка → последовательно.
	// Файлы и S3 независимы (разные имена/ключи) → параллельно.
	if opts.OutputFile == "" || opts.OutputFile == "-" {
		for i, pkt := range packets {
			if err := chain.ProcessPacket(ctx, pkt); err != nil {
				return err
			}
			if err := writePacket(ctx, pkt, i+1, total, opts, store); err != nil {
				return err
			}
			packets[i] = nil
		}
	} else {
		if err := parallelProcessAndWrite(ctx, packets, chain, total, opts, store); err != nil {
			return err
		}
	}

	if opts.EnableChecksum {
		fmt.Printf("✓ Checksums generated (xxh3)\n")
	}
	if opts.IntegrityV14 {
		if opts.MercuryURL != "" {
			fmt.Printf("✓ v1.4 integrity hashes stamped + registered in Mercury\n")
		} else {
			fmt.Printf("✓ v1.4 integrity hashes stamped (local only)\n")
		}
	}

	return nil
}

// parallelProcessAndWrite обрабатывает и записывает пакеты параллельно.
// Пакеты независимы (разные файлы/S3-ключи) → каждый пакет обрабатывается
// в отдельной горутине. Размер пула = min(len(packets), runtime.NumCPU()).
func parallelProcessAndWrite(
	ctx context.Context,
	packets []*packet.DataPacket,
	chain *processors.PacketChain,
	total int,
	opts ExportOptions,
	store storage.ObjectStorage,
) error {
	workers := runtime.NumCPU()
	if workers > len(packets) {
		workers = len(packets)
	}

	type job struct {
		i   int
		pkt *packet.DataPacket
	}

	jobCh := make(chan job, len(packets))
	for i, pkt := range packets {
		jobCh <- job{i, pkt}
	}
	close(jobCh)

	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				if err := chain.ProcessPacket(ctx, j.pkt); err != nil {
					errCh <- err
					return
				}
				if err := writePacket(ctx, j.pkt, j.i+1, total, opts, store); err != nil {
					errCh <- err
					return
				}
				packets[j.i] = nil // освобождаем память сразу после записи
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// writePacket writes a single packet to the configured destination (S3, stdout, or local file).
func writePacket(ctx context.Context, pkt *packet.DataPacket, n, total int, opts ExportOptions, store storage.ObjectStorage) error {
	switch {
	case store != nil:
		key := opts.StorageKey
		if total > 1 {
			key = generatePacketFilename(opts.StorageKey, n, total)
		}
		if err := uploadPacketToStorage(ctx, store, pkt, key); err != nil {
			return err
		}
		if total == 1 {
			fmt.Printf("✓ Uploaded to: s3://%s/%s\n", opts.StorageCfg.S3.Bucket, key)
		} else {
			fmt.Printf("✓ Uploaded packet %d/%d to: s3://%s/%s\n", n, total, opts.StorageCfg.S3.Bucket, key)
		}

	case opts.OutputFile == "" || opts.OutputFile == "-":
		generator := packet.NewGenerator()
		xml, err := generator.ToXML(pkt, true)
		if err != nil {
			return fmt.Errorf("failed to marshal packet: %w", err)
		}
		fmt.Println(string(xml))

	default:
		filename := opts.OutputFile
		if total > 1 {
			filename = generatePacketFilename(opts.OutputFile, n, total)
		}
		if err := writePacketToFile(pkt, filename); err != nil {
			return err
		}
		if total == 1 {
			fmt.Printf("✓ Written to: %s\n", filename)
		} else {
			fmt.Printf("✓ Written packet %d/%d to: %s\n", n, total, filename)
		}
	}
	return nil
}

// uploadPacketToStorage serializes pkt to XML and streams it to store via io.Pipe.
// Metadata includes table name, row count, and checksum (if present).
func uploadPacketToStorage(ctx context.Context, store storage.ObjectStorage, pkt *packet.DataPacket, key string) error {
	generator := packet.NewGenerator()
	xmlBytes, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	meta := map[string]string{
		"table":    pkt.Header.TableName,
		"protocol": "TDTP 1.0",
		"rows":     strconv.Itoa(pkt.Header.RecordsInPart),
	}
	if pkt.Data.Checksum != "" {
		meta["checksum"] = pkt.Data.Checksum
	}

	// io.Pipe: uploader reads from pr while we write to pw concurrently.
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- store.Put(ctx, key, pr, meta)
	}()

	if _, err := io.Copy(pw, bytes.NewReader(xmlBytes)); err != nil {
		pw.CloseWithError(err)
		<-errCh
		return fmt.Errorf("failed to write to storage pipe: %w", err)
	}
	_ = pw.Close()

	if err := <-errCh; err != nil {
		return fmt.Errorf("storage Put failed: %w", err)
	}
	return nil
}

// writePacketToFile writes a TDTP packet to a file
func writePacketToFile(pkt *packet.DataPacket, filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Marshal to XML
	generator := packet.NewGenerator()
	xml, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filename, xml, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// generatePacketFilename generates filename for packet N of total
func generatePacketFilename(baseFile string, n, total int) string {
	ext := filepath.Ext(baseFile)
	base := baseFile[:len(baseFile)-len(ext)]
	return fmt.Sprintf("%s_part_%d_of_%d%s", base, n, total, ext)
}

// compressPacketData compresses the Data section of a packet using the specified algorithm.
// and optionally generates XXH3 checksum for data integrity verification
func compressPacketData(pkt *packet.DataPacket, level int, algo string, enableChecksum bool) error {
	// Materialize rawRows (GenerateReference fast-path) before compression.
	// MaterializeRows() очищает rawRows — иначе writePacketTo пишет fast-path вместо сжатых данных.
	pkt.MaterializeRows()
	if len(pkt.Data.Rows) == 0 {
		return nil
	}

	if algo == "" {
		algo = processors.AlgoZstd
	}

	// Extract row values
	rows := make([]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}

	// Compress
	compressed, stats, err := processors.CompressDataForTdtpAlgo(rows, algo, level)
	if err != nil {
		return err
	}

	// Generate checksum if enabled (hash compressed Base64 data for efficiency)
	if enableChecksum {
		checksum := processors.ComputeChecksum([]byte(compressed))
		pkt.Data.Checksum = checksum
	}

	// Update packet with compressed data
	pkt.Data.Compression = algo
	pkt.Data.Rows = []packet.Row{{Value: compressed}}

	// Log compression stats
	fmt.Printf("  → Compressed: %d → %d bytes (ratio: %.2fx)\n",
		stats.OriginalSize, stats.CompressedSize, stats.Ratio)
	if enableChecksum {
		fmt.Printf("  → Checksum: %s\n", pkt.Data.Checksum)
	}

	return nil
}

// decompressPacketData decompresses the Data section of a packet.
// Алгоритм определяется из pkt.Data.Compression — поддерживает zstd и kanzi.
func decompressPacketData(pkt *packet.DataPacket) error {
	if pkt.Data.Compression == "" {
		return nil // Not compressed
	}

	if len(pkt.Data.Rows) != 1 {
		return fmt.Errorf("compressed packet should have exactly 1 row, got %d", len(pkt.Data.Rows))
	}

	compressedData := pkt.Data.Rows[0].Value

	// Validate checksum if present (BEFORE decompression for speed)
	if pkt.Data.Checksum != "" {
		if err := processors.ValidateChecksum([]byte(compressedData), pkt.Data.Checksum); err != nil {
			return fmt.Errorf("data corruption detected: %w", err)
		}
		fmt.Printf("  ✓ Checksum validated: %s\n", pkt.Data.Checksum)
	}

	// Decompress — dispatch by algorithm stored in packet
	rows, err := processors.DecompressDataForTdtpAlgo(compressedData, pkt.Data.Compression)
	if err != nil {
		return err
	}

	// Update packet with decompressed data
	pkt.Data.Compression = ""
	pkt.Data.Checksum = "" // Clear checksum after validation
	pkt.Data.Rows = make([]packet.Row, len(rows))
	for i, row := range rows {
		pkt.Data.Rows[i] = packet.Row{Value: row}
	}

	return nil
}

// IsCompressedFile checks if filename suggests compressed content
func IsCompressedFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".zst") ||
		strings.HasSuffix(strings.ToLower(filename), ".zstd")
}
