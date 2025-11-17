# SQLite Adapter for TDTP Framework v0.5

## Описание

SQLite адаптер обеспечивает двунаправленную интеграцию между SQLite базами данных и TDTP протоколом:
- **Export**: БД → TDTP пакеты
- **Import**: TDTP пакеты → БД

## Установка

### Требуется SQLite драйвер

Адаптер работает с `database/sql` и требует SQLite драйвер:

```bash
# Pure Go версия (без CGO)
go get modernc.org/sqlite

# Или классическая версия (требует CGO)
go get github.com/mattn/go-sqlite3
```

## Быстрый старт

```go
package main

import (
    "fmt"
    "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
    _ "modernc.org/sqlite" // или _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Открываем БД
    adapter, err := sqlite.NewAdapter("test.db")
    if err != nil {
        panic(err)
    }
    defer adapter.Close()
    
    // Экспорт таблицы
    packets, err := adapter.ExportTable("Users")
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Exported %d packets\n", len(packets))
}
```

## API Reference

### Создание адаптера

```go
// NewAdapter создает новый адаптер
func NewAdapter(filePath string) (*Adapter, error)

// Close закрывает соединение
func (a *Adapter) Close() error
```

### Export (БД → TDTP)

```go
// ExportTable экспортирует всю таблицу в reference пакеты
func (a *Adapter) ExportTable(tableName string) ([]*packet.DataPacket, error)

// ExportTableWithQuery экспортирует с TDTQL фильтрацией (v0.6+)
func (a *Adapter) ExportTableWithQuery(
    tableName string, 
    query *packet.Query, 
    sender, recipient string,
) ([]*packet.DataPacket, error)

// GetTableSchema читает схему таблицы
func (a *Adapter) GetTableSchema(tableName string) (packet.Schema, error)

// GetRowCount возвращает количество строк
func (a *Adapter) GetRowCount(tableName string) (int64, error)
```

### Import (TDTP → БД)

```go
// ImportPacket импортирует один пакет
func (a *Adapter) ImportPacket(pkt *packet.DataPacket, strategy ImportStrategy) error

// ImportPackets импортирует несколько пакетов (в транзакции)
func (a *Adapter) ImportPackets(packets []*packet.DataPacket, strategy ImportStrategy) error

// CreateTable создает таблицу по TDTP схеме
func (a *Adapter) CreateTable(tableName string, schema packet.Schema) error

// DropTable удаляет таблицу
func (a *Adapter) DropTable(tableName string) error
```

### Стратегии импорта

```go
const (
    StrategyReplace ImportStrategy = "REPLACE" // INSERT OR REPLACE
    StrategyIgnore  ImportStrategy = "IGNORE"  // INSERT OR IGNORE  
    StrategyFail    ImportStrategy = "FAIL"    // INSERT (ошибка при дубликатах)
)
```

### Утилиты

```go
// TableExists проверяет существование таблицы
func (a *Adapter) TableExists(tableName string) (bool, error)

// GetTableNames возвращает список всех таблиц
func (a *Adapter) GetTableNames() ([]string, error)

// BeginTx начинает транзакцию
func (a *Adapter) BeginTx() (*sql.Tx, error)
```

## Маппинг типов

### SQLite → TDTP

| SQLite Type | TDTP Type | Примечания |
|-------------|-----------|------------|
| INTEGER, INT, BIGINT | INTEGER | Целые числа |
| REAL, FLOAT, DOUBLE | REAL | С плавающей точкой |
| NUMERIC, DECIMAL | DECIMAL | Фиксированная точность |
| TEXT, VARCHAR, CHAR | TEXT | Строки |
| BOOLEAN (INTEGER) | BOOLEAN | 0=false, 1=true |
| DATE | DATE | YYYY-MM-DD |
| DATETIME, TIMESTAMP | TIMESTAMP | RFC3339, UTC |
| BLOB | BLOB | Base64 encoded |

### TDTP → SQLite CREATE TABLE

```go
// Примеры конвертации
field := packet.Field{Name: "Balance", Type: "DECIMAL", Precision: 18, Scale: 2}
sqlType := TDTPToSQLite(field) // "NUMERIC(18,2)"

field := packet.Field{Name: "Name", Type: "TEXT", Length: 200}
sqlType := TDTPToSQLite(field) // "TEXT"
```

## Примеры использования

### 1. Export таблицы

```go
adapter, _ := sqlite.NewAdapter("database.db")
defer adapter.Close()

// Экспорт всей таблицы
packets, err := adapter.ExportTable("Customers")
if err != nil {
    log.Fatal(err)
}

// Сохраняем в XML файлы
for i, pkt := range packets {
    filename := fmt.Sprintf("customers_part_%d.xml", i+1)
    xml, _ := pkt.ToXML()
    os.WriteFile(filename, []byte(xml), 0644)
}
```

### 1a. Export с TDTQL фильтрацией (v0.6+)

```go
adapter, _ := sqlite.NewAdapter("database.db")
defer adapter.Close()

// SQL запрос
sql := "SELECT * FROM Customers WHERE IsActive = 1 AND Balance > 1000 ORDER BY Balance DESC"

// Трансляция в TDTQL
translator := tdtql.NewTranslator()
query, _ := translator.Translate(sql)

// Export с фильтрацией
packets, err := adapter.ExportTableWithQuery(
    "Customers",
    query,
    "CustomerService", // sender
    "SyncQueue",       // recipient
)
if err != nil {
    log.Fatal(err)
}

// Результат - Response пакеты с QueryContext
pkt := packets[0]
fmt.Printf("Total in table: %d\n", pkt.QueryContext.ExecutionResults.TotalRecordsInTable)
fmt.Printf("After filters: %d\n", pkt.QueryContext.ExecutionResults.RecordsAfterFilters)
fmt.Printf("Returned: %d\n", pkt.QueryContext.ExecutionResults.RecordsReturned)
fmt.Printf("More available: %v\n", pkt.QueryContext.ExecutionResults.MoreDataAvailable)
```

### 2. Import из TDTP

```go
adapter, _ := sqlite.NewAdapter("database.db")
defer adapter.Close()

// Парсим TDTP пакет
xml, _ := os.ReadFile("customers.xml")
parser := packet.NewParser()
pkt, _ := parser.Parse(xml)

// Импортируем (с заменой дубликатов)
err := adapter.ImportPacket(pkt, sqlite.StrategyReplace)
if err != nil {
    log.Fatal(err)
}
```

### 3. Синхронизация двух БД

```go
// Source
source, _ := sqlite.NewAdapter("source.db")
defer source.Close()

// Target
target, _ := sqlite.NewAdapter("target.db")
defer target.Close()

// Экспорт из source
packets, _ := source.ExportTable("Products")

// Импорт в target
err := target.ImportPackets(packets, sqlite.StrategyReplace)
```

### 4. Создание таблицы из схемы

```go
adapter, _ := sqlite.NewAdapter("new.db")
defer adapter.Close()

// Создаем схему
builder := schema.NewBuilder()
schemaObj := builder.
    AddInteger("ID", true).
    AddText("Name", 100).
    AddDecimal("Price", 18, 2).
    AddBoolean("InStock").
    Build()

// Создаем таблицу
err := adapter.CreateTable("Products", schemaObj)
```

## Особенности SQLite

### Динамическая типизация

SQLite использует динамическую типизацию - типы данных являются рекомендациями, но не строгими ограничениями. Адаптер:
- При export читает заявленный тип из PRAGMA table_info
- При import создает таблицы с корректными типами
- Валидация данных выполняется на уровне TDTP schema converter

### BOOLEAN как INTEGER

SQLite не имеет нативного BOOLEAN типа:
- В БД хранится как INTEGER (0/1)
- В TDTP конвертируется в BOOLEAN тип
- При импорте обратно конвертируется в INTEGER

### DECIMAL/NUMERIC

SQLite NUMERIC является аффинити для REAL:
- Хранит как REAL внутри
- При импорте создается как NUMERIC(precision,scale)
- Точность зависит от лимитов REAL (float64)

### Транзакции

```go
// Явная транзакция
tx, _ := adapter.BeginTx()

// Работа с tx через adapter.db...

tx.Commit() // или tx.Rollback()

// Автоматическая транзакция для ImportPackets
adapter.ImportPackets(packets, sqlite.StrategyReplace)
```

## Ограничения v0.5

1. **ExportTableWithQuery** - заглушка, реализация в v0.6
2. **Нет поддержки JOIN** - экспорт только одной таблицы
3. **Нет инкрементального sync** - только полный экспорт/импорт
4. **Нет миграции схем** - только CREATE/DROP таблиц
5. **Требуется внешний SQLite драйвер** - не включен из-за сетевых ограничений

## Roadmap

### v0.6 - Query Integration
- Реализация ExportTableWithQuery через TDTQL executor
- Трансляция TDTQL → SQL для оптимизации
- Поддержка инкрементального экспорта (по timestamp)

### v0.7 - Schema Migration
- ALTER TABLE для обновления схем
- Валидация совместимости схем
- Автоматическая миграция данных

### v1.0 - Production Ready
- Connection pooling
- Batch operations оптимизация
- Streaming для больших таблиц
- Метрики и мониторинг

## Тестирование

Для полноценного тестирования необходим SQLite драйвер:

```bash
# Установка драйвера
go get modernc.org/sqlite

# Запуск тестов
go test ./pkg/adapters/sqlite/... -v
```

Без драйвера адаптер компилируется, но не может подключиться к БД.

## Поддержка

- GitHub: [queuebridge/tdtp](https://github.com/queuebridge/tdtp)
- Issues: Создавайте issue с тегом [sqlite-adapter]
- Документация: `/docs/SQLITE_ADAPTER.md`

---

*Версия: v0.5*
*Дата: 14.11.2025*
*Статус: Beta - API стабилен, требуется тестирование*
