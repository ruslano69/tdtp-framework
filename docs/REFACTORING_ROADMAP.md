# План рефакторинга TDTP Framework

**Цель**: Устранение дублирования кода и улучшение архитектуры фреймворка

**Основание**: Анализ от 2025-12-25 выявил ~2050 строк дублированного кода (~33% кодовой базы)

---

## Фаза 1: Критичные рефакторинги (Priority: HIGH)

### 1.1 Создание базовых хелперов для адаптеров

**Задача**: Вынести общую логику Export/Import из 4 адаптеров

**Новые файлы**:
```
pkg/adapters/base/
  ├── export_helper.go      - Общая логика экспорта
  ├── import_helper.go      - Общая логика импорта
  ├── type_converter.go     - Универсальный конвертер типов
  └── query_context.go      - Создание QueryContext
```

**Интерфейсы**:

```go
// pkg/adapters/base/export_helper.go
type SchemaReader interface {
    GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error)
}

type DataReader interface {
    ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error)
    ReadRowsWithSQL(ctx context.Context, sql string, schema packet.Schema) ([][]string, error)
    GetRowCount(ctx context.Context, tableName string) (int64, error)
}

type ValueConverter interface {
    ConvertValueToTDTP(field packet.Field, value string) string
}

type ExportHelper struct {
    schemaReader   SchemaReader
    dataReader     DataReader
    valueConverter ValueConverter
}

func (h *ExportHelper) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)
func (h *ExportHelper) ExportTableWithQuery(...) ([]*packet.DataPacket, error)
func (h *ExportHelper) CreateQueryContext(...) *packet.QueryContext
```

**Миграция адаптеров**:

1. **SQLite**: `pkg/adapters/sqlite/adapter.go`
   - Добавить поле `exportHelper *base.ExportHelper`
   - Заменить `ExportTable()` → делегирование в `exportHelper.ExportTable()`
   - Заменить `ExportTableWithQuery()` → делегирование

2. **PostgreSQL**: `pkg/adapters/postgres/adapter.go`
   - Аналогично SQLite

3. **MS SQL Server**: `pkg/adapters/mssql/adapter.go`
   - Аналогично + специфика read-only полей

4. **MySQL**: `pkg/adapters/mysql/adapter.go`
   - Аналогично

**Эффект**: -800 строк кода, единая точка поддержки

**Оценка времени**: 3-4 дня

**Тесты**:
- Unit-тесты для `ExportHelper`
- Integration тесты для каждого адаптера (проверка обратной совместимости)

---

### 1.2 Централизация утилит

**Задача**: Вынести дублированные функции из diff/merge в core

**Новый файл**: `pkg/core/packet/utils.go`

**Функции**:

```go
// BuildRowKey создает ключ для строки на основе key-полей
func BuildRowKey(row []string, keyIndices []int) string

// ValidateSchemaCompatibility проверяет совместимость схем
func ValidateSchemaCompatibility(schemas []Schema) error

// FindKeyFieldIndices возвращает индексы key-полей в схеме
func FindKeyFieldIndices(schema Schema) []int

// CompareRows сравнивает две строки и возвращает индексы измененных полей
func CompareRows(row1, row2 []string, ignoreIndices []int) ([]int, bool)
```

**Рефакторинг модулей**:

1. **diff**: `pkg/diff/diff.go`
   - Удалить `buildKey()` → использовать `packet.BuildRowKey()`
   - Удалить `validateSchemas()` → использовать `packet.ValidateSchemaCompatibility()`

2. **merge**: `pkg/merge/merge.go`
   - Удалить `buildKey()` → использовать `packet.BuildRowKey()`
   - Удалить `validateSchemas()` → использовать `packet.ValidateSchemaCompatibility()`

**Эффект**: -150 строк, единая точка логики

**Оценка времени**: 1 день

**Тесты**:
- Unit-тесты для всех утилит
- Regression тесты для diff и merge

---

### 1.3 Универсальный конвертер типов

**Задача**: Заменить 4 реализации `convertValueToTDTP()` одной

**Новый файл**: `pkg/adapters/base/type_converter.go`

```go
type UniversalTypeConverter struct {
    converter *schema.Converter
}

func (c *UniversalTypeConverter) ConvertValueToTDTP(field packet.Field, value string) string {
    // Общая логика (уже есть в schema.Converter)
    fieldDef := schema.FieldDef{...}
    typedValue, _ := c.converter.ParseValue(value, fieldDef)
    return c.converter.FormatValue(typedValue)
}

// Специализированные конвертеры для специфичных типов СУБД
func (c *UniversalTypeConverter) PGValueToString(value interface{}, field packet.Field) string
func (c *UniversalTypeConverter) MSSQLValueToString(value interface{}, field packet.Field) string
```

**Рефакторинг адаптеров**:
- SQLite: использовать `UniversalTypeConverter`
- PostgreSQL: использовать `UniversalTypeConverter.PGValueToString()`
- MS SQL Server: использовать `UniversalTypeConverter.MSSQLValueToString()`
- MySQL: использовать `UniversalTypeConverter`

**Эффект**: -300 строк, консистентность конвертации

**Оценка времени**: 2 дня

---

## Фаза 2: Средние рефакторинги (Priority: MEDIUM)

### 2.1 AbstractFieldProcessor для процессоров

**Задача**: Вынести общую логику `Process()` из 3 процессоров

**Новый файл**: `pkg/processors/base_processor.go`

```go
type RuleApplier interface {
    ApplyRule(value string, rule interface{}) (string, error)
}

type AbstractFieldProcessor struct {
    name          string
    fieldsToProcess map[string]interface{}
    ruleApplier   RuleApplier
}

func (p *AbstractFieldProcessor) Process(ctx context.Context, data [][]string,
                                        schema packet.Schema) ([][]string, error) {
    // Общая логика (поиск индексов + обработка)
}
```

**Рефакторинг процессоров**:

1. **FieldMasker**: Только `ApplyRule()` метод
2. **FieldNormalizer**: Только `ApplyRule()` метод
3. **FieldValidator**: Только `ApplyRule()` метод

**Эффект**: -200 строк, упрощение новых процессоров

**Оценка времени**: 2 дня

---

### 2.2 SQL диалекты в TDTQL

**Задача**: Централизовать адаптацию SQL под разные СУБД

**Изменения**: `pkg/core/tdtql/sql_generator.go`

```go
type SQLDialect string

const (
    DialectStandard SQLDialect = "standard"  // LIMIT/OFFSET (SQLite, PostgreSQL, MySQL)
    DialectMSSQL    SQLDialect = "mssql"     // OFFSET/FETCH
    DialectOracle   SQLDialect = "oracle"    // ROWNUM (для будущего)
)

type SQLGenerator struct {
    dialect SQLDialect
}

func (g *SQLGenerator) GenerateSQL(tableName string, query *Query) (string, error) {
    // Базовый SQL
    sql := g.buildBaseSQL(tableName, query)

    // Адаптация под диалект
    switch g.dialect {
    case DialectMSSQL:
        sql = g.adaptForMSSQL(sql, query)
    case DialectOracle:
        sql = g.adaptForOracle(sql, query)
    default:
        // Standard SQL (LIMIT/OFFSET)
    }

    return sql, nil
}
```

**Рефакторинг адаптеров**:
- MS SQL Server: использовать `SQLGenerator{dialect: DialectMSSQL}`
- Остальные: использовать `SQLGenerator{dialect: DialectStandard}`

**Эффект**: Упрощение адаптации SQL, легкость добавления Oracle/DB2

**Оценка времени**: 2 дня

---

## Фаза 3: Архитектурные улучшения (Priority: LOW)

### 3.1 Рефакторинг Import логики

**Задача**: Вынести общую логику `ImportPacket()` и `ImportPackets()`

**Новый файл**: `pkg/adapters/base/import_helper.go`

```go
type TableManager interface {
    TableExists(ctx context.Context, tableName string) (bool, error)
    CreateTableFromSchema(ctx context.Context, tableName string, schema packet.Schema) error
}

type DataInserter interface {
    InsertRows(ctx context.Context, tableName string, schema packet.Schema,
              rows [][]string, strategy ImportStrategy) error
}

type ImportHelper struct {
    tableManager TableManager
    dataInserter DataInserter
}

func (h *ImportHelper) ImportPacket(ctx context.Context, pkt *packet.DataPacket,
                                   strategy ImportStrategy) error
func (h *ImportHelper) ImportPacketsInTransaction(ctx context.Context,
                                                  packets []*packet.DataPacket,
                                                  strategy ImportStrategy) error
```

**Эффект**: -600 строк кода

**Оценка времени**: 3 дня

---

### 3.2 Документация и примеры

**Задача**: Обновить документацию после рефакторинга

**Файлы для обновления**:
- `docs/ADAPTER_DEVELOPMENT_GUIDE.md` - Как создать новый адаптер
- `docs/PROCESSOR_DEVELOPMENT_GUIDE.md` - Как создать новый процессор
- `examples/custom_adapter/` - Пример кастомного адаптера
- `examples/custom_processor/` - Пример кастомного процессора

**Оценка времени**: 1-2 дня

---

## Метрики успеха

| Метрика | До рефакторинга | Целевое значение | Способ измерения |
|---------|----------------|------------------|------------------|
| Строк кода в адаптерах | ~4500 | ~2800 (-38%) | `tokei pkg/adapters/` |
| Строк кода в процессорах | ~900 | ~650 (-28%) | `tokei pkg/processors/` |
| Дублирование кода | ~33% | <10% | `gocyclo` или `dupl` |
| Цикломатическая сложность | 8-12 | <8 | `gocyclo` |
| Покрытие тестами | ~70% | >80% | `go test -cover` |
| Время добавления нового адаптера | ~500 строк | ~150 строк | Пример кастомного адаптера |

---

## Риски и миtigations

| Риск | Вероятность | Влияние | Mitigation |
|------|------------|---------|------------|
| Регрессии после рефакторинга | Средняя | Высокое | Полный набор integration тестов |
| Поломка обратной совместимости | Низкая | Высокое | Сохранить публичные API |
| Увеличение сложности для новичков | Средняя | Среднее | Хорошая документация + примеры |
| Затяжной рефакторинг | Средняя | Среднее | Инкрементальный подход (по фазам) |

---

## План реализации (Timeline)

### Спринт 1 (2 недели) - Фаза 1.1

- [ ] День 1-2: Создать `pkg/adapters/base/export_helper.go` + тесты
- [ ] День 3-4: Мигрировать SQLite адаптер + тесты
- [ ] День 5-6: Мигрировать PostgreSQL адаптер + тесты
- [ ] День 7-8: Мигрировать MS SQL Server адаптер + тесты
- [ ] День 9-10: Мигрировать MySQL адаптер + тесты

### Спринт 2 (1 неделя) - Фаза 1.2 и 1.3

- [ ] День 1-2: Создать `pkg/core/packet/utils.go` + рефакторинг diff/merge
- [ ] День 3-4: Создать `pkg/adapters/base/type_converter.go` + миграция адаптеров
- [ ] День 5: Полное тестирование Фазы 1

### Спринт 3 (1 неделя) - Фаза 2

- [ ] День 1-2: AbstractFieldProcessor + миграция процессоров
- [ ] День 3-4: SQL диалекты в TDTQL
- [ ] День 5: Тестирование Фазы 2

### Спринт 4 (1 неделя) - Фаза 3

- [ ] День 1-3: Import helper + миграция адаптеров
- [ ] День 4-5: Обновление документации и примеров

**Общая длительность**: ~5 недель (1 месяц)

---

## Критерии готовности (Definition of Done)

Для каждой фазы:

- [ ] Код написан и соответствует Go best practices
- [ ] Unit-тесты написаны (покрытие >80%)
- [ ] Integration тесты пройдены
- [ ] Benchmark тесты показывают не ухудшение производительности
- [ ] Code review пройден
- [ ] Документация обновлена
- [ ] Примеры работают
- [ ] Обратная совместимость сохранена (публичные API не изменены)

---

## Коммуникация

### Stakeholders

- **Команда разработки**: Исполнители рефакторинга
- **Пользователи фреймворка**: Уведомление о изменениях
- **Maintainers**: Ревью и одобрение

### Процесс

1. Создать GitHub Issues для каждой фазы
2. Pull Requests для каждого рефакторинга с детальным описанием
3. Changelog запись для каждого релиза
4. Migration guide для пользователей (если нужно)

---

## После рефакторинга

### Continuous Improvement

1. Настроить автоматические проверки дублирования (CI/CD)
2. Добавить `golangci-lint` с правилом `dupl`
3. Code review guidelines: проверять на дублирование
4. Периодический аудит кода (раз в квартал)

### Следующие шаги

После завершения рефакторинга:
1. Добавить новые адаптеры (Oracle, DB2) используя новые базовые классы
2. Расширить TDTQL (подзапросы, JOIN)
3. Улучшить производительность (streaming, pagination)

---

**Автор**: Claude Code
**Дата**: 2025-12-25
**Версия**: 1.0
