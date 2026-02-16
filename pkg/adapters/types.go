package adapters

import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// TypeMapper - интерфейс для маппинга типов данных
// Каждый адаптер должен реализовать свой TypeMapper для конвертации типов
// между TDTP и специфичными типами СУБД
type TypeMapper interface {
	// TDTPToSQL конвертирует поле TDTP в SQL тип
	// Пример:
	//   PostgreSQL: Field{Type:"INTEGER", Subtype:"bigint"} → "BIGINT"
	//   SQLite:     Field{Type:"INTEGER"} → "INTEGER"
	TDTPToSQL(field packet.Field) string

	// SQLToTDTP конвертирует SQL тип в поле TDTP
	// Пример:
	//   PostgreSQL: "VARCHAR(100)" → Field{Type:"TEXT", Length:100}
	//   SQLite:     "INTEGER" → Field{Type:"INTEGER"}
	SQLToTDTP(sqlType string, nullable bool) (packet.Field, error)

	// ValueToString конвертирует значение БД в строку для TDTP пакета
	// Обрабатывает специфичные типы каждой СУБД (UUID, JSON, массивы и т.д.)
	ValueToString(value any) string

	// StringToValue конвертирует строку из TDTP пакета в значение для БД
	// Используется при импорте данных
	StringToValue(str string, field packet.Field) (any, error)
}

// QueryBuilder - интерфейс для построения SQL запросов
// Каждый адаптер реализует свой QueryBuilder с учетом синтаксиса СУБД
type QueryBuilder interface {
	// BuildSelectAll строит SELECT * FROM table
	BuildSelectAll(tableName string) string

	// BuildSelectWithFilter строит SELECT с WHERE/LIMIT/OFFSET
	// Возвращает SQL запрос и массив параметров
	BuildSelectWithFilter(tableName string, query *packet.Query) (sql string, args []any, err error)

	// BuildInsert строит INSERT INTO table (cols) VALUES (...)
	BuildInsert(tableName string, fields []packet.Field) string

	// BuildUpsert строит запрос для UPSERT операции
	// pkFields - список колонок Primary Key
	// Синтаксис зависит от СУБД:
	//   PostgreSQL: INSERT ... ON CONFLICT DO UPDATE
	//   SQLite:     INSERT OR REPLACE
	//   MS SQL:     MERGE
	BuildUpsert(tableName string, fields []packet.Field, pkFields []string) string

	// BuildCreateTable строит CREATE TABLE запрос
	BuildCreateTable(tableName string, schema packet.Schema) string

	// QuoteIdentifier экранирует идентификатор (имя таблицы/колонки)
	// PostgreSQL: "table_name"
	// SQLite:     "table_name" или `table_name`
	// MS SQL:     [table_name]
	QuoteIdentifier(identifier string) string
}

// ========== Вспомогательные типы ==========

// SchemaInfo - информация о схеме таблицы (для СУБД с поддержкой схем)
type SchemaInfo struct {
	Name        string   // Имя схемы
	Tables      []string // Список таблиц в схеме
	Description string   // Описание схемы (если есть)
}

// ColumnInfo - расширенная информация о колонке
type ColumnInfo struct {
	Name         string       // Имя колонки
	Type         string       // SQL тип
	TDTPField    packet.Field // TDTP представление
	Nullable     bool         // Допускает NULL
	DefaultValue *string      // Значение по умолчанию
	IsPrimaryKey bool         // Является ли Primary Key
	IsAutoIncr   bool         // Auto increment/SERIAL
	Comment      string       // Комментарий (если поддерживается)
}

// TableInfo - расширенная информация о таблице
type TableInfo struct {
	Name      string       // Имя таблицы
	Schema    string       // Схема (для PostgreSQL/MS SQL)
	Columns   []ColumnInfo // Колонки
	RowCount  int64        // Примерное количество строк
	SizeBytes int64        // Размер таблицы в байтах (если доступно)
	Comment   string       // Комментарий таблицы
}

// DatabaseInfo - информация о БД
type DatabaseInfo struct {
	Type      string       // "sqlite", "postgres", "mssql"
	Version   string       // Версия СУБД
	Schemas   []SchemaInfo // Схемы (для PostgreSQL/MS SQL)
	Tables    []TableInfo  // Таблицы
	TotalSize int64        // Общий размер БД
	Encoding  string       // Кодировка
}

// ========== Опции для импорта/экспорта ==========

// ExportOptions - опции для экспорта данных
type ExportOptions struct {
	// BatchSize - размер батча (количество строк в одном пакете)
	BatchSize int

	// IncludeSchema - включать ли схему в пакеты
	IncludeSchema bool

	// Sender - отправитель (для заполнения Header.Sender)
	Sender string

	// Recipient - получатель (для заполнения Header.Recipient)
	Recipient string

	// Compression - использовать ли сжатие (будущая функциональность)
	Compression bool
}

// ImportOptions - опции для импорта данных
type ImportOptions struct {
	// Strategy - стратегия импорта
	Strategy ImportStrategy

	// CreateTable - создавать ли таблицу автоматически
	CreateTable bool

	// TruncateFirst - очистить таблицу перед импортом
	TruncateFirst bool

	// BatchSize - размер батча для массовых вставок
	BatchSize int

	// UseTransaction - использовать транзакцию
	UseTransaction bool

	// ContinueOnError - продолжать при ошибках (не рекомендуется)
	ContinueOnError bool
}

// DefaultExportOptions возвращает опции экспорта по умолчанию
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		BatchSize:     1000,
		IncludeSchema: true,
		Compression:   false,
	}
}

// DefaultImportOptions возвращает опции импорта по умолчанию
func DefaultImportOptions() ImportOptions {
	return ImportOptions{
		Strategy:        StrategyReplace,
		CreateTable:     true,
		TruncateFirst:   false,
		BatchSize:       1000,
		UseTransaction:  true,
		ContinueOnError: false,
	}
}
