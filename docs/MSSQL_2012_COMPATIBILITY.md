# MS SQL Server 2012+ Compatibility Guide

**Дата:** 16.11.2025
**Версия:** v1.2
**Цель:** Обеспечить обратную совместимость с MS SQL Server 2012 и выше

## 📋 Требования

**Минимальная версия:** MS SQL Server 2012 (11.x)
**Поддерживаемые версии:**
- ✅ SQL Server 2012 (11.x)
- ✅ SQL Server 2014 (12.x)
- ✅ SQL Server 2016 (13.x)
- ✅ SQL Server 2017 (14.x)
- ✅ SQL Server 2019 (15.x)
- ✅ SQL Server 2022 (16.x)

## 🔍 Ключевые ограничения SQL Server 2012

### 1. Типы данных

#### ✅ Поддерживаемые типы (SQL Server 2012+)

**Числовые:**
```sql
-- Целочисленные
TINYINT, SMALLINT, INT, BIGINT

-- Точные числа
DECIMAL(p,s), NUMERIC(p,s)
MONEY, SMALLMONEY

-- Приближенные числа
FLOAT, REAL
```

**Строковые:**
```sql
-- Односбайтные
CHAR(n), VARCHAR(n), VARCHAR(MAX)
TEXT (deprecated, но работает)

-- Unicode
NCHAR(n), NVARCHAR(n), NVARCHAR(MAX)
NTEXT (deprecated, но работает)
```

**Дата/Время:**
```sql
-- SQL Server 2008+
DATE                    -- Только дата
TIME                    -- Только время
DATETIME2               -- Высокая точность (100ns)
DATETIMEOFFSET         -- С timezone

-- Legacy (SQL Server 2000+)
DATETIME                -- Точность 3.33ms
SMALLDATETIME          -- Точность 1 минута
```

**Бинарные:**
```sql
BINARY(n), VARBINARY(n), VARBINARY(MAX)
IMAGE (deprecated, но работает)
```

**Другие:**
```sql
BIT                     -- Boolean (0/1)
UNIQUEIDENTIFIER       -- GUID
XML                    -- XML документы
```

#### ❌ НЕ поддерживаемые типы (появились позже)

**JSON (SQL Server 2016+):**
```sql
-- ❌ Не работает в SQL Server 2012
SELECT * FROM table WHERE JSON_VALUE(data, '$.name') = 'value'

-- ✅ Решение: хранить как NVARCHAR(MAX)
-- Парсинг JSON на стороне приложения
```

**GEOGRAPHY/GEOMETRY (CLR types):**
```sql
-- ⚠️ Требуют дополнительной настройки
-- Можно хранить как VARBINARY или WKT в NVARCHAR
```

### 2. SQL Синтаксис

#### ✅ Поддерживаемые конструкции

**OFFSET/FETCH (SQL Server 2012+):**
```sql
-- ✅ Работает в SQL Server 2012+
SELECT * FROM Users
ORDER BY ID
OFFSET 10 ROWS
FETCH NEXT 20 ROWS ONLY
```

**MERGE (SQL Server 2008+):**
```sql
-- ✅ Работает для UPSERT операций
MERGE INTO target AS T
USING source AS S ON T.ID = S.ID
WHEN MATCHED THEN UPDATE SET T.Name = S.Name
WHEN NOT MATCHED THEN INSERT (ID, Name) VALUES (S.ID, S.Name);
```

**TRY_CONVERT (SQL Server 2012+):**
```sql
-- ✅ Работает
SELECT TRY_CONVERT(INT, '123') -- Вернет NULL если не удается
```

**FORMAT (SQL Server 2012+):**
```sql
-- ✅ Работает
SELECT FORMAT(GETDATE(), 'yyyy-MM-dd')
```

**IIF (SQL Server 2012+):**
```sql
-- ✅ Работает (inline IF)
SELECT IIF(Age >= 18, 'Adult', 'Minor') AS Status
```

#### ❌ НЕ поддерживаемые конструкции

**STRING_SPLIT (SQL Server 2016+):**
```sql
-- ❌ Не работает в SQL Server 2012
SELECT value FROM STRING_SPLIT('a,b,c', ',')

-- ✅ Решение: использовать XML
SELECT Split.a.value('.', 'VARCHAR(100)') AS value
FROM (
    SELECT CAST('<M>' + REPLACE('a,b,c', ',', '</M><M>') + '</M>' AS XML) AS Data
) AS A
CROSS APPLY Data.nodes('/M') AS Split(a)
```

**JSON функции (SQL Server 2016+):**
```sql
-- ❌ Не работает
SELECT JSON_VALUE(data, '$.name')
SELECT JSON_QUERY(data, '$.items')
FOR JSON AUTO

-- ✅ Решение: парсить JSON в приложении
```

**TRIM/CONCAT_WS (SQL Server 2017+):**
```sql
-- ❌ Не работает
SELECT TRIM(name)
SELECT CONCAT_WS(',', col1, col2, col3)

-- ✅ Решение:
SELECT LTRIM(RTRIM(name))
SELECT ISNULL(col1, '') + ',' + ISNULL(col2, '') + ',' + ISNULL(col3, '')
```

**STRING_AGG (SQL Server 2017+):**
```sql
-- ❌ Не работает
SELECT STRING_AGG(name, ',')

-- ✅ Решение: использовать FOR XML PATH
SELECT STUFF((
    SELECT ',' + name
    FROM table
    FOR XML PATH(''), TYPE
).value('.', 'NVARCHAR(MAX)'), 1, 1, '')
```

### 3. Bulk Operations

#### ✅ BULK INSERT (SQL Server 2005+)
```sql
-- ✅ Работает
BULK INSERT table
FROM 'C:\data.csv'
WITH (
    FIELDTERMINATOR = ',',
    ROWTERMINATOR = '\n',
    FIRSTROW = 2
)
```

#### ⚠️ Ограничения
- Требует file system доступ на сервере
- Не работает с Azure SQL Database

#### ✅ Альтернатива: Table-Valued Parameters
```sql
-- Создаем User-Defined Table Type
CREATE TYPE dbo.BulkInsertType AS TABLE (
    ID INT,
    Name NVARCHAR(100),
    Value DECIMAL(18,2)
)

-- Используем в хранимой процедуре
CREATE PROCEDURE dbo.BulkInsertData
    @Data dbo.BulkInsertType READONLY
AS
BEGIN
    INSERT INTO TargetTable (ID, Name, Value)
    SELECT ID, Name, Value FROM @Data
END
```

## 🎯 Рекомендации для TDTP Adapter

### 1. Type Mapping Strategy

**Консервативный подход:**
```go
// Маппинг типов с учетом SQL Server 2012
var typeMapping = map[string]string{
    // Числовые
    "TINYINT":        "INTEGER",
    "SMALLINT":       "INTEGER",
    "INT":            "INTEGER",
    "BIGINT":         "INTEGER",
    "DECIMAL":        "DECIMAL",
    "NUMERIC":        "DECIMAL",
    "MONEY":          "DECIMAL", // → DECIMAL(19,4)
    "SMALLMONEY":     "DECIMAL", // → DECIMAL(10,4)
    "FLOAT":          "REAL",
    "REAL":           "REAL",

    // Строковые
    "CHAR":           "TEXT",
    "VARCHAR":        "TEXT",
    "NCHAR":          "TEXT",
    "NVARCHAR":       "TEXT",
    "TEXT":           "TEXT",    // Legacy
    "NTEXT":          "TEXT",    // Legacy

    // Дата/Время
    "DATE":           "DATE",
    "TIME":           "TIME",
    "DATETIME":       "TIMESTAMP",
    "DATETIME2":      "TIMESTAMP",
    "SMALLDATETIME":  "TIMESTAMP",
    "DATETIMEOFFSET": "TIMESTAMP", // Сохраняем с timezone

    // Другие
    "BIT":            "BOOLEAN",
    "UNIQUEIDENTIFIER": "TEXT",   // UUID as string
    "XML":            "TEXT",      // XML as string
    "VARBINARY":      "BLOB",
    "BINARY":         "BLOB",
    "IMAGE":          "BLOB",      // Legacy
}
```

### 2. Query Generation

**Пагинация - используем OFFSET/FETCH:**
```go
func (a *Adapter) generatePaginationQuery(table string, limit, offset int) string {
    // SQL Server 2012+ поддерживает OFFSET/FETCH
    return fmt.Sprintf(`
        SELECT * FROM %s
        ORDER BY (SELECT NULL)  -- Требуется ORDER BY
        OFFSET %d ROWS
        FETCH NEXT %d ROWS ONLY
    `, table, offset, limit)
}
```

**UPSERT - используем MERGE:**
```go
func (a *Adapter) generateUpsertQuery(table string, schema packet.Schema) string {
    // MERGE работает с SQL Server 2008+
    keys := getKeyFields(schema)

    return fmt.Sprintf(`
        MERGE INTO %s AS target
        USING (VALUES %s) AS source (%s)
        ON %s
        WHEN MATCHED THEN
            UPDATE SET %s
        WHEN NOT MATCHED THEN
            INSERT (%s) VALUES (%s)
    `, table, valuesPlaceholder, columnList,
       joinCondition, updateSet, columnList, valuesList)
}
```

### 3. Feature Detection

**Определение версии сервера:**
```go
func (a *Adapter) detectServerVersion(ctx context.Context) (int, error) {
    var version string
    err := a.db.QueryRowContext(ctx, `
        SELECT SERVERPROPERTY('ProductVersion') AS Version
    `).Scan(&version)

    // Парсим версию: "11.0.2100.60" → 11 (SQL Server 2012)
    major := parseVersionMajor(version)
    return major, nil
}

func (a *Adapter) supportsJSON() bool {
    return a.serverVersion >= 13 // SQL Server 2016+
}

func (a *Adapter) supportsStringSplit() bool {
    return a.serverVersion >= 13 // SQL Server 2016+
}
```

**Использование:**
```go
func (a *Adapter) ExportTableWithQuery(ctx context.Context, table string, query *tdtql.Query) {
    if a.supportsJSON() && hasJSONFilters(query) {
        // Используем JSON функции
        return a.exportWithJSONFilters(ctx, table, query)
    } else {
        // Fallback на обычные фильтры
        return a.exportWithStandardFilters(ctx, table, query)
    }
}
```

### 4. Connection String

**SQL Server 2012 compatible DSN:**
```go
// SQL Authentication
dsn := fmt.Sprintf(
    "server=%s;port=%d;database=%s;user id=%s;password=%s;encrypt=disable",
    host, port, database, user, password,
)

// Windows Authentication
dsn := fmt.Sprintf(
    "server=%s;port=%d;database=%s;trusted_connection=yes;encrypt=disable",
    host, port, database,
)

// Параметры совместимости
dsn += ";TrustServerCertificate=true"  // Для старых сертификатов
```

### 5. Bulk Import Strategy

**Table-Valued Parameters (рекомендуется):**
```go
func (a *Adapter) BulkImport(ctx context.Context, table string, rows [][]string) error {
    // 1. Создаем временный тип (один раз)
    a.createTempTableType(ctx, table, schema)

    // 2. Вызываем процедуру с TVP
    _, err := a.db.ExecContext(ctx, "EXEC BulkInsertProc @Data",
        sql.Named("Data", rows))

    return err
}
```

**Альтернатива - Batch INSERT:**
```go
func (a *Adapter) BatchInsert(ctx context.Context, table string, rows [][]string) error {
    // SQL Server 2012+ поддерживает INSERT с множественными VALUES
    // INSERT INTO table VALUES (1,'a'), (2,'b'), (3,'c')

    const batchSize = 1000 // Ограничение SQL Server

    for i := 0; i < len(rows); i += batchSize {
        batch := rows[i:min(i+batchSize, len(rows))]

        values := buildValuesClause(batch)
        query := fmt.Sprintf("INSERT INTO %s VALUES %s", table, values)

        _, err := a.db.ExecContext(ctx, query)
        if err != nil {
            return err
        }
    }

    return nil
}
```

## 🧪 Testing Strategy

### Test Matrix

**Тестируемые версии:**
```yaml
matrix:
  mssql_version:
    - "2012"     # Minimum supported
    - "2014"     # LTS
    - "2017"     # Popular in production
    - "2019"     # Modern features
    - "2022"     # Latest
```

### Docker Compose для тестирования

```yaml
version: '3.8'

services:
  # SQL Server 2019 (минимум для Docker)
  mssql-2019:
    image: mcr.microsoft.com/mssql/server:2019-latest
    environment:
      ACCEPT_EULA: Y
      SA_PASSWORD: YourPassword123!
      MSSQL_PID: Developer
    ports:
      - "1433:1433"
    volumes:
      - mssql2019_data:/var/opt/mssql

  # SQL Server 2022
  mssql-2022:
    image: mcr.microsoft.com/mssql/server:2022-latest
    environment:
      ACCEPT_EULA: Y
      SA_PASSWORD: YourPassword123!
    ports:
      - "1434:1433"
```

**Note:** SQL Server 2012-2017 официально не поддерживаются в Docker.
Для тестирования на реальных версиях используйте Windows VM или Azure SQL.

### Compatibility Tests

```go
func TestMSSQL2012Compatibility(t *testing.T) {
    tests := []struct {
        name        string
        minVersion  int
        query       string
        shouldWork  bool
    }{
        {
            name:       "OFFSET/FETCH pagination",
            minVersion: 11, // SQL Server 2012+
            query:      "SELECT * FROM Users ORDER BY ID OFFSET 10 ROWS FETCH NEXT 20 ROWS ONLY",
            shouldWork: true,
        },
        {
            name:       "JSON functions",
            minVersion: 13, // SQL Server 2016+
            query:      "SELECT JSON_VALUE(data, '$.name') FROM Users",
            shouldWork: false, // Не работает в 2012
        },
        {
            name:       "STRING_SPLIT",
            minVersion: 13,
            query:      "SELECT value FROM STRING_SPLIT('a,b,c', ',')",
            shouldWork: false,
        },
        {
            name:       "MERGE statement",
            minVersion: 10, // SQL Server 2008+
            query:      "MERGE INTO target...",
            shouldWork: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if serverVersion >= tt.minVersion {
                assert.Equal(t, tt.shouldWork, canExecuteQuery(tt.query))
            }
        })
    }
}
```

## 📊 Type Conversion Examples

### MS SQL → TDTP

```go
func convertMSSQLTypeToTDTP(sqlType string, precision, scale int) string {
    switch strings.ToUpper(sqlType) {
    case "INT", "BIGINT", "SMALLINT", "TINYINT":
        return "INTEGER"

    case "DECIMAL", "NUMERIC":
        return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)

    case "MONEY":
        return "DECIMAL(19,4)"

    case "SMALLMONEY":
        return "DECIMAL(10,4)"

    case "FLOAT", "REAL":
        return "REAL"

    case "CHAR", "VARCHAR", "NCHAR", "NVARCHAR", "TEXT", "NTEXT":
        return "TEXT"

    case "DATE":
        return "DATE"

    case "TIME":
        return "TIME"

    case "DATETIME", "DATETIME2", "SMALLDATETIME":
        return "TIMESTAMP"

    case "DATETIMEOFFSET":
        // Сохраняем с timezone info
        return "TIMESTAMP"

    case "BIT":
        return "BOOLEAN"

    case "UNIQUEIDENTIFIER":
        return "TEXT" // UUID as string (36 chars)

    case "XML":
        return "TEXT" // XML as string

    case "VARBINARY", "BINARY", "IMAGE":
        return "BLOB"

    default:
        return "TEXT" // Fallback
    }
}
```

### TDTP → MS SQL

```go
func convertTDTPTypeToMSSQL(tdtpType string, length, precision, scale int) string {
    switch tdtpType {
    case "INTEGER":
        return "BIGINT" // Безопасный выбор

    case "DECIMAL":
        if precision > 0 && scale > 0 {
            return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)
        }
        return "DECIMAL(18,2)" // Default

    case "REAL":
        return "FLOAT"

    case "TEXT":
        if length > 0 && length <= 4000 {
            return fmt.Sprintf("NVARCHAR(%d)", length)
        }
        return "NVARCHAR(MAX)"

    case "BOOLEAN":
        return "BIT"

    case "DATE":
        return "DATE"

    case "TIME":
        return "TIME"

    case "TIMESTAMP":
        return "DATETIME2" // Высокая точность

    case "BLOB":
        return "VARBINARY(MAX)"

    default:
        return "NVARCHAR(MAX)"
    }
}
```

## ⚠️ Known Issues & Workarounds

### 1. TEXT/NTEXT deprecation

**Проблема:** TEXT/NTEXT deprecated с SQL Server 2005, но все еще работают.

**Решение:**
```go
// При создании таблиц используем VARCHAR(MAX)/NVARCHAR(MAX)
func (a *Adapter) createTable(schema packet.Schema) {
    // Используем современные типы
    for _, field := range schema.Fields {
        if field.Type == "TEXT" {
            sqlType = "NVARCHAR(MAX)" // Вместо NTEXT
        }
    }
}

// При чтении существующих таблиц поддерживаем оба
func (a *Adapter) readSchema(table string) {
    // Обрабатываем как TEXT, так и NTEXT
}
```

### 2. DATETIME precision

**Проблема:** DATETIME имеет точность 3.33ms, может приводить к округлению.

**Решение:**
```go
// Используем DATETIME2 для новых таблиц
func getTimestampType(serverVersion int) string {
    if serverVersion >= 10 { // SQL Server 2008+
        return "DATETIME2(7)" // 100ns precision
    }
    return "DATETIME" // Fallback
}
```

### 3. Unicode handling

**Проблема:** VARCHAR vs NVARCHAR для Unicode.

**Решение:**
```go
// Всегда используем NVARCHAR для TEXT типов
func getTextType(length int) string {
    if length > 0 && length <= 4000 {
        return fmt.Sprintf("NVARCHAR(%d)", length)
    }
    return "NVARCHAR(MAX)"
}
```

### 4. Identity columns

**Проблема:** IDENTITY columns требуют SET IDENTITY_INSERT ON для явной вставки.

**Решение:**
```go
func (a *Adapter) ImportWithIdentity(ctx context.Context, table string, data [][]string) error {
    // Проверяем наличие IDENTITY
    hasIdentity := a.tableHasIdentity(ctx, table)

    if hasIdentity {
        // Включаем IDENTITY_INSERT
        a.exec(ctx, fmt.Sprintf("SET IDENTITY_INSERT %s ON", table))
        defer a.exec(ctx, fmt.Sprintf("SET IDENTITY_INSERT %s OFF", table))
    }

    // Импортируем данные
    return a.insertData(ctx, table, data)
}
```

## 📚 References

**Official Documentation:**
- [SQL Server 2012 Features](https://docs.microsoft.com/sql/sql-server/what-s-new-in-sql-server-2012)
- [Data Types](https://docs.microsoft.com/sql/t-sql/data-types/data-types-transact-sql)
- [MERGE Statement](https://docs.microsoft.com/sql/t-sql/statements/merge-transact-sql)
- [OFFSET/FETCH](https://docs.microsoft.com/sql/t-sql/queries/select-order-by-clause-transact-sql)

**Go Driver Documentation:**
- [go-mssqldb](https://github.com/denisenkom/go-mssqldb)
- [microsoft/go-mssqldb](https://github.com/microsoft/go-mssqldb)

---

**Версия документа:** 1.0
**Последнее обновление:** 16.11.2025
**Статус:** Draft - Требования совместимости с SQL Server 2012+
