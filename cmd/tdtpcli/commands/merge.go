package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/merge"
)

// MergeOptions опции для команды merge
type MergeOptions struct {
	InputFiles    []string // TDTP файлы для объединения
	OutputFile    string   // Выходной файл
	Strategy      string   // Стратегия: union, intersection, left, right, append
	KeyFields     []string // Ключевые поля (опционально)
	Compress      bool     // Сжать результат
	ShowConflicts bool     // Показать конфликты
}

// MergeFiles объединяет несколько TDTP файлов
func MergeFiles(ctx context.Context, options MergeOptions) error {
	if len(options.InputFiles) < 2 {
		return fmt.Errorf("need at least 2 files to merge, got %d", len(options.InputFiles))
	}

	// Парсим все файлы
	parser := packet.NewParser()
	packets := make([]*packet.DataPacket, len(options.InputFiles))

	fmt.Printf("Merging %d files...\n", len(options.InputFiles))
	for i, file := range options.InputFiles {
		pkt, err := parser.ParseFile(file)
		if err != nil {
			return fmt.Errorf("failed to parse file %s: %w", file, err)
		}
		packets[i] = pkt
		fmt.Printf("  ✓ Loaded %s (%d rows)\n", file, len(pkt.Data.Rows))
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
	fmt.Printf("\n%s", result.FormatText())

	if options.ShowConflicts && len(result.Conflicts) > 0 {
		fmt.Printf("\nDetailed conflicts:\n")
		for i, c := range result.Conflicts {
			if i >= 20 {
				fmt.Printf("... and %d more\n", len(result.Conflicts)-20)
				break
			}
			fmt.Printf("  Key %s: %s\n", c.Key, c.Resolution)
		}
	}

	// Сохраняем результат
	generator := packet.NewGenerator()
	if options.Compress {
		generator.EnableCompression()
	}

	err = generator.WriteToFile(result.Packet, options.OutputFile)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("\n✓ Merged file saved to: %s\n", options.OutputFile)
	return nil
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
