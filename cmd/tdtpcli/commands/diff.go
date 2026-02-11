package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/diff"
)

// DiffOptions опции для команды diff
type DiffOptions struct {
	FileA         string   // Первый TDTP файл
	FileB         string   // Второй TDTP файл
	KeyFields     []string // Ключевые поля (опционально)
	IgnoreFields  []string // Игнорировать поля
	CaseSensitive bool     // Учитывать регистр
	OutputFormat  string   // Формат вывода: text, json
}

// DiffFiles сравнивает два TDTP файла
func DiffFiles(ctx context.Context, options *DiffOptions) error {
	// Парсим первый файл
	parser := packet.NewParser()
	packetA, err := parser.ParseFile(options.FileA)
	if err != nil {
		return fmt.Errorf("failed to parse file A (%s): %w", options.FileA, err)
	}

	// Парсим второй файл
	packetB, err := parser.ParseFile(options.FileB)
	if err != nil {
		return fmt.Errorf("failed to parse file B (%s): %w", options.FileB, err)
	}

	// Выполняем сравнение
	differ := diff.NewDiffer(diff.DiffOptions{
		KeyFields:     options.KeyFields,
		IgnoreFields:  options.IgnoreFields,
		CaseSensitive: options.CaseSensitive,
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		return fmt.Errorf("failed to compare files: %w", err)
	}

	// Выводим результат
	switch options.OutputFormat {
	case "json":
		// TODO: Можно добавить JSON форматирование позже
		fmt.Println("JSON output not yet implemented")
		return fmt.Errorf("JSON output not yet implemented")
	default:
		// Text формат
		output := result.FormatText()
		fmt.Print(output)
	}

	// Возвращаем exit code в зависимости от результата
	if result.IsEqual() {
		fmt.Println("\n✓ Files are identical")
		return nil
	} else {
		fmt.Println("\n✗ Files differ")
		// Не возвращаем ошибку, просто информируем о различиях
		return nil
	}
}

// PrintDiffHelp выводит справку по команде diff
func PrintDiffHelp() {
	fmt.Print(`Usage: tdtpcli --diff <file-a> <file-b> [options]

Compare two TDTP XML files and show differences.

Options:
  --key-fields <field1,field2>    Fields to use as primary key (comma-separated)
  --ignore-fields <field1,field2> Fields to ignore during comparison
  --case-sensitive                Enable case-sensitive comparison (default: false)
  --output-format <text|json>     Output format (default: text)

Examples:
  # Compare two files
  tdtpcli --diff data-old.xml data-new.xml

  # Compare with custom key field
  tdtpcli --diff data-old.xml data-new.xml --key-fields user_id

  # Ignore timestamp fields
  tdtpcli --diff data-old.xml data-new.xml --ignore-fields created_at,updated_at

Exit codes:
  0 - Files are identical or comparison successful
  1 - Error occurred
`)
}
