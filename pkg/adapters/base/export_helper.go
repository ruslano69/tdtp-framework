package base

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// SchemaReader предоставляет методы для чтения схемы таблицы
type SchemaReader interface {
	GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error)
}

// DataReader предоставляет методы для чтения данных из таблицы
type DataReader interface {
	// ReadAllRows читает все строки из таблицы
	ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error)

	// ReadRowsWithSQL выполняет SQL запрос и возвращает строки
	ReadRowsWithSQL(ctx context.Context, sql string, schema packet.Schema) ([][]string, error)

	// GetRowCount возвращает количество строк в таблице
	GetRowCount(ctx context.Context, tableName string) (int64, error)
}

// ValueConverter предоставляет методы для конвертации значений
type ValueConverter interface {
	// ConvertValueToTDTP конвертирует значение из БД в TDTP формат
	ConvertValueToTDTP(field packet.Field, value string) string
}

// SQLAdapter предоставляет методы для адаптации SQL под конкретную СУБД
type SQLAdapter interface {
	// AdaptSQL адаптирует стандартный SQL под синтаксис СУБД
	AdaptSQL(standardSQL string, tableName string, schema packet.Schema, query *packet.Query) string
}

// RowPostProcessor — опциональный интерфейс для постобработки строк после чтения.
// Адаптеры реализуют его когда нужна специфичная фильтрация столбцов
// (например, MSSQL фильтрует read-only поля: identity, computed, timestamp).
// ExportHelper проверяет DataReader на реализацию этого интерфейса
// и вызывает PostProcessRows перед сборкой пакетов.
type RowPostProcessor interface {
	PostProcessRows(ctx context.Context, schema packet.Schema, rows [][]string) (packet.Schema, [][]string)
}

// ExportHelper содержит общую логику экспорта для всех адаптеров
// Устраняет дублирование кода между SQLite, PostgreSQL, MS SQL Server, MySQL
type ExportHelper struct {
	schemaReader   SchemaReader
	dataReader     DataReader
	valueConverter ValueConverter
	sqlAdapter     SQLAdapter
	maxMessageSize int // 0 = use generator default
}

// NewExportHelper создает новый ExportHelper
func NewExportHelper(
	schemaReader SchemaReader,
	dataReader DataReader,
	valueConverter ValueConverter,
	sqlAdapter SQLAdapter,
) *ExportHelper {
	return &ExportHelper{
		schemaReader:   schemaReader,
		dataReader:     dataReader,
		valueConverter: valueConverter,
		sqlAdapter:     sqlAdapter,
	}
}

// SetMaxMessageSize задаёт максимальный размер одного TDTP пакета в байтах.
// Используется адаптерами для передачи настройки --packet-size из CLI.
func (h *ExportHelper) SetMaxMessageSize(size int) {
	h.maxMessageSize = size
}

// newGenerator возвращает генератор с учётом настройки maxMessageSize.
func (h *ExportHelper) newGenerator() *packet.Generator {
	g := packet.NewGenerator()
	if h.maxMessageSize > 0 {
		g.SetMaxMessageSize(h.maxMessageSize)
	}
	return g
}

// ExportTable экспортирует всю таблицу в TDTP reference пакеты
// Общая реализация для всех адаптеров
func (h *ExportHelper) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	// 1. Получаем схему
	schema, err := h.schemaReader.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// 2. Читаем все данные
	rows, err := h.dataReader.ReadAllRows(ctx, tableName, schema)
	if err != nil {
		return nil, err
	}

	// 3. Постобработка (опционально): адаптер может отфильтровать столбцы (например, MSSQL read-only)
	if pp, ok := h.dataReader.(RowPostProcessor); ok {
		schema, rows = pp.PostProcessRows(ctx, schema, rows)
	}

	// 4. Генерируем reference пакеты
	generator := h.newGenerator()
	return generator.GenerateReference(tableName, schema, rows)
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
// Общая реализация с SQL оптимизацией для всех адаптеров
func (h *ExportHelper) ExportTableWithQuery(
	ctx context.Context,
	tableName string,
	query *packet.Query,
	sender, recipient string,
) ([]*packet.DataPacket, error) {
	// query == nil означает полный экспорт без фильтрации — делегируем в ExportTable
	if query == nil {
		return h.ExportTable(ctx, tableName)
	}

	// 1. Получаем полную схему таблицы
	fullSchema, err := h.schemaReader.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// 2. Если задана проекция колонок — валидируем и строим filtered schema
	pkgSchema := fullSchema
	var fieldIndices []int // позиции выбранных полей в полной схеме (для fallback)
	if len(query.Fields) > 0 {
		pkgSchema, fieldIndices, err = filterSchemaByFields(fullSchema, query.Fields)
		if err != nil {
			return nil, err
		}
	}

	// 3. Валидация полей запроса (фильтры и ORDER BY) до чтения данных
	executor := tdtql.NewExecutor()
	if err := executor.ValidateQuery(query, fullSchema); err != nil {
		return nil, err
	}
	// Нормализация имён полей к каноническим из схемы (критично для PostgreSQL quoted identifiers)
	executor.NormalizeQueryFields(query, fullSchema)

	// 4. Пробуем транслировать TDTQL → SQL для оптимизации (pushdown filtering)
	sqlGenerator := tdtql.NewSQLGenerator()
	if sqlGenerator.CanTranslateToSQL(query) {
		// Оптимизированный путь: фильтрация на уровне SQL
		standardSQL, err := sqlGenerator.GenerateSQL(tableName, query)
		if err == nil {
			// Адаптируем SQL под конкретную СУБД (если нужно)
			adaptedSQL := standardSQL
			if h.sqlAdapter != nil {
				adaptedSQL = h.sqlAdapter.AdaptSQL(standardSQL, tableName, fullSchema, query)
			}

			// Выполняем SQL запрос с filtered schema (количество колонок совпадает)
			rows, err := h.dataReader.ReadRowsWithSQL(ctx, adaptedSQL, pkgSchema)
			if err == nil {
				// Постобработка (опционально): фильтрация read-only полей и т.п.
				if pp, ok := h.dataReader.(RowPostProcessor); ok {
					pkgSchema, rows = pp.PostProcessRows(ctx, pkgSchema, rows)
				}

				queryContext := h.createQueryContextForSQL(ctx, query, rows, tableName)

				generator := h.newGenerator()
				return generator.GenerateResponse(
					tableName,
					packet.InReplyToDirectExport,
					pkgSchema,
					rows,
					queryContext,
					sender,
					recipient,
				)
			}
			// Если SQL запрос не удался, fallback на in-memory фильтрацию
		}
	}

	// Fallback путь: in-memory фильтрация (для сложных запросов или если SQL не удался)
	allRows, err := h.dataReader.ReadAllRows(ctx, tableName, fullSchema)
	if err != nil {
		return nil, err
	}

	// Применяем TDTQL фильтрацию в памяти (по полной схеме)
	result, err := executor.Execute(query, allRows, fullSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Применяем проекцию колонок если задана (после фильтрации)
	filteredRows := result.FilteredRows
	filteredSchema := fullSchema
	if len(fieldIndices) > 0 {
		filteredRows = projectRows(filteredRows, fieldIndices)
		filteredSchema = pkgSchema
	}

	// Постобработка (опционально): фильтрация read-only полей и т.п.
	if pp, ok := h.dataReader.(RowPostProcessor); ok {
		filteredSchema, filteredRows = pp.PostProcessRows(ctx, filteredSchema, filteredRows)
	}

	// Генерируем Response пакеты с QueryContext
	generator := h.newGenerator()
	return generator.GenerateResponse(
		tableName,
		packet.InReplyToDirectExport,
		filteredSchema,
		filteredRows,
		result.QueryContext,
		sender,
		recipient,
	)
}

// FilterSchemaByFields возвращает схему только с запрошенными полями и их индексы
// в исходной полной схеме. Возвращает ошибку если поле не найдено.
// Экспортируется для использования в адаптерах (например, mssql).
func FilterSchemaByFields(full packet.Schema, fields []string) (packet.Schema, []int, error) {
	return filterSchemaByFields(full, fields)
}

// filterSchemaByFields возвращает схему только с запрошенными полями и их индексы
// в исходной полной схеме. Возвращает ошибку если поле не найдено.
func filterSchemaByFields(full packet.Schema, fields []string) (packet.Schema, []int, error) {
	nameToIdx := make(map[string]int, len(full.Fields))
	for i, f := range full.Fields {
		nameToIdx[strings.ToLower(f.Name)] = i
	}

	var filtered packet.Schema
	indices := make([]int, 0, len(fields))
	for _, name := range fields {
		idx, ok := nameToIdx[strings.ToLower(name)]
		if !ok {
			return packet.Schema{}, nil, fmt.Errorf("field '%s' not found in schema", name)
		}
		filtered.Fields = append(filtered.Fields, full.Fields[idx])
		indices = append(indices, idx)
	}
	return filtered, indices, nil
}

// ProjectRows возвращает только колонки по указанным индексам из каждой строки.
// Экспортируется для использования в адаптерах (например, mssql).
func ProjectRows(rows [][]string, indices []int) [][]string {
	return projectRows(rows, indices)
}

// projectRows возвращает только колонки по указанным индексам из каждой строки.
func projectRows(rows [][]string, indices []int) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		projected := make([]string, len(indices))
		for j, idx := range indices {
			if idx < len(row) {
				projected[j] = row[idx]
			}
		}
		result[i] = projected
	}
	return result
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Общая реализация для адаптеров поддерживающих инкрементальную синхронизацию
func (h *ExportHelper) ExportTableIncremental(
	ctx context.Context,
	tableName string,
	incrementalConfig adapters.IncrementalConfig,
	buildIncrementalSQL func(tableName string, config adapters.IncrementalConfig) (string, []any),
	executeIncrementalQuery func(ctx context.Context, sql string, args []any, schema packet.Schema) ([][]string, string, error),
) ([]*packet.DataPacket, string, error) {
	// Валидация конфигурации
	if err := incrementalConfig.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid incremental config: %w", err)
	}

	// Получаем схему
	pkgSchema, err := h.schemaReader.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, "", err
	}

	// Проверяем что tracking field существует в схеме
	trackingFieldExists := false
	for _, field := range pkgSchema.Fields {
		if field.Name == incrementalConfig.TrackingField {
			trackingFieldExists = true
			break
		}
	}

	if !trackingFieldExists {
		return nil, "", fmt.Errorf("tracking field '%s' not found in table schema", incrementalConfig.TrackingField)
	}

	// Построение SQL запроса (делегируем адаптеру, т.к. синтаксис разный)
	sql, args := buildIncrementalSQL(tableName, incrementalConfig)

	// Выполнение запроса (делегируем адаптеру для специфичной обработки типов)
	rows, lastTrackingValue, err := executeIncrementalQuery(ctx, sql, args, pkgSchema)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read incremental data: %w", err)
	}

	// Если нет данных, возвращаем пустой результат
	if len(rows) == 0 {
		return []*packet.DataPacket{}, incrementalConfig.InitialValue, nil
	}

	// Генерируем пакеты
	generator := h.newGenerator()
	packets, err := generator.GenerateReference(tableName, pkgSchema, rows)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate packets: %w", err)
	}

	return packets, lastTrackingValue, nil
}

// createQueryContextForSQL создает QueryContext для SQL-based export
// Общая реализация для всех адаптеров
func (h *ExportHelper) createQueryContextForSQL(
	ctx context.Context,
	query *packet.Query,
	rows [][]string,
	tableName string,
) *packet.QueryContext {
	// Получаем общее количество записей в таблице
	totalCount, err := h.dataReader.GetRowCount(ctx, tableName)
	if err != nil {
		totalCount = 0 // игнорируем ошибку, используем 0 если не удалось получить count
	}

	recordsReturned := len(rows)
	moreDataAvailable := false
	nextOffset := 0

	if query != nil && query.Limit > 0 {
		// Проверяем есть ли еще данные: offset + returned < total
		currentPosition := query.Offset + recordsReturned
		if currentPosition < int(totalCount) {
			moreDataAvailable = true
			nextOffset = query.Offset + recordsReturned
		}
	}

	return &packet.QueryContext{
		OriginalQuery: *query,
		ExecutionResults: packet.ExecutionResults{
			TotalRecordsInTable: int(totalCount),
			RecordsAfterFilters: recordsReturned,
			RecordsReturned:     recordsReturned,
			MoreDataAvailable:   moreDataAvailable,
			NextOffset:          nextOffset,
		},
	}
}
