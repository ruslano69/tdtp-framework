# MS SQL Development vs Production Environment

**Дата:** 16.11.2025
**Критично:** Различия между dev и production окружениями

## 🎯 Ваше окружение

### Development (Локально)
- **Платформа:** Docker контейнер
- **Версия:** SQL Server 2019 или 2022 (минимальные для Docker)
- **Доступные функции:** Все современные SQL Server функции

### Production
- **Версия:** SQL Server 2012
- **Ограничения:** Нет JSON, STRING_SPLIT, и других функций появившихся после 2012

## ⚠️ КРИТИЧНАЯ ПРОБЛЕМА

**Код, работающий в Docker, может НЕ работать в production!**

### Пример проблемы:

```go
// ❌ ОПАСНО: Работает в Docker (SQL Server 2019+)
// ❌ НЕ РАБОТАЕТ в production (SQL Server 2012)
func exportWithJSON(table string) {
    query := fmt.Sprintf(`
        SELECT
            ID,
            JSON_VALUE(Data, '$.name') AS Name
        FROM %s
    `, table)

    // Это выполнится успешно в dev (Docker)
    // Но упадет в production с ошибкой:
    // "Invalid object name 'JSON_VALUE'"
}
```

```go
// ❌ ОПАСНО: Работает в Docker, не работает в production
func splitString(values string) {
    query := `SELECT value FROM STRING_SPLIT(@values, ',')`
    // STRING_SPLIT появилась в SQL Server 2016
    // В SQL Server 2012 этой функции НЕТ
}
```

## ✅ Стратегия защиты

### 1. Compatibility Mode - ОБЯЗАТЕЛЬНО!

**В начале каждой сессии:**

```sql
-- Установить compatibility level SQL Server 2012
ALTER DATABASE YourDevDB SET COMPATIBILITY_LEVEL = 110;
-- 110 = SQL Server 2012
-- 120 = SQL Server 2014
-- 130 = SQL Server 2016
```

**Проверка текущего уровня:**

```sql
SELECT name, compatibility_level
FROM sys.databases
WHERE name = DB_NAME();
```

**В Docker container при запуске:**

```dockerfile
# docker-compose.yml
services:
  mssql-dev:
    image: mcr.microsoft.com/mssql/server:2019-latest
    environment:
      ACCEPT_EULA: Y
      SA_PASSWORD: YourPassword123!
    volumes:
      - ./init-db.sql:/docker-entrypoint-initdb.d/init.sql
```

```sql
-- init-db.sql
CREATE DATABASE DevDB;
GO

ALTER DATABASE DevDB SET COMPATIBILITY_LEVEL = 110;
GO

USE DevDB;
GO
```

### 2. Automated Compatibility Check

**В тестах:**

```go
func TestSQLServer2012Compatibility(t *testing.T) {
    // Проверяем compatibility level
    var compatLevel int
    err := db.QueryRow(`
        SELECT compatibility_level
        FROM sys.databases
        WHERE name = DB_NAME()
    `).Scan(&compatLevel)

    require.NoError(t, err)

    // Требуем SQL Server 2012 compatibility
    assert.Equal(t, 110, compatLevel,
        "Database must be in SQL Server 2012 compatibility mode (110)")
}
```

### 3. Forbidden Functions List

**Функции, которые НЕЛЬЗЯ использовать:**

#### SQL Server 2016+ (запрещены!)
```sql
-- ❌ JSON функции
JSON_VALUE()
JSON_QUERY()
JSON_MODIFY()
OPENJSON()
FOR JSON AUTO

-- ❌ STRING функции
STRING_SPLIT(string, separator)
STRING_ESCAPE()

-- ❌ Другие
DROP IF EXISTS
```

#### SQL Server 2017+ (запрещены!)
```sql
-- ❌ STRING функции
STRING_AGG(value, separator)
TRIM()
CONCAT_WS(separator, string1, string2, ...)
TRANSLATE()

-- ❌ Графы
MATCH, NODE, EDGE
```

#### SQL Server 2022+ (запрещены!)
```sql
-- ❌ Новые функции
LEAST(), GREATEST()
DATE_BUCKET()
GENERATE_SERIES()
WINDOW clause enhancements
```

### 4. Allowed Functions (SQL Server 2012)

**Безопасные функции для использования:**

```sql
-- ✅ Пагинация (SQL Server 2012+)
SELECT * FROM table
ORDER BY ID
OFFSET 10 ROWS
FETCH NEXT 20 ROWS ONLY

-- ✅ IIF (SQL Server 2012+)
SELECT IIF(Age >= 18, 'Adult', 'Minor')

-- ✅ FORMAT (SQL Server 2012+)
SELECT FORMAT(GETDATE(), 'yyyy-MM-dd')

-- ✅ TRY_CONVERT (SQL Server 2012+)
SELECT TRY_CONVERT(INT, '123')

-- ✅ EOMONTH (SQL Server 2012+)
SELECT EOMONTH(GETDATE())

-- ✅ MERGE (SQL Server 2008+)
MERGE INTO target
USING source ON target.ID = source.ID
WHEN MATCHED THEN UPDATE SET ...
WHEN NOT MATCHED THEN INSERT ...
```

### 5. Workarounds для запрещенных функций

#### JSON (SQL Server 2016+) → XML (SQL Server 2005+)

```sql
-- ❌ Не работает в SQL Server 2012
SELECT JSON_VALUE(data, '$.name') FROM table

-- ✅ Работает через XML
SELECT
    data.value('(/root/name)[1]', 'NVARCHAR(100)') AS Name
FROM table
```

#### STRING_SPLIT (SQL Server 2016+) → XML workaround

```sql
-- ❌ Не работает в SQL Server 2012
SELECT value FROM STRING_SPLIT('a,b,c', ',')

-- ✅ Работает через XML
SELECT
    Split.a.value('.', 'VARCHAR(100)') AS value
FROM (
    SELECT CAST('<M>' + REPLACE('a,b,c', ',', '</M><M>') + '</M>' AS XML) AS Data
) AS A
CROSS APPLY Data.nodes('/M') AS Split(a)
```

#### STRING_AGG (SQL Server 2017+) → FOR XML PATH

```sql
-- ❌ Не работает в SQL Server 2012
SELECT STRING_AGG(name, ',') FROM table

-- ✅ Работает через XML
SELECT STUFF((
    SELECT ',' + name
    FROM table
    FOR XML PATH(''), TYPE
).value('.', 'NVARCHAR(MAX)'), 1, 1, '')
```

#### TRIM (SQL Server 2017+) → LTRIM/RTRIM

```sql
-- ❌ Не работает в SQL Server 2012
SELECT TRIM(name) FROM table

-- ✅ Работает
SELECT LTRIM(RTRIM(name)) FROM table
```

## 🧪 Testing Strategy

### Локальное тестирование (Docker)

**1. Создайте два контейнера:**

```yaml
# docker-compose.yml
version: '3.8'

services:
  # Development - с новыми функциями
  mssql-dev:
    image: mcr.microsoft.com/mssql/server:2019-latest
    container_name: mssql-dev
    environment:
      ACCEPT_EULA: Y
      SA_PASSWORD: DevPassword123!
    ports:
      - "1433:1433"
    volumes:
      - ./init-dev.sql:/docker-entrypoint-initdb.d/init.sql

  # Production simulation - SQL Server 2012 compatibility
  mssql-prod-sim:
    image: mcr.microsoft.com/mssql/server:2019-latest
    container_name: mssql-prod-sim
    environment:
      ACCEPT_EULA: Y
      SA_PASSWORD: ProdPassword123!
    ports:
      - "1434:1433"
    volumes:
      - ./init-prod.sql:/docker-entrypoint-initdb.d/init.sql
```

**init-prod.sql (симуляция production):**
```sql
CREATE DATABASE ProdSimDB;
GO

-- КРИТИЧНО: Устанавливаем SQL Server 2012 compatibility
ALTER DATABASE ProdSimDB SET COMPATIBILITY_LEVEL = 110;
GO

USE ProdSimDB;
GO

-- Создаем тестовые таблицы
CREATE TABLE Users (
    ID INT PRIMARY KEY,
    Name NVARCHAR(100),
    Email NVARCHAR(100),
    CreatedAt DATETIME2
);
GO
```

**2. Интеграционные тесты на оба окружения:**

```go
func TestMSSQLAdapter_BothEnvironments(t *testing.T) {
    tests := []struct {
        name   string
        dsn    string
        compatLevel int
        description string
    }{
        {
            name:   "Development",
            dsn:    "server=localhost,1433;user id=sa;password=DevPassword123!;database=DevDB",
            compatLevel: 140, // SQL Server 2017 or higher
            description: "Tests modern SQL Server features",
        },
        {
            name:   "Production Simulation",
            dsn:    "server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB",
            compatLevel: 110, // SQL Server 2012
            description: "Tests with SQL Server 2012 compatibility",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            adapter, err := NewAdapter(tt.dsn)
            require.NoError(t, err)
            defer adapter.Close()

            // Проверяем compatibility level
            var actualCompat int
            err = adapter.db.QueryRow(`
                SELECT compatibility_level
                FROM sys.databases
                WHERE name = DB_NAME()
            `).Scan(&actualCompat)
            require.NoError(t, err)

            t.Logf("%s: Compatibility Level = %d (expected %d)",
                tt.name, actualCompat, tt.compatLevel)

            // Запускаем тесты
            testExport(t, adapter, tt.compatLevel)
            testImport(t, adapter, tt.compatLevel)
        })
    }
}

func testExport(t *testing.T, adapter *Adapter, compatLevel int) {
    // Тест должен работать на ЛЮБОМ compatibility level
    packets, err := adapter.ExportTable(context.Background(), "Users")

    require.NoError(t, err,
        "Export must work on compatibility level %d (SQL Server 2012)", compatLevel)

    assert.NotEmpty(t, packets)
}
```

### CI/CD Pipeline

**GitHub Actions / GitLab CI:**

```yaml
name: SQL Server Compatibility Tests

on: [push, pull_request]

jobs:
  test-mssql-2012-compat:
    runs-on: ubuntu-latest

    services:
      mssql:
        image: mcr.microsoft.com/mssql/server:2019-latest
        env:
          ACCEPT_EULA: Y
          SA_PASSWORD: TestPassword123!
        ports:
          - 1433:1433
        options: >-
          --health-cmd="/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P TestPassword123! -Q 'SELECT 1'"
          --health-interval=10s
          --health-timeout=5s
          --health-retries=5

    steps:
      - uses: actions/checkout@v3

      - name: Setup SQL Server 2012 Compatibility
        run: |
          /opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P TestPassword123! -Q "
            CREATE DATABASE TestDB;
            ALTER DATABASE TestDB SET COMPATIBILITY_LEVEL = 110;
          "

      - name: Run Tests
        run: |
          go test ./pkg/adapters/mssql/... -v -tags=integration
        env:
          MSSQL_DSN: "server=localhost;user id=sa;password=TestPassword123!;database=TestDB"
```

## 🔍 Code Review Checklist

**Перед каждым коммитом проверять:**

- [ ] Не используются JSON функции (SQL Server 2016+)
- [ ] Не используется STRING_SPLIT (SQL Server 2016+)
- [ ] Не используется STRING_AGG (SQL Server 2017+)
- [ ] Не используется TRIM (SQL Server 2017+)
- [ ] Используется OFFSET/FETCH для пагинации (не ROW_NUMBER())
- [ ] Используется MERGE для UPSERT (не INSERT ... ON DUPLICATE KEY)
- [ ] Используется NVARCHAR для Unicode строк
- [ ] Используется DATETIME2 для timestamps
- [ ] Тесты проходят в SQL Server 2012 compatibility mode
- [ ] Feature detection используется для условных функций

## 📝 Development Workflow

### 1. Локальная разработка

```bash
# Запустить dev контейнер (с современными функциями)
docker-compose up -d mssql-dev

# Работать с кодом
go run examples/mssql/main.go

# КРИТИЧНО: Перед коммитом тестировать на prod simulation
docker-compose up -d mssql-prod-sim

# Запустить тесты на SQL Server 2012 compatibility
go test ./pkg/adapters/mssql/... -v -tags=integration \
    -mssql-dsn="server=localhost,1434;..."
```

### 2. Pre-commit Hook

```bash
# .git/hooks/pre-commit
#!/bin/bash

echo "Checking SQL Server 2012 compatibility..."

# Запускаем тесты на prod simulation
docker-compose up -d mssql-prod-sim
sleep 10  # Ждем запуска SQL Server

go test ./pkg/adapters/mssql/... -tags=integration \
    -mssql-dsn="server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB"

if [ $? -ne 0 ]; then
    echo "❌ Tests failed on SQL Server 2012 compatibility mode!"
    echo "Please fix compatibility issues before committing."
    exit 1
fi

echo "✅ SQL Server 2012 compatibility tests passed"
```

### 3. Feature Detection в коде

```go
// Определяем версию при подключении
type Adapter struct {
    db            *sql.DB
    serverVersion int  // 11=2012, 13=2016, 14=2017, etc.
    compatLevel   int  // 110=2012, 130=2016, 140=2017, etc.
}

func NewAdapter(dsn string) (*Adapter, error) {
    db, err := sql.Open("mssql", dsn)
    if err != nil {
        return nil, err
    }

    a := &Adapter{db: db}

    // Определяем версию сервера
    var versionStr string
    err = db.QueryRow("SELECT SERVERPROPERTY('ProductVersion')").Scan(&versionStr)
    if err != nil {
        return nil, err
    }
    a.serverVersion = parseVersion(versionStr) // "11.0.2100" → 11

    // Определяем compatibility level
    err = db.QueryRow(`
        SELECT compatibility_level
        FROM sys.databases
        WHERE name = DB_NAME()
    `).Scan(&a.compatLevel)
    if err != nil {
        return nil, err
    }

    // КРИТИЧНО: Проверяем что в dev тоже установлен SQL 2012 compat
    if a.compatLevel > 110 {
        log.Printf("⚠️  WARNING: Database compatibility level is %d (not 110 for SQL Server 2012)", a.compatLevel)
        log.Printf("⚠️  Set compatibility: ALTER DATABASE %s SET COMPATIBILITY_LEVEL = 110;", db.Name())
    }

    return a, nil
}

// Условное использование функций
func (a *Adapter) splitString(value string) ([]string, error) {
    if a.compatLevel >= 130 { // SQL Server 2016+
        // Используем STRING_SPLIT
        return a.splitWithFunction(value)
    } else {
        // Используем XML workaround
        return a.splitWithXML(value)
    }
}
```

## 🎯 Рекомендации

### ДЛЯ РАЗРАБОТКИ:

1. **ВСЕГДА** устанавливайте SQL Server 2012 compatibility level:
   ```sql
   ALTER DATABASE DevDB SET COMPATIBILITY_LEVEL = 110;
   ```

2. **ВСЕГДА** тестируйте на prod simulation контейнере перед коммитом

3. **НИКОГДА** не используйте функции появившиеся после SQL Server 2012

4. Используйте **feature detection** только для опциональных оптимизаций

### ДЛЯ PRODUCTION:

1. Перед деплоем запускайте **full test suite** на prod simulation

2. Используйте **staging environment** с реальным SQL Server 2012

3. Мониторьте логи на предмет ошибок совместимости

4. Имейте **rollback plan** на случай проблем

## 📊 Comparison Table

| Функция | SQL 2012 | SQL 2019 (Docker) | Решение |
|---------|----------|-------------------|---------|
| OFFSET/FETCH | ✅ | ✅ | Использовать |
| MERGE | ✅ | ✅ | Использовать |
| IIF | ✅ | ✅ | Использовать |
| TRY_CONVERT | ✅ | ✅ | Использовать |
| JSON_VALUE | ❌ | ✅ | XML workaround |
| STRING_SPLIT | ❌ | ✅ | XML workaround |
| STRING_AGG | ❌ | ✅ | FOR XML PATH |
| TRIM | ❌ | ✅ | LTRIM(RTRIM()) |
| DROP IF EXISTS | ❌ | ✅ | IF EXISTS DROP |

## 🚨 Red Flags

**Признаки проблем с совместимостью:**

1. ❌ Тесты проходят локально, но падают в production
2. ❌ В коде есть JSON_VALUE, STRING_SPLIT, STRING_AGG
3. ❌ Compatibility level > 110 в dev базе
4. ❌ Нет тестов на prod simulation контейнере
5. ❌ Используются функции без проверки версии

## ✅ Green Flags

**Признаки хорошей совместимости:**

1. ✅ Все тесты проходят на compatibility level 110
2. ✅ Используются только функции SQL Server 2012
3. ✅ Feature detection для опциональных функций
4. ✅ Prod simulation контейнер в CI/CD
5. ✅ Pre-commit hook проверяет совместимость

---

**Версия:** 1.0
**Последнее обновление:** 16.11.2025
**Статус:** Critical - Must read before development
