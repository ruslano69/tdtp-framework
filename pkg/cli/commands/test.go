package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// partPattern matches filenames like name_part_3_of_6.tdtp.xml
var partPattern = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

type parsedPart struct {
	path string
	pkt  *packet.DataPacket
}

// TestFile performs a dry-run integrity check on a TDTP file or a multi-part batch.
// storageCfg may be nil for local files; required when filePath is an s3:// URI.
//
// Checks performed:
//  1. XML parse
//  2. Missing parts (if multi-part batch detected)
//  3. Consistent InReplyTo UUID and TableName across all parts
//  4. Unique MessageID per part (no duplicates)
//  5. RecordsInPart header vs actual row count
//  6. XXH3 checksum (if present)
//  7. Decompression (if compressed)
func TestFile(ctx context.Context, filePath string, storageCfg *storage.Config) error {
	return TestFileTo(os.Stdout, ctx, filePath, storageCfg)
}

// TestFileTo is TestFile writing its report to w instead of stdout, so
// embedders (tdtpcli_v2 --quiet/--json) control the output stream.
func TestFileTo(w io.Writer, ctx context.Context, filePath string, storageCfg *storage.Config) error {
	var files, missing []string
	var err error

	if storage.IsRemote(filePath) {
		if storageCfg == nil {
			return fmt.Errorf("s3:// URI requires storage configuration (use --config)")
		}
		files, missing, err = resolvePartSetRemote(ctx, filePath, storageCfg)
	} else {
		files, missing, err = resolvePartSet(filePath)
	}
	if err != nil {
		return err
	}

	// Report missing parts immediately — they cannot be verified further
	if len(missing) > 0 {
		for _, m := range missing {
			reportf(w, "  ✗ missing: %s\n", filepath.Base(m))
		}
		return fmt.Errorf("batch is incomplete: %d part(s) missing", len(missing))
	}

	reportf(w, "Testing %d TDTP file(s)...\n", len(files))

	// Open storage once for all files if needed (remote path)
	var store storage.ObjectStorage
	if storage.IsRemote(filePath) && storageCfg != nil {
		_, uriBucket, _, _ := storage.ParseURI(filePath)
		cfg := *storageCfg
		if uriBucket != "" {
			cfg.S3.Bucket = uriBucket
		}
		store, err = storage.New(cfg)
		if err != nil {
			return fmt.Errorf("failed to open storage: %w", err)
		}
		defer func() { _ = store.Close() }()
	}

	parser := packet.NewParser()
	start := time.Now()
	parts := make([]parsedPart, 0, len(files))

	// --- Pass 1: parse XML ---
	parseErrors := 0
	for _, f := range files {
		var pkt *packet.DataPacket
		if store != nil {
			_, _, key, _ := storage.ParseURI(f)
			rc, getErr := store.Get(ctx, key)
			if getErr != nil {
				reportf(w, "  ✗ %s: S3 read failed: %v\n", key, getErr)
				parseErrors++
				continue
			}
			data, readErr := io.ReadAll(rc)
			_ = rc.Close()
			if readErr != nil {
				reportf(w, "  ✗ %s: S3 read failed: %v\n", key, readErr)
				parseErrors++
				continue
			}
			pkt, err = parseForTest(parser, data)
		} else {
			data, readErr := os.ReadFile(f)
			if readErr != nil {
				reportf(w, "  ✗ %s: read failed: %v\n", filepath.Base(f), readErr)
				parseErrors++
				continue
			}
			pkt, err = parseForTest(parser, data)
		}
		if err != nil {
			reportf(w, "  ✗ %s: XML parse failed: %v\n", filepath.Base(f), err)
			parseErrors++
			continue
		}
		parts = append(parts, parsedPart{f, pkt})
	}
	if parseErrors > 0 {
		return fmt.Errorf("XML parse errors: %d file(s)", parseErrors)
	}

	// --- Pass 2: cross-packet consistency (multi-part only) ---
	if len(parts) > 1 {
		if err := validateBatchConsistency(w, parts); err != nil {
			return err
		}
	}

	// --- Pass 3: per-packet checks ---
	totalRows := 0
	packErrors := 0
	for _, p := range parts {
		rows, err := validatePacket(w, p.pkt, filepath.Base(p.path))
		if err != nil {
			packErrors++
		}
		totalRows += rows
	}
	if packErrors > 0 {
		return fmt.Errorf("integrity check failed: %d packet error(s)", packErrors)
	}

	reportf(w, "✓ Total rows: %d\n", totalRows)
	reportf(w, "✓ Integrity check passed (%s)\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// validateBatchConsistency checks cross-packet invariants for multi-part sets:
// same InReplyTo, same TableName, no duplicate MessageIDs.
func validateBatchConsistency(w io.Writer, parts []parsedPart) error {
	firstInReplyTo := parts[0].pkt.Header.InReplyTo
	firstTable := parts[0].pkt.Header.TableName
	seen := make(map[string]string, len(parts)) // MessageID → filename

	var errs []string
	for _, p := range parts {
		label := filepath.Base(p.path)

		if p.pkt.Header.InReplyTo != firstInReplyTo {
			errs = append(errs, fmt.Sprintf("  ✗ %s: InReplyTo=%q, expected %q (mixed batches?)",
				label, p.pkt.Header.InReplyTo, firstInReplyTo))
		}
		if p.pkt.Header.TableName != firstTable {
			errs = append(errs, fmt.Sprintf("  ✗ %s: TableName=%q, expected %q",
				label, p.pkt.Header.TableName, firstTable))
		}
		msgID := p.pkt.Header.MessageID
		if prev, dup := seen[msgID]; dup {
			errs = append(errs, fmt.Sprintf("  ✗ %s: duplicate MessageID=%q (also in %s)", label, msgID, prev))
		}
		seen[msgID] = label
	}

	if len(errs) > 0 {
		for _, e := range errs {
			reportln(w, e)
		}
		return fmt.Errorf("batch consistency: %d error(s)", len(errs))
	}
	reportf(w, "  ✓ batch: %d parts, InReplyTo=%q, table=%q\n",
		len(parts), firstInReplyTo, firstTable)
	return nil
}

// validatePacket performs single-packet checks: row count, checksum, decompression.
// Returns actual row count (best-effort even on error).
func validatePacket(w io.Writer, pkt *packet.DataPacket, label string) (int, error) {
	if pkt.Data.Compression == "" {
		actual := len(pkt.Data.Rows)
		if pkt.Header.RecordsInPart > 0 && actual != pkt.Header.RecordsInPart {
			reportf(w, "  ✗ %s: RecordsInPart=%d but XML has %d rows\n",
				label, pkt.Header.RecordsInPart, actual)
			return actual, fmt.Errorf("row count mismatch")
		}
		tmp := *pkt
		mark, err := verifyIntegrityStamp(&tmp)
		if err != nil {
			reportf(w, "  ✗ %s: %v\n", label, err)
			return actual, err
		}
		reportf(w, "  ✓ %s: uncompressed, %d rows, table=%q%s\n", label, actual, pkt.Header.TableName, mark)
		return actual, nil
	}

	// Compressed: must have exactly 1 blob row
	if len(pkt.Data.Rows) != 1 {
		reportf(w, "  ✗ %s: compressed packet must have 1 data row, got %d\n",
			label, len(pkt.Data.Rows))
		return 0, fmt.Errorf("invalid compressed structure")
	}

	// Распаковываем и СЧИТАЕМ строки, а не просто проверяем блоб на целость —
	// --test обещает "count rows vs header" даже для сжатых пакетов. tmp — не
	// pkt: этот вызов не должен видимо мутировать пакет, который дальше могут
	// использовать другие проходы. DecompressPacket сверяет RecordsInPart сам;
	// tmp.Data.Compression пуст ровно тогда, когда распаковка реально прошла,
	// так что настоящее число строк доступно даже если она отказала уже на
	// этой сверке.
	decompStart := time.Now()
	tmp := *pkt
	err := processors.DecompressPacket(context.Background(), &tmp)
	decompTime := time.Since(decompStart)
	if err != nil && tmp.Data.Compression != "" {
		reportf(w, "  ✗ %s: decompress failed (%s): %v\n",
			label, decompTime.Round(time.Millisecond), err)
		return 0, err
	}
	actual := len(tmp.Data.Rows)
	if err != nil {
		reportf(w, "  ✗ %s: algo=%s, %v\n", label, pkt.Data.Compression, err)
		return actual, err
	}
	checksumMark := ""
	if pkt.Data.Checksum != "" {
		checksumMark = ", checksum OK"
	}
	// Hashes cover the plain rows, so they are checked on tmp — after
	// decompression, never on the blob.
	integrityMark, err := verifyIntegrityStamp(&tmp)
	if err != nil {
		reportf(w, "  ✗ %s: algo=%s, %v\n", label, pkt.Data.Compression, err)
		return actual, err
	}
	reportf(w, "  ✓ %s: algo=%s, %d rows, decompressed %s%s%s\n",
		label, pkt.Data.Compression, actual, decompTime.Round(time.Millisecond), checksumMark, integrityMark)
	return actual, nil
}

// parseForTest reads a packet the way import does, because --test now
// checks the same xxh3 import checks. ParseBytes leaves compact rows folded:
// the hash is computed on them (export chain: compact → integrity →
// columnar → compress), and ParseFile's early compact expansion made every
// uncompressed compact+integrity packet mismatch. Columnar IS expanded, as
// ParseFile does — it is laid out after the hash, and the row count needs
// rows, not columns (ExpandColumnarRows is a no-op on compressed data;
// DecompressPacket expands those). Local and S3 inputs now share this path;
// S3 used to skip the columnar step.
func parseForTest(p *packet.Parser, data []byte) (*packet.DataPacket, error) {
	pkt, err := p.ParseBytes(data)
	if err != nil {
		return nil, err
	}
	if err := packet.ExpandColumnarRows(pkt); err != nil {
		return nil, err
	}
	return pkt, nil
}

// verifyIntegrityStamp is the xxh3 half of --test. The command is named
// check-integrity in v2 and prints "Integrity check passed", yet it used
// to compare only row counts and the compression checksum: a v1.4 packet
// with one row altered, or relabelled 1.4 with no hashes at all, passed.
// Now: v1.4+ must carry hashes (packet.CheckDeclaredIntegrity, the rule
// import applies) and they must match (packet.VerifyIntegrity).
// pkt must hold plain rows; it may be materialized in place.
func verifyIntegrityStamp(pkt *packet.DataPacket) (string, error) {
	if err := packet.CheckDeclaredIntegrity(pkt); err != nil {
		return "", err
	}
	if !packet.HasIntegrity(pkt) {
		return "", nil
	}
	if err := packet.VerifyIntegrity(pkt); err != nil {
		return "", err
	}
	return ", xxh3 OK", nil
}

// resolvePartSet takes any file in a multi-part set and returns:
//   - files:   sorted list of all found part files (or just the single file)
//   - missing: list of expected-but-absent part paths
func resolvePartSet(filePath string) (files, missing []string, err error) {
	if _, statErr := os.Stat(filePath); statErr != nil {
		return nil, nil, fmt.Errorf("cannot access %q: %w", filePath, statErr)
	}

	base := filepath.Base(filePath)
	dir := filepath.Dir(filePath)

	m := partPattern.FindStringSubmatch(base)
	if m == nil {
		return []string{filePath}, nil, nil
	}

	prefix := m[1]
	total, _ := strconv.Atoi(m[3])
	ext := m[4]

	// Build expected names for all parts
	expected := make(map[int]string, total)
	for i := 1; i <= total; i++ {
		name := fmt.Sprintf("%s_part_%d_of_%d%s", prefix, i, total, ext)
		expected[i] = filepath.Join(dir, name)
	}

	// Discover which exist on disk
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		return nil, nil, fmt.Errorf("cannot read directory %q: %w", dir, readErr)
	}
	existingNames := make(map[string]bool, len(entries))
	for _, e := range entries {
		existingNames[e.Name()] = true
	}

	var foundFiles []string
	for i := 1; i <= total; i++ {
		path := expected[i]
		if existingNames[filepath.Base(path)] {
			foundFiles = append(foundFiles, path)
		} else {
			missing = append(missing, path)
		}
	}
	sort.Strings(foundFiles)

	return foundFiles, missing, nil
}

// resolvePartSetRemote resolves a multi-part set from S3-compatible storage.
// Uses store.List(prefix) to discover sibling parts from any single part URI.
// Returns full s3://bucket/key URIs so the caller can read each part uniformly.
func resolvePartSetRemote(ctx context.Context, uri string, storageCfg *storage.Config) (files, missing []string, err error) {
	_, uriBucket, key, _ := storage.ParseURI(uri)

	cfg := *storageCfg
	if uriBucket != "" {
		cfg.S3.Bucket = uriBucket
	}

	store, openErr := storage.New(cfg)
	if openErr != nil {
		return nil, nil, fmt.Errorf("failed to open storage: %w", openErr)
	}
	defer func() { _ = store.Close() }()

	base := filepath.Base(key)
	m := partPattern.FindStringSubmatch(base)
	if m == nil {
		// Single file — verify it exists
		if _, statErr := store.Stat(ctx, key); statErr != nil {
			return nil, nil, fmt.Errorf("S3 object not found: %s", key)
		}
		return []string{uri}, nil, nil
	}

	prefix := strings.TrimSuffix(key, base) + m[1] // dir/ + namePrefix
	total, _ := strconv.Atoi(m[3])
	ext := m[4]

	// List all objects with the common prefix
	objects, listErr := store.List(ctx, prefix)
	if listErr != nil {
		return nil, nil, fmt.Errorf("failed to list S3 objects with prefix %q: %w", prefix, listErr)
	}
	existingKeys := make(map[string]bool, len(objects))
	for _, obj := range objects {
		existingKeys[obj.Key] = true
	}

	dir := strings.TrimSuffix(key, base) // dir/ part of key
	bucket := cfg.S3.Bucket
	for i := 1; i <= total; i++ {
		partName := fmt.Sprintf("%s_part_%d_of_%d%s", m[1], i, total, ext)
		partKey := dir + partName
		partURI := fmt.Sprintf("s3://%s/%s", bucket, partKey)
		if existingKeys[partKey] {
			files = append(files, partURI)
		} else {
			missing = append(missing, partURI)
		}
	}
	sort.Strings(files)

	return files, missing, nil
}
