# TDTP Framework - Модули

Полное описание всех модулей и компонентов TDTP Framework v1.2.

---

## 📚 Содержание

- [Core Modules](#core-modules)
  - [Packet Module](#packet-module)
  - [Schema Module](#schema-module)
  - [TDTQL Module](#tdtql-module)
- [Data Processing](#data-processing)
  - [Processors](#processors)
- [Database Integration](#database-integration)
  - [Adapters Architecture](#adapters-architecture)
  - [SQLite Adapter](#sqlite-adapter)
  - [PostgreSQL Adapter](#postgresql-adapter)
  - [MS SQL Server Adapter](#ms-sql-server-adapter)
  - [MySQL Adapter](#mysql-adapter)
- [Message Brokers](#message-brokers)
  - [RabbitMQ Broker](#rabbitmq-broker)
  - [Kafka Broker](#kafka-broker)
  - [MSMQ Broker](#msmq-broker)
- [Production Features](#production-features)
  - [Circuit Breaker](#circuit-breaker)
  - [Retry Mechanism](#retry-mechanism)
  - [Audit Logger](#audit-logger)
  - [Incremental Sync](#incremental-sync)
- [Data Conversion](#data-conversion)
  - [XLSX Converter](#xlsx-converter)
- [CLI Utility](#cli-utility)

---

## Core Modules

### Packet Module

**Расположение:** `pkg/core/packet/`

Ядро фреймворка для работы с TDTP пакетами.

#### Основные компоненты:

**Parser** (`parser.go`)
- XML парсинг с валидацией TDTP v1.0
- Поддержка всех типов сообщений: Reference, Delta, Response, Request
- Методы:
  - `ParseFile(path)` - парсинг из файла
  - `ParseBytes(data)` - парсинг из байтов
  - `ParseReader(r)` - парсинг из io.Reader
  - `ParseWithDecompression()` - автоматическая распаковка сжатых данных
  - `IsCompressed(packet)` - проверка сжатия
  - `DecompressData()` - распаковка данных пакета

**Generator** (`generator.go`)
- Генерация TDTP пакетов всех типов
- Автоматическая пагинация (до 3.8MB на пакет)
- **Поддержка сжатия zstd** (v1.2+)
- Методы:
  - `GenerateReference()` - полный экспорт таблицы
  - `GenerateDelta()` - инкрементальные изменения
  - `GenerateResponse()` - ответ на запрос
  - `GenerateRequest()` - запрос данных
  - `EnableCompression()` - включение сжатия
  - `SetCompression(opts)` - настройка сжатия
  - `WriteToFile()` - сохранение в файл

**Types** (`types.go`)
- XML структуры: DataPacket, Header, Schema, Data, Row
- Атрибуты сжатия: `compression="zstd"`

**QueryContext** (`query.go`)
- Stateless паттерн для пагинации
- Статистика: TotalRows, TotalPages, CurrentPage

#### Возможности:

✅ XML валидация по спецификации TDTP v1.0
✅ Генерация пакетов с автоматической пагинацией
✅ **Сжатие данных zstd с настраиваемыми уровнями (1-22)**
✅ QueryContext для stateless обработки
✅ Экранирование специальных символов XML
✅ Поддержка subtypes (UUID, JSONB, TIMESTAMPTZ)

#### Пример использования:

```go
import "github.com/queuebridge/tdtp/pkg/core/packet"

// Генерация с сжатием
generator := packet.NewGenerator()
generator.EnableCompression()

packets, err := generator.GenerateReference("users", schema, rows)

// Парсинг со сжатием
parser := packet.NewParser()
pkt, err := parser.ParseFileWithDecompression("data.xml", decompressor)
```

---

### Schema Module

**Расположение:** `pkg/core/schema/`

Валидация типов данных и работа со схемами.

#### Основные компоненты:

**Validator** (`validator.go`)
- Валидация всех TDTP типов данных
- Типы: INTEGER, BIGINT, REAL, DECIMAL, TEXT, BOOLEAN, DATE, TIME, DATETIME, TIMESTAMP, BLOB
- Проверка length, precision, scale
- Валидация строк на соответствие схеме

**Converter** (`converter.go`)
- Универсальный конвертер значений
- Go types → TDTP types
- TDTP types → Go types
- Используется всеми адаптерами

**Builder** (`builder.go`)
- Fluent API для создания схем
- Методы: AddField(), SetKey(), Build()

#### Возможности:

✅ Валидация 12 базовых типов данных
✅ Поддержка subtypes (UUID, JSONB, TIMESTAMPTZ)
✅ Проверка constraints (length, precision, scale)
✅ Конвертация между Go и TDTP типами
✅ Builder API для программного создания схем

#### Пример использования:

```go
import "github.com/queuebridge/tdtp/pkg/core/schema"

// Builder API
builder := schema.NewBuilder()
schema := builder.
    AddField("id", "INTEGER", true).
    AddField("name", "TEXT", false).WithLength(200).
    AddField("balance", "DECIMAL", false).WithPrecision(10, 2).
    Build()

// Валидация
validator := schema.NewValidator()
err := validator.ValidateRow(schema, row)
```

---

### TDTQL Module

**Расположение:** `pkg/core/tdtql/`

TDTP Query Language - язык запросов для фильтрации, сортировки и пагинации.

#### Основные компоненты:

**Translator** (`translator.go`)
- SQL → TDTQL трансляция
- Поддерживает: WHERE, ORDER BY, LIMIT, OFFSET
- Операторы: =, !=, <, >, >=, <=, IN, BETWEEN, LIKE, IS NULL
- Логические группы: AND, OR с вложенностью

**Executor** (`executor.go`)
- In-memory фильтрация данных
- Применение TDTQL запросов к массивам строк
- Сортировка и пагинация

**SQL Generator** (`sql_generator.go`)
- TDTQL → SQL оптимизация
- Генерация оптимизированных SQL запросов
- Поддержка специфики разных СУБД

**Parser** (`parser.go`)
- Парсинг TDTQL выражений
- Построение AST (Abstract Syntax Tree)

**Comparator** (`comparator.go`)
- Сравнение значений разных типов
- Типобезопасные операции

**Sorter** (`sorter.go`)
- Сортировка по одному или нескольким полям
- ASC/DESC направления

#### Возможности:

✅ Все SQL операторы (=, !=, <, >, >=, <=, IN, BETWEEN, LIKE, IS NULL)
✅ Логические группы (AND/OR) с неограниченной вложенностью
✅ Множественная сортировка
✅ Пагинация с LIMIT/OFFSET
✅ In-memory фильтрация
✅ Оптимизация в SQL для push-down execution

#### Пример использования:

```go
import "github.com/queuebridge/tdtp/pkg/core/tdtql"

// SQL → TDTQL
translator := tdtql.NewTranslator()
tdtqlQuery, err := translator.TranslateSQL(
    "SELECT * FROM users WHERE age > 18 AND status = 'active' ORDER BY name LIMIT 10"
)

// In-memory фильтрация
executor := tdtql.NewExecutor()
filtered, err := executor.Filter(rows, schema, tdtqlQuery)

// TDTQL → SQL оптимизация
generator := tdtql.NewSQLGenerator()
sql, err := generator.GenerateSQL("users", schema, tdtqlQuery)
```

---

## Data Processing

### Processors

**Расположение:** `pkg/processors/`

Обработка и трансформация данных в TDTP пакетах.

#### Компоненты:

**CompressionProcessor** (`compression.go`) 🆕 v1.2
- **Сжатие/распаковка zstd**
- Уровни сжатия: 1 (fastest) - 22 (best), по умолчанию 3
- Base64-кодирование для безопасной передачи
- Многопоточная обработка (до 4 ядер)
- Порог сжатия: по умолчанию 1KB
- Статистика: OriginalSize, CompressedSize, Ratio, Duration
- Функции:
  - `Compress(data, level)` - сжатие
  - `Decompress(data)` - распаковка
  - `CompressDataForTdtp()` - сжатие для TDTP пакетов
  - `DecompressDataForTdtp()` - распаковка TDTP данных
  - `ShouldCompress(size, minSize)` - проверка порога

**FieldMasker** (`field_masker.go`)
- Маскирование PII данных (GDPR/HIPAA compliance)
- Email: `user@example.com` → `u***@example.com`
- Phone: `+1234567890` → `+12345***90`
- Card: `4111111111111111` → `4111********1111`

**FieldValidator** (`field_validator.go`)
- Валидация данных по правилам
- Regex валидация
- Range validation (min/max)
- Format validation (email, phone, etc.)

**FieldNormalizer** (`field_normalizer.go`)
- Нормализация данных
- Email: lowercase, trim
- Phone: удаление не-цифр
- Date: унификация формата

**Chain** (`chain.go`)
- Цепочки процессоров
- Последовательное применение трансформаций

**Factory** (`factory.go`)
- Создание процессоров по типу
- Регистрация кастомных процессоров

#### Возможности:

✅ **Сжатие данных zstd с настраиваемыми уровнями**
✅ Маскирование PII для compliance
✅ Валидация данных
✅ Нормализация форматов
✅ Цепочки процессоров для сложных трансформаций

#### Пример использования:

```go
import "github.com/queuebridge/tdtp/pkg/processors"

// Сжатие
compressed, stats, err := processors.Compress(data, 3)
fmt.Printf("Ratio: %.2f%%\n", stats.Ratio*100)

// Маскирование
masker := processors.NewFieldMasker()
masker.MaskEmail(0) // маскировать колонку 0
masked := masker.Process(packet)

// Цепочка процессоров
chain := processors.NewChain().
    Add(masker).
    Add(validator).
    Add(normalizer)
result := chain.Process(packet)
```

---

## Database Integration

### Adapters Architecture

**Расположение:** `pkg/adapters/`

Двухуровневая архитектура для унификации работы с СУБД.

#### Основные компоненты:

**Interface** (`adapter.go`)
```go
type Adapter interface {
    ExportTable(ctx, tableName) ([]packet.DataPacket, error)
    ExportTableWithQuery(ctx, tableName, query) ([]packet.DataPacket, error)
    ImportPacket(ctx, packet, strategy) error
    ListTables(ctx) ([]string, error)
    BeginTx(ctx) (Transaction, error)
    Close(ctx) error
}
```

**Factory** (`factory.go`)
- Автоматическая регистрация адаптеров
- Создание адаптеров по типу
- Конфигурация через Config struct

**Стратегии импорта:**
- `StrategyReplace` - полная замена (DELETE + INSERT)
- `StrategyIgnore` - пропуск существующих (INSERT IGNORE)
- `StrategyFail` - ошибка при дубликатах
- `StrategyCopy` - bulk insert (PostgreSQL COPY)

#### Возможности:

✅ Унифицированный интерфейс для всех СУБД
✅ Context-aware операции
✅ Transaction support
✅ Автоматический маппинг типов
✅ TDTQL → SQL оптимизация на уровне БД

---

### SQLite Adapter

**Расположение:** `pkg/adapters/sqlite/`
**Драйвер:** `modernc.org/sqlite`

#### Особенности:

- Pure Go реализация (без CGO)
- Автоматическое создание таблиц
- Export/Import с маппингом типов
- TDTQL → SQL оптимизация
- Высокая производительность: 10K+ rows/sec

#### Маппинг типов:

| TDTP Type | SQLite Type |
|-----------|-------------|
| INTEGER   | INTEGER     |
| BIGINT    | INTEGER     |
| REAL      | REAL        |
| DECIMAL   | REAL        |
| TEXT      | TEXT        |
| BOOLEAN   | INTEGER     |
| DATE      | TEXT        |
| DATETIME  | TEXT        |
| BLOB      | BLOB        |

#### Пример:

```go
import _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"

cfg := adapters.Config{
    Type: "sqlite",
    DSN:  "database.db",
}
adapter, err := adapters.New(ctx, cfg)
packets, err := adapter.ExportTable(ctx, "users")
```

**Документация:** [pkg/adapters/sqlite/README.md](../pkg/adapters/sqlite/README.md)

---

### PostgreSQL Adapter

**Расположение:** `pkg/adapters/postgres/`
**Драйвер:** `github.com/jackc/pgx/v5`

#### Особенности:

- Connection pool (pgxpool)
- Поддержка schemas (public/custom)
- COPY protocol для bulk operations
- Специальные типы: UUID, JSONB, JSON, INET, ARRAY, NUMERIC
- ON CONFLICT для стратегий импорта

#### Специальные типы:

| TDTP Type      | PostgreSQL Type | Subtype   |
|----------------|-----------------|-----------|
| TEXT           | UUID            | uuid      |
| TEXT           | JSONB           | jsonb     |
| TEXT           | JSON            | json      |
| TIMESTAMP      | TIMESTAMPTZ     | timestamptz |
| TEXT           | INET            | inet      |
| DECIMAL        | NUMERIC         | -         |

#### Пример:

```go
import _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"

cfg := adapters.Config{
    Type: "postgres",
    DSN:  "postgres://user:pass@localhost/dbname?sslmode=disable",
}
adapter, err := adapters.New(ctx, cfg)

// COPY стратегия для быстрого импорта
err = adapter.ImportPacket(ctx, packet, adapters.StrategyCopy)
```

**Документация:** [pkg/adapters/postgres/README.md](../pkg/adapters/postgres/README.md)

---

### MS SQL Server Adapter

**Расположение:** `pkg/adapters/mssql/`
**Драйвер:** `github.com/microsoft/go-mssqldb`

#### Особенности:

- Поддержка MS SQL 2012+
- IDENTITY_INSERT для импорта ключевых полей
- Специальные типы: NVARCHAR, UNIQUEIDENTIFIER, DATETIME2
- Параметризованные запросы
- Transaction support

#### Маппинг типов:

| TDTP Type | MS SQL Type       |
|-----------|-------------------|
| INTEGER   | INT               |
| BIGINT    | BIGINT            |
| REAL      | FLOAT             |
| DECIMAL   | DECIMAL(p,s)      |
| TEXT      | NVARCHAR(length)  |
| BOOLEAN   | BIT               |
| DATE      | DATE              |
| DATETIME  | DATETIME2         |
| BLOB      | VARBINARY(MAX)    |

#### Пример:

```go
import _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"

cfg := adapters.Config{
    Type: "mssql",
    DSN:  "sqlserver://user:pass@localhost:1433?database=mydb",
}
adapter, err := adapters.New(ctx, cfg)
```

**Документация:** [pkg/adapters/mssql/README.md](../pkg/adapters/mssql/README.md)

---

### MySQL Adapter

**Расположение:** `pkg/adapters/mysql/`
**Драйвер:** `github.com/go-sql-driver/mysql`

#### Особенности:

- Поддержка MySQL 5.7+, MariaDB 10.2+
- Multi-statement transactions
- UTF-8 encoding
- ON DUPLICATE KEY UPDATE для стратегий

#### Маппинг типов:

| TDTP Type | MySQL Type        |
|-----------|-------------------|
| INTEGER   | INT               |
| BIGINT    | BIGINT            |
| REAL      | DOUBLE            |
| DECIMAL   | DECIMAL(p,s)      |
| TEXT      | VARCHAR(length)   |
| BOOLEAN   | TINYINT(1)        |
| DATE      | DATE              |
| DATETIME  | DATETIME          |
| BLOB      | BLOB              |

#### Пример:

```go
import _ "github.com/queuebridge/tdtp/pkg/adapters/mysql"

cfg := adapters.Config{
    Type: "mysql",
    DSN:  "user:password@tcp(localhost:3306)/dbname?parseTime=true",
}
adapter, err := adapters.New(ctx, cfg)
```

**Документация:** [pkg/adapters/mysql/README.md](../pkg/adapters/mysql/README.md)

---

## Message Brokers

### RabbitMQ Broker

**Расположение:** `pkg/brokers/rabbitmq.go`
**Библиотека:** `github.com/rabbitmq/amqp091-go`

#### Возможности:

- Publish/Consume TDTP пакетов
- Manual ACK для надежной доставки
- Queue parameters: durable, auto_delete, exclusive
- Connection recovery
- Prefetch control

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

broker := brokers.NewRabbitMQ("amqp://guest:guest@localhost:5672/")
err := broker.Connect()
defer broker.Close()

// Publish
err = broker.Publish("my_queue", packet)

// Consume
packets, err := broker.Consume("my_queue")
```

---

### Kafka Broker

**Расположение:** `pkg/brokers/kafka.go`
**Библиотека:** `github.com/segmentio/kafka-go`

#### Возможности:

- High-throughput message streaming
- Producer/Consumer с batching
- Configurable partitioning
- Compression: Snappy (transport-level)
- Offset management

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

broker := brokers.NewKafka([]string{"localhost:9092"})
err := broker.Connect()
defer broker.Close()

// Publish
err = broker.Publish("tdtp-topic", packet)

// Consume
packets, err := broker.Consume("tdtp-topic")
```

---

### MSMQ Broker

**Расположение:** `pkg/brokers/msmq.go`
**Платформа:** Windows only

#### Возможности:

- Windows Message Queue integration
- Transactional queues support
- Private/Public queues
- Direct format names

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

broker := brokers.NewMSMQ(".\\Private$\\MyQueue")
err := broker.Connect()
defer broker.Close()

// Publish
err = broker.Publish("", packet)

// Consume
packets, err := broker.Consume("")
```

---

## Production Features

### Circuit Breaker

**Расположение:** `pkg/resilience/`

Защита от каскадных сбоев при вызовах внешних сервисов.

#### Состояния:

- **Closed** - нормальная работа
- **Open** - все вызовы блокируются
- **Half-Open** - тестовые вызовы для восстановления

#### Возможности:

✅ Автоматическое восстановление с таймаутом
✅ Ограничение параллельных вызовов
✅ Порог успешных вызовов для восстановления
✅ Callbacks на смену состояния
✅ Кастомная логика срабатывания
✅ Circuit Breaker groups

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/resilience"

cb := resilience.NewCircuitBreaker(resilience.Config{
    MaxFailures:    5,
    ResetTimeout:   30 * time.Second,
    SuccessThreshold: 2,
})

err := cb.Call(func() error {
    return externalAPI.DoSomething()
})
```

**Документация:** [pkg/resilience/README.md](../pkg/resilience/README.md)

---

### Retry Mechanism

**Расположение:** `pkg/retry/`

Автоматические повторы операций с различными стратегиями backoff.

#### Стратегии backoff:

- **Constant** - фиксированная задержка
- **Linear** - линейное увеличение
- **Exponential** - экспоненциальное увеличение

#### Возможности:

✅ Jitter для предотвращения thundering herd
✅ Настраиваемые retryable ошибки
✅ Context-aware cancellation
✅ OnRetry callbacks для мониторинга
✅ Dead Letter Queue (DLQ) support

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/retry"

r := retry.New(retry.Config{
    MaxAttempts: 3,
    Delay:       1 * time.Second,
    MaxDelay:    10 * time.Second,
    Multiplier:  2.0,
    Jitter:      true,
})

err := r.Do(ctx, func() error {
    return unreliableOperation()
})
```

**Документация:** [pkg/retry/README.md](../pkg/retry/README.md)

---

### Audit Logger

**Расположение:** `pkg/audit/`

Система аудита для compliance (GDPR/HIPAA/SOX).

#### Appenders:

- **File** - логи в файлы с ротацией
- **Database** - хранение в БД с batch inserts
- **Console** - вывод в консоль

#### Уровни логирования:

- **Minimal** - только критичные события
- **Standard** - стандартные операции
- **Full** - полная детализация (включая данные)

#### Возможности:

✅ Async/Sync режимы с буферизацией
✅ Файловая ротация по размеру
✅ Batch inserts в БД
✅ Query, filter, cleanup операции
✅ Builder pattern для создания записей
✅ Thread-safe операции

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/audit"

logger := audit.NewLogger(audit.Config{
    Level:      audit.LevelStandard,
    AsyncMode:  true,
    BufferSize: 100,
})

logger.AddAppender(audit.NewFileAppender("audit.log"))

logger.Log(audit.Entry{
    Action:   "EXPORT",
    Resource: "users",
    User:     "admin",
})
```

**Документация:** [pkg/audit/README.md](../pkg/audit/README.md)

---

### Incremental Sync

**Расположение:** `pkg/sync/`

Инкрементальная синхронизация для больших таблиц.

#### Стратегии отслеживания:

- **Timestamp** - по времени модификации
- **Sequence** - по последовательности
- **Version** - по версии записи

#### Возможности:

✅ StateManager с checkpoint tracking
✅ Batch processing с настраиваемым размером
✅ Resume from last checkpoint
✅ **200x быстрее для больших таблиц**
✅ Recovery mechanisms

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/sync"

sm := sync.NewStateManager("checkpoint.json")
state, err := sm.Load("users")

strategy := sync.NewTimestampStrategy("updated_at")
changes, err := strategy.GetChanges(adapter, "users", state)

sm.Save("users", sync.State{
    LastValue: changes.LastValue,
    LastSync:  time.Now(),
})
```

**Документация:** [pkg/sync/README.md](../pkg/sync/README.md)

---

## Data Conversion

### XLSX Converter

**Расположение:** `pkg/xlsx/`

Конвертация между TDTP и Excel форматами.

#### Возможности:

✅ **TDTP → XLSX** (Database → Excel для бизнес-анализа)
✅ **XLSX → TDTP** (Excel → Database bulk loading)
✅ Сохранение типов данных
✅ Форматированные заголовки с типами и ключами
✅ Автоформатирование (числа, даты, булевы)
✅ **Business-friendly** интерфейс (не требует знания SQL)
✅ Round-trip data integrity

#### Автоформатирование:

- Числа: выравнивание по правому краю
- Даты: DD.MM.YYYY
- DateTime: DD.MM.YYYY HH:MM:SS
- Boolean: TRUE/FALSE
- Primary keys: жирный шрифт в заголовке

#### Пример:

```go
import "github.com/queuebridge/tdtp/pkg/xlsx"

// TDTP → XLSX
err := xlsx.ToXLSX(packet, "output.xlsx")

// XLSX → TDTP
packet, err := xlsx.FromXLSX("input.xlsx")
```

**Документация:** [pkg/xlsx/README.md](../pkg/xlsx/README.md)

---

## CLI Utility

### tdtpcli

**Расположение:** `cmd/tdtpcli/`

Утилита командной строки для работы с TDTP.

#### Команды:

- `--list` - список таблиц
- `--export <table>` - экспорт в файл/stdout
- `--import <file>` - импорт из файла
- `--export-broker <table>` - экспорт в message queue
- `--import-broker` - импорт из message queue

#### TDTQL фильтры:

- `--where "field > value"` - условия фильтрации
- `--order-by "field DESC"` - сортировка
- `--limit N` - лимит записей
- `--offset N` - пропуск записей

#### Конфигурация:

- YAML конфигурационные файлы
- `--create-config-sqlite/pg/mssql` - генерация конфигов
- Поддержка всех адаптеров и брокеров

#### Пример:

```bash
# Экспорт с фильтром
tdtpcli --export users --where "age > 18" --order-by "name" > users.xml

# Импорт
tdtpcli --import users.xml

# Экспорт в RabbitMQ
tdtpcli --export-broker users --queue tdtp_users

# Генерация конфига
tdtpcli --create-config-pg > config.yaml
```

**Документация:** [docs/USER_GUIDE.md](./USER_GUIDE.md)

---

## Зависимости

### Core Dependencies

```go
// Database drivers
github.com/jackc/pgx/v5                  // PostgreSQL
github.com/microsoft/go-mssqldb          // MS SQL Server
github.com/go-sql-driver/mysql           // MySQL
modernc.org/sqlite                       // SQLite

// Message brokers
github.com/rabbitmq/amqp091-go           // RabbitMQ
github.com/segmentio/kafka-go            // Kafka

// Data processing
github.com/klauspost/compress            // zstd compression
github.com/xuri/excelize/v2              // Excel files

// Utilities
gopkg.in/yaml.v3                         // YAML config
```

---

## Архитектурные паттерны

### Factory Pattern
Используется в:
- `pkg/adapters/factory.go` - создание адаптеров
- `pkg/processors/factory.go` - создание процессоров

### Builder Pattern
Используется в:
- `pkg/core/schema/builder.go` - построение схем
- `pkg/audit/logger.go` - создание audit записей

### Strategy Pattern
Используется в:
- `pkg/adapters/` - стратегии импорта
- `pkg/sync/` - стратегии синхронизации
- `pkg/retry/` - стратегии backoff

### Circuit Breaker Pattern
Реализован в:
- `pkg/resilience/` - защита от сбоев

### Chain of Responsibility
Реализован в:
- `pkg/processors/chain.go` - цепочки обработки

---

## Производительность

### Benchmarks

| Операция | Производительность |
|----------|-------------------|
| SQLite Export | 10,000+ rows/sec |
| PostgreSQL COPY | 50,000+ rows/sec |
| Packet Parse | 5,000+ packets/sec |
| TDTQL Filter | 100,000+ rows/sec |
| Compression (level 3) | 100 MB/sec |
| Incremental Sync | 200x faster vs full export |

### Оптимизации

✅ Connection pooling (PostgreSQL)
✅ Batch inserts (все адаптеры)
✅ COPY protocol (PostgreSQL)
✅ Prepared statements (все адаптеры)
✅ In-memory TDTQL filtering
✅ Multi-threaded compression
✅ Checkpoint-based sync

---

## Безопасность

### Compliance Features

- **GDPR**: PII masking, audit logging
- **HIPAA**: Encryption, audit trail
- **SOX**: Immutable audit logs

### Security Measures

✅ SQL injection protection (prepared statements)
✅ XML injection protection (escaping)
✅ TLS support для всех адаптеров
✅ Context-aware операции с timeout
✅ Audit logging всех операций

---

## Тестирование

### Test Coverage

- `pkg/core/packet/` - 85%+
- `pkg/core/schema/` - 90%+
- `pkg/core/tdtql/` - 88%+
- `pkg/adapters/` - 80%+
- `pkg/processors/` - 85%+
- `pkg/resilience/` - 92%+

### Test Types

- Unit tests - все модули
- Integration tests - адаптеры + БД
- Benchmark tests - критичные операции
- Example tests - документация API

---

## Версионирование

**Текущая версия:** v1.2

**Совместимость:**
- TDTP Specification: v1.0
- Go: 1.21+
- PostgreSQL: 9.6+
- MySQL: 5.7+, MariaDB 10.2+
- MS SQL Server: 2012+
- SQLite: 3.x

---

## См. также

- **[README.md](../README.md)** - Обзор проекта
- **[DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)** - Руководство разработчика
- **[USER_GUIDE.md](./USER_GUIDE.md)** - CLI утилита
- **[SPECIFICATION.md](./SPECIFICATION.md)** - Спецификация TDTP v1.0
- **[examples/](../examples/)** - Production-ready примеры

---

**Последнее обновление:** 08.12.2025
**Версия документа:** 1.0
