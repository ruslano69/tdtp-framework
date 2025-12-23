package merge

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// MergeStrategy определяет стратегию объединения
type MergeStrategy int

const (
	// StrategyUnion - объединение всех строк (дедупликация по ключу)
	StrategyUnion MergeStrategy = iota

	// StrategyIntersection - только строки, присутствующие во всех пакетах
	StrategyIntersection

	// StrategyLeftPriority - при конфликтах приоритет левому (первому) пакету
	StrategyLeftPriority

	// StrategyRightPriority - при конфликтах приоритет правому (последнему) пакету
	StrategyRightPriority

	// StrategyAppend - просто добавить все строки (без дедупликации)
	StrategyAppend
)

// MergeOptions опции для объединения
type MergeOptions struct {
	// Strategy - стратегия объединения
	Strategy MergeStrategy

	// KeyFields - поля для идентификации строк (по умолчанию primary key)
	KeyFields []string

	// ConflictResolver - функция для разрешения конфликтов (опционально)
	// Возвращает true если оставляем newRow, false если оставляем existingRow
	ConflictResolver func(existingRow, newRow []string, schema packet.Schema) bool
}

// MergeResult результат объединения
type MergeResult struct {
	Packet    *packet.DataPacket // Результирующий пакет
	Stats     MergeStats         // Статистика
	Conflicts []Conflict         // Обнаруженные конфликты
}

// MergeStats статистика объединения
type MergeStats struct {
	TotalPackets   int // Количество объединённых пакетов
	TotalRowsIn    int // Всего строк на входе
	TotalRowsOut   int // Строк в результате
	Duplicates     int // Дубликаты (удалено при дедупликации)
	ConflictsCount int // Конфликты (разрешено)
}

// Conflict представляет конфликт при объединении
type Conflict struct {
	Key            string   // Ключ строки
	ExistingRow    []string // Существующая строка
	ConflictingRow []string // Конфликтующая строка
	Resolution     string   // Как был разрешён: "kept_existing", "used_new"
}

// Merger выполняет объединение TDTP пакетов
type Merger struct {
	options MergeOptions
}

// NewMerger создаёт новый Merger
func NewMerger(options MergeOptions) *Merger {
	if options.Strategy == 0 {
		// По умолчанию Union
		options.Strategy = StrategyUnion
	}
	return &Merger{
		options: options,
	}
}

// Merge объединяет несколько TDTP пакетов в один
func (m *Merger) Merge(packets ...*packet.DataPacket) (*MergeResult, error) {
	if len(packets) == 0 {
		return nil, fmt.Errorf("no packets to merge")
	}

	if len(packets) == 1 {
		// Нечего объединять
		return &MergeResult{
			Packet: packets[0],
			Stats: MergeStats{
				TotalPackets: 1,
				TotalRowsIn:  len(packets[0].Data.Rows),
				TotalRowsOut: len(packets[0].Data.Rows),
			},
		}, nil
	}

	// Проверяем совместимость схем
	baseSchema := packets[0].Schema
	for i := 1; i < len(packets); i++ {
		if err := m.validateSchemas(baseSchema, packets[i].Schema); err != nil {
			return nil, fmt.Errorf("packet %d schema mismatch: %w", i, err)
		}
	}

	// Определяем ключевые поля
	keyFields := m.options.KeyFields
	if len(keyFields) == 0 {
		keyFields = m.extractKeyFields(baseSchema)
	}

	if len(keyFields) == 0 && m.options.Strategy != StrategyAppend {
		return nil, fmt.Errorf("no key fields specified and no primary key in schema")
	}

	// Выполняем merge по выбранной стратегии
	result := &MergeResult{
		Stats: MergeStats{
			TotalPackets: len(packets),
		},
	}

	switch m.options.Strategy {
	case StrategyUnion:
		return m.mergeUnion(packets, baseSchema, keyFields)
	case StrategyIntersection:
		return m.mergeIntersection(packets, baseSchema, keyFields)
	case StrategyLeftPriority:
		return m.mergeWithPriority(packets, baseSchema, keyFields, true)
	case StrategyRightPriority:
		return m.mergeWithPriority(packets, baseSchema, keyFields, false)
	case StrategyAppend:
		return m.mergeAppend(packets, baseSchema)
	default:
		return nil, fmt.Errorf("unknown merge strategy: %d", m.options.Strategy)
	}

	return result, nil
}

// mergeUnion объединяет все уникальные строки
func (m *Merger) mergeUnion(packets []*packet.DataPacket, schema packet.Schema, keyFields []string) (*MergeResult, error) {
	parser := packet.NewParser()
	keyIndices := m.getFieldIndices(schema, keyFields)

	rowMap := make(map[string][]string)
	result := &MergeResult{
		Stats: MergeStats{
			TotalPackets: len(packets),
		},
	}

	for _, pkt := range packets {
		rows := m.parseRows(pkt.Data.Rows, parser)
		result.Stats.TotalRowsIn += len(rows)

		for _, row := range rows {
			key := m.buildKey(row, keyIndices)

			if existingRow, exists := rowMap[key]; exists {
				result.Stats.Duplicates++

				// Есть дубликат - разрешаем конфликт
				useNew := false
				if m.options.ConflictResolver != nil {
					useNew = m.options.ConflictResolver(existingRow, row, schema)
				}

				if useNew {
					rowMap[key] = row
					result.Conflicts = append(result.Conflicts, Conflict{
						Key:            key,
						ExistingRow:    existingRow,
						ConflictingRow: row,
						Resolution:     "used_new",
					})
				} else {
					result.Conflicts = append(result.Conflicts, Conflict{
						Key:            key,
						ExistingRow:    existingRow,
						ConflictingRow: row,
						Resolution:     "kept_existing",
					})
				}
				result.Stats.ConflictsCount++
			} else {
				rowMap[key] = row
			}
		}
	}

	// Собираем результат
	mergedRows := make([][]string, 0, len(rowMap))
	for _, row := range rowMap {
		mergedRows = append(mergedRows, row)
	}

	result.Packet = m.buildPacket(mergedRows, schema, packets[0].Header.TableName)
	result.Stats.TotalRowsOut = len(mergedRows)

	return result, nil
}

// mergeIntersection оставляет только строки, присутствующие во всех пакетах
func (m *Merger) mergeIntersection(packets []*packet.DataPacket, schema packet.Schema, keyFields []string) (*MergeResult, error) {
	parser := packet.NewParser()
	keyIndices := m.getFieldIndices(schema, keyFields)

	// Создаём map для первого пакета
	firstRows := m.parseRows(packets[0].Data.Rows, parser)
	keyCounts := make(map[string]int)
	rowMap := make(map[string][]string)

	for _, row := range firstRows {
		key := m.buildKey(row, keyIndices)
		keyCounts[key] = 1
		rowMap[key] = row
	}

	// Проверяем наличие в остальных пакетах
	for i := 1; i < len(packets); i++ {
		rows := m.parseRows(packets[i].Data.Rows, parser)
		for _, row := range rows {
			key := m.buildKey(row, keyIndices)
			if _, exists := keyCounts[key]; exists {
				keyCounts[key]++
			}
		}
	}

	// Оставляем только те, что есть во всех пакетах
	result := &MergeResult{
		Stats: MergeStats{
			TotalPackets: len(packets),
			TotalRowsIn:  len(firstRows),
		},
	}

	intersectionRows := make([][]string, 0)
	for key, count := range keyCounts {
		if count == len(packets) {
			intersectionRows = append(intersectionRows, rowMap[key])
		}
	}

	result.Packet = m.buildPacket(intersectionRows, schema, packets[0].Header.TableName)
	result.Stats.TotalRowsOut = len(intersectionRows)

	return result, nil
}

// mergeWithPriority объединяет с приоритетом (левым или правым)
func (m *Merger) mergeWithPriority(packets []*packet.DataPacket, schema packet.Schema, keyFields []string, leftPriority bool) (*MergeResult, error) {
	parser := packet.NewParser()
	keyIndices := m.getFieldIndices(schema, keyFields)

	rowMap := make(map[string][]string)
	result := &MergeResult{
		Stats: MergeStats{
			TotalPackets: len(packets),
		},
	}

	// Для leftPriority - прямой порядок, для rightPriority - обратный
	processingOrder := packets
	if !leftPriority {
		// Обрабатываем в обратном порядке для right priority
		processingOrder = make([]*packet.DataPacket, len(packets))
		for i := 0; i < len(packets); i++ {
			processingOrder[i] = packets[len(packets)-1-i]
		}
	}

	for _, pkt := range processingOrder {
		rows := m.parseRows(pkt.Data.Rows, parser)
		result.Stats.TotalRowsIn += len(rows)

		for _, row := range rows {
			key := m.buildKey(row, keyIndices)

			if existingRow, exists := rowMap[key]; exists {
				result.Stats.Duplicates++

				// При leftPriority оставляем существующую (первую)
				// При rightPriority перезаписываем новой (последней)
				if leftPriority {
					result.Conflicts = append(result.Conflicts, Conflict{
						Key:            key,
						ExistingRow:    existingRow,
						ConflictingRow: row,
						Resolution:     "kept_existing",
					})
				} else {
					rowMap[key] = row
					result.Conflicts = append(result.Conflicts, Conflict{
						Key:            key,
						ExistingRow:    existingRow,
						ConflictingRow: row,
						Resolution:     "used_new",
					})
				}
				result.Stats.ConflictsCount++
			} else {
				rowMap[key] = row
			}
		}
	}

	// Собираем результат
	mergedRows := make([][]string, 0, len(rowMap))
	for _, row := range rowMap {
		mergedRows = append(mergedRows, row)
	}

	result.Packet = m.buildPacket(mergedRows, schema, packets[0].Header.TableName)
	result.Stats.TotalRowsOut = len(mergedRows)

	return result, nil
}

// mergeAppend просто добавляет все строки без дедупликации
func (m *Merger) mergeAppend(packets []*packet.DataPacket, schema packet.Schema) (*MergeResult, error) {
	parser := packet.NewParser()

	allRows := make([][]string, 0)
	totalRowsIn := 0

	for _, pkt := range packets {
		rows := m.parseRows(pkt.Data.Rows, parser)
		totalRowsIn += len(rows)
		allRows = append(allRows, rows...)
	}

	result := &MergeResult{
		Packet: m.buildPacket(allRows, schema, packets[0].Header.TableName),
		Stats: MergeStats{
			TotalPackets: len(packets),
			TotalRowsIn:  totalRowsIn,
			TotalRowsOut: len(allRows),
		},
	}

	return result, nil
}

// buildPacket создаёт DataPacket из строк
func (m *Merger) buildPacket(rows [][]string, schema packet.Schema, tableName string) *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.TypeReference, tableName)
	pkt.Schema = schema

	generator := packet.NewGenerator()
	pkt.Data = generator.RowsToData(rows)

	return pkt
}

// Вспомогательные функции (такие же как в diff)

func (m *Merger) validateSchemas(schemaA, schemaB packet.Schema) error {
	if len(schemaA.Fields) != len(schemaB.Fields) {
		return fmt.Errorf("different number of fields: %d vs %d", len(schemaA.Fields), len(schemaB.Fields))
	}

	for i, fieldA := range schemaA.Fields {
		fieldB := schemaB.Fields[i]
		if fieldA.Name != fieldB.Name {
			return fmt.Errorf("field name mismatch at position %d: %s vs %s", i, fieldA.Name, fieldB.Name)
		}
	}

	return nil
}

func (m *Merger) extractKeyFields(schema packet.Schema) []string {
	var keys []string
	for _, field := range schema.Fields {
		if field.Key {
			keys = append(keys, field.Name)
		}
	}
	return keys
}

func (m *Merger) getFieldIndices(schema packet.Schema, fieldNames []string) []int {
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

func (m *Merger) parseRows(rows []packet.Row, parser *packet.Parser) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		result[i] = parser.GetRowValues(row)
	}
	return result
}

func (m *Merger) buildKey(row []string, keyIndices []int) string {
	var parts []string
	for _, idx := range keyIndices {
		if idx < len(row) {
			parts = append(parts, row[idx])
		}
	}
	return strings.Join(parts, "|")
}

// FormatText форматирует результат в текстовый вид
func (r *MergeResult) FormatText() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Merge Statistics ===\n"))
	sb.WriteString(fmt.Sprintf("Packets merged: %d\n", r.Stats.TotalPackets))
	sb.WriteString(fmt.Sprintf("Total rows in:  %d\n", r.Stats.TotalRowsIn))
	sb.WriteString(fmt.Sprintf("Total rows out: %d\n", r.Stats.TotalRowsOut))
	sb.WriteString(fmt.Sprintf("Duplicates:     %d\n", r.Stats.Duplicates))
	sb.WriteString(fmt.Sprintf("Conflicts:      %d\n\n", r.Stats.ConflictsCount))

	if len(r.Conflicts) > 0 && len(r.Conflicts) <= 10 {
		sb.WriteString("=== Conflicts ===\n")
		for _, c := range r.Conflicts {
			sb.WriteString(fmt.Sprintf("Key %s: %s\n", c.Key, c.Resolution))
		}
	} else if len(r.Conflicts) > 10 {
		sb.WriteString(fmt.Sprintf("=== Conflicts (%d total, showing first 10) ===\n", len(r.Conflicts)))
		for i := 0; i < 10; i++ {
			c := r.Conflicts[i]
			sb.WriteString(fmt.Sprintf("Key %s: %s\n", c.Key, c.Resolution))
		}
	}

	return sb.String()
}
