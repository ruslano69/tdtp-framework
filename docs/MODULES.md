# TDTP Framework - Описание модулей

Полное описание всех модулей TDTP Framework с API, примерами использования и лучшими практиками.

**Версия:** 1.2
**Дата:** 16.11.2025

---

## Содержание

1. [Core Modules](#core-modules)
   - [Packet Module](#packet-module)
   - [Schema Module](#schema-module)
   - [TDTQL Module](#tdtql-module)
2. [Adapters](#adapters)
   - [Universal Interface](#universal-interface)
   - [SQLite Adapter](#sqlite-adapter)
   - [PostgreSQL Adapter](#postgresql-adapter)
   - [MS SQL Server Adapter](#ms-sql-server-adapter)
3. [Brokers](#brokers)
   - [RabbitMQ Broker](#rabbitmq-broker)
   - [MSMQ Broker](#msmq-broker)
4. [CLI](#cli)

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

// Schema - схема таблицы
type Schema struct {
    Fields []Field // Поля таблицы
}

// Field - описание поля
type Field struct {
    Name      string // Имя поля
    Type      string // INTEGER | TEXT | DECIMAL | ...
    Length    int    // Длина (для TEXT)
    Precision int    // Точность (для DECIMAL)
    Scale     int    // Масштаб (для DECIMAL)
    Timezone  string // Часовой пояс (для TIMESTAMP)
    Key       bool   // Первичный ключ
    Subtype   string // Подтип (uuid, jsonb, ...)
}
```

#### API Parser

```go
import "github.com/queuebridge/tdtp/pkg/core/packet"

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
import "github.com/queuebridge/tdtp/pkg/core/packet"

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

#### Другие типы пакетов

**Request (запрос данных):**
```go
query := packet.NewQuery()
query.Filters = &packet.Filters{
    And: &packet.LogicalGroup{
        Filters: []packet.Filter{
            {Field: "balance", Operator: "gte", Value: "1000"},
        },
    },
}

pkt, err := generator.GenerateRequest("users", query, "ClientApp", "ServerDB")
```

**Response (ответ на запрос):**
```go
queryContext := &packet.QueryContext{
    OriginalQuery: query,
    ExecutionResults: packet.ExecutionResults{
        TotalRecordsInTable:   1000,
        RecordsAfterFilters:   50,
        RecordsReturned:       50,
        MoreDataAvailable:     false,
    },
}

packets, err := generator.GenerateResponse(
    "users",
    "REQ-2025-xyz123",  // InReplyTo
    schema,
    rows,
    queryContext,
    "ServerDB",
    "ClientApp",
)
```

---

### Schema Module

**Расположение:** `pkg/core/schema/`

**Назначение:** Валидация типов данных, конвертация значений, построение схем.

#### Основные компоненты

##### Builder

Построение схем программным способом.

```go
import "github.com/queuebridge/tdtp/pkg/core/schema"

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

##### Converter

Конвертация строковых значений в типизированные и обратно.

```go
import "github.com/queuebridge/tdtp/pkg/core/schema"

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

##### Validator

Валидация данных по схеме.

```go
import "github.com/queuebridge/tdtp/pkg/core/schema"

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

#### Типы данных

```go
const (
    TypeInteger   DataType = "INTEGER"
    TypeReal      DataType = "REAL"
    TypeDecimal   DataType = "DECIMAL"
    TypeText      DataType = "TEXT"
    TypeBlob      DataType = "BLOB"
    TypeBoolean   DataType = "BOOLEAN"
    TypeDate      DataType = "DATE"
    TypeTime      DataType = "TIME"
    TypeTimestamp DataType = "TIMESTAMP"
)
```

---

### TDTQL Module

**Расположение:** `pkg/core/tdtql/`

**Назначение:** Трансляция SQL → TDTQL, выполнение запросов in-memory, генерация SQL.

#### Компоненты

##### Translator (SQL → TDTQL)

```go
import "github.com/queuebridge/tdtp/pkg/core/tdtql"

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

##### Executor (in-memory filtering)

```go
import "github.com/queuebridge/tdtp/pkg/core/tdtql"

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
fmt.Printf("Records after filters: %d\n", result.QueryContext.ExecutionResults.RecordsAfterFilters)

// result.FilteredRows содержит отфильтрованные данные
for _, row := range result.FilteredRows {
    fmt.Println(row)
}
```

##### SQL Generator (TDTQL → SQL)

```go
import "github.com/queuebridge/tdtp/pkg/core/tdtql"

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

## Adapters

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
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"   // Регистрация
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres" // Регистрация
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"    // Регистрация
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
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
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
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
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

### MS SQL Server Adapter

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
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
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

## Brokers

### RabbitMQ Broker

**Расположение:** `pkg/brokers/rabbitmq.go`

**Особенности:**
- AMQP 0.9.1 протокол
- Manual ACK для надежной доставки
- Queue parameters (durable, auto_delete, exclusive)
- Connection pooling

**Пример использования:**

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

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

### MSMQ Broker

**Расположение:** `pkg/brokers/msmq.go`

**Особенности:**
- Работает только на Windows
- Поддержка локальных и сетевых очередей
- Transactional queues

**Пример использования:**

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

config := brokers.BrokerConfig{
    Type:  "msmq",
    Queue: ".\\private$\\tdtp_queue", // Локальная очередь
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
```

---

## CLI

**Расположение:** `cmd/tdtpcli/`

**Компоненты:**
- `main.go` - основная логика CLI
- `config.go` - работа с YAML конфигурацией

**Подробная документация:** См. [USER_GUIDE.md](USER_GUIDE.md)

**Основные функции:**

```go
// Чтение конфигурации
config, err := readConfig("config.yaml")

// Создание адаптера
adapter, err := createAdapter(config)

// Export
packets, err := adapter.ExportTable(ctx, tableName)

// Import
pkt, err := parsePacket(filename)
err = adapter.ImportPacket(ctx, pkt, strategy)

// Broker operations
broker, err := createBroker(config)
err = broker.PublishPackets(packets)
packets, err = broker.ConsumePackets()
```

---

## Лучшие практики

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

---

## Дополнительные ресурсы

- **[SPECIFICATION.md](SPECIFICATION.md)** - Спецификация TDTP v1.0
- **[USER_GUIDE.md](USER_GUIDE.md)** - Руководство пользователя tdtpcli
- **[PACKET_MODULE.md](PACKET_MODULE.md)** - Детальная документация Packet
- **[SCHEMA_MODULE.md](SCHEMA_MODULE.md)** - Детальная документация Schema
- **[TDTQL_TRANSLATOR.md](TDTQL_TRANSLATOR.md)** - Детальная документация TDTQL

---

*Последнее обновление: 16.11.2025*
