/*
Package adapters предоставляет универсальный интерфейс для работы с различными СУБД.

# Архитектура двухуровневого адаптера

Пакет реализует паттерн "двухуровневый адаптер":

	┌─────────────────────────────────────────┐
	│    Business Logic (TDTP Core)           │
	│  - packet.DataPacket                    │
	│  - schema.Schema                        │
	│  - tdtql.Query                          │
	└─────────────────┬───────────────────────┘
	                  │
	┌─────────────────▼───────────────────────┐
	│  Level 1: Universal Adapter Interface   │  ← pkg/adapters/adapter.go
	│                                          │
	│  type Adapter interface {               │
	│    Connect(ctx, Config) error           │
	│    ExportTable(ctx, name) (...)         │
	│    ImportPacket(ctx, pkt, strategy)     │
	│    ...                                   │
	│  }                                       │
	└─────────────────┬───────────────────────┘
	                  │
	        ┌─────────┼─────────┐
	        │         │         │
	┌───────▼────┐ ┌──▼──────┐ ┌▼────────┐
	│ SQLite     │ │PostgreSQL│ │MS SQL   │  ← Level 2: Specific
	│ Adapter    │ │ Adapter  │ │Adapter  │     Implementations
	└────────────┘ └──────────┘ └─────────┘

# Level 1: Универсальный интерфейс

Level 1 определяет единый API для всех операций с БД:
  - Lifecycle: Connect, Close, Ping
  - Export: ExportTable, ExportTableWithQuery
  - Import: ImportPacket, ImportPackets
  - Schema: GetTableSchema, TableExists, GetTableNames
  - Transactions: BeginTx
  - Metadata: GetDatabaseVersion, GetDatabaseType

# Level 2: Специфичные реализации

Каждая СУБД имеет свою реализацию адаптера:
  - pkg/adapters/sqlite - SQLite адаптер
  - pkg/adapters/postgres - PostgreSQL адаптер
  - pkg/adapters/mssql - MS SQL Server адаптер (будущее)

Каждая реализация оптимизирована для своей СУБД:
  - PostgreSQL: поддержка COPY, JSONB, массивов, схем
  - SQLite: оптимизация для embedded БД
  - MS SQL: MERGE для UPSERT, специфика типов

# Использование

Основной способ создания адаптера - через фабрику:

	import "github.com/queuebridge/tdtp/pkg/adapters"

	// Создание PostgreSQL адаптера
	adapter, err := adapters.New(ctx, adapters.Config{
	    Type: "postgres",
	    DSN:  "postgresql://user:pass@localhost:5432/db",
	    Schema: "public",
	})
	if err != nil {
	    log.Fatal(err)
	}
	defer adapter.Close(ctx)

	// Экспорт таблицы
	packets, err := adapter.ExportTable(ctx, "users")
	if err != nil {
	    log.Fatal(err)
	}

	// Импорт в другую БД
	targetAdapter, _ := adapters.New(ctx, adapters.Config{
	    Type: "sqlite",
	    DSN:  "file:app.db",
	})
	defer targetAdapter.Close(ctx)

	err = targetAdapter.ImportPackets(ctx, packets, adapters.StrategyReplace)

# Регистрация адаптеров

Адаптеры регистрируются автоматически через init():

	// В pkg/adapters/postgres/adapter.go
	func init() {
	    adapters.Register("postgres", func() adapters.Adapter {
	        return &Adapter{}
	    })
	}

После импорта пакета адаптера он становится доступен через фабрику:

	import _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
	import _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"

# Стратегии импорта

Поддерживаются 4 стратегии:

  - StrategyReplace: UPSERT (вставить или обновить)
  - StrategyIgnore: пропустить дубликаты
  - StrategyFail: ошибка при дубликатах
  - StrategyCopy: массовая вставка (PostgreSQL COPY)

Пример:

	// UPSERT: обновить существующие строки
	adapter.ImportPacket(ctx, packet, adapters.StrategyReplace)

	// Игнорировать дубликаты
	adapter.ImportPacket(ctx, packet, adapters.StrategyIgnore)

	// Максимальная производительность (только PostgreSQL)
	adapter.ImportPacket(ctx, packet, adapters.StrategyCopy)

# Транзакции

Для атомарного импорта используйте транзакции:

	tx, err := adapter.BeginTx(ctx)
	if err != nil {
	    log.Fatal(err)
	}
	defer tx.Rollback(ctx)

	// Импорт в рамках транзакции
	for _, packet := range packets {
	    if err := adapter.ImportPacket(ctx, packet, strategy); err != nil {
	        return err // автоматический Rollback
	    }
	}

	tx.Commit(ctx)

Или используйте ImportPackets для автоматической транзакции:

	// Автоматически создает транзакцию
	adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)

# Маппинг типов

Каждый адаптер реализует TypeMapper для конвертации типов:

	TDTP Type         → PostgreSQL    → SQLite      → MS SQL
	─────────────────────────────────────────────────────────
	INTEGER           → INTEGER       → INTEGER     → INT
	INTEGER (bigint)  → BIGINT        → INTEGER     → BIGINT
	INTEGER (smallint)→ SMALLINT      → INTEGER     → SMALLINT
	REAL              → DOUBLE PRECISION → REAL     → FLOAT
	DECIMAL(10,2)     → NUMERIC(10,2)→ REAL        → DECIMAL(10,2)
	TEXT              → TEXT          → TEXT        → NVARCHAR(MAX)
	TEXT(100)         → VARCHAR(100)  → TEXT        → NVARCHAR(100)
	BOOLEAN           → BOOLEAN       → INTEGER     → BIT
	DATETIME          → TIMESTAMP     → TEXT        → DATETIME2
	TEXT (uuid)       → UUID          → TEXT        → UNIQUEIDENTIFIER
	TEXT (json)       → JSONB         → TEXT        → NVARCHAR(MAX)
	TEXT (array:*)    → ARRAY         → TEXT        → не поддерживается

# Производительность

Для оптимальной производительности:

1. Используйте StrategyCopy для PostgreSQL:
  - COPY в 5-10x быстрее обычного INSERT
  - Идеально для начального импорта больших объемов данных

2. Используйте батчинг:
  - ImportPackets автоматически батчирует INSERT запросы
  - Размер батча: 1000 строк (настраиваемо)

3. Используйте транзакции:
  - ImportPackets автоматически оборачивает в транзакцию
  - Для ручного контроля используйте BeginTx

Benchmark результаты (PostgreSQL, 100k строк):
  - INSERT (StrategyFail):    ~45s
  - UPSERT (StrategyReplace): ~50s
  - COPY (StrategyCopy):      ~8s

# Создание нового адаптера

Для добавления поддержки новой СУБД:

1. Создайте пакет pkg/adapters/yourdb

2. Реализуйте интерфейс Adapter:

	type Adapter struct {
	    // Ваши поля (БЕЗ context.Context!)
	}

	func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error
	func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)
	// ... остальные методы интерфейса

3. Зарегистрируйте адаптер в init():

	func init() {
	    adapters.Register("yourdb", func() adapters.Adapter {
	        return &Adapter{}
	    })
	}

4. Реализуйте TypeMapper и QueryBuilder для вашей СУБД

5. Добавьте тесты:
  - Unit тесты с моками
  - Integration тесты с testcontainers

Смотрите pkg/adapters/sqlite и pkg/adapters/postgres как примеры.

# Совместимость

	Feature              │ SQLite │ PostgreSQL │ MS SQL
	─────────────────────┼────────┼────────────┼────────
	Export               │ ✅     │ ✅         │ 🚧
	Import               │ ✅     │ ✅         │ 🚧
	UPSERT               │ ✅     │ ✅         │ 🚧
	Bulk COPY            │ ❌     │ ✅         │ 🚧
	Transactions         │ ✅     │ ✅         │ 🚧
	Arrays               │ ❌     │ ✅         │ 🚧
	JSON/JSONB           │ ✅     │ ✅         │ 🚧
	UUID                 │ ❌     │ ✅         │ 🚧
	Custom Schema        │ ❌     │ ✅         │ 🚧

	✅ Полная поддержка
	🚧 В разработке
	❌ Не поддерживается

# Changelog

v1.0 (текущая версия):
  - Двухуровневая архитектура адаптеров
  - Единый интерфейс Adapter
  - Фабрика для создания адаптеров
  - SQLite и PostgreSQL адаптеры
  - Поддержка стратегий импорта
  - Транзакции
  - TypeMapper и QueryBuilder интерфейсы

v0.9 (предыдущая):
  - Отдельные адаптеры без общего интерфейса
  - context.Context в struct (анти-паттерн)
  - Разные API для каждого адаптера
*/
package adapters
