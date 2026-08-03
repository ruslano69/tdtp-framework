# MS SQL Server Adapter - TDTP Framework

MS SQL Server адаптер для двунаправленной интеграции с Microsoft SQL Server 2012+.

## Статус

✅ **Production Ready** (v1.0)

**Реализовано:**
- ✅ `types.go` - полный маппинг типов MS SQL Server ↔ TDTP
- ✅ `adapter.go` - подключение через github.com/microsoft/go-mssqldb
- ✅ `export.go` - экспорт таблиц с TDTQL оптимизацией
- ✅ `import.go` - импорт данных с IDENTITY_INSERT
- ✅ `integration_test.go` - полное интеграционное тестирование
- ✅ Transaction support - BEGIN/COMMIT/ROLLBACK
- ✅ Стратегии импорта - REPLACE, IGNORE, FAIL

---

## Требования

- **MS SQL Server:** 2012 или выше (поддержка DATE, DATETIME2, OFFSET/FETCH)
- **Driver:** `github.com/microsoft/go-mssqldb`
- **Go:** 1.21+

---

## Возможности

### Поддержка типов данных

**Стандартные типы:**
```
MS SQL Server       TDTP            Обратно
─────────────────────────────────────────────────
INT                 INTEGER         INT
BIGINT              INTEGER         BIGINT
SMALLINT            INTEGER         SMALLINT (subtype)
TINYINT             INTEGER         TINYINT (subtype)
DECIMAL(p,s)        DECIMAL         DECIMAL(p,s)
NUMERIC(p,s)        DECIMAL         NUMERIC(p,s)
NVARCHAR(n)         TEXT            NVARCHAR(n)
VARCHAR(n)          TEXT            VARCHAR(n)
NTEXT               TEXT            NVARCHAR(MAX)
TEXT                TEXT            VARCHAR(MAX)
BIT                 BOOLEAN         BIT
DATE                DATE            DATE
DATETIME2           TIMESTAMP       DATETIME2
DATETIME            TIMESTAMP       DATETIME (subtype: datetime)
VARBINARY(n)        BLOB            VARBINARY(n)
VARBINARY(MAX)      BLOB            VARBINARY(MAX)
```

**Специальные типы MS SQL Server (через subtype):**
```
MS SQL Server       TDTP                        Обратно
─────────────────────────────────────────────────────────────────
UNIQUEIDENTIFIER    TEXT(36) (subtype="uuid")   UNIQUEIDENTIFIER
MONEY               DECIMAL(19,4) (subtype="money") MONEY
SMALLMONEY          DECIMAL(10,4) (subtype="smallmoney") SMALLMONEY
XML                 TEXT (subtype="xml")        XML
FLOAT               REAL (subtype="float")      FLOAT
REAL                REAL (subtype="real")       REAL
```

### Особенности реализации

**IDENTITY_INSERT:**
- Автоматически включается при импорте таблиц с IDENTITY-колонками
- Позволяет вставлять явные значения в IDENTITY-поля
- Автоматически отключается после импорта

**Unicode поддержка:**
- По умолчанию используется NVARCHAR (Unicode)
- VARCHAR только если указан subtype
- Рекомендуется NVARCHAR для международных приложений

**Транзакции:**
- Полная поддержка transactions: BEGIN TRANSACTION, COMMIT, ROLLBACK
- Изоляция на уровне READ COMMITTED (по умолчанию)
- Автоматический rollback при ошибках

**TDTQL Оптимизация:**
- Фильтры TDTQL транслируются в native SQL WHERE
- Push-down execution на уровне БД
- Поддержка ORDER BY, LIMIT, OFFSET через SQL Server синтаксис

---

## Установка

```bash
go get github.com/queuebridge/tdtp/pkg/adapters/mssql
```

### Импорт

```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"  // Регистрация адаптера
)
```

---

## Использование

### Подключение

**Через Factory:**
```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
)

ctx := context.Background()

cfg := adapters.Config{
    Type: "mssql",
    DSN:  "sqlserver://tdtp_user:password@localhost:1433?database=MyDB",
}

adapter, err := adapters.New(ctx, cfg)
if err != nil {
    panic(err)
}
defer adapter.Close(ctx)
```

**Прямое создание:**
```go
import "github.com/queuebridge/tdtp/pkg/adapters/mssql"

adapter, err := mssql.NewAdapter(ctx,
    "sqlserver://sa:MyPassword123@localhost:1433?database=TestDB")
defer adapter.Close(ctx)
```

**DSN форматы:**

```go
// Стандартный формат
"sqlserver://username:password@host:port?database=dbname"

// С указанием instance
"sqlserver://user:pass@localhost:1433?database=MyDB&instance=SQLEXPRESS"

// Windows Authentication
"sqlserver://localhost?database=MyDB&trusted_connection=yes"

// Дополнительные параметры
"sqlserver://user:pass@localhost:1433?database=MyDB&connection+timeout=30&encrypt=true"
```

---

### Export

**Полный экспорт таблицы:**
```go
packets, err := adapter.ExportTable(ctx, "Users")
if err != nil {
    panic(err)
}

// Сохранение в файл
for i, pkt := range packets {
    filename := fmt.Sprintf("users_part_%d.xml", i+1)
    generator := packet.NewGenerator()
    generator.WriteToFile(pkt, filename)
}
```

**Экспорт с TDTQL фильтром:**
```go
import "github.com/queuebridge/tdtp/pkg/core/tdtql"

// SQL → TDTQL трансляция
translator := tdtql.NewTranslator()
query, err := translator.TranslateSQL(
    "SELECT * FROM Users WHERE is_active = 1 AND balance > 1000 ORDER BY created_at DESC LIMIT 100"
)

// Экспорт с оптимизацией на уровне БД
packets, err := adapter.ExportTableWithQuery(ctx, "Users", query)
```

**Экспорт специальных типов:**
```go
// Таблица с UNIQUEIDENTIFIER и XML
/*
CREATE TABLE Documents (
    id UNIQUEIDENTIFIER PRIMARY KEY,
    content XML,
    metadata NVARCHAR(MAX),
    price MONEY
)
*/

packets, err := adapter.ExportTable(ctx, "Documents")

// В TDTP пакете:
// id: TEXT(36) с subtype="uuid"
// content: TEXT с subtype="xml"
// price: DECIMAL(19,4) с subtype="money"
```

---

### Import

**Стратегия REPLACE (полная замена):**
```go
import "github.com/queuebridge/tdtp/pkg/adapters"

// Удаляет все записи, затем вставляет новые
err = adapter.ImportPacket(ctx, packet, adapters.StrategyReplace)
```

**Стратегия IGNORE (пропуск дубликатов):**
```go
// Игнорирует записи с существующими PRIMARY KEY
err = adapter.ImportPacket(ctx, packet, adapters.StrategyIgnore)
```

**Стратегия FAIL (ошибка при дубликатах):**
```go
// Выдает ошибку при дублировании PRIMARY KEY
err = adapter.ImportPacket(ctx, packet, adapters.StrategyFail)
```

**Импорт с IDENTITY-полями:**
```go
// IDENTITY_INSERT автоматически включается/выключается
/*
CREATE TABLE Users (
    id INT IDENTITY(1,1) PRIMARY KEY,
    username NVARCHAR(100)
)
*/

// Импорт с явными значениями id
err = adapter.ImportPacket(ctx, packet, adapters.StrategyReplace)
// Фреймворк автоматически:
// 1. SET IDENTITY_INSERT Users ON
// 2. INSERT INTO Users (id, username) VALUES (...)
// 3. SET IDENTITY_INSERT Users OFF
```

**Batch импорт:**
```go
// Импорт множества пакетов
for _, pkt := range packets {
    err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
    if err != nil {
        return err
    }
}
```

---

### Транзакции

```go
// Начало транзакции
tx, err := adapter.BeginTx(ctx)
if err != nil {
    panic(err)
}

// Импорт в транзакции
err = tx.ImportPacket(ctx, packet1, adapters.StrategyReplace)
if err != nil {
    tx.Rollback(ctx)
    panic(err)
}

err = tx.ImportPacket(ctx, packet2, adapters.StrategyReplace)
if err != nil {
    tx.Rollback(ctx)
    panic(err)
}

// Commit
err = tx.Commit(ctx)
if err != nil {
    panic(err)
}
```

---

### Список таблиц

```go
tables, err := adapter.ListTables(ctx)
if err != nil {
    panic(err)
}

for _, table := range tables {
    fmt.Println(table)
}
// Output:
// Users
// Orders
// Products
```

---

## Миграционные сценарии

### SQLite → MS SQL Server

```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
)

ctx := context.Background()

// Source: SQLite
sourceCfg := adapters.Config{
    Type: "sqlite",
    DSN:  "app.db",
}
source, _ := adapters.New(ctx, sourceCfg)
defer source.Close(ctx)

// Target: MS SQL Server
targetCfg := adapters.Config{
    Type: "mssql",
    DSN:  "sqlserver://sa:Pass123@localhost:1433?database=Production",
}
target, _ := adapters.New(ctx, targetCfg)
defer target.Close(ctx)

// Миграция
packets, _ := source.ExportTable(ctx, "Users")
target.ImportPacket(ctx, packets[0], adapters.StrategyReplace)

// Автоматическая конвертация типов:
// SQLite INTEGER → MS SQL INT
// SQLite TEXT → MS SQL NVARCHAR
// SQLite REAL → MS SQL FLOAT
```

### PostgreSQL → MS SQL Server

```go
// Source: PostgreSQL
sourceCfg := adapters.Config{
    Type: "postgres",
    DSN:  "postgres://user:pass@localhost/sourcedb",
}
source, _ := adapters.New(ctx, sourceCfg)

// Target: MS SQL Server
targetCfg := adapters.Config{
    Type: "mssql",
    DSN:  "sqlserver://sa:Pass@localhost:1433?database=targetdb",
}
target, _ := adapters.New(ctx, targetCfg)

packets, _ := source.ExportTable(ctx, "customers")
target.ImportPacket(ctx, packets[0], adapters.StrategyReplace)

// Специальные типы конвертируются:
// PostgreSQL UUID → MS SQL UNIQUEIDENTIFIER
// PostgreSQL JSONB → MS SQL NVARCHAR (JSON хранится как текст)
// PostgreSQL TIMESTAMPTZ → MS SQL DATETIME2
```

### MS SQL Server → MS SQL Server (репликация)

```go
// Source: Production
source, _ := mssql.NewAdapter(ctx,
    "sqlserver://reader:pass@prod-server:1433?database=Production")

// Target: Reporting
target, _ := mssql.NewAdapter(ctx,
    "sqlserver://writer:pass@report-server:1433?database=Reporting")

// Инкрементальная репликация
translator := tdtql.NewTranslator()
query, _ := translator.TranslateSQL(
    "SELECT * FROM Orders WHERE modified_at > '2025-12-01'")

packets, _ := source.ExportTableWithQuery(ctx, "Orders", query)
target.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

---

## Примеры

### Пример 1: Простой экспорт/импорт

```go
package main

import (
    "context"
    "fmt"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
)

func main() {
    ctx := context.Background()

    cfg := adapters.Config{
        Type: "mssql",
        DSN:  "sqlserver://sa:MyPassword@localhost:1433?database=TestDB",
    }

    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Export
    packets, err := adapter.ExportTable(ctx, "Users")
    if err != nil {
        panic(err)
    }

    fmt.Printf("Exported %d packets\n", len(packets))

    // Import
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
    if err != nil {
        panic(err)
    }

    fmt.Println("Import successful!")
}
```

### Пример 2: Экспорт с фильтром через message broker

```go
package main

import (
    "context"

    "github.com/queuebridge/tdtp/pkg/adapters"
    "github.com/queuebridge/tdtp/pkg/brokers"
    "github.com/queuebridge/tdtp/pkg/core/tdtql"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
)

func main() {
    ctx := context.Background()

    // MS SQL Server adapter
    cfg := adapters.Config{
        Type: "mssql",
        DSN:  "sqlserver://user:pass@localhost:1433?database=Sales",
    }
    adapter, _ := adapters.New(ctx, cfg)
    defer adapter.Close(ctx)

    // RabbitMQ broker
    broker := brokers.NewRabbitMQ("amqp://guest:guest@localhost:5672/")
    broker.Connect()
    defer broker.Close()

    // TDTQL фильтр
    translator := tdtql.NewTranslator()
    query, _ := translator.TranslateSQL(
        "SELECT * FROM Orders WHERE total > 1000 AND status = 'completed'")

    // Export с фильтром
    packets, _ := adapter.ExportTableWithQuery(ctx, "Orders", query)

    // Publish в RabbitMQ
    for _, pkt := range packets {
        broker.Publish("sales_queue", pkt)
    }
}
```

### Пример 3: Работа с транзакциями

```go
package main

import (
    "context"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
)

func main() {
    ctx := context.Background()

    cfg := adapters.Config{
        Type: "mssql",
        DSN:  "sqlserver://sa:Pass@localhost:1433?database=Shop",
    }
    adapter, _ := adapters.New(ctx, cfg)
    defer adapter.Close(ctx)

    // Начинаем транзакцию
    tx, err := adapter.BeginTx(ctx)
    if err != nil {
        panic(err)
    }

    // Импортируем несколько таблиц в одной транзакции
    usersPacket, _ := /* load packet */
    ordersPacket, _ := /* load packet */

    err = tx.ImportPacket(ctx, usersPacket, adapters.StrategyReplace)
    if err != nil {
        tx.Rollback(ctx)
        panic(err)
    }

    err = tx.ImportPacket(ctx, ordersPacket, adapters.StrategyReplace)
    if err != nil {
        tx.Rollback(ctx)
        panic(err)
    }

    // Commit транзакции
    err = tx.Commit(ctx)
    if err != nil {
        panic(err)
    }
}
```

---

## Производительность

### Benchmarks

Тестирование на MS SQL Server 2019, Windows Server 2019, SSD:

| Операция | Производительность |
|----------|-------------------|
| Export (1000 rows) | ~50ms |
| Export (10,000 rows) | ~300ms |
| Export (100,000 rows) | ~2.5s |
| Import REPLACE (1000 rows) | ~80ms |
| Import IGNORE (1000 rows) | ~120ms |
| TDTQL filter (10K rows) | ~100ms |

### Оптимизации

✅ **Prepared statements** - все запросы используют параметризацию
✅ **Batch inserts** - группировка INSERT для ускорения
✅ **IDENTITY_INSERT** - автоматическое управление
✅ **Push-down execution** - TDTQL транслируется в SQL WHERE/ORDER BY
✅ **Connection pooling** - реиспользование соединений

### Рекомендации

1. **Используйте NVARCHAR** вместо VARCHAR для Unicode данных
2. **DATETIME2** предпочтительнее DATETIME (выше точность)
3. **Batch operations** для больших объемов (>1000 rows)
4. **Транзакции** для атомарности при импорте связанных таблиц
5. **TDTQL фильтры** для экспорта подмножества данных

---

## Troubleshooting

### Ошибка: "Login failed for user"
```
Решение: Проверьте credentials в DSN и права пользователя в SQL Server
```

### Ошибка: "Cannot insert explicit value for identity column"
```
Причина: IDENTITY_INSERT не включен
Решение: Фреймворк делает это автоматически - проверьте права пользователя
```

### Ошибка: "Violation of PRIMARY KEY constraint"
```
Решение: Используйте StrategyIgnore или StrategyReplace вместо StrategyFail
```

### Медленный импорт больших таблиц
```
Решение 1: Используйте транзакции для batch импорта
Решение 2: Временно отключите индексы перед импортом
Решение 3: Увеличьте размер batch в настройках адаптера
```

---

## Совместимость

- **MS SQL Server:** 2012, 2014, 2016, 2017, 2019, 2022
- **Azure SQL Database:** Полная поддержка
- **Azure SQL Managed Instance:** Полная поддержка
- **SQL Server Express:** Полная поддержка

---

## См. также

- **[Adapter Interface](../adapter.go)** - унифицированный интерфейс
- **[TDTP Specification](../../../docs/SPECIFICATION.md)** - спецификация протокола
- **[PostgreSQL Adapter](../postgres/README.md)** - PostgreSQL интеграция
- **[MySQL Adapter](../mysql/README.md)** - MySQL интеграция
- **[SQLite Adapter](../sqlite/README.md)** - SQLite интеграция
- **[examples/02-rabbitmq-mssql](../../../examples/02-rabbitmq-mssql/)** - полный пример

---

## Лицензия

MIT

---

**Версия:** 1.0
**Последнее обновление:** 08.12.2025
