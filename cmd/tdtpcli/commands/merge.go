package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework/pkg/merge"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// MergeOptions опции для команды merge
type MergeOptions struct {
	InputFiles    []string // TDTP файлы для объединения
	OutputFile    string   // Выходной файл
	Strategy      string   // Стратегия: union, intersection, left, right, append
	KeyFields     []string // Ключевые поля (опционально)
	Compress      bool     // Сжать результат
	ShowConflicts bool     // Показать конфликты
	// SortFields orders the merged rows by these columns for deterministic
	// output (union order follows Go map iteration and varies run to run).
	// Empty = keep engine order. Shared by v1/v2 CLIs; v2 exposes --sort.
	SortFields []string
	// SortDesc reverses the sort (v2 --order desc; default asc).
	SortDesc bool
}

// MergeFiles объединяет несколько TDTP файлов
func MergeFiles(ctx context.Context, options MergeOptions) error {
	return MergeFilesTo(os.Stdout, ctx, options)
}

// MergeFilesTo is MergeFiles writing its report to w instead of stdout,
// so embedders (tdtpcli_v2 --quiet/--json) control the output stream.
func MergeFilesTo(w io.Writer, ctx context.Context, options MergeOptions) error {
	if len(options.InputFiles) < 2 {
		return fmt.Errorf("need at least 2 files to merge, got %d", len(options.InputFiles))
	}

	// Парсим все файлы
	parser := packet.NewParser()
	packets := make([]*packet.DataPacket, len(options.InputFiles))

	reportf(w, "Merging %d files...\n", len(options.InputFiles))
	hadIntegrity := false
	firstAlgo := ""
	for i, file := range options.InputFiles {
		pkt, err := parser.ParseFile(file)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", file, err)
		}
		// Normalize to plain rows BEFORE merging. The merger reads
		// Data.Rows as logical rows — a compressed blob is one opaque
		// row, columnar Rs are columns, compact rows are short. Merging
		// those as-is produced garbage (found live: two identical
		// 25,908-row kanzi+columnar files merged into 1 row).
		if packet.HasIntegrity(pkt) {
			hadIntegrity = true
		}
		if pkt.Data.Compression != "" {
			// Output format follows the FIRST file: remember its
			// algorithm so the merged result stays compressed the
			// same way unless --compress says otherwise. The level is
			// not recorded in the packet — defaults apply on write.
			if i == 0 {
				firstAlgo = pkt.Data.Compression
			}
			if err := processors.DecompressPacket(ctx, pkt); err != nil {
				return fmt.Errorf("failed to decompress file %s: %w", file, err)
			}
		}
		if pkt.Data.Layout == packet.LayoutColumns {
			if err := packet.ExpandColumnarRows(pkt); err != nil {
				return fmt.Errorf("failed to expand columns in file %s: %w", file, err)
			}
		}
		if pkt.Data.Compact {
			if err := packet.ExpandCompactRows(pkt); err != nil {
				return fmt.Errorf("failed to expand compact rows in file %s: %w", file, err)
			}
		}
		packets[i] = pkt
		reportf(w, "  ✓ Loaded %s (%d rows)\n", file, len(pkt.Data.Rows))
	}

	// Определяем стратегию
	mergeStrategy := merge.StrategyUnion
	switch strings.ToLower(options.Strategy) {
	case "union", "":
		// default already set above
	case "intersection", "intersect":
		mergeStrategy = merge.StrategyIntersection
	case "left", "left-priority":
		mergeStrategy = merge.StrategyLeftPriority
	case "right", "right-priority":
		mergeStrategy = merge.StrategyRightPriority
	case "append":
		mergeStrategy = merge.StrategyAppend
	default:
		return fmt.Errorf("unknown merge strategy: %s", options.Strategy)
	}

	// Выполняем merge
	merger := merge.NewMerger(merge.MergeOptions{
		Strategy:  mergeStrategy,
		KeyFields: options.KeyFields,
	})

	result, err := merger.Merge(packets...)
	if err != nil {
		return fmt.Errorf("failed to merge files: %w", err)
	}

	// Выводим статистику
	reportf(w, "\n%s", result.FormatText())

	if options.ShowConflicts && len(result.Conflicts) > 0 {
		reportf(w, "\nDetailed conflicts:\n")
		for i, c := range result.Conflicts {
			if i >= 20 {
				reportf(w, "... and %d more\n", len(result.Conflicts)-20)
				break
			}
			reportf(w, "  Key %s: %s\n", c.Key, c.Resolution)
		}
	}

	// Deterministic order on request: sort merged rows before writing.
	// Union order follows Go map iteration (fast, random per run); sorting
	// makes the output reproducible and diffable at O(n log n).
	if len(options.SortFields) > 0 {
		if err := sortMergedRows(result.Packet, options.SortFields, options.SortDesc); err != nil {
			return err
		}
	}

	// Merged content is new content: input integrity stamps (if any) describe
	// somebody else's rows and would be lies here. Recompute fresh when at
	// least one input was stamped; plain inputs keep the plain 1.0 output
	// exactly as before.
	if hadIntegrity {
		packet.BumpVersion(result.Packet, "1.4")
		if _, err := packet.ComputeIntegrity(result.Packet); err != nil {
			return fmt.Errorf("failed to stamp merged integrity: %w", err)
		}
	}

	// Optional compression of the merged output. The generator-level flag
	// never compressed anything (found live: --compress produced a plain
	// file), so this compresses explicitly — AFTER integrity, so hashes
	// cover the plain rows exactly like the export chain does. Bumps the
	// version to 1.2 unless integrity already raised it higher.
	//
	// What to compress WITH: explicit --compress means zstd, otherwise the
	// output inherits the FIRST input's algorithm (output format follows
	// the first file). Levels are packet-unrecorded, defaults apply.
	algo := ""
	if options.Compress {
		algo = "zstd"
	} else {
		algo = firstAlgo
	}
	if algo != "" {
		result.Packet.MaterializeRows()
		rows := make([]string, len(result.Packet.Data.Rows))
		for i, r := range result.Packet.Data.Rows {
			rows[i] = r.Value
		}
		level := 3
		if algo == "kanzi" {
			level = 6
		}
		blob, _, err := processors.CompressDataForTdtpAlgo(rows, algo, level)
		if err != nil {
			return fmt.Errorf("failed to compress merged output: %w", err)
		}
		result.Packet.Data.Checksum = processors.ComputeChecksum([]byte(blob))
		result.Packet.Data.Compression = algo
		result.Packet.Data.Rows = []packet.Row{{Value: blob}}
		packet.BumpVersion(result.Packet, "1.2")
	}

	// Сохраняем результат (compression already applied above when requested;
	// the generator flag path never compressed — see above).
	generator := packet.NewGenerator()

	err = generator.WriteToFile(result.Packet, options.OutputFile)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	reportf(w, "\n✓ Merged file saved to: %s\n", options.OutputFile)
	return nil
}

// sortMergedRows orders packet rows by the given columns (case-insensitive
// names, in listed priority). Comparison is TYPE-AWARE, driven by the
// schema column type through schema.Converter — never bare string order:
// INTEGER/REAL/DECIMAL compare numerically ("10" after "9"), DATE and
// DATETIME/TIMESTAMP chronologically, BOOLEAN as false < true, everything
// else lexicographically. NULL sorts before any value (flipped with the
// direction, like PostgreSQL). Unknown columns are an error — silently
// ignoring a typo would sort by nothing while claiming determinism.
func sortMergedRows(pkt *packet.DataPacket, fields []string, desc bool) error {
	nameIdx := make(map[string]int, len(pkt.Schema.Fields))
	for i, f := range pkt.Schema.Fields {
		nameIdx[strings.ToLower(f.Name)] = i
	}
	cols := make([]int, 0, len(fields))
	for _, name := range fields {
		i, ok := nameIdx[strings.ToLower(name)]
		if !ok {
			return fmt.Errorf("--sort: column %q not found in schema", name)
		}
		cols = append(cols, i)
	}
	parser := packet.NewParser()
	conv := schema.NewConverter()
	defs := make([]schema.FieldDef, len(pkt.Schema.Fields))
	for i, fld := range pkt.Schema.Fields {
		defs[i] = schema.FieldDef{
			Name:      fld.Name,
			Type:      schema.DataType(fld.Type),
			Length:    fld.Length,
			Precision: fld.Precision,
			Scale:     fld.Scale,
			Timezone:  fld.Timezone,
			Key:       fld.Key,
			Nullable:  true,
		}
	}
	type typedRow struct {
		raw    packet.Row
		values []*schema.TypedValue
	}
	typed := make([]typedRow, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		values := parser.GetRowValues(row)
		tv := make([]*schema.TypedValue, len(pkt.Schema.Fields))
		for c := range pkt.Schema.Fields {
			raw := ""
			if c < len(values) {
				raw = values[c]
			}
			v, err := conv.ParseValue(raw, defs[c])
			if err != nil {
				v = &schema.TypedValue{RawValue: raw}
			}
			tv[c] = v
		}
		typed[i] = typedRow{raw: row, values: tv}
	}
	less := func(a, b typedRow) bool {
		for _, c := range cols {
			if d := compareTyped(a.values[c], b.values[c]); d != 0 {
				if desc {
					return d > 0
				}
				return d < 0
			}
		}
		return false
	}
	sort.SliceStable(typed, func(i, j int) bool { return less(typed[i], typed[j]) })
	joined := make([]packet.Row, len(typed))
	for i, t := range typed {
		joined[i] = t.raw
	}
	pkt.Data.Rows = joined
	pkt.Header.RecordsInPart = len(joined)
	return nil
}

// compareTyped orders two parsed values: NULL first, then by kind —
// integers and floats numerically (mixed int/float promotes to float),
// booleans false < true, timestamps chronologically, strings
// lexicographically. Unparsable cells fall back to their raw text so the
// order stays total (and therefore deterministic) no matter the data.
func compareTyped(a, b *schema.TypedValue) int {
	if a.IsNull != b.IsNull {
		if a.IsNull {
			return -1
		}
		return 1
	}
	if a.IsNull {
		return 0
	}
	if av, bv := numericOf(a), numericOf(b); av != nil && bv != nil {
		switch {
		case *av < *bv:
			return -1
		case *av > *bv:
			return 1
		default:
			return 0
		}
	}
	if a.BoolValue != nil && b.BoolValue != nil {
		switch {
		case !*a.BoolValue && *b.BoolValue:
			return -1
		case *a.BoolValue && !*b.BoolValue:
			return 1
		default:
			return 0
		}
	}
	if a.TimeValue != nil && b.TimeValue != nil {
		return a.TimeValue.Compare(*b.TimeValue)
	}
	as, bs := textOf(a), textOf(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// numericOf returns the float64 value of an int/float cell, or nil.
func numericOf(tv *schema.TypedValue) *float64 {
	if tv.IntValue != nil {
		v := float64(*tv.IntValue)
		return &v
	}
	if tv.FloatValue != nil {
		v := *tv.FloatValue
		return &v
	}
	return nil
}

// textOf renders a cell for lexicographic fallback: strings verbatim,
// anything else its raw text.
func textOf(tv *schema.TypedValue) string {
	if tv.StringValue != nil {
		return *tv.StringValue
	}
	return tv.RawValue
}

// PrintMergeHelp выводит справку по команде merge
func PrintMergeHelp() {
	fmt.Print(`Usage: tdtpcli --merge <file1> <file2> [file3...] --output <output-file> [options]

Merge multiple TDTP XML files into a single file.

Options:
  --output <file>                Output file (required)
  --strategy <strategy>          Merge strategy (default: union)
                                   union        - all unique rows (deduplicated by key)
                                   intersection - only rows present in all files
                                   left         - conflicts resolved with first file priority
                                   right        - conflicts resolved with last file priority
                                   append       - append all rows (no deduplication)
  --key-fields <field1,field2>   Fields to use as primary key (comma-separated)
  --compress                     Compress output with zstd
  --show-conflicts               Show detailed conflict information

Examples:
  # Merge two files (union by default)
  tdtpcli --merge data1.xml data2.xml --output merged.xml

  # Merge with intersection (only common rows)
  tdtpcli --merge data1.xml data2.xml --output common.xml --strategy intersection

  # Merge with custom key and compression
  tdtpcli --merge data1.xml data2.xml data3.xml \
          --output merged.xml \
          --key-fields user_id \
          --compress \
          --show-conflicts

  # Append all rows without deduplication
  tdtpcli --merge part1.xml part2.xml --output full.xml --strategy append

Merge strategies:
  union        - Include all unique rows (based on key fields). Default behavior.
  intersection - Include only rows that exist in ALL input files.
  left         - When duplicate keys found, keep data from first (leftmost) file.
  right        - When duplicate keys found, keep data from last (rightmost) file.
  append       - Simply concatenate all rows without checking for duplicates.

Exit codes:
  0 - Merge successful
  1 - Error occurred
`)
}
