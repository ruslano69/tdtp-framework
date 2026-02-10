# Пример миграции SQLite адаптера на base helpers

Этот документ показывает, как мигрировать существующий SQLite адаптер на использование base helpers.

## До миграции

### Файл: `pkg/adapters/sqlite/export.go` (~300 строк)

```go
package sqlite

// ExportTable экспортирует всю таблицу в TDTP reference пакеты
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
    // Получаем схему
    schema, err := a.GetTableSchema(ctx, tableName)
    if err != nil {
        return nil, err
    }

    // Читаем все данные
    rows, err := a.readAllRows(ctx, tableName, schema)
    if err != nil {
        return nil, err
    }

    // Генерируем reference пакеты
    generator := packet.NewGenerator()
    return generator.GenerateReference(tableName, schema, rows)
}

// ExportTableWithQuery экспортирует таблицу с применением TDTQL фильтрации
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
    // Получаем схему
    pkgSchema, err := a.GetTableSchema(ctx, tableName)
    if err != nil {
        return nil, err
    }

    // Пробуем транслировать TDTQL → SQL для оптимизации
    sqlGenerator := tdtql.NewSQLGenerator()
    if sqlGenerator.CanTranslateToSQL(query) {
        // Оптимизированный путь...
        sql, err := sqlGenerator.GenerateSQL(tableName, query)
        if err == nil {
            rows, err := a.readRowsWithSQL(ctx, sql, pkgSchema)
            if err == nil {
                queryContext := a.createQueryContextForSQL(ctx, query, rows, tableName)
                generator := packet.NewGenerator()
                return generator.GenerateResponse(tableName, "", pkgSchema, rows, queryContext, sender, recipient)
            }
        }
    }

    // Fallback: in-memory фильтрация
    rows, err := a.readAllRows(ctx, tableName, pkgSchema)
    if err != nil {
        return nil, err
    }

    executor := tdtql.NewExecutor()
    result, err := executor.Execute(query, rows, pkgSchema)
    if err != nil {
        return nil, fmt.Errorf("failed to execute query: %w", err)
    }

    generator := packet.NewGenerator()
    return generator.GenerateResponse(tableName, "", pkgSchema, result.FilteredRows, result.QueryContext, sender, recipient)
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
    // ~50 строк кода...
}

// readRowsWithSQL читает строки используя произвольный SQL запрос
func (a *Adapter) readRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
    // ~30 строк кода...
}

// convertValueToTDTP конвертирует значение из БД в TDTP формат
func (a *Adapter) convertValueToTDTP(field packet.Field, value string) string {
    // ~25 строк кода...
}

// createQueryContextForSQL создает QueryContext для SQL-based export
func (a *Adapter) createQueryContextForSQL(ctx context.Context, query *packet.Query, rows [][]string, tableName string) *packet.QueryContext {
    // ~15 строк кода...
}
```

## После миграции

### Файл: `pkg/adapters/sqlite/adapter.go` (добавлено)

```go
package sqlite

import (
    "database/sql"
    "github.com/ruslano69/tdtp-framework/pkg/adapters/base"
)

type Adapter struct {
    db           *sql.DB
    config       adapters.Config

    // Base helpers (NEW!)
    exportHelper *base.ExportHelper
    importHelper *base.ImportHelper
    converter    *base.UniversalTypeConverter
}

func NewAdapter() *Adapter {
    return &Adapter{}
}

func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
    // Открываем соединение с БД
    db, err := sql.Open("sqlite", cfg.DSN)
    if err != nil {
        return fmt.Errorf("failed to open database: %w", err)
    }
    a.db = db
    a.config = cfg

    // Инициализируем base helpers (NEW!)
    a.initHelpers()

    return nil
}

// initHelpers инициализирует базовые хелперы
func (a *Adapter) initHelpers() {
    // Создаем универсальный конвертер типов
    a.converter = base.NewUniversalTypeConverter()

    // Создаем export helper
    // self реализует SchemaReader и DataReader интерфейсы
    // nil = не нужна адаптация SQL для SQLite (стандартный LIMIT/OFFSET)
    a.exportHelper = base.NewExportHelper(a, a, a.converter, nil)

    // Создаем import helper
    // self реализует TableManager, DataInserter, TransactionManager интерфейсы
    // true = использовать временные таблицы для атомарной замены
    a.importHelper = base.NewImportHelper(a, a, a, true)
}
```

### Файл: `pkg/adapters/sqlite/export.go` (упрощено до ~80 строк)

```go
package sqlite

import (
    "context"
    "database/sql"
    "fmt"
    "strings"
    "github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ========== Делегирование в ExportHelper ==========

// ExportTable экспортирует всю таблицу (делегирование в helper)
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
    return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery экспортирует таблицу с TDTQL фильтрацией (делегирование в helper)
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
    return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// ========== Реализация интерфейсов для ExportHelper ==========

// ReadAllRows реализует base.DataReader
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
    // Формируем список полей для SELECT
    fieldNames := make([]string, len(schema.Fields))
    for i, field := range schema.Fields {
        fieldNames[i] = field.Name
    }

    query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(fieldNames, ", "), tableName)

    rows, err := a.db.QueryContext(ctx, query)
    if err != nil {
        return nil, fmt.Errorf("failed to query table: %w", err)
    }
    defer rows.Close()

    return a.scanRows(rows, schema)
}

// ReadRowsWithSQL реализует base.DataReader
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
    rows, err := a.db.QueryContext(ctx, sqlQuery)
    if err != nil {
        return nil, fmt.Errorf("failed to query: %w", err)
    }
    defer rows.Close()

    return a.scanRows(rows, schema)
}

// GetRowCount реализует base.DataReader
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
    query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)

    var count int64
    err := a.db.QueryRowContext(ctx, query).Scan(&count)
    if err != nil {
        return 0, fmt.Errorf("failed to count rows: %w", err)
    }

    return count, nil
}

// scanRows сканирует sql.Rows в [][]string (вспомогательная функция)
func (a *Adapter) scanRows(rows *sql.Rows, schema packet.Schema) ([][]string, error) {
    var result [][]string

    // Подготавливаем scanner для всех колонок
    scanArgs := make([]interface{}, len(schema.Fields))
    for i := range scanArgs {
        var v sql.NullString
        scanArgs[i] = &v
    }

    for rows.Next() {
        if err := rows.Scan(scanArgs...); err != nil {
            return nil, fmt.Errorf("failed to scan row: %w", err)
        }

        // Конвертируем в строки согласно TDTP формату
        row := make([]string, len(schema.Fields))
        for i, arg := range scanArgs {
            v := arg.(*sql.NullString)
            if v.Valid {
                // Используем универсальный конвертер из base
                row[i] = a.converter.ConvertValueToTDTP(schema.Fields[i], v.String)
            } else {
                row[i] = "" // NULL представляется пустой строкой
            }
        }

        result = append(result, row)
    }

    return result, rows.Err()
}
```

### Файл: `pkg/adapters/sqlite/import.go` (упрощено до ~100 строк)

```go
package sqlite

import (
    "context"
    "fmt"
    "strings"
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    "github.com/ruslano69/tdtp-framework/pkg/adapters/base"
    "github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ========== Делегирование в ImportHelper ==========

// ImportPacket импортирует один пакет (делегирование в helper)
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
    return a.importHelper.ImportPacket(ctx, pkt, strategy)
}

// ImportPackets импортирует несколько пакетов (делегирование в helper)
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
    return a.importHelper.ImportPackets(ctx, packets, strategy)
}

// ========== Реализация интерфейсов для ImportHelper ==========

// TableExists реализует base.TableManager
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
    query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
    var count int
    err := a.db.QueryRowContext(ctx, query, tableName).Scan(&count)
    if err != nil {
        return false, err
    }
    return count > 0, nil
}

// CreateTable реализует base.TableManager
func (a *Adapter) CreateTable(ctx context.Context, tableName string, schema packet.Schema) error {
    var columns []string
    var pkColumns []string

    for _, field := range schema.Fields {
        sqlType := TDTPToSQLite(field)
        colDef := fmt.Sprintf("%s %s", field.Name, sqlType)
        columns = append(columns, colDef)

        if field.Key {
            pkColumns = append(pkColumns, field.Name)
        }
    }

    if len(pkColumns) > 0 {
        pkDef := fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkColumns, ", "))
        columns = append(columns, pkDef)
    }

    query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)", tableName, strings.Join(columns, ",\n  "))

    _, err := a.db.ExecContext(ctx, query)
    if err != nil {
        return fmt.Errorf("failed to create table: %w", err)
    }

    return nil
}

// DropTable реализует base.TableManager
func (a *Adapter) DropTable(ctx context.Context, tableName string) error {
    query := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
    _, err := a.db.ExecContext(ctx, query)
    return err
}

// RenameTable реализует base.TableManager
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
    query := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldName, newName)
    _, err := a.db.ExecContext(ctx, query)
    return err
}

// InsertRows реализует base.DataInserter
func (a *Adapter) InsertRows(ctx context.Context, tableName string, schema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
    if len(rows) == 0 {
        return nil
    }

    // Формируем INSERT запрос
    fieldNames := make([]string, len(schema.Fields))
    for i, field := range schema.Fields {
        fieldNames[i] = field.Name
    }

    placeholders := make([]string, len(schema.Fields))
    for i := range placeholders {
        placeholders[i] = "?"
    }

    var insertCmd string
    switch strategy {
    case adapters.StrategyReplace:
        insertCmd = "INSERT OR REPLACE"
    case adapters.StrategyIgnore:
        insertCmd = "INSERT OR IGNORE"
    case adapters.StrategyFail:
        insertCmd = "INSERT"
    case adapters.StrategyCopy:
        insertCmd = "INSERT OR REPLACE" // SQLite не поддерживает COPY
    default:
        insertCmd = "INSERT OR REPLACE"
    }

    query := fmt.Sprintf("%s INTO %s (%s) VALUES (%s)",
        insertCmd, tableName, strings.Join(fieldNames, ", "), strings.Join(placeholders, ", "))

    stmt, err := a.db.PrepareContext(ctx, query)
    if err != nil {
        return fmt.Errorf("failed to prepare statement: %w", err)
    }
    defer stmt.Close()

    // Вставляем каждую строку
    for rowIdx, row := range rows {
        // Парсим строку (используем утилиту из base)
        values := base.ParseRowValues(row)

        // Конвертируем значения (используем утилиту из base)
        args, err := base.ConvertRowToSQLValues(values, schema, a.converter, "sqlite")
        if err != nil {
            return fmt.Errorf("row %d: %w", rowIdx, err)
        }

        // Выполняем INSERT
        if _, err := stmt.ExecContext(ctx, args...); err != nil {
            return fmt.Errorf("failed to insert row %d: %w", rowIdx, err)
        }
    }

    return nil
}
```

## Сравнение

| Метрика | До | После | Улучшение |
|---------|-----|-------|-----------|
| **Строк кода в export.go** | ~300 | ~80 | **-73%** |
| **Строк кода в import.go** | ~290 | ~100 | **-66%** |
| **Общее уменьшение** | ~590 | ~180 | **-69%** |
| **Дублированный код** | Весь код дублируется в 4 адаптерах | Только SQLite-специфичная логика | **Устранено** |
| **Методы для реализации** | 15+ методов | 10 методов (простых) | **-33%** |

## Что удалено

❌ `ExportTable()` - делегировано в `exportHelper.ExportTable()`
❌ `ExportTableWithQuery()` - делегировано в `exportHelper.ExportTableWithQuery()`
❌ `convertValueToTDTP()` - заменено на `converter.ConvertValueToTDTP()`
❌ `createQueryContextForSQL()` - логика в `exportHelper`
❌ `ImportPacket()` - делегировано в `importHelper.ImportPacket()`
❌ `ImportPackets()` - делегировано в `importHelper.ImportPackets()`
❌ `replaceTables()` - логика в `importHelper`
❌ `generateTempTableName()` - заменено на `base.GenerateTempTableName()`
❌ `parseRow()` - заменено на `base.ParseRowValues()`
❌ `typedValueToSQL()` - заменено на `converter.TypedValueToSQL()`

## Что осталось (SQLite-специфичное)

✅ `GetTableSchema()` - использует `PRAGMA table_info()`
✅ `ReadAllRows()` / `ReadRowsWithSQL()` - использует `sql.NullString`
✅ `GetRowCount()` - простой COUNT(*)
✅ `TableExists()` - проверка через `sqlite_master`
✅ `CreateTable()` - создание с PRIMARY KEY
✅ `DropTable()` - DROP TABLE IF EXISTS
✅ `RenameTable()` - ALTER TABLE RENAME TO
✅ `InsertRows()` - INSERT OR REPLACE/IGNORE
✅ `TDTPToSQLite()` - маппинг типов TDTP → SQLite

## Выгоды

✅ **Меньше кода** - 69% сокращение
✅ **Проще поддержка** - изменения в base применяются ко всем адаптерам
✅ **Консистентность** - одинаковое поведение
✅ **Тестируемость** - хелперы легко тестировать
✅ **Быстрее добавление новых БД** - только специфичная логика

---

**Примечание**: Это пример миграции. Фактическая миграция будет выполнена в отдельной задаче после тестирования base helpers.
