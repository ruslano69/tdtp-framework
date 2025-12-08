# TDTP Framework

**Table Data Transfer Protocol** - фреймворк для универсального обмена табличными данными через message brokers.

## 🎯 Цели проекта

- **Универсальность** - работа с любыми таблицами и СУБД
- **Прозрачность** - самодокументируемые XML сообщения
- **Надежность** - stateless паттерн, валидация, пагинация
- **Безопасность** - TLS, аутентификация, audit trail
- **Удобство** - простое API, понятная структура

## 📦 Что реализовано (v1.2)

### ✅ Core Modules

**Packet Module:**
- XML парсер с валидацией TDTP v1.0
- Генератор для всех типов сообщений (Reference, Delta, Response, Request)
- Автоматическое разбиение на части (пагинация до 3.8MB)
- **Поддержка сжатия данных zstd**: 🆕
  - CompressionOptions для настройки (enabled, level, minSize, algorithm)
  - Автоматическое сжатие при генерации пакетов (порог 1KB)
  - Автоматическая распаковка при парсинге
  - XML-атрибут `compression="zstd"` для идентификации сжатых данных
- QueryContext для stateless паттерна
- Поддержка subtypes (UUID, JSONB, TIMESTAMPTZ)

**Schema Module:**
- Валидация всех типов данных TDTP
- Универсальный Converter для всех адаптеров
- Проверка соответствия данных схеме
- Builder API для создания схем

**TDTQL Module:**
- Translator: SQL → TDTQL (WHERE, ORDER BY, LIMIT, OFFSET)
- Executor: in-memory фильтрация данных
- SQL Generator: TDTQL → SQL оптимизация
- Все операторы (=, !=, <, >, >=, <=, IN, BETWEEN, LIKE, IS NULL)
- Логические группы (AND/OR) с вложенностью
- Сортировка (одиночная и множественная)
- Пагинация с QueryContext статистикой

### ✅ Database Adapters

**Universal Interface:**
- Двухуровневая архитектура (Interface + Implementations)
- Фабрика адаптеров с автоматической регистрацией
- Context-aware операции (context.Context)
- Стратегии импорта: REPLACE, IGNORE, FAIL, COPY
- ExportTable / ExportTableWithQuery
- ImportPacket с transaction support

**SQLite Adapter:**
- Подключение через modernc.org/sqlite
- Export/Import с автоматическим маппингом типов
- TDTQL → SQL оптимизация на уровне БД
- Автоматическое создание таблиц
- Benchmark тесты (10K+ rows/sec)

**PostgreSQL Adapter:**
- Подключение через pgx/v5 connection pool
- Export с поддержкой schemas (public/custom)
- Import с COPY для bulk operations
- Специальные типы: UUID, JSONB, JSON, INET, ARRAY, NUMERIC
- ON CONFLICT для стратегий импорта
- TDTQL → SQL оптимизация с безопасной заменой schema

**MS SQL Server Adapter:**
- Подключение через github.com/microsoft/go-mssqldb
- Export с параметризованными запросами
- IDENTITY_INSERT для импорта ключевых полей
- Поддержка NVARCHAR, UNIQUEIDENTIFIER, DATETIME2
- Совместимость с MS SQL 2012+

### ✅ Message Brokers

**RabbitMQ:**
- Publish/Consume TDTP пакетов
- Manual ACK для надежной доставки
- Queue parameters (durable, auto_delete, exclusive)
- Tested with PostgreSQL adapter

**MSMQ (Windows):**
- Windows Message Queue integration
- Transactional queues support
- Tested with MS SQL adapter

**Kafka:** 🆕 v1.1
- High-throughput message streaming
- Producer/Consumer with batching
- Configurable partitioning
- Tested with PostgreSQL adapter

### ✅ Resilience & Production Features 🆕 v1.2

**CircuitBreaker (pkg/resilience):**
- Three states: Closed, Half-Open, Open
- Automatic recovery with configurable timeout
- Concurrent call limiting
- Success threshold for recovery
- State change callbacks
- Custom trip logic
- Circuit Breaker groups
- 13 comprehensive tests

**AuditLogger (pkg/audit):**
- Multiple appenders: File, Database, Console
- Three logging levels: Minimal, Standard, Full (GDPR compliance)
- Async/Sync modes with configurable buffering
- File rotation with size limits and backups
- Database storage with SQL support (batch inserts)
- Query, filter, and cleanup operations
- Builder pattern for fluent entry creation
- Thread-safe concurrent operations
- GDPR/HIPAA/SOX compliance features
- 17 comprehensive tests

**Retry Mechanism (pkg/retry):**
- Three backoff strategies: Constant, Linear, Exponential
- Jitter support to prevent thundering herd
- Configurable retryable errors
- Context-aware cancellation
- OnRetry callbacks for monitoring
- Dead Letter Queue (DLQ) support
- 20 comprehensive tests

**IncrementalSync (pkg/sync):**
- StateManager with checkpoint tracking
- Three tracking strategies: Timestamp, Sequence, Version
- Batch processing with configurable sizes
- Resume from last checkpoint
- 200x faster for large tables

**Data Processors (pkg/processors):**
- **CompressionProcessor**: Сжатие/распаковка zstd (уровни 1-22, по умолчанию 3)
  - Автоматическое base64-кодирование для безопасной передачи
  - Многопоточная обработка (до 4 ядер)
  - Порог сжатия (по умолчанию 1KB)
  - Статистика сжатия (коэффициент, время)
  - Интеграция с packet generator/parser
- **FieldMasker**: Email, phone, card masking (GDPR/PII)
- **FieldValidator**: Regex, range, format validation
- **FieldNormalizer**: Email, phone, date normalization
- **Processor chain**: Цепочки процессоров для сложных трансформаций

**XLSX Converter (pkg/xlsx):** 🍒 **NEW!**
- TDTP → XLSX export (Database → Excel for business analysis)
- XLSX → TDTP import (Excel → Database bulk loading)
- Type preservation (INTEGER, REAL, BOOLEAN, DATE, DATETIME, etc.)
- Formatted headers with field types and primary keys
- Auto-formatting (numbers, dates, booleans)
- Business-friendly interface (no SQL knowledge required)
- Round-trip data integrity
- **Instant business value** - work with data in familiar Excel interface

### ✅ CLI Utility (tdtpcli)

**Commands:**
- `--list` - список таблиц
- `--export <table>` - экспорт в файл/stdout
- `--import <file>` - импорт из файла
- `--export-broker <table>` - экспорт в message queue
- `--import-broker` - импорт из message queue

**TDTQL Filters:**
- `--where "field > value"` - условия фильтрации
- `--order-by "field DESC"` - сортировка
- `--limit N` - лимит записей
- `--offset N` - пропуск записей

**Configuration:**
- YAML конфигурационные файлы
- `--create-config-sqlite/pg/mssql` - генерация конфигов
- Поддержка всех адаптеров и брокеров

## 🏗️ Архитектура

```
tdtp-framework/
├─ pkg/core/
│  ├─ packet/            ✅ Парсинг/генерация TDTP пакетов + компрессия
│  ├─ schema/            ✅ Валидация типов, Converter, Builder
│  └─ tdtql/             ✅ Translator, Executor, SQL Generator
│
├─ pkg/adapters/
│  ├─ adapter.go         ✅ Универсальный интерфейс
│  ├─ factory.go         ✅ Фабрика адаптеров
│  ├─ sqlite/            ✅ SQLite adapter (modernc.org/sqlite)
│  ├─ postgres/          ✅ PostgreSQL adapter (pgx/v5)
│  ├─ mssql/             ✅ MS SQL Server adapter (go-mssqldb)
│  └─ mysql/             ✅ MySQL adapter (go-sql-driver/mysql)
│
├─ pkg/processors/       ✅ Обработка и трансформация данных
│  ├─ compression.go     ✅ Сжатие/распаковка zstd (klauspost/compress)
│  ├─ field_masker.go    ✅ Маскирование PII (email, phone, card)
│  ├─ field_validator.go ✅ Валидация полей (regex, range, format)
│  ├─ field_normalizer.go✅ Нормализация данных
│  ├─ chain.go           ✅ Цепочки процессоров
│  └─ factory.go         ✅ Фабрика процессоров
│
├─ pkg/resilience/       ✅ Circuit Breaker паттерн
│  └─ circuit_breaker.go ✅ Защита от каскадных сбоев
│
├─ pkg/audit/            ✅ Audit Logger
│  ├─ logger.go          ✅ Система аудита (File, DB, Console)
│  └─ appenders.go       ✅ Appenders для логов
│
├─ pkg/retry/            ✅ Retry механизм
│  └─ retry.go           ✅ Стратегии повтора с backoff
│
├─ pkg/sync/             ✅ Incremental Sync
│  └─ state_manager.go   ✅ Инкрементальная синхронизация
│
├─ pkg/xlsx/             ✅ Excel интеграция
│  └─ converter.go       ✅ TDTP ↔ XLSX конвертер
│
├─ pkg/brokers/
│  ├─ broker.go          ✅ Интерфейс брокеров
│  ├─ rabbitmq.go        ✅ RabbitMQ интеграция
│  ├─ kafka.go           ✅ Kafka интеграция
│  └─ msmq.go            ✅ MSMQ интеграция (Windows)
│
├─ cmd/tdtpcli/          ✅ CLI утилита
│  ├─ main.go            ✅ Команды export/import/list
│  ├─ config.go          ✅ YAML конфигурация
│  ├─ processors.go      ✅ Интеграция процессоров
│  └─ commands/          ✅ Команды CLI
│
├─ docs/                 ✅ Документация
│  ├─ SPECIFICATION.md   ✅ Спецификация TDTP v1.0
│  ├─ PACKET_MODULE.md   ✅ Документация Packet
│  ├─ SCHEMA_MODULE.md   ✅ Документация Schema
│  ├─ TDTQL_TRANSLATOR.md✅ Документация TDTQL
│  ├─ SQLITE_ADAPTER.md  ✅ Документация SQLite
│  └─ ...                ✅ Прочие документы
│
├─ examples/             🆕 Production-ready examples
│  ├─ 01-basic-export/   ✅ PostgreSQL → TDTP XML export
│  ├─ 02-rabbitmq-mssql/ ✅ MSSQL → RabbitMQ integration (Circuit Breaker + Audit)
│  ├─ 03-incremental-sync/✅ PostgreSQL → MySQL incremental sync
│  ├─ 04-tdtp-xlsx/      ✅ Database ↔ Excel converter 🍒 (instant business value!)
│  ├─ 04-audit-masking/  ✅ Compliance: Audit logging + PII masking
│  ├─ 05-circuit-breaker/✅ API resilience patterns
│  └─ 06-etl-pipeline/   ✅ Complete ETL pipeline
│
└─ scripts/              ✅ Вспомогательные скрипты
   ├─ create_sqlite_test_db.py
   ├─ create_postgres_test_db.py
   └─ README.md          ✅ Руководство по скриптам
```

## 🚀 Быстрый старт

### Примеры

**Начните с готовых production-ready примеров:**

```bash
# Database ↔ Excel converter (instant business value!) 🍒
cd examples/04-tdtp-xlsx
go run main.go
# Генерирует: ./output/orders.xlsx - готов для работы в Excel!

# RabbitMQ + MSSQL integration (Circuit Breaker, Audit, Retry)
cd examples/02-rabbitmq-mssql
go run main.go

# Incremental Sync (200x faster for large tables)
cd examples/03-incremental-sync
go run main.go

# См. все примеры с описанием
cd examples
cat README.md
```

**Полная документация примеров**: [examples/README.md](./examples/README.md)

### Установка

```bash
git clone https://github.com/queuebridge/tdtp
cd tdtp-framework
go mod tidy
```

### Использование

```go
import "github.com/queuebridge/tdtp/pkg/core/packet"

// Создание схемы
schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "ID", Type: "INTEGER", Key: true},
        {Name: "Name", Type: "TEXT", Length: 200},
        {Name: "Balance", Type: "DECIMAL"},
    },
}

// Подготовка данных
rows := [][]string{
    {"1", "Company A", "150000.50"},
    {"2", "Company B", "250000.00"},
}

// Генерация пакета
generator := packet.NewGenerator()
packets, err := generator.GenerateReference("Companies", schema, rows)

// Сохранение
generator.WriteToFile(packets[0], "reference.xml")

// Парсинг
parser := packet.NewParser()
pkt, err := parser.ParseFile("reference.xml")
```

### Использование сжатия данных 🆕

```go
import (
    "github.com/queuebridge/tdtp/pkg/core/packet"
    "github.com/queuebridge/tdtp/pkg/processors"
)

// Генерация с автоматическим сжатием
generator := packet.NewGenerator()

// Включение сжатия с настройками
generator.SetCompression(packet.CompressionOptions{
    Enabled:   true,
    Level:     3,      // 1 (быстро) - 19 (лучшее сжатие)
    MinSize:   1024,   // Минимальный размер для сжатия (байты)
    Algorithm: "zstd",
})

// Или просто включить с настройками по умолчанию
generator.EnableCompression()

// Генерация пакета (автоматически сжимается если данных > 1KB)
packets, err := generator.GenerateReference("LargeTable", schema, rows)

// Парсинг со сжатием
parser := packet.NewParser()
decompressor := func(data []byte) ([]byte, error) {
    return processors.Decompress(data)
}

pkt, err := parser.ParseFileWithDecompression("compressed.xml", decompressor)
// Данные автоматически распакованы и готовы к использованию

// Прямое использование процессора сжатия
compressed, stats, err := processors.Compress([]byte("large data"), 3)
fmt.Printf("Сжатие: %d -> %d байт (%.2f%%)\n",
    stats.OriginalSize, stats.CompressedSize, stats.Ratio*100)

decompressed, err := processors.Decompress(compressed)
```

### Использование адаптеров (v1.0)

```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"   // Регистрация
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres" // Регистрация
)

func main() {
    ctx := context.Background()

    // Создаем адаптер через фабрику
    cfg := adapters.Config{
        Type: "sqlite",  // или "postgres"
        DSN:  "database.db",
    }

    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Export: БД → TDTP
    packets, err := adapter.ExportTable(ctx, "users")

    // Import: TDTP → БД
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)

    // Транзакции
    tx, _ := adapter.BeginTx(ctx)
    // ... операции ...
    tx.Commit(ctx)
}
```

### Запуск примера

```bash
cd examples/basic
go run main.go
```

## 📚 Документация

### Руководства

- **[Installation Guide](INSTALLATION_GUIDE.md)** ⭐ **НАЧНИТЕ ЗДЕСЬ** - установка, настройка, quick start
- **[User Guide](docs/USER_GUIDE.md)** - полное руководство по CLI утилите
- **[Documentation Index](docs/README.md)** - полный каталог документации

### Технические спецификации

- [TDTP Specification](docs/SPECIFICATION.md) - спецификация протокола TDTP v1.0
- [Packet Module](docs/PACKET_MODULE.md) - парсинг и генерация пакетов
- [Schema Module](docs/SCHEMA_MODULE.md) - валидация типов и схем
- [TDTQL Translator](docs/TDTQL_TRANSLATOR.md) - язык запросов
- [SQLite Adapter](docs/SQLITE_ADAPTER.md) - интеграция с SQLite

### Package READMEs

- [Circuit Breaker](pkg/resilience/README.md) - защита от каскадных сбоев
- [Audit Logger](pkg/audit/README.md) - compliance и security
- [XLSX Converter](pkg/xlsx/README.md) 🍒 - Database ↔ Excel

## 🧪 Тестирование

```bash
# Запуск всех тестов
go test ./...

# С покрытием
go test -cover ./...

# Verbose
go test -v ./pkg/core/packet/
```

## 📋 Roadmap

### ~~v1.0~~ ✅ Завершено
**Core Modules:**
- [x] Packet module (XML парсинг/генерация, пагинация)
- [x] Schema module (валидация типов, конвертер, builder)
- [x] TDTQL Translator (SQL → TDTQL, все операторы)
- [x] TDTQL Executor (in-memory фильтрация, сортировка, пагинация)
- [x] TDTQL SQL Generator (TDTQL → SQL оптимизация)

**Adapters:**
- [x] Двухуровневая архитектура адаптеров
- [x] Фабрика адаптеров с регистрацией
- [x] Context-aware API
- [x] Унифицированные стратегии импорта
- [x] SQLite adapter (полная поддержка, benchmarks)
- [x] PostgreSQL adapter (pgx/v5, UUID, JSONB, COPY)
- [x] MS SQL Server adapter (sqlserver driver, IDENTITY_INSERT)

### ~~v1.2~~ ✅ Завершено
**CLI & Message Brokers:**
- [x] CLI утилита (tdtpcli)
- [x] YAML конфигурационные файлы
- [x] Export/Import команды для всех адаптеров
- [x] TDTQL фильтры в CLI (--where, --order-by, --limit, --offset)
- [x] RabbitMQ broker integration
- [x] MSMQ broker integration (Windows)
- [x] Export/Import to message brokers
- [x] Manual ACK для надежной доставки
- [x] Увеличен max packet size до 3.8MB

### ~~v1.3~~ ✅ Завершено (08.12.2025)
- [x] Документация пользователя (USER_GUIDE.md) - существовала
- [x] Описание модулей (MODULES.md) - создан полный обзор всех модулей
- [x] Актуализация SPECIFICATION.md - добавлена поддержка сжатия zstd
- [x] PostgreSQL adapter documentation - существовала
- [x] MS SQL adapter documentation - создана полная документация
- [x] SQLite adapter documentation - создана полная документация

### v1.5 (планируется)
- [ ] CLI расширения (convert, stats, diff, merge)
- [ ] Schema migration (ALTER TABLE)
- [ ] Incremental sync (delta exports)
- [ ] Query optimization (автовыбор стратегии)

### v2.0 (планируется)
- [ ] Kafka broker integration
- [ ] Python bindings
- [ ] Docker образ
- [ ] Production deployment guide
- [ ] Monitoring & metrics

## 🤝 Вклад в проект

Проект находится в активной разработке. Приветствуются:
- Баг-репорты
- Предложения по улучшению
- Pull requests

## 📄 Лицензия

MIT

## 📞 Контакты

- GitHub: https://github.com/queuebridge/tdtp
- Email: support@queuebridge.io

---

**Статус:** v1.2 - Message Brokers Integration Complete!
**Последнее обновление:** 16.11.2025
