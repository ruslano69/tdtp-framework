package base

import (
	"context"
	"fmt"

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

// ExportHelper содержит общую логику экспорта для всех адаптеров
// Устраняет дублирование кода между SQLite, PostgreSQL, MS SQL Server, MySQL
type ExportHelper struct {
	schemaReader   SchemaReader
	dataReader     DataReader
	valueConverter ValueConverter
	sqlAdapter     SQLAdapter
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

	// 3. Генерируем reference пакеты
	generator := packet.NewGenerator()
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
	// 1. Получаем схему
	pkgSchema, err := h.schemaReader.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// 2. Валидация полей запроса (поля фильтров и ORDER BY) до чтения данных
	executor := tdtql.NewExecutor()
	if err := executor.ValidateQuery(query, pkgSchema); err != nil {
		return nil, err
	}
	// Нормализация имён полей к каноническим из схемы (критично для PostgreSQL quoted identifiers)
	executor.NormalizeQueryFields(query, pkgSchema)

	// 3. Пробуем транслировать TDTQL → SQL для оптимизации (pushdown filtering)
	sqlGenerator := tdtql.NewSQLGenerator()
	if sqlGenerator.CanTranslateToSQL(query) {
		// Оптимизированный путь: фильтрация на уровне SQL
		standardSQL, err := sqlGenerator.GenerateSQL(tableName, query)
		if err == nil {
			// Адаптируем SQL под конкретную СУБД (если нужно)
			adaptedSQL := standardSQL
			if h.sqlAdapter != nil {
				adaptedSQL = h.sqlAdapter.AdaptSQL(standardSQL, tableName, pkgSchema, query)
			}

			// Выполняем SQL запрос напрямую
			rows, err := h.dataReader.ReadRowsWithSQL(ctx, adaptedSQL, pkgSchema)
			if err == nil {
				// Генерируем Response пакеты
				queryContext := h.createQueryContextForSQL(ctx, query, rows, tableName)

				generator := packet.NewGenerator()
				return generator.GenerateResponse(
					tableName,
					"",
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
	rows, err := h.dataReader.ReadAllRows(ctx, tableName, pkgSchema)
	if err != nil {
		return nil, err
	}

	// Применяем TDTQL фильтрацию в памяти
	result, err := executor.Execute(query, rows, pkgSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Генерируем Response пакеты с QueryContext
	generator := packet.NewGenerator()
	return generator.GenerateResponse(
		tableName,
		"",
		pkgSchema,
		result.FilteredRows,
		result.QueryContext,
		sender,
		recipient,
	)
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Общая реализация для адаптеров поддерживающих инкрементальную синхронизацию
func (h *ExportHelper) ExportTableIncremental(
	ctx context.Context,
	tableName string,
	incrementalConfig adapters.IncrementalConfig,
	buildIncrementalSQL func(tableName string, config adapters.IncrementalConfig) (string, []interface{}),
	executeIncrementalQuery func(ctx context.Context, sql string, args []interface{}, schema packet.Schema) ([][]string, string, error),
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
	generator := packet.NewGenerator()
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
