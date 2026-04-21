# MySQL Adapter для TDTP Framework

Высокопроизводительный адаптер для работы с MySQL/MariaDB базами данных.

> **Статус: полностью рабочий** — 58/58 CLI integration tests pass (MySQL 8.4, 2026-04-21)

## 🎯 Особенности

- ✅ **Полная поддержка TDTP спецификации**
- ✅ **Использование schema.Converter** для строгой типизации
- ✅ **Все стратегии импорта**: Replace (ON DUPLICATE KEY UPDATE), Ignore (INSERT IGNORE), Fail, Copy
- ✅ **TDTQL фильтрация** с оптимизацией на уровне SQL (WHERE, ORDER BY, LIMIT/OFFSET, IN)
- ✅ **Bracket-quoted имена** с пробелами и `$` (NAV/BC/ERP-стиль)
- ✅ **Правильная обработка ошибок** через MySQL driver типы
- ✅ **Транзакционная безопасность**
- ✅ **Поддержка всех MySQL типов данных**
- ✅ **Compact format** (v1.3.1), сжатие zstd/kanzi, хэш-верификация
- ✅ **Cross-DB roundtrip**: MySQL → MySQL и MySQL → SQLite

## 📦 Установка

```bash
go get github.com/go-sql-driver/mysql
```

## 🚀 Быстрый старт

```go
package main

import (
    "context"
    "fmt"

    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/mysql"
)

func main() {
    ctx := context.Background()

    // Подключение к MySQL
    adapter, err := adapters.New("mysql", adapters.Config{
        DSN: "user:password@tcp(localhost:3306)/dbname?parseTime=true",
    })
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Экспорт данных
    packets, err := adapter.ExportTable(ctx, "users")
    if err != nil {
        panic(err)
    }

    fmt.Printf("Exported %d packets\n", len(packets))
}
```

## 🔧 Конфигурация DSN

### Базовый формат
```
user:password@tcp(host:port)/dbname?параметры
```

### Рекомендуемые параметры
```
user:password@tcp(localhost:3306)/mydb?parseTime=true&charset=utf8mb4&loc=UTC
```

**Важные параметры:**
- `parseTime=true` - автоматический парсинг DATE/DATETIME/TIMESTAMP
- `charset=utf8mb4` - полная поддержка Unicode
- `loc=UTC` - временная зона для TIMESTAMP

## 📋 Поддерживаемые типы данных

### Маппинг TDTP → MySQL

| TDTP Type | MySQL Type | Примечания |
|-----------|------------|------------|
| INTEGER | BIGINT | INT для Length ≤ 4 |
| REAL | FLOAT | - |
| DOUBLE | DOUBLE | - |
| DECIMAL(p,s) | DECIMAL(p,s) | По умолчанию (18,2) |
| TEXT | VARCHAR(n) / TEXT | VARCHAR до 65535 |
| VARCHAR(n) | VARCHAR(n) | По умолчанию 255 |
| CHAR(n) | CHAR(n) | По умолчанию 1 |
| BOOLEAN | TINYINT(1) | 0/1 |
| DATE | DATE | YYYY-MM-DD |
| DATETIME | DATETIME | С timezone |
| TIMESTAMP | TIMESTAMP | UTC |
| BLOB | BLOB | Base64 в TDTP |

## 🔄 Стратегии импорта

### 1. StrategyReplace (UPSERT)
```go
// INSERT ... ON DUPLICATE KEY UPDATE
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
```
- При совпадении PK → UPDATE существующей записи
- При отсутствии PK → использует REPLACE INTO

### 2. StrategyIgnore
```go
// INSERT IGNORE
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyIgnore)
```
- Пропускает дубликаты без ошибок
- Оптимальная производительность

### 3. StrategyFail
```go
// INSERT
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyFail)
```
- Возвращает ошибку при дубликатах
- Строгий контроль данных

### 4. StrategyCopy
```go
// Аналог INSERT (MySQL не имеет COPY)
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyCopy)
```

## 🔍 TDTQL Фильтрация

### Оптимизированный SQL-путь
```go
query := &packet.Query{
    Filters: &packet.Filters{
        Condition: &packet.Condition{
            Field:    "age",
            Operator: ">",
            Value:    "18",
        },
    },
    Limit: 100,
    Offset: 0,
}

packets, err := adapter.ExportTableWithQuery(ctx, "users", query, "", "")
```

**Автоматическая трансляция** TDTQL → MySQL SQL с:
- Обратными кавычками для идентификаторов
- Нативной поддержкой LIMIT/OFFSET
- Оптимизацией на уровне БД

### Fallback на in-memory
Если SQL-трансляция невозможна → автоматическая фильтрация в памяти.

## ⚡ Производительность

### Batch Insert
```go
packets := []*packet.DataPacket{pkt1, pkt2, pkt3}
err := adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
// Все пакеты в одной транзакции
```

### Оптимизация запросов
- Использование подготовленных statements
- Транзакционная обработка множественных пакетов
- Прямая SQL-фильтрация (избегает загрузки всей таблицы)

## 🛡️ Обработка ошибок

### Duplicate Key
```go
if err != nil {
    if mysqlErr, ok := err.(*mysql.MySQLError); ok {
        if mysqlErr.Number == 1062 {
            // Обработка дубликата ключа
        }
    }
}
```

### Типы ошибок
- **1062** - Duplicate entry (PRIMARY/UNIQUE KEY)
- **1451** - Foreign key constraint
- **1452** - Cannot add or update child row

## 📊 Примеры использования

### Экспорт с фильтрацией
```go
query := &packet.Query{
    Filters: &packet.Filters{
        Logic: "AND",
        Conditions: []*packet.Condition{
            {Field: "status", Operator: "=", Value: "active"},
            {Field: "created_at", Operator: ">", Value: "2024-01-01"},
        },
    },
    OrderBy: []packet.OrderField{
        {Field: "created_at", Direction: "DESC"},
    },
    Limit: 1000,
}

packets, _ := adapter.ExportTableWithQuery(ctx, "users", query, "sender", "recipient")
```

### Импорт с автосозданием таблицы
```go
// Адаптер автоматически создаст таблицу если её нет
err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
```

### Получение метаданных
```go
// Схема таблицы
schema, err := adapter.GetTableSchema(ctx, "users")

// Количество строк
count, err := adapter.GetTableRowCount(ctx, "users")

// Размер таблицы
size, err := adapter.GetTableSize(ctx, "users")
```

## 🔧 Технические детали

### Type Conversion
Использует `schema.Converter` для:
- Валидации типов данных
- Конвертации TDTP ↔ MySQL
- Поддержки precision/scale для DECIMAL
- Правильного форматирования DATE/DATETIME/TIMESTAMP
- Base64 кодирования для BLOB

### Transaction Safety
- Все операции импорта выполняются в транзакциях
- Автоматический ROLLBACK при ошибках
- Поддержка множественных пакетов в одной транзакции

### SQL Generation
- Автоматическое экранирование идентификаторов (`)
- Параметризованные запросы (защита от SQL-injection)
- Оптимизация с использованием индексов

## 🎓 Best Practices

1. **Всегда используйте `parseTime=true`** в DSN для работы с временными типами
2. **Используйте `charset=utf8mb4`** для полной поддержки Unicode
3. **Создавайте индексы** на полях, используемых в фильтрах
4. **Используйте StrategyReplace** для idempotent операций
5. **Обрабатывайте ошибки** через типы MySQL driver

## ✅ Статус тестирования

CLI integration tests: **58 / 58 PASS** (MySQL 8.4.9, 2026-04-21)

| Группа | Описание | Тесты |
|--------|----------|-------|
| T1 | Basic Export (rows, fields, list) | 4 |
| T2 | TDTQL Filters (WHERE, IN, ORDER BY, LIMIT, bracket-quoted) | 9 |
| T3 | Compression (zstd/kanzi, --hash, corruption) | 6 |
| T4 | MySQL → MySQL Roundtrip (strategies, projection, ERP-style names) | 8 |
| T5 | File Integrity (--test, --inspect) | 3 |
| T6 | Edge Cases (empty result, errors) | 3 |
| T7 | Compact Format v1.3.1 | 4 |
| T8 | MySQL → SQLite Roundtrip (cross-DB) | 5 |
| T9 | Diff | 7 |
| T10 | Merge (union, intersection, append, left/right priority) | 9 |

Запуск тестов:
```bash
# 1. Поднять контейнер (из корня репозитория)
docker compose up -d mysql

# 2. Запустить тесты
TDTPCLI_BIN=/tmp/tdtpcli.exe py -3 tests/cli/test_mysql.py

# 3. Только одна группа
TDTPCLI_BIN=/tmp/tdtpcli.exe py -3 tests/cli/test_mysql.py T4
```

## 📝 Совместимость

- ✅ MySQL 5.7+
- ✅ MySQL 8.0+ (integration-tested on 8.4.9)
- ✅ MariaDB 10.3+
- ✅ Percona Server 5.7+

## 🔗 Ссылки

- [MySQL Driver Documentation](https://github.com/go-sql-driver/mysql)
- [TDTP Specification](../../docs/TDTP_SPEC.md)
- [TDTQL Query Language](../../docs/TDTQL.md)
