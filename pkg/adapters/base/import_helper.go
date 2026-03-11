package base

import (
	"context"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// TableManager предоставляет методы для управления таблицами
type TableManager interface {
	// TableExists проверяет существование таблицы
	TableExists(ctx context.Context, tableName string) (bool, error)

	// CreateTable создает таблицу по TDTP схеме
	CreateTable(ctx context.Context, tableName string, schema packet.Schema) error

	// DropTable удаляет таблицу
	DropTable(ctx context.Context, tableName string) error

	// RenameTable переименовывает таблицу
	RenameTable(ctx context.Context, oldName, newName string) error
}

// DataInserter предоставляет методы для вставки данных
type DataInserter interface {
	// InsertRows вставляет строки данных с использованием стратегии
	InsertRows(ctx context.Context, tableName string, schema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error
}

// TransactionManager предоставляет методы для работы с транзакциями
type TransactionManager interface {
	// BeginTx начинает транзакцию
	BeginTx(ctx context.Context) (adapters.Tx, error)
}

// ImportHelper содержит общую логику импорта для всех адаптеров
// Устраняет дублирование кода между адаптерами
type ImportHelper struct {
	tableManager       TableManager
	dataInserter       DataInserter
	transactionManager TransactionManager
	useTemporaryTables bool // Использовать ли временные таблицы для атомарной замены
}

// NewImportHelper создает новый ImportHelper
func NewImportHelper(
	tableManager TableManager,
	dataInserter DataInserter,
	transactionManager TransactionManager,
	useTemporaryTables bool,
) *ImportHelper {
	return &ImportHelper{
		tableManager:       tableManager,
		dataInserter:       dataInserter,
		transactionManager: transactionManager,
		useTemporaryTables: useTemporaryTables,
	}
}

// ImportPacket импортирует один TDTP пакет в БД
// Общая реализация для всех адаптеров
func (h *ImportHelper) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	// Проверяем тип пакета
	if pkt.Header.Type != packet.TypeReference && pkt.Header.Type != packet.TypeResponse {
		return fmt.Errorf("can only import reference or response packets, got: %s", pkt.Header.Type)
	}

	tableName := pkt.Header.TableName

	// Если включен режим временных таблиц - используем атомарную замену
	if h.useTemporaryTables {
		return h.importWithTemporaryTable(ctx, pkt, strategy)
	}

	// Иначе - прямая вставка в таблицу
	return h.importDirect(ctx, tableName, pkt.Schema, pkt.Data.Rows, strategy)
}

// ImportPackets импортирует несколько пакетов атомарно (в одной транзакции)
// Общая реализация для всех адаптеров
func (h *ImportHelper) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

	tableName := packets[0].Header.TableName

	// Начинаем транзакцию
	tx, err := h.transactionManager.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx) // игнорируем ошибку rollback при ошибке импорта
		}
	}()

	// Если включен режим временных таблиц - используем один раз
	if h.useTemporaryTables {
		tempTableName := GenerateTempTableName(tableName)
		fmt.Printf("📋 Import %d packets to temporary table: %s\n", len(packets), tempTableName)

		// 1. Создаем временную таблицу (используем схему из первого пакета)
		if err := h.tableManager.CreateTable(ctx, tempTableName, packets[0].Schema); err != nil {
			return fmt.Errorf("failed to create temporary table: %w", err)
		}

		// 2. Импортируем каждый пакет во временную таблицу
		canonicalSchema := packets[0].Schema
		for i, pkt := range packets {
			if !packet.SchemaEquals(canonicalSchema, pkt.Schema) {
				fmt.Printf("  ⚠️  Skipping packet %d/%d: schema mismatch (expected %d fields, got %d)\n",
					i+1, len(packets), len(canonicalSchema.Fields), len(pkt.Schema.Fields))
				continue
			}

			fmt.Printf("  📦 Importing packet %d/%d\n", i+1, len(packets))

			if err := h.dataInserter.InsertRows(ctx, tempTableName, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
				_ = h.tableManager.DropTable(ctx, tempTableName) // игнорируем ошибку cleanup
				return fmt.Errorf("failed to import packet %d: %w", i+1, err)
			}
		}

		fmt.Printf("✅ All packets loaded to temporary table\n")
		fmt.Printf("🔄 Replacing production table: %s\n", tableName)

		// 3. Заменяем продакшен таблицу временной
		if err := h.replaceTables(ctx, tableName, tempTableName); err != nil {
			_ = h.tableManager.DropTable(ctx, tempTableName) // игнорируем ошибку cleanup
			return fmt.Errorf("failed to replace tables: %w", err)
		}

	} else {
		// Прямая вставка без временных таблиц
		canonicalSchema := packets[0].Schema
		for i, pkt := range packets {
			if !packet.SchemaEquals(canonicalSchema, pkt.Schema) {
				fmt.Printf("  ⚠️  Skipping packet %d/%d: schema mismatch (expected %d fields, got %d)\n",
					i+1, len(packets), len(canonicalSchema.Fields), len(pkt.Schema.Fields))
				continue
			}

			fmt.Printf("  📦 Importing packet %d/%d\n", i+1, len(packets))

			if err := h.importDirect(ctx, tableName, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
				return fmt.Errorf("failed to import packet %d: %w", i+1, err)
			}
		}
	}

	// Коммитим транзакцию
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("✅ Import completed successfully\n")

	return nil
}

// importWithTemporaryTable импортирует данные через временную таблицу (атомарная замена)
func (h *ImportHelper) importWithTemporaryTable(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	tableName := pkt.Header.TableName
	tempTableName := GenerateTempTableName(tableName)

	fmt.Printf("📋 Import to temporary table: %s\n", tempTableName)

	// 1. Создаем временную таблицу
	if err := h.tableManager.CreateTable(ctx, tempTableName, pkt.Schema); err != nil {
		return fmt.Errorf("failed to create temporary table: %w", err)
	}

	// 2. Импортируем данные во временную таблицу
	if err := h.dataInserter.InsertRows(ctx, tempTableName, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
		// Откатываем - удаляем временную таблицу
		_ = h.tableManager.DropTable(ctx, tempTableName) // игнорируем ошибку cleanup
		return fmt.Errorf("failed to import to temporary table: %w", err)
	}

	fmt.Printf("✅ Data loaded to temporary table\n")
	fmt.Printf("🔄 Replacing production table: %s\n", tableName)

	// 3. Заменяем продакшен таблицу временной (атомарная операция)
	if err := h.replaceTables(ctx, tableName, tempTableName); err != nil {
		// Откатываем - удаляем временную таблицу
		_ = h.tableManager.DropTable(ctx, tempTableName) // игнорируем ошибку cleanup
		return fmt.Errorf("failed to replace tables: %w", err)
	}

	fmt.Printf("✅ Production table replaced successfully\n")

	return nil
}

// importDirect импортирует данные напрямую в таблицу (без временных таблиц)
func (h *ImportHelper) importDirect(ctx context.Context, tableName string, pkgSchema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	// Проверяем существование таблицы
	exists, err := h.tableManager.TableExists(ctx, tableName)
	if err != nil {
		return err
	}

	// Если таблицы нет - создаем
	if !exists {
		if err := h.tableManager.CreateTable(ctx, tableName, pkgSchema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Вставляем данные
	return h.dataInserter.InsertRows(ctx, tableName, pkgSchema, rows, strategy)
}

// replaceTables заменяет продакшен таблицу временной (атомарная операция)
// Общая логика для всех адаптеров:
// 1. Если prod таблица существует: old_table ← prod_table, prod_table ← temp_table, DROP old_table
// 2. Если prod таблицы нет: prod_table ← temp_table
func (h *ImportHelper) replaceTables(ctx context.Context, targetTable, tempTable string) error {
	// Проверяем существует ли целевая таблица
	exists, err := h.tableManager.TableExists(ctx, targetTable)
	if err != nil {
		return err
	}

	if exists {
		// Если таблица существует - делаем атомарную замену
		oldTableName := targetTable + "_old"

		// 1. Переименовываем старую таблицу в _old
		if err := h.tableManager.RenameTable(ctx, targetTable, oldTableName); err != nil {
			return fmt.Errorf("failed to rename old table: %w", err)
		}

		// 2. Переименовываем временную таблицу в продакшен
		if err := h.tableManager.RenameTable(ctx, tempTable, targetTable); err != nil {
			// Откатываем - возвращаем старое имя
			if rollbackErr := h.tableManager.RenameTable(ctx, oldTableName, targetTable); rollbackErr != nil {
				return fmt.Errorf("failed to rename temp table: %w; rollback also failed: %v", err, rollbackErr)
			}
			return fmt.Errorf("failed to rename temp table: %w", err)
		}

		// 3. Удаляем старую таблицу
		if err := h.tableManager.DropTable(ctx, oldTableName); err != nil {
			// Не критично, можно оставить для ручной очистки
			fmt.Printf("⚠️  Warning: failed to drop old table %s: %v\n", oldTableName, err)
		}
	} else {
		// Если таблицы нет - просто переименовываем временную
		if err := h.tableManager.RenameTable(ctx, tempTable, targetTable); err != nil {
			return fmt.Errorf("failed to rename temp table: %w", err)
		}
	}

	return nil
}

// ParseRowValues парсит строку TDTP в массив значений
// Общая утилита для всех адаптеров
func ParseRowValues(row packet.Row) []string {
	parser := packet.NewParser()
	return parser.GetRowValues(row)
}

// ConvertRowToSQLValues конвертирует строку TDTP в SQL значения для PreparedStatement
// Общая утилита для всех адаптеров
func ConvertRowToSQLValues(
	rowValues []string,
	pkgSchema packet.Schema,
	converter *UniversalTypeConverter,
	dbType string,
) ([]any, error) {
	if len(rowValues) != len(pkgSchema.Fields) {
		return nil, fmt.Errorf("expected %d values, got %d", len(pkgSchema.Fields), len(rowValues))
	}

	schemaConverter := schema.NewConverter()
	args := make([]any, len(rowValues))

	for i, value := range rowValues {
		field := pkgSchema.Fields[i]

		// Декодируем маркеры SpecialValues (v1.3.1) перед разбором типа.
		// Это гарантирует что SQL NULL восстанавливается корректно для всех типов,
		// включая TEXT где "" — валидная пустая строка, а не NULL.
		if sv := field.SpecialValues; sv != nil {
			if sv.Null != nil && value == sv.Null.Marker {
				args[i] = nil // принудительный SQL NULL
				continue
			}
			// Числовые specials: приводим к strconv-совместимым значениям
			if sv.Infinity != nil && value == sv.Infinity.Marker {
				value = "+Inf"
			} else if sv.NegInfinity != nil && value == sv.NegInfinity.Marker {
				value = "-Inf"
			} else if sv.NaN != nil && value == sv.NaN.Marker {
				value = "NaN"
			}
		}

		// Для ключевых полей (PRIMARY KEY) NULL не допускается
		nullable := true
		if field.Key {
			nullable = false
		}

		fieldDef := schema.FieldDef{
			Name:      field.Name,
			Type:      schema.DataType(field.Type),
			Length:    field.Length,
			Precision: field.Precision,
			Scale:     field.Scale,
			Timezone:  field.Timezone,
			Key:       field.Key,
			Nullable:  nullable, // Ключевые поля: false, остальные: true
		}

		// Парсим значение
		typedValue, err := schemaConverter.ParseValue(value, fieldDef)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", fieldDef.Name, err)
		}

		// Конвертируем в SQL значение
		args[i] = converter.TypedValueToSQL(*typedValue, dbType)
	}

	return args, nil
}

// GenerateTempTableName генерирует имя временной таблицы
// Формат: {table_name}_tmp_{timestamp}
func GenerateTempTableName(baseName string) string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_tmp_%s", baseName, timestamp)
}
