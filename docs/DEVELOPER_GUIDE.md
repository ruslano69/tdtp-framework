# TDTP Framework - Developer Guide

**Руководство разработчика** для TDTP (Table Data Transfer Protocol) Framework.

**Версия:** 1.2
**Дата:** 17.11.2025
**Репозиторий:** https://github.com/ruslano69/tdtp-framework

---

## Содержание

1. [Архитектура фреймворка](#архитектура-фреймворка)
2. [Настройка тестовой среды](#настройка-тестовой-среды)
3. [Core Modules](#core-modules)
   - [Packet Module](#packet-module)
   - [Schema Module](#schema-module)
   - [TDTQL Module](#tdtql-module)
4. [Database Adapters](#database-adapters)
   - [Universal Interface](#universal-interface)
   - [SQLite Adapter](#sqlite-adapter)
   - [PostgreSQL Adapter](#postgresql-adapter)
   - [MS SQL Server Adapter](#mssql-adapter)
   - [MySQL Adapter](#mysql-adapter)
5. [Message Brokers](#message-brokers)
6. [Production Features](#production-features-v12)
7. [Разработка нового адаптера](#разработка-нового-адаптера)
8. [Best Practices](#best-practices)
9. [Testing](#testing)

---

## Архитектура фреймворка

### Общая структура

```
tdtp-framework/
├── pkg/
│   ├── core/              # Ядро протокола
│   │   ├── packet/        # Парсер и генератор TDTP XML
│   │   ├── schema/        # Валидация типов данных
│   │   └── tdtql/         # Query language translator
│   │
│   ├── adapters/          # Адаптеры БД
│   │   ├── adapter.go     # Универсальный интерфейс
│   │   ├── sqlite/        # SQLite адаптер
│   │   ├── postgres/      # PostgreSQL адаптер
│   │   ├── mssql/         # MS SQL Server адаптер
│   │   └── mysql/         # MySQL адаптер
│   │
│   ├── brokers/           # Message brokers
│   │   ├── rabbitmq.go    # RabbitMQ
│   │   ├── msmq.go        # MSMQ (Windows)
│   │   └── kafka.go       # Apache Kafka
│   │
│   ├── xlsx/              # XLSX Converter 🆕 v1.2
│   ├── audit/             # Audit Logger 🆕 v1.2
│   ├── resilience/        # Circuit Breaker 🆕 v1.2
│   ├── retry/             # Retry mechanism 🆕 v1.2
│   ├── sync/              # Incremental Sync 🆕 v1.1
│   └── processors/        # Data Processors 🆕 v1.2
│
├── cmd/
│   └── tdtpcli/           # CLI утилита
│
├── docs/                  # Документация
├── examples/              # Примеры
└── tests/                 # Интеграционные тесты
```

### Слои архитектуры

**Layer 1: Protocol Core**
- `packet` - сериализация/десериализация TDTP XML
- `schema` - типизация и валидация данных
- `tdtql` - язык запросов

**Layer 2: Data Access**
- `adapters` - двунаправленная интеграция с СУБД
- Стратегии импорта (REPLACE, IGNORE, FAIL, COPY)
- TDTQL → SQL оптимизация

**Layer 3: Transport**
- `brokers` - асинхронный обмен через очереди
- RabbitMQ, MSMQ, Kafka

**Layer 4: Production Features**
- `resilience` - Circuit Breaker
- `retry` - Retry with backoff
- `audit` - Audit logging
- `processors` - Data masking/validation
- `sync` - Incremental synchronization

**Layer 5: Applications**
- `tdtpcli` - CLI утилита
- Custom applications

---

## Настройка тестовой среды

### Требования

- **Go:** 1.21+ (рекомендуется 1.22+)
- **Docker** (опционально, для БД и брокеров)
- **Make** (опционально, для автоматизации)

### Шаг 1: Клонирование и установка зависимостей

```bash
# Клонирование
git clone https://github.com/ruslano69/tdtp-framework.git
cd tdtp-framework

# Установка зависимостей
go mod tidy
go mod download

# Проверка сборки
go build ./...
```

### Шаг 2: Запуск тестовых БД через Docker

**PostgreSQL:**
```bash
docker run -d \
  --name tdtp-postgres \
  -e POSTGRES_USER=tdtp_user \
  -e POSTGRES_PASSWORD=tdtp_pass \
  -e POSTGRES_DB=tdtp_test \
  -p 5432:5432 \
  postgres:15-alpine

# Проверка
docker exec tdtp-postgres psql -U tdtp_user -d tdtp_test -c '\dt'
```

**MS SQL Server:**
```bash
docker run -d \
  --name tdtp-mssql \
  -e ACCEPT_EULA=Y \
  -e SA_PASSWORD=MyStr0ng@Passw0rd \
  -p 1433:1433 \
  mcr.microsoft.com/mssql/server:2022-latest

# Проверка
docker exec tdtp-mssql /opt/mssql-tools/bin/sqlcmd \
  -S localhost -U sa -P 'MyStr0ng@Passw0rd' \
  -Q "SELECT @@VERSION"
```

**MySQL:**
```bash
docker run -d \
  --name tdtp-mysql \
  -e MYSQL_ROOT_PASSWORD=root_pass \
  -e MYSQL_DATABASE=tdtp_test \
  -e MYSQL_USER=tdtp_user \
  -e MYSQL_PASSWORD=tdtp_pass \
  -p 3306:3306 \
  mysql:8.0

# Проверка
docker exec tdtp-mysql mysql -u tdtp_user -ptdtp_pass -e "SHOW DATABASES;"
```

### Шаг 3: Запуск RabbitMQ

```bash
docker run -d \
  --name tdtp-rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  rabbitmq:3-management

# Web UI: http://localhost:15672 (guest/guest)
```

### Шаг 4: Генерация тестовых данных

```bash
# SQLite тестовая БД
go run scripts/generate_sqlite_testdb.go

# Проверка
sqlite3 test_database.db ".tables"
sqlite3 test_database.db "SELECT COUNT(*) FROM CustTable;"
```

### Шаг 5: Запуск unit тестов

```bash
# Все тесты
go test ./... -v

# Только core модули
go test ./pkg/core/... -v

# С покрытием
go test ./pkg/core/packet -cover
go test ./pkg/core/schema -cover
go test ./pkg/core/tdtql -cover

# Интеграционные тесты (требуют Docker)
go test ./tests/integration/... -v
```

### Шаг 6: Сборка CLI

```bash
# Сборка
go build -o tdtpcli ./cmd/tdtpcli

# Проверка
./tdtpcli --help
./tdtpcli --create-config-sqlite
./tdtpcli -config config.sqlite.yaml --list
```

### Переменные окружения для тестов

Создайте `.env` файл:

```bash
# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=tdtp_user
POSTGRES_PASSWORD=tdtp_pass
POSTGRES_DB=tdtp_test

# MS SQL
MSSQL_HOST=localhost
MSSQL_PORT=1433
MSSQL_USER=sa
MSSQL_PASSWORD=MyStr0ng@Passw0rd
MSSQL_DB=master

# MySQL
MYSQL_HOST=localhost
MYSQL_PORT=3306
MYSQL_USER=tdtp_user
MYSQL_PASSWORD=tdtp_pass
MYSQL_DB=tdtp_test

# RabbitMQ
RABBITMQ_HOST=localhost
RABBITMQ_PORT=5672
RABBITMQ_USER=guest
RABBITMQ_PASSWORD=guest
```

---

## Core Modules

### Packet Module

**Расположение:** `pkg/core/packet/`

**Назначение:** Парсинг и генерация TDTP XML пакетов.

#### Основные типы

```go
// DataPacket - основной контейнер TDTP
type DataPacket struct {
    Protocol     string        // "TDTP"
    Version      string        // "1.0"
    Header       Header        // Заголовок
    Schema       Schema        // Схема данных
    Data         Data          // Данные
    Query        *Query        // Запрос (опционально)
    QueryContext *QueryContext // Контекст (опционально)
    Alarm        *Alarm        // Тревога (опционально)
}

// Header - заголовок пакета
type Header struct {
    Type           string    // reference | delta | request | response | alarm
    TableName      string    // Имя таблицы
    MessageID      string    // UUID сообщения
    PartNumber     int       // Номер части
    TotalParts     int       // Всего частей
    RecordsInPart  int       // Записей в части
    Timestamp      time.Time // Время создания
    Sender         string    // Отправитель
    Recipient      string    // Получатель
    InReplyTo      string    // ID запроса (для response)
}
```

#### API Parser

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// Создание парсера
parser := packet.NewParser()

// Парсинг из файла
pkt, err := parser.ParseFile("data.tdtp.xml")
if err != nil {
    log.Fatal(err)
}

// Парсинг из []byte
xmlData := []byte(`<DataPacket>...</DataPacket>`)
pkt, err = parser.ParseBytes(xmlData)

// Парсинг из io.Reader
file, _ := os.Open("data.tdtp.xml")
pkt, err = parser.Parse(file)

// Извлечение значений строки
for _, row := range pkt.Data.Rows {
    values := parser.GetRowValues(row)
    fmt.Println(values) // []string{"1", "John", "john@example.com"}
}
```

#### API Generator

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// Создание генератора
generator := packet.NewGenerator()

// Настройка максимального размера пакета (опционально)
generator.SetMaxMessageSize(3800000) // 3.8MB

// Генерация Reference (полный справочник)
schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "id", Type: "INTEGER", Key: true},
        {Name: "username", Type: "TEXT", Length: 100},
        {Name: "email", Type: "TEXT", Length: 255},
    },
}

rows := [][]string{
    {"1", "john_doe", "john@example.com"},
    {"2", "jane_smith", "jane@example.com"},
}

packets, err := generator.GenerateReference("users", schema, rows)
if err != nil {
    log.Fatal(err)
}

// Сохранение в файл
err = generator.WriteToFile(packets[0], "users.tdtp.xml")

// Или в XML string
xmlData, err := generator.ToXML(packets[0], true) // true = с отступами
fmt.Println(string(xmlData))
```

#### Автоматическое разбиение на части

Генератор автоматически разбивает большие наборы данных на части по ~3.8MB:

```go
generator.SetMaxMessageSize(3800000)
packets, _ := generator.GenerateReference(tableName, schema, bigData)
// packets[0].Header.PartNumber = 1
// packets[0].Header.TotalParts = 3
// packets[1].Header.PartNumber = 2
// ...
```

#### Валидация

Parser автоматически проверяет:
- Обязательные поля (Type, TableName, MessageID, Timestamp)
- Валидность типа сообщения
- InReplyTo для response
- Корректность PartNumber/TotalParts
- Наличие Schema при наличии Data

---

### Schema Module

**Расположение:** `pkg/core/schema/`

**Назначение:** Валидация типов данных, конвертация значений, построение схем.

#### Поддерживаемые типы данных

```go
TypeInteger   // INTEGER, INT
TypeReal      // REAL, FLOAT, DOUBLE
TypeDecimal   // DECIMAL(precision, scale)
TypeText      // TEXT, VARCHAR, CHAR, STRING
TypeBoolean   // BOOLEAN, BOOL (0/1)
TypeDate      // DATE (YYYY-MM-DD)
TypeDatetime  // DATETIME (RFC3339 с таймзоной)
TypeTimestamp // TIMESTAMP (RFC3339, всегда UTC)
TypeBlob      // BLOB (Base64)
```

#### Builder - построение схем

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/schema"

builder := schema.NewBuilder()

// Добавление полей
schemaObj := builder.
    AddInteger("id", true).                    // key=true
    AddText("username", 100).
    AddText("email", 255).
    AddDecimal("balance", 12, 2).              // precision=12, scale=2
    AddBoolean("is_active").
    AddTimestamp("created_at", "UTC", false).
    Build()

// Использование
for _, field := range schemaObj.Fields {
    fmt.Printf("%s: %s\n", field.Name, field.Type)
}
```

#### Converter - конвертация значений

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/schema"

converter := schema.NewConverter()

// Парсинг значения
field := schema.FieldDef{
    Name: "balance",
    Type: schema.TypeDecimal,
    Precision: 12,
    Scale: 2,
}

value, err := converter.ParseValue("1234.56", field)
if err != nil {
    log.Fatal(err)
}

// Форматирование обратно в строку
formatted := converter.FormatValue(value)
fmt.Println(formatted) // "1234.56"
```

#### Validator - валидация данных

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/schema"

validator := schema.NewValidator()

// Валидация строки данных
row := []string{"1", "john_doe", "john@example.com", "1500.50", "1"}

err := validator.ValidateRow(row, schemaObj)
if err != nil {
    fmt.Println("Validation error:", err)
}

// Валидация отдельного значения
err = validator.ValidateValue("1500.50", schemaObj.Fields[3])
```

---

### TDTQL Module

**Расположение:** `pkg/core/tdtql/`

**Назначение:** Трансляция SQL → TDTQL, выполнение запросов in-memory, генерация SQL.

#### Translator (SQL → TDTQL)

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/tdtql"

translator := tdtql.NewTranslator()

// Трансляция SQL WHERE в TDTQL
sqlQuery := "SELECT * FROM users WHERE age >= 18 AND is_active = 1 ORDER BY balance DESC LIMIT 100"

query, err := translator.Translate(sqlQuery)
if err != nil {
    log.Fatal(err)
}

// query теперь содержит TDTQL структуру
fmt.Printf("Filters: %+v\n", query.Filters)
fmt.Printf("OrderBy: %+v\n", query.OrderBy)
fmt.Printf("Limit: %d\n", query.Limit)
```

#### Поддерживаемые операторы

**Сравнение:**
- `=`, `!=`, `<>` - равенство/неравенство
- `>`, `>=`, `<`, `<=` - сравнение
- `LIKE`, `NOT LIKE` - паттерны с wildcards (`%`, `_`)

**Диапазоны:**
- `IN (value1, value2, ...)` - в списке
- `NOT IN (...)` - не в списке
- `BETWEEN value1 AND value2` - в диапазоне

**NULL:**
- `IS NULL` - значение NULL
- `IS NOT NULL` - значение НЕ NULL

**Логические:**
- `AND` - логическое И
- `OR` - логическое ИЛИ
- `NOT` - отрицание
- Поддержка скобок для приоритета

#### Executor (in-memory filtering)

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/tdtql"

executor := tdtql.NewExecutor()

// Создание запроса
query := packet.NewQuery()
query.Filters = &packet.Filters{
    And: &packet.LogicalGroup{
        Filters: []packet.Filter{
            {Field: "age", Operator: "gte", Value: "18"},
        },
    },
}
query.OrderBy = &packet.OrderBy{Field: "age", Direction: "DESC"}
query.Limit = 10

// Данные для фильтрации
rows := [][]string{
    {"1", "john", "25"},
    {"2", "jane", "17"},
    {"3", "bob", "30"},
}

schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "id", Type: "INTEGER"},
        {Name: "name", Type: "TEXT"},
        {Name: "age", Type: "INTEGER"},
    },
}

// Выполнение
result, err := executor.Execute(query, rows, schema)
if err != nil {
    log.Fatal(err)
}

// Результаты
fmt.Printf("Total rows: %d\n", len(rows))
fmt.Printf("Filtered rows: %d\n", len(result.FilteredRows))
```

#### SQL Generator (TDTQL → SQL)

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/tdtql"

generator := tdtql.NewSQLGenerator()

// Проверка возможности трансляции
if generator.CanTranslateToSQL(query) {
    // Генерация SQL
    sql, err := generator.GenerateSQL("users", query)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(sql)
    // SELECT * FROM users WHERE age >= 18 ORDER BY age DESC LIMIT 10
}
```

---

## Database Adapters

### Universal Interface

**Расположение:** `pkg/adapters/adapter.go`

**Назначение:** Универсальный интерфейс для всех адаптеров БД.

#### Интерфейс Adapter

```go
type Adapter interface {
    // Подключение и закрытие
    Connect(ctx context.Context) error
    Close(ctx context.Context) error
    Ping(ctx context.Context) error

    // Метаданные
    GetDatabaseType() string
    GetVersion(ctx context.Context) (string, error)
    ListTables(ctx context.Context) ([]string, error)
    TableExists(ctx context.Context, tableName string) (bool, error)
    GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error)

    // Export
    ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)
    ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error)

    // Import
    ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy ImportStrategy) error

    // Транзакции
    BeginTx(ctx context.Context) (Tx, error)

    // Утилиты
    Exec(ctx context.Context, query string, args ...interface{}) error
}
```

#### Фабрика адаптеров

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"   // Регистрация
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres" // Регистрация
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"    // Регистрация
)

ctx := context.Background()

// Создание адаптера через фабрику
cfg := adapters.Config{
    Type: "postgres",
    DatabaseConfig: adapters.DatabaseConfig{
        Host:     "localhost",
        Port:     5432,
        User:     "myuser",
        Password: "mypass",
        DBName:   "mydb",
        Schema:   "public",
        SSLMode:  "disable",
    },
}

adapter, err := adapters.New(ctx, cfg)
if err != nil {
    log.Fatal(err)
}
defer adapter.Close(ctx)

// Использование
tables, err := adapter.ListTables(ctx)
packets, err := adapter.ExportTable(ctx, "users")
```

#### Стратегии импорта

```go
const (
    StrategyReplace ImportStrategy = "replace" // Полная замена через temp table
    StrategyIgnore  ImportStrategy = "ignore"  // Игнорировать конфликты
    StrategyFail    ImportStrategy = "fail"    // Прервать при конфликте
    StrategyCopy    ImportStrategy = "copy"    // Копировать (INSERT)
)

// Использование
err = adapter.ImportPacket(ctx, packet, adapters.StrategyReplace)
```

---

### SQLite Adapter

**Расположение:** `pkg/adapters/sqlite/`

**Особенности:**
- Драйвер: `modernc.org/sqlite` (pure Go, без CGo)
- Автоматическое создание таблиц
- TDTQL → SQL оптимизация
- Transaction support

**Пример использования:**

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

cfg := adapters.Config{
    Type: "sqlite",
    DatabaseConfig: adapters.DatabaseConfig{
        Path: "./database.db",
    },
}

adapter, err := adapters.New(ctx, cfg)
if err != nil {
    log.Fatal(err)
}
defer adapter.Close(ctx)

// Export
packets, err := adapter.ExportTable(ctx, "users")

// Export с фильтрацией
query := packet.NewQuery()
query.Filters = &packet.Filters{
    And: &packet.LogicalGroup{
        Filters: []packet.Filter{
            {Field: "balance", Operator: "gte", Value: "1000"},
        },
    },
}
packets, err = adapter.ExportTableWithQuery(ctx, "users", query, "", "")

// Import
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

**Маппинг типов:**

| TDTP | SQLite |
|------|--------|
| INTEGER | INTEGER |
| REAL | REAL |
| DECIMAL | TEXT |
| TEXT | TEXT |
| BOOLEAN | INTEGER |
| TIMESTAMP | TEXT |
| BLOB | BLOB |

---

### PostgreSQL Adapter

**Расположение:** `pkg/adapters/postgres/`

**Особенности:**
- Драйвер: `github.com/jackc/pgx/v5` (connection pool)
- Поддержка schemas (public/custom)
- COPY для bulk import (высокая производительность)
- Специальные типы: UUID, JSONB, INET, ARRAY, NUMERIC
- ON CONFLICT для стратегий импорта

**Пример использования:**

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
)

cfg := adapters.Config{
    Type: "postgres",
    DatabaseConfig: adapters.DatabaseConfig{
        Host:     "localhost",
        Port:     5432,
        User:     "tdtp_user",
        Password: "password",
        DBName:   "tdtp_db",
        Schema:   "public",
        SSLMode:  "disable",
    },
}

adapter, err := adapters.New(ctx, cfg)

// Export с schema-aware SQL
packets, err := adapter.ExportTable(ctx, "users")

// Export с TDTQL фильтрами (SQL-level optimization)
query := packet.NewQuery()
query.Filters = &packet.Filters{
    And: &packet.LogicalGroup{
        Filters: []packet.Filter{
            {Field: "balance", Operator: "gte", Value: "5000"},
        },
    },
}
query.OrderBy = &packet.OrderBy{Field: "balance", Direction: "DESC"}
query.Limit = 20

packets, err = adapter.ExportTableWithQuery(ctx, "users", query, "", "")
```

**Маппинг типов:**

| TDTP | PostgreSQL |
|------|------------|
| INTEGER | INTEGER, SERIAL |
| REAL | DOUBLE PRECISION |
| DECIMAL | NUMERIC(p,s) |
| TEXT | VARCHAR, TEXT |
| TEXT (subtype=uuid) | UUID |
| TEXT (subtype=jsonb) | JSONB |
| TEXT (subtype=inet) | INET |
| BOOLEAN | BOOLEAN |
| TIMESTAMP | TIMESTAMP, TIMESTAMPTZ |

---

### MSSQL Adapter

**Расположение:** `pkg/adapters/mssql/`

**Особенности:**
- Драйвер: `github.com/microsoft/go-mssqldb`
- IDENTITY_INSERT для импорта с ключевыми полями
- Поддержка NVARCHAR, UNIQUEIDENTIFIER, DATETIME2
- Совместимость с MS SQL 2012+
- Параметризованные запросы (защита от SQL injection)

**Пример использования:**

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
)

cfg := adapters.Config{
    Type: "mssql",
    DatabaseConfig: adapters.DatabaseConfig{
        Host:     "localhost",
        Port:     1433,
        User:     "sa",
        Password: "YourStrong@Passw0rd",
        DBName:   "TestDB",
        Instance: "SQLEXPRESS",
        Encrypt:  false,
        TrustServerCertificate: true,
    },
}

adapter, err := adapters.New(ctx, cfg)

// Export
packets, err := adapter.ExportTable(ctx, "dbo.users")

// Import с IDENTITY_INSERT
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

**Маппинг типов:**

| TDTP | MS SQL |
|------|--------|
| INTEGER | INT, BIGINT |
| REAL | FLOAT, REAL |
| DECIMAL | DECIMAL(p,s), NUMERIC |
| TEXT | NVARCHAR, VARCHAR |
| TEXT (subtype=uuid) | UNIQUEIDENTIFIER |
| BOOLEAN | BIT |
| TIMESTAMP | DATETIME2, DATETIME |

---

### MySQL Adapter

**Расположение:** `pkg/adapters/mysql/`

**Особенности:**
- Драйвер: `github.com/go-sql-driver/mysql`
- Поддержка LOAD DATA LOCAL INFILE для bulk import
- JSON и GEOMETRY типы
- Auto-increment handling
- Charset UTF-8

**Пример использования:**

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
)

cfg := adapters.Config{
    Type: "mysql",
    DatabaseConfig: adapters.DatabaseConfig{
        Host:     "localhost",
        Port:     3306,
        User:     "tdtp_user",
        Password: "password",
        DBName:   "tdtp_db",
        Charset:  "utf8mb4",
    },
}

adapter, err := adapters.New(ctx, cfg)

// Export
packets, err := adapter.ExportTable(ctx, "users")

// Import
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

---

## Message Brokers

### RabbitMQ Broker

**Расположение:** `pkg/brokers/rabbitmq.go`

**Особенности:**
- AMQP 0.9.1 протокол
- Manual ACK для надежной доставки
- Queue parameters (durable, auto_delete, exclusive)
- Connection pooling

**Пример использования:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/brokers"

config := brokers.BrokerConfig{
    Type:       "rabbitmq",
    Host:       "localhost",
    Port:       5672,
    User:       "guest",
    Password:   "guest",
    Queue:      "tdtp_queue",
    VHost:      "/",
    Durable:    true,
    AutoDelete: false,
    Exclusive:  false,
}

broker, err := brokers.NewBroker(config)
if err != nil {
    log.Fatal(err)
}
defer broker.Close()

// Publish
packets, _ := adapter.ExportTable(ctx, "users")
err = broker.PublishPackets(packets)

// Consume
packets, err = broker.ConsumePackets()
for _, pkt := range packets {
    err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
    if err != nil {
        log.Printf("Import error: %v", err)
        continue
    }
    // ACK происходит автоматически после успешного импорта
}
```

---

## Production Features (v1.2)

### Circuit Breaker (pkg/resilience)

**Защита от каскадных сбоев:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/resilience"

// Конфигурация
config := resilience.Config{
    MaxFailures:        5,
    Timeout:            30 * time.Second,
    MaxConcurrentCalls: 100,
    SuccessThreshold:   2,
}

cb, err := resilience.NewCircuitBreaker(config)

// Выполнение операции
err = cb.Execute(ctx, func(ctx context.Context) error {
    return adapter.ExportTable(ctx, "large_table")
})

// Состояния: Closed → Open → Half-Open → Closed
```

### Retry Mechanism (pkg/retry)

**Автоматические повторы с backoff:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/retry"

config := retry.Config{
    MaxAttempts:     5,
    InitialInterval: 1 * time.Second,
    MaxInterval:     30 * time.Second,
    Multiplier:      2.0,
    Strategy:        retry.BackoffExponential,
}

retryer, err := retry.NewRetryer(config)

// Retry с контекстом
err = retryer.Do(ctx, func(ctx context.Context) error {
    return adapter.ExportTable(ctx, "users")
})
```

### Audit Logger (pkg/audit)

**Логирование операций для GDPR/HIPAA:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/audit"

// File appender
appender, err := audit.NewFileAppender(audit.FileAppenderConfig{
    FilePath:   "/var/log/tdtp/audit.log",
    MaxSize:    100 * 1024 * 1024, // 100MB
    MaxBackups: 10,
    Level:      audit.LevelFull,
    FormatJSON: true,
})

logger, err := audit.NewAuditLogger([]audit.Appender{appender})

// Логирование операции
logger.Log(ctx, audit.Entry{
    Operation:  audit.OpExport,
    Table:      "users",
    Success:    true,
    RecordCount: 1000,
    Metadata: map[string]string{
        "user": "admin",
        "duration_ms": "1234",
    },
})
```

### Data Processors (pkg/processors)

**Маскирование, валидация, нормализация:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/processors"

// Field Masker (PII protection)
masker := processors.NewFieldMasker(map[string]processors.MaskPattern{
    "email": processors.MaskPartial,      // j***@example.com
    "phone": processors.MaskMiddle,       // +7 (9**) ***-45-67
    "card":  processors.MaskFirst2Last2,  // 12** **** **** **89
})

// Field Validator
validator, err := processors.NewFieldValidator(map[string][]processors.FieldValidationRule{
    "email": {{Type: processors.ValidateEmail}},
    "age":   {{Type: processors.ValidateRange, Param: "18,100"}},
}, false)

// Field Normalizer
normalizer, err := processors.NewFieldNormalizer(map[string]processors.NormalizationType{
    "email": processors.NormalizeEmail,      // ToLower, trim
    "phone": processors.NormalizePhone,      // Remove spaces, dashes
})

// Processor Chain
chain := processors.NewChain()
chain.Add(validator)
chain.Add(normalizer)
chain.Add(masker)

// Применение к данным
result, err := chain.Process(ctx, data, schema)
```

### Incremental Sync (pkg/sync)

**Синхронизация с checkpoint tracking:**

```go
import "github.com/ruslano69/tdtp-framework/pkg/sync"

// State Manager
stateMgr, err := sync.NewStateManager("checkpoints.json", true)

// Получить последнее состояние
state := stateMgr.GetState("users")
lastValue := state.LastSyncValue

// Экспорт инкрементальных изменений
query := packet.NewQuery()
query.Filters = &packet.Filters{
    And: &packet.LogicalGroup{
        Filters: []packet.Filter{
            {Field: "updated_at", Operator: "gt", Value: lastValue},
        },
    },
}

packets, err := adapter.ExportTableWithQuery(ctx, "users", query, "", "")

// Обновить checkpoint
newLastValue := extractMaxValue(packets, "updated_at")
stateMgr.UpdateState("users", newLastValue, len(packets))
```

---

## Разработка нового адаптера

### Шаблон адаптера

```go
package mydb

import (
    "context"
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    "github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Adapter для MyDB
type Adapter struct {
    db     *MyDBClient
    config adapters.DatabaseConfig
}

// Регистрация в фабрике
func init() {
    adapters.Register("mydb", func(ctx context.Context, cfg adapters.Config) (adapters.Adapter, error) {
        return NewAdapter(ctx, cfg.DatabaseConfig)
    })
}

// NewAdapter создает новый адаптер
func NewAdapter(ctx context.Context, config adapters.DatabaseConfig) (*Adapter, error) {
    adapter := &Adapter{config: config}

    // Подключение
    if err := adapter.Connect(ctx); err != nil {
        return nil, err
    }

    return adapter, nil
}

// Connect подключается к БД
func (a *Adapter) Connect(ctx context.Context) error {
    // Реализация подключения
    return nil
}

// Close закрывает соединение
func (a *Adapter) Close(ctx context.Context) error {
    if a.db != nil {
        return a.db.Close()
    }
    return nil
}

// GetDatabaseType возвращает тип БД
func (a *Adapter) GetDatabaseType() string {
    return "mydb"
}

// ExportTable экспортирует таблицу
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
    // 1. Получить схему таблицы
    schema, err := a.GetTableSchema(ctx, tableName)
    if err != nil {
        return nil, err
    }

    // 2. Прочитать данные
    rows, err := a.queryRows(ctx, fmt.Sprintf("SELECT * FROM %s", tableName))
    if err != nil {
        return nil, err
    }

    // 3. Сгенерировать пакеты
    generator := packet.NewGenerator()
    packets, err := generator.GenerateReference(tableName, schema, rows)

    return packets, err
}

// ImportPacket импортирует пакет
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
    // 1. Проверить существование таблицы
    exists, err := a.TableExists(ctx, pkt.Header.TableName)
    if err != nil {
        return err
    }

    // 2. Создать таблицу если нужно
    if !exists {
        if err := a.createTable(ctx, pkt.Header.TableName, pkt.Schema); err != nil {
            return err
        }
    }

    // 3. Импортировать данные согласно стратегии
    switch strategy {
    case adapters.StrategyReplace:
        return a.importReplace(ctx, pkt)
    case adapters.StrategyIgnore:
        return a.importIgnore(ctx, pkt)
    case adapters.StrategyFail:
        return a.importFail(ctx, pkt)
    default:
        return fmt.Errorf("unknown strategy: %s", strategy)
    }
}

// Остальные методы интерфейса...
```

### Маппинг типов

Создайте файл `types.go` с маппингом TDTP → MyDB:

```go
func tdtpToMyDB(field packet.Field) string {
    switch field.Type {
    case "INTEGER":
        return "INT"
    case "REAL":
        return "DOUBLE"
    case "DECIMAL":
        return fmt.Sprintf("DECIMAL(%d,%d)", field.Precision, field.Scale)
    case "TEXT":
        if field.Length > 0 {
            return fmt.Sprintf("VARCHAR(%d)", field.Length)
        }
        return "TEXT"
    case "BOOLEAN":
        return "BOOLEAN"
    case "DATE":
        return "DATE"
    case "TIMESTAMP":
        return "TIMESTAMP"
    case "BLOB":
        return "BLOB"
    default:
        return "TEXT"
    }
}

func myDBToTDTP(mydbType string) string {
    // Обратный маппинг
}
```

### Тестирование

Создайте `adapter_test.go`:

```go
func TestAdapter_ExportImport(t *testing.T) {
    ctx := context.Background()

    // Setup
    adapter, err := NewAdapter(ctx, testConfig)
    require.NoError(t, err)
    defer adapter.Close(ctx)

    // Export
    packets, err := adapter.ExportTable(ctx, "test_table")
    require.NoError(t, err)
    require.NotEmpty(t, packets)

    // Import
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
    require.NoError(t, err)
}
```

---

## Best Practices

### 1. Использование Context

Всегда передавайте context.Context для возможности отмены операций:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

packets, err := adapter.ExportTable(ctx, "large_table")
```

### 2. Обработка ошибок

Проверяйте ошибки на каждом этапе:

```go
adapter, err := adapters.New(ctx, cfg)
if err != nil {
    return fmt.Errorf("failed to create adapter: %w", err)
}
defer adapter.Close(ctx)

if err := adapter.Connect(ctx); err != nil {
    return fmt.Errorf("failed to connect: %w", err)
}
```

### 3. Закрытие ресурсов

Используйте defer для гарантированного закрытия:

```go
adapter, _ := adapters.New(ctx, cfg)
defer adapter.Close(ctx)

broker, _ := brokers.NewBroker(config)
defer broker.Close()
```

### 4. Пагинация больших таблиц

Для больших таблиц используйте LIMIT/OFFSET:

```go
pageSize := 10000
offset := 0

for {
    query := packet.NewQuery()
    query.Limit = pageSize
    query.Offset = offset

    packets, err := adapter.ExportTableWithQuery(ctx, "large_table", query, "", "")
    if err != nil || len(packets) == 0 {
        break
    }

    // Обработка пакетов...

    offset += pageSize
}
```

### 5. Транзакции для batch операций

```go
tx, err := adapter.BeginTx(ctx)
if err != nil {
    return err
}
defer tx.Rollback(ctx)

for _, pkt := range packets {
    if err := tx.ImportPacket(ctx, pkt, strategy); err != nil {
        return err
    }
}

return tx.Commit(ctx)
```

### 6. Production-ready конфигурация

```go
// Circuit Breaker + Retry + Audit
features := ProductionFeatures{
    CircuitBreaker: circuitBreaker,
    RetryManager:   retryer,
    AuditLogger:    logger,
}

err := features.ExecuteWithResilience(ctx, "export-users", func() error {
    return adapter.ExportTable(ctx, "users")
})
```

---

## Testing

### Unit Tests

```bash
# Все тесты
go test ./... -v

# Конкретный пакет
go test ./pkg/core/packet -v

# С покрытием
go test ./pkg/core/... -cover

# Бенчмарки
go test ./pkg/core/packet -bench=. -benchmem
```

### Integration Tests

```bash
# Требуют Docker с БД
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export POSTGRES_USER=tdtp_user
export POSTGRES_PASSWORD=tdtp_pass

go test ./tests/integration/... -v
```

### End-to-End Tests

```bash
# Полный цикл Export → Broker → Import
go test ./tests/e2e/... -v
```

---

## Дополнительные ресурсы

- **[SPECIFICATION.md](SPECIFICATION.md)** - Спецификация TDTP v1.0 & TDTQL
- **[USER_GUIDE.md](USER_GUIDE.md)** - Руководство пользователя CLI
- **[ROADMAP.md](../ROADMAP.md)** - Дорожная карта развития
- **GitHub:** https://github.com/ruslano69/tdtp-framework
- **Issues:** https://github.com/ruslano69/tdtp-framework/issues

---

*Последнее обновление: 17.11.2025*
