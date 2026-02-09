package adapters

import (
	"context"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/sync"
)

// Type aliases для удобства
type IncrementalConfig = sync.IncrementalConfig

// Config - универсальная конфигурация подключения к БД
type Config struct {
	// Type - тип СУБД: "sqlite", "postgres", "mssql"
	Type string

	// DSN - строка подключения (connection string)
	// Примеры:
	//   SQLite:     "file:app.db"
	//   PostgreSQL: "postgresql://user:pass@localhost:5432/dbname"
	//   MS SQL:     "sqlserver://user:pass@localhost:1433?database=dbname"
	DSN string

	// Schema - схема по умолчанию (для PostgreSQL/MS SQL)
	// SQLite игнорирует это поле
	Schema string

	// Timeout - таймаут для запросов
	Timeout time.Duration

	// MaxConns - максимальное количество подключений в пуле
	MaxConns int

	// MinConns - минимальное количество idle подключений
	MinConns int

	// SSL - настройки SSL/TLS
	SSL SSLConfig

	// CompatibilityMode - режим совместимости для MS SQL Server
	// Значения: "2012", "2016", "2019", "auto" (по умолчанию)
	// Используется только для MS SQL Server adapter
	CompatibilityMode string

	// StrictCompatibility - строгий режим совместимости
	// Если true, ошибка при попытке использовать недоступные функции
	// Если false, предупреждение и fallback на альтернативные методы
	StrictCompatibility bool

	// WarnOnIncompatible - предупреждать о несовместимых функциях
	// Показывает предупреждения когда используются функции недоступные
	// в текущем compatibility mode
	WarnOnIncompatible bool
}

// SSLConfig - настройки SSL/TLS подключения
type SSLConfig struct {
	// Mode - режим SSL:
	//   "disable"     - без SSL
	//   "require"     - требовать SSL
	//   "verify-ca"   - проверять CA сертификат
	//   "verify-full" - полная проверка сертификата
	Mode string

	// CertPath - путь к клиентскому сертификату
	CertPath string

	// KeyPath - путь к приватному ключу
	KeyPath string

	// CAPath - путь к CA сертификату
	CAPath string
}

// Adapter - универсальный интерфейс для всех адаптеров БД
// Этот интерфейс реализуется каждым специфичным адаптером (SQLite, PostgreSQL, MS SQL)
type Adapter interface {
	// ========== Lifecycle ==========

	// Connect устанавливает подключение к БД
	Connect(ctx context.Context, cfg Config) error

	// Close закрывает подключение к БД
	Close(ctx context.Context) error

	// Ping проверяет доступность БД
	Ping(ctx context.Context) error

	// ========== Export ==========

	// ExportTable экспортирует всю таблицу в TDTP пакеты
	ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)

	// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
	ExportTableWithQuery(
		ctx context.Context,
		tableName string,
		query *packet.Query,
		sender, recipient string,
	) ([]*packet.DataPacket, error)

	// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
	// Использует IncrementalConfig для отслеживания изменений
	// Возвращает пакеты и последнее значение tracking поля для следующей синхронизации
	ExportTableIncremental(
		ctx context.Context,
		tableName string,
		incrementalConfig IncrementalConfig,
	) ([]*packet.DataPacket, string, error)

	// ========== Import ==========

	// ImportPacket импортирует один TDTP пакет в БД
	ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy ImportStrategy) error

	// ImportPackets импортирует множество пакетов атомарно (в одной транзакции)
	ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy ImportStrategy) error

	// ========== Schema ==========

	// GetTableSchema возвращает схему таблицы в формате TDTP
	GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error)

	// GetTableNames возвращает список всех таблиц в БД
	GetTableNames(ctx context.Context) ([]string, error)

	// GetViewNames возвращает список всех views в БД с информацией об updatable/read-only
	GetViewNames(ctx context.Context) ([]ViewInfo, error)

	// TableExists проверяет существование таблицы
	TableExists(ctx context.Context, tableName string) (bool, error)

	// ========== Transactions ==========

	// BeginTx начинает транзакцию
	BeginTx(ctx context.Context) (Tx, error)

	// ========== Metadata ==========

	// GetDatabaseVersion возвращает версию СУБД
	GetDatabaseVersion(ctx context.Context) (string, error)

	// GetDatabaseType возвращает тип СУБД: "sqlite", "postgres", "mssql"
	GetDatabaseType() string
}

// Tx - интерфейс транзакции
// Позволяет выполнять операции атомарно
type Tx interface {
	// Commit фиксирует изменения транзакции
	Commit(ctx context.Context) error

	// Rollback откатывает изменения транзакции
	Rollback(ctx context.Context) error
}

// ViewInfo - информация о database view
type ViewInfo struct {
	// Name - имя view
	Name string

	// IsUpdatable - можно ли выполнять INSERT/UPDATE/DELETE на этом view
	// true  = updatable view (можно импортировать)
	// false = read-only view (только экспорт)
	IsUpdatable bool
}

// ImportStrategy - стратегия импорта данных
type ImportStrategy string

const (
	// StrategyReplace - UPSERT (вставить или обновить)
	// SQLite:     INSERT OR REPLACE
	// PostgreSQL: INSERT ... ON CONFLICT DO UPDATE
	// MS SQL:     MERGE
	StrategyReplace ImportStrategy = "replace"

	// StrategyIgnore - пропустить дубликаты
	// SQLite:     INSERT OR IGNORE
	// PostgreSQL: INSERT ... ON CONFLICT DO NOTHING
	// MS SQL:     MERGE с пропуском
	StrategyIgnore ImportStrategy = "ignore"

	// StrategyFail - ошибка при дубликатах
	// Все СУБД: обычный INSERT
	StrategyFail ImportStrategy = "fail"

	// StrategyCopy - массовая вставка (если поддерживается)
	// SQLite:     не поддерживается (fallback на StrategyFail)
	// PostgreSQL: COPY FROM
	// MS SQL:     BULK INSERT
	StrategyCopy ImportStrategy = "copy"
)
