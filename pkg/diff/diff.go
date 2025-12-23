package diff

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// DiffResult представляет результат сравнения двух TDTP пакетов
type DiffResult struct {
	Added    [][]string    // Добавленные строки (есть в B, нет в A)
	Removed  [][]string    // Удалённые строки (есть в A, нет в B)
	Modified []ModifiedRow // Изменённые строки
	Stats    DiffStats     // Статистика
	Schema   packet.Schema // Схема данных
}

// ModifiedRow представляет изменённую строку
type ModifiedRow struct {
	Key     string              // Значение ключевого поля
	OldRow  []string            // Старые значения
	NewRow  []string            // Новые значения
	Changes map[int]FieldChange // Изменения по индексам полей
}

// FieldChange представляет изменение одного поля
type FieldChange struct {
	FieldName string
	OldValue  string
	NewValue  string
}

// DiffStats содержит статистику сравнения
type DiffStats struct {
	TotalInA       int // Всего строк в A
	TotalInB       int // Всего строк в B
	AddedCount     int // Количество добавленных
	RemovedCount   int // Количество удалённых
	ModifiedCount  int // Количество изменённых
	UnchangedCount int // Количество неизменённых
}

// DiffOptions опции для сравнения
type DiffOptions struct {
	// KeyFields - поля для идентификации строк (по умолчанию primary key)
	KeyFields []string

	// IgnoreFields - игнорировать эти поля при сравнении
	IgnoreFields []string

	// CaseSensitive - учитывать регистр при сравнении строк
	CaseSensitive bool
}

// Differ выполняет сравнение TDTP пакетов
type Differ struct {
	options DiffOptions
}

// NewDiffer создаёт новый Differ
func NewDiffer(options DiffOptions) *Differ {
	if !options.CaseSensitive {
		// По умолчанию case-insensitive
		options.CaseSensitive = false
	}
	return &Differ{
		options: options,
	}
}

// Compare сравнивает два TDTP пакета
func (d *Differ) Compare(packetA, packetB *packet.DataPacket) (*DiffResult, error) {
	if packetA == nil || packetB == nil {
		return nil, fmt.Errorf("packets cannot be nil")
	}

	// Проверяем совместимость схем
	if err := d.validateSchemas(packetA.Schema, packetB.Schema); err != nil {
		return nil, fmt.Errorf("schema mismatch: %w", err)
	}

	// Определяем ключевые поля
	keyFields := d.options.KeyFields
	if len(keyFields) == 0 {
		// Используем primary key из схемы
		keyFields = d.extractKeyFields(packetA.Schema)
	}

	if len(keyFields) == 0 {
		return nil, fmt.Errorf("no key fields specified and no primary key in schema")
	}

	// Получаем индексы ключевых и игнорируемых полей
	keyIndices := d.getFieldIndices(packetA.Schema, keyFields)
	ignoreIndices := d.getFieldIndices(packetA.Schema, d.options.IgnoreFields)

	// Парсим строки
	parser := packet.NewParser()
	rowsA := d.parseRows(packetA.Data.Rows, parser)
	rowsB := d.parseRows(packetB.Data.Rows, parser)

	// Создаем map для быстрого поиска
	mapA := d.buildRowMap(rowsA, keyIndices)
	mapB := d.buildRowMap(rowsB, keyIndices)

	result := &DiffResult{
		Schema: packetA.Schema,
		Stats: DiffStats{
			TotalInA: len(rowsA),
			TotalInB: len(rowsB),
		},
	}

	// Находим удалённые и изменённые строки
	for key, rowA := range mapA {
		rowB, existsInB := mapB[key]

		if !existsInB {
			// Строка удалена
			result.Removed = append(result.Removed, rowA)
			result.Stats.RemovedCount++
		} else {
			// Проверяем изменения
			if modified, changes := d.compareRows(rowA, rowB, ignoreIndices, packetA.Schema); modified {
				result.Modified = append(result.Modified, ModifiedRow{
					Key:     key,
					OldRow:  rowA,
					NewRow:  rowB,
					Changes: changes,
				})
				result.Stats.ModifiedCount++
			} else {
				result.Stats.UnchangedCount++
			}
		}
	}

	// Находим добавленные строки
	for key, rowB := range mapB {
		if _, existsInA := mapA[key]; !existsInA {
			result.Added = append(result.Added, rowB)
			result.Stats.AddedCount++
		}
	}

	return result, nil
}

// validateSchemas проверяет совместимость схем
func (d *Differ) validateSchemas(schemaA, schemaB packet.Schema) error {
	if len(schemaA.Fields) != len(schemaB.Fields) {
		return fmt.Errorf("different number of fields: %d vs %d", len(schemaA.Fields), len(schemaB.Fields))
	}

	for i, fieldA := range schemaA.Fields {
		fieldB := schemaB.Fields[i]
		if fieldA.Name != fieldB.Name {
			return fmt.Errorf("field name mismatch at position %d: %s vs %s", i, fieldA.Name, fieldB.Name)
		}
		if fieldA.Type != fieldB.Type {
			return fmt.Errorf("field type mismatch for '%s': %s vs %s", fieldA.Name, fieldA.Type, fieldB.Type)
		}
	}

	return nil
}

// extractKeyFields извлекает ключевые поля из схемы
func (d *Differ) extractKeyFields(schema packet.Schema) []string {
	var keys []string
	for _, field := range schema.Fields {
		if field.Key {
			keys = append(keys, field.Name)
		}
	}
	return keys
}

// getFieldIndices возвращает индексы полей по их именам
func (d *Differ) getFieldIndices(schema packet.Schema, fieldNames []string) []int {
	var indices []int
	for _, name := range fieldNames {
		for i, field := range schema.Fields {
			if field.Name == name {
				indices = append(indices, i)
				break
			}
		}
	}
	return indices
}

// parseRows парсит TDTP строки
func (d *Differ) parseRows(rows []packet.Row, parser *packet.Parser) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		result[i] = parser.GetRowValues(row)
	}
	return result
}

// buildRowMap создаёт map для быстрого поиска строк по ключу
func (d *Differ) buildRowMap(rows [][]string, keyIndices []int) map[string][]string {
	m := make(map[string][]string)
	for _, row := range rows {
		key := d.buildKey(row, keyIndices)
		m[key] = row
	}
	return m
}

// buildKey создаёт ключ из значений полей
func (d *Differ) buildKey(row []string, keyIndices []int) string {
	var parts []string
	for _, idx := range keyIndices {
		if idx < len(row) {
			val := row[idx]
			if !d.options.CaseSensitive {
				val = strings.ToLower(val)
			}
			parts = append(parts, val)
		}
	}
	return strings.Join(parts, "|")
}

// compareRows сравнивает две строки
func (d *Differ) compareRows(rowA, rowB []string, ignoreIndices []int, schema packet.Schema) (bool, map[int]FieldChange) {
	changes := make(map[int]FieldChange)
	modified := false

	for i := 0; i < len(rowA) && i < len(rowB); i++ {
		// Пропускаем игнорируемые поля
		if d.contains(ignoreIndices, i) {
			continue
		}

		valA := rowA[i]
		valB := rowB[i]

		if !d.options.CaseSensitive {
			valA = strings.ToLower(valA)
			valB = strings.ToLower(valB)
		}

		if valA != valB {
			modified = true
			changes[i] = FieldChange{
				FieldName: schema.Fields[i].Name,
				OldValue:  rowA[i],
				NewValue:  rowB[i],
			}
		}
	}

	return modified, changes
}

// contains проверяет наличие элемента в slice
func (d *Differ) contains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// FormatText форматирует результат в текстовый вид
func (r *DiffResult) FormatText() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Diff Statistics ===\n"))
	sb.WriteString(fmt.Sprintf("Total in A: %d\n", r.Stats.TotalInA))
	sb.WriteString(fmt.Sprintf("Total in B: %d\n", r.Stats.TotalInB))
	sb.WriteString(fmt.Sprintf("Added:      %d\n", r.Stats.AddedCount))
	sb.WriteString(fmt.Sprintf("Removed:    %d\n", r.Stats.RemovedCount))
	sb.WriteString(fmt.Sprintf("Modified:   %d\n", r.Stats.ModifiedCount))
	sb.WriteString(fmt.Sprintf("Unchanged:  %d\n\n", r.Stats.UnchangedCount))

	if len(r.Added) > 0 {
		sb.WriteString(fmt.Sprintf("=== Added (%d) ===\n", len(r.Added)))
		for _, row := range r.Added {
			sb.WriteString(fmt.Sprintf("+ %s\n", strings.Join(row, " | ")))
		}
		sb.WriteString("\n")
	}

	if len(r.Removed) > 0 {
		sb.WriteString(fmt.Sprintf("=== Removed (%d) ===\n", len(r.Removed)))
		for _, row := range r.Removed {
			sb.WriteString(fmt.Sprintf("- %s\n", strings.Join(row, " | ")))
		}
		sb.WriteString("\n")
	}

	if len(r.Modified) > 0 {
		sb.WriteString(fmt.Sprintf("=== Modified (%d) ===\n", len(r.Modified)))
		for _, mod := range r.Modified {
			sb.WriteString(fmt.Sprintf("~ Key: %s\n", mod.Key))
			for idx, change := range mod.Changes {
				sb.WriteString(fmt.Sprintf("  [%d] %s: '%s' → '%s'\n",
					idx, change.FieldName, change.OldValue, change.NewValue))
			}
		}
	}

	return sb.String()
}

// IsEqual проверяет идентичность данных
func (r *DiffResult) IsEqual() bool {
	return r.Stats.AddedCount == 0 &&
		r.Stats.RemovedCount == 0 &&
		r.Stats.ModifiedCount == 0
}
