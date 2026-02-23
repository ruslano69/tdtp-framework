# TDTP Framework

**Table Data Transfer Protocol** - фреймворк для универсального обмена табличными данными через message brokers.

## Цели проекта

- **Универсальность** - работа с любыми таблицами и СУБД
- **Прозрачность** - самодокументируемые XML сообщения
- **Надежность** - stateless паттерн, валидация, пагинация
- **Безопасность** - TLS, аутентификация, audit trail
- **Удобство** - простое API, понятная структура

## Что реализовано (v1.6.0)

### Core Modules

**Packet Module:**
- XML парсер с валидацией TDTP v1.0
- Генератор для всех типов сообщений (Reference, Delta, Response, Request)
- Автоматическое разбиение на части (пагинация до 3.8MB)
- Поддержка сжатия данных zstd:
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

### Database Adapters

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

**MySQL Adapter:**
- Подключение через go-sql-driver/mysql
- Export/Import с маппингом типов MySQL
- Поддержка специфичных типов MySQL

### Message Brokers

**RabbitMQ:**
- Publish/Consume TDTP пакетов
- Manual ACK для надежной доставки
- Queue parameters (durable, auto_delete, exclusive)
- Tested with PostgreSQL adapter

**MSMQ (Windows):**
- Windows Message Queue integration
- Transactional queues support
- Tested with MS SQL adapter

**Kafka:**
- High-throughput message streaming
- Producer/Consumer with manual commit
- Configurable partitioning and consumer groups
- Stats and offset management (replay capability)
- Tested with PostgreSQL adapter

### Resilience & Production Features

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

**XLSX Converter (pkg/xlsx):**
- TDTP → XLSX export (Database → Excel для бизнес-анализа)
- XLSX → TDTP import (Excel → Database bulk loading)
- Type preservation (INTEGER, REAL, BOOLEAN, DATE, DATETIME, etc.)
- Formatted headers with field types and primary keys
- Auto-formatting (numbers, dates, booleans)
- Business-friendly interface (без знания SQL)
- Round-trip data integrity

**HTML Viewer (pkg/html):**
- TDTP → HTML конвертация для быстрого просмотра данных в браузере
- Поддержка диапазонов строк (`--row 100-500`)
- Tail-mode просмотр (`--limit -50` — последние 50 строк)
- Комбинирование диапазонов и limit
- Открытие в браузере одной командой (`--open`)

**Diff & Merge (pkg/diff):**
- Сравнение двух TDTP файлов (added / modified / deleted)
- Настраиваемые ключевые поля (`--key-fields`)
- Игнорирование полей при сравнении (`--ignore-fields`)
- Регистрозависимое/независимое сравнение
- Пять стратегий слияния: `union`, `intersection`, `left`, `right`, `append`
- Детальный отчёт о конфликтах (`--show-conflicts`)

### ETL Pipeline Processor (pkg/etl)

**Multi-Database ETL с 4-уровневой безопасностью:**

**Ключевые возможности:**
- Множественные источники: PostgreSQL, MS SQL Server, MySQL, SQLite
- Параллельная загрузка: все источники загружаются одновременно
- SQLite :memory: workspace: быстрые JOIN операции без дисковых операций
- SQL трансформации: полная мощь SQL для обработки данных
- Множественные выходы: TDTP XML, RabbitMQ, Kafka
- 4-уровневая безопасность: READ-ONLY по умолчанию, защита от случайного повреждения
- Детальная статистика: время выполнения, количество строк, ошибки

**Компоненты ETL:**
- **Loader** (pkg/etl/loader.go): параллельная загрузка из источников
- **Workspace** (pkg/etl/workspace.go): SQLite :memory: управление для JOIN
- **Executor** (pkg/etl/executor.go): выполнение SQL трансформаций
- **Exporter** (pkg/etl/exporter.go): экспорт в TDTP/RabbitMQ/Kafka
- **Processor** (pkg/etl/processor.go): главный оркестратор ETL

**Безопасность (4 уровня):**
1. **Code level**: SQLValidator блокирует запрещенные операции (INSERT, UPDATE, DELETE, DROP)
2. **OS level**: IsAdmin() проверяет права администратора для unsafe режима
3. **CLI level**: READ-ONLY по умолчанию, --unsafe требует явного указания
4. **SQL level**: только SELECT/WITH в safe mode, все операции в unsafe

**Режимы работы:**
- Safe mode (по умолчанию): только SELECT/WITH, без admin прав
- Unsafe mode (--unsafe): все SQL операции, требует права администратора

**Пример конфигурации:**
```yaml
name: "Multi-DB Report"
sources:
  - name: pg_users
    type: postgres
    dsn: "postgres://localhost/db1"
    table_alias: users
    query: "SELECT * FROM users WHERE active = true"

  - name: mssql_orders
    type: mssql
    dsn: "server=localhost;database=orders;user id=sa"
    table_alias: orders
    query: "SELECT * FROM orders WHERE year = 2024"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "report"
  sql: |
    SELECT
      u.username,
      COUNT(o.order_id) as total_orders,
      SUM(o.amount) as total_spent
    FROM users u
    LEFT JOIN orders o ON u.user_id = o.user_id
    GROUP BY u.username
    ORDER BY total_spent DESC

output:
  type: TDTP
  tdtp:
    destination: "report.xml"
    compress: true
```

**Документация**: [docs/ETL_PIPELINE_GUIDE.md](docs/ETL_PIPELINE_GUIDE.md)

### CLI Utility (tdtpcli)

#### Команды

**Database:**
```
--list                     Список всех таблиц
--list-views               Список database views (U* updatable, R* read-only)
--export <table>           Экспорт таблицы/view в TDTP XML
--import <file>            Импорт TDTP XML в базу данных
```

**File:**
```
--diff <file-a> <file-b>   Сравнение двух TDTP файлов
--merge <files>            Слияние нескольких TDTP файлов
--to-html <file>           Конвертация TDTP в HTML viewer
```

**XLSX:**
```
--to-xlsx <tdtp-file>      TDTP → XLSX
--from-xlsx <xlsx-file>    XLSX → TDTP
--export-xlsx <table>      Таблица → XLSX (напрямую, без промежуточного XML)
--import-xlsx <xlsx-file>  XLSX → Database (напрямую)
```

**Broker:**
```
--export-broker <table>    Экспорт в message broker
--import-broker            Импорт из message broker
```

**ETL:**
```
--sync-incremental <table> Инкрементальная синхронизация таблицы
--pipeline <file>          Выполнение ETL pipeline из YAML конфига
```

#### Опции

**General:**
```
--config <file>            Конфигурационный файл (по умолчанию: config.yaml)
--output <file>            Путь к выходному файлу
--table <name>             Имя целевой таблицы (переопределяет имя из XML при импорте)
--strategy <name>          Стратегия импорта: replace, ignore, fail, copy
--batch <size>             Размер batch для bulk операций (по умолчанию: 1000)
--readonly-fields          Включить read-only поля (timestamp, computed, identity)
```

**Compression:**
```
--compress                 Включить сжатие zstd для экспортируемых данных
--compress-level <n>       Уровень сжатия: 1 (быстрее) — 19 (лучше), по умолчанию: 3
```

**TDTQL Filters:**
```
--where <condition>        WHERE условие (пример: 'age > 18 AND status = active')
--order-by <fields>        ORDER BY (пример: 'name ASC, age DESC')
--limit <n>                Лимит строк: +N = первые N, -N = последние N (как tail)
--offset <n>               Пропуск N строк
```

**HTML Viewer:**
```
--open                     Открыть в браузере после конвертации
--row <range>              Диапазон строк (пример: 100-500)
```

**XLSX:**
```
--sheet <name>             Имя листа Excel (по умолчанию: Sheet1)
```

**Incremental Sync:**
```
--tracking-field <field>   Поле для отслеживания изменений (по умолчанию: updated_at)
--checkpoint-file <file>   Файл checkpoint (по умолчанию: checkpoint.yaml)
--batch-size <size>        Размер batch для синхронизации (по умолчанию: 1000)
```

**ETL:**
```
--unsafe                   Небезопасный режим (все SQL операции, требует admin)
```

**Diff:**
```
--key-fields <fields>      Ключевые поля для сравнения (через запятую)
--ignore-fields <fields>   Поля, игнорируемые при сравнении (через запятую)
--case-sensitive           Регистрозависимое сравнение (по умолчанию: false)
```

**Merge:**
```
--merge-strategy <name>    Стратегия: union, intersection, left, right, append
                           (по умолчанию: union)
--show-conflicts           Показать детальную информацию о конфликтах
```

**Data Processors:**
```
--mask <fields>            Маскировать чувствительные поля (через запятую)
--validate <file>          Валидация полей (YAML файл правил)
--normalize <file>         Нормализация полей (YAML файл правил)
```

**Configuration:**
```
--create-config-pg         Создать шаблон конфига PostgreSQL
--create-config-mssql      Создать шаблон конфига MS SQL
--create-config-sqlite     Создать шаблон конфига SQLite
--create-config-mysql      Создать шаблон конфига MySQL
```

**Misc:**
```
--version                  Показать версию
-h                         Краткая справка
--help                     Полная справка с примерами
```

#### Работа с Views

```
tdtpcli --list-views показывает все views с маркерами:
  U* = Updatable view (можно импортировать)
  R* = Read-only view (только экспорт)
```

- `--export` поддерживает все database views
- `--import` работает только с updatable views

## Архитектура

```
tdtp-framework/
├─ pkg/core/
│  ├─ packet/            Парсинг/генерация TDTP пакетов + компрессия
│  ├─ schema/            Валидация типов, Converter, Builder
│  └─ tdtql/             Translator, Executor, SQL Generator
│
├─ pkg/adapters/
│  ├─ adapter.go         Универсальный интерфейс
│  ├─ factory.go         Фабрика адаптеров
│  ├─ sqlite/            SQLite adapter (modernc.org/sqlite)
│  ├─ postgres/          PostgreSQL adapter (pgx/v5)
│  ├─ mssql/             MS SQL Server adapter (go-mssqldb)
│  └─ mysql/             MySQL adapter (go-sql-driver/mysql)
│
├─ pkg/processors/       Обработка и трансформация данных
│  ├─ compression.go     Сжатие/распаковка zstd (klauspost/compress)
│  ├─ field_masker.go    Маскирование PII (email, phone, card)
│  ├─ field_validator.go Валидация полей (regex, range, format)
│  ├─ field_normalizer.go Нормализация данных
│  ├─ chain.go           Цепочки процессоров
│  └─ factory.go         Фабрика процессоров
│
├─ pkg/security/         Система безопасности
│  ├─ privileges.go      IsAdmin() для Unix/Windows
│  └─ validator.go       SQL валидатор (safe/unsafe режимы)
│
├─ pkg/etl/              ETL Pipeline процессор
│  ├─ config.go          YAML конфигурация с валидацией
│  ├─ workspace.go       SQLite :memory: workspace management
│  ├─ loader.go          Параллельная загрузка из источников
│  ├─ executor.go        Выполнение SQL трансформаций
│  ├─ exporter.go        Экспорт в TDTP/RabbitMQ/Kafka
│  └─ processor.go       Главный оркестратор ETL
│
├─ pkg/resilience/       Circuit Breaker паттерн
│  └─ circuit_breaker.go Защита от каскадных сбоев
│
├─ pkg/audit/            Audit Logger
│  ├─ logger.go          Система аудита (File, DB, Console)
│  └─ appenders.go       Appenders для логов
│
├─ pkg/retry/            Retry механизм
│  └─ retry.go           Стратегии повтора с backoff
│
├─ pkg/sync/             Incremental Sync
│  └─ state_manager.go   Инкрементальная синхронизация
│
├─ pkg/xlsx/             Excel интеграция
│  └─ converter.go       TDTP ↔ XLSX конвертер
│
├─ pkg/brokers/
│  ├─ broker.go          Интерфейс брокеров
│  ├─ rabbitmq.go        RabbitMQ интеграция
│  ├─ kafka.go           Kafka интеграция
│  └─ msmq.go            MSMQ интеграция (Windows)
│
├─ cmd/tdtpcli/          CLI утилита
│  ├─ main.go            Точка входа
│  ├─ help.go            Справочная информация
│  ├─ config.go          YAML конфигурация
│  ├─ processors.go      Интеграция процессоров
│  └─ commands/          Обработчики команд
│
├─ docs/                 Документация
│  ├─ SPECIFICATION.md   Спецификация TDTP v1.0
│  ├─ PACKET_MODULE.md   Документация Packet
│  ├─ SCHEMA_MODULE.md   Документация Schema
│  ├─ TDTQL_TRANSLATOR.md Документация TDTQL
│  ├─ SQLITE_ADAPTER.md  Документация SQLite
│  └─ ETL_PIPELINE_GUIDE.md ETL руководство
│
├─ examples/             Production-ready примеры
│  ├─ 01-basic-export/   PostgreSQL → TDTP XML export
│  ├─ 02-rabbitmq-mssql/ MSSQL → RabbitMQ (Circuit Breaker + Audit)
│  ├─ 03-incremental-sync/ PostgreSQL → MySQL incremental sync
│  ├─ 04-tdtp-xlsx/      Database ↔ Excel converter
│  ├─ 04-audit-masking/  Compliance: Audit logging + PII masking
│  ├─ 05-circuit-breaker/ API resilience patterns
│  └─ 06-etl-pipeline/   Complete ETL pipeline
│
└─ scripts/              Вспомогательные скрипты
   ├─ create_sqlite_test_db.py
   ├─ create_postgres_test_db.py
   └─ README.md
```

## Быстрый старт

### Установка

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
```

### Сборка CLI

```bash
go build -o tdtpcli ./cmd/tdtpcli
```

### Примеры использования CLI

```bash
# Список таблиц
tdtpcli --list --config pg.yaml

# Экспорт таблицы
tdtpcli --export users --output users.xml

# Экспорт с фильтрами и сжатием
tdtpcli --export orders --where 'status = active AND amount > 1000' --limit 100 --compress

# Экспорт последних 50 строк (tail mode)
tdtpcli --export logs --order-by 'created_at DESC' --limit -50

# Просмотр данных в браузере
tdtpcli --to-html customers.xml --open

# Просмотр диапазона строк 100-500
tdtpcli --to-html data.xml --row 100-500 --open

# Просмотр последних 20 строк из диапазона
tdtpcli --to-html data.xml --row 100-500 --limit -20 --open

# Экспорт напрямую в Excel
tdtpcli --export-xlsx orders --output orders.xlsx

# Конвертация TDTP в Excel с выбором листа
tdtpcli --to-xlsx orders.xml --output orders.xlsx --sheet Orders

# Конвертация Excel в TDTP
tdtpcli --from-xlsx orders.xlsx --output orders.xml

# Импорт Excel в базу данных
tdtpcli --import-xlsx orders.xlsx --strategy replace

# Сравнение двух TDTP файлов
tdtpcli --diff users-old.xml users-new.xml

# Сравнение с указанием ключей и игнорированием полей
tdtpcli --diff old.xml new.xml --key-fields user_id --ignore-fields updated_at

# Слияние нескольких файлов (стратегия union)
tdtpcli --merge file1.xml,file2.xml,file3.xml --output merged.xml

# Слияние с разрешением конфликтов
tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy right --show-conflicts

# Инкрементальная синхронизация
tdtpcli --sync-incremental orders --tracking-field updated_at --checkpoint-file orders.yaml

# Экспорт с маскированием PII
tdtpcli --export customers --mask email,phone

# ETL pipeline (safe mode)
tdtpcli --pipeline pipeline.yaml

# ETL pipeline (unsafe mode, требует admin)
sudo tdtpcli --pipeline pipeline.yaml --unsafe

# Создание конфигурационного файла
tdtpcli --create-config-pg > config.yaml
tdtpcli --create-config-mysql > mysql.yaml
```

### Использование в коде

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// Создание схемы
schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "ID", Type: "INTEGER", Key: true},
        {Name: "Name", Type: "TEXT", Length: 200},
        {Name: "Balance", Type: "DECIMAL"},
    },
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

### Использование сжатия

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/core/packet"
    "github.com/ruslano69/tdtp-framework/pkg/processors"
)

generator := packet.NewGenerator()
generator.SetCompression(packet.CompressionOptions{
    Enabled:   true,
    Level:     3,      // 1 (быстро) — 19 (лучшее сжатие)
    MinSize:   1024,   // Минимальный размер для сжатия (байты)
    Algorithm: "zstd",
})

packets, err := generator.GenerateReference("LargeTable", schema, rows)

// Прямое использование
compressed, stats, err := processors.Compress([]byte("large data"), 3)
decompressed, err := processors.Decompress(compressed)
```

### Использование адаптеров

```go
import (
    "context"
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
)

ctx := context.Background()

adapter, err := adapters.New(ctx, adapters.Config{
    Type: "postgres",
    DSN:  "postgres://localhost/mydb",
})
defer adapter.Close(ctx)

// Export: БД → TDTP
packets, err := adapter.ExportTable(ctx, "users")

// Import: TDTP → БД
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

### Готовые примеры

```bash
# Database ↔ Excel converter
cd examples/04-tdtp-xlsx
go run main.go

# RabbitMQ + MSSQL (Circuit Breaker, Audit, Retry)
cd examples/02-rabbitmq-mssql
go run main.go

# Incremental Sync (200x faster for large tables)
cd examples/03-incremental-sync
go run main.go

# Полный ETL pipeline
cd examples/06-etl-pipeline
go run main.go
```

**Документация примеров**: [examples/README.md](./examples/README.md)

## Документация

### Руководства

- **[Installation Guide](INSTALLATION_GUIDE.md)** — установка, настройка, quick start
- **[User Guide](docs/USER_GUIDE.md)** — полное руководство по CLI утилите
- **[ETL Pipeline Guide](docs/ETL_PIPELINE_GUIDE.md)** — руководство по ETL pipeline
- **[Documentation Index](docs/README.md)** — полный каталог документации

### Технические спецификации

- [TDTP Specification](docs/SPECIFICATION.md) — спецификация протокола TDTP v1.0
- [Packet Module](docs/PACKET_MODULE.md) — парсинг и генерация пакетов
- [Schema Module](docs/SCHEMA_MODULE.md) — валидация типов и схем
- [TDTQL Translator](docs/TDTQL_TRANSLATOR.md) — язык запросов
- [SQLite Adapter](docs/SQLITE_ADAPTER.md) — интеграция с SQLite

### Package READMEs

- [Circuit Breaker](pkg/resilience/README.md) — защита от каскадных сбоев
- [Audit Logger](pkg/audit/README.md) — compliance и security
- [XLSX Converter](pkg/xlsx/README.md) — Database ↔ Excel

## Тестирование

```bash
# Запуск всех тестов
go test ./...

# С покрытием
go test -cover ./...

# Verbose для конкретного пакета
go test -v ./pkg/core/packet/
```

## Roadmap

### v1.0 — v1.3 (завершено)
- [x] Packet, Schema, TDTQL модули
- [x] SQLite, PostgreSQL, MS SQL адаптеры
- [x] RabbitMQ, MSMQ, Kafka брокеры
- [x] CLI утилита с TDTQL фильтрами
- [x] CircuitBreaker, AuditLogger, Retry механизм
- [x] IncrementalSync, Data Processors
- [x] XLSX Converter (Database ↔ Excel)
- [x] ETL Pipeline Processor с 4-уровневой безопасностью
- [x] MySQL адаптер
- [x] Полная документация

### v1.6.0 (текущая)
- [x] HTML Viewer (`--to-html`, `--open`, `--row`)
- [x] Diff & Merge (`--diff`, `--merge`, `--merge-strategy`, `--show-conflicts`)
- [x] Расширенные XLSX команды (`--from-xlsx`, `--export-xlsx`, `--import-xlsx`)
- [x] Инкрементальная синхронизация через CLI (`--sync-incremental`)
- [x] Data Processors в CLI (`--mask`, `--validate`, `--normalize`)
- [x] Tail mode в limit (`--limit -N`)
- [x] `--batch`, `--readonly-fields` опции

### v2.0 (планируется)
- [ ] Streaming export/import (TotalParts=0, "TCP для таблиц")
- [ ] Parallel import workers
- [ ] Python bindings (ctypes wrapper)
- [ ] Docker образ (multi-stage build)
- [ ] Monitoring & metrics (Prometheus exporter)
- [ ] Schema migration (ALTER TABLE)

## Вклад в проект

Проект находится в активной разработке. Приветствуются:
- Баг-репорты
- Предложения по улучшению
- Pull requests

## Лицензия

MIT

## Контакты

- GitHub: https://github.com/ruslano69/tdtp-framework
- Issues: https://github.com/ruslano69/tdtp-framework/issues
- Email: ruslano69@gmail.com

---

**Версия:** v1.6.0
**Последнее обновление:** 23.02.2026
