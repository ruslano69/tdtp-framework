# TDTP X-Ray - Validation Service

Сервис валидации SQL трансформаций для ETL конфигураций.

## Основные возможности

### 1️⃣ Обнаружение конфликтов имён колонок (Multi-Source)

При объединении данных из нескольких источников может возникнуть конфликт имён:

```sql
-- ❌ ПРОБЛЕМА:
SELECT
    [Users].[ID],      -- конфликт!
    [User2].[ID],      -- конфликт!
    [Users].[Name],    -- конфликт!
    [User2].[Name]     -- конфликт!
FROM [Users]
INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
```

**Validation Service автоматически обнаруживает и предлагает решение!**

### 2️⃣ Проверка синтаксиса CAST()

Детектирует типичные ошибки:

```sql
-- ❌ ОШИБКА: Запятая перед alias
CAST([Users].[Balance] AS INT) AS , [Users].[Balance_C]

-- ✅ ПРАВИЛЬНО:
CAST([Users].[Balance] AS INT) AS [Users].[Balance_C]
```

### 3️⃣ Генерация правильных CAST выражений

Автоматически создаёт CAST с префиксами и суффиксами:

```go
service.GenerateCastWithPrefix("Users", "Balance", "INT")
// Результат: CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
```

---

## API

### Go Backend (`validation_service.go`)

#### Структура ValidationResult

```go
type ValidationResult struct {
    Valid         bool              `json:"valid"`
    Conflicts     []ColumnConflict  `json:"conflicts"`
    CastErrors    []CastSyntaxError `json:"castErrors"`
    Warnings      []string          `json:"warnings"`
    ErrorMessages []string          `json:"errorMessages"`
}
```

#### Методы

**ValidateTransformationSQL(sql string) ValidationResult**

Полная валидация SQL трансформации:

```go
service := services.NewValidationService()
result := service.ValidateTransformationSQL(sqlCode)

if result.Valid {
    fmt.Println("✅ Validation passed!")
} else {
    for _, err := range result.ErrorMessages {
        fmt.Println("❌", err)
    }
}
```

**GenerateCastWithPrefix(table, column, targetType string) string**

Генерирует CAST выражение:

```go
cast := service.GenerateCastWithPrefix("Users", "Balance", "INT")
fmt.Println(cast)
// Output: CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
```

---

### JavaScript Frontend (`validation.js`)

#### Класс TransformationValidator

**Пример использования:**

```javascript
const validator = new TransformationValidator();

const sql = `
    SELECT
        [Users].[ID],
        [User2].[ID],
        CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
    FROM [Users]
    INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
`;

const result = validator.validate(sql);

if (result.valid) {
    console.log('✅ Validation passed!');
} else {
    console.log('❌ Errors:', result.errors);
    console.log('💡 Suggestions:', result.conflicts);
}
```

**Методы:**

- `validate(sql)` — полная валидация
- `findColumnConflicts(sql)` — поиск конфликтов
- `validateCastSyntax(sql)` — проверка CAST синтаксиса
- `checkNamingConventions(sql)` — проверка naming conventions
- `generateCast(table, column, type)` — генерация CAST выражения

---

## Интеграция в UI

### Валидация в реальном времени

```javascript
// В редакторе SQL (например, textarea)
const sqlEditor = document.getElementById('sqlEditor');

sqlEditor.addEventListener('input', (e) => {
    const sql = e.target.value;
    const result = validateSQLRealtime(sql);

    // Результат автоматически отображается в #validationResults
});
```

### Отображение результатов

```javascript
// Автоматическое отображение в HTML
displayValidationResults(result);
```

**Пример вывода:**

```html
<div class="validation-errors">
    <h3>🔴 Validation Failed</h3>
    <h4>Errors:</h4>
    <ul>
        <li class="error">❌ Column 'ID' conflicts between 'Users' and 'User2'</li>
        <li class="error">❌ Column 'Name' conflicts between 'Users' and 'User2'</li>
    </ul>
    <h4>Suggested Fixes:</h4>
    <ul>
        <li class="suggestion">
            💡 Use prefixes: [Users].[ID] AS [Users_ID], [User2].[ID] AS [User2_ID]
        </li>
        <li class="suggestion">
            💡 Use prefixes: [Users].[Name] AS [Users_Name], [User2].[Name] AS [User2_Name]
        </li>
    </ul>
</div>
```

---

## Примеры использования

### Пример 1: Валидация Multi-Source ETL

**Исходный SQL (с ошибками):**

```sql
SELECT
    [Users].[ID],
    [Users].[Balance],
    [User2].[ID],
    [User2].[Balance]
FROM [Users]
INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
```

**Результат валидации:**

```
❌ Column 'ID' conflicts between 'Users' and 'User2'
💡 Suggestion: [Users].[ID] AS [Users_ID], [User2].[ID] AS [User2_ID]

❌ Column 'Balance' conflicts between 'Users' and 'User2'
💡 Suggestion: [Users].[Balance] AS [Users_Balance], [User2].[Balance] AS [User2_Balance]
```

**Исправленный SQL:**

```sql
SELECT
    [Users].[ID] AS [Users_ID],
    CAST([Users].[Balance] AS INT) AS [Users_Balance_C],
    [User2].[ID] AS [User2_ID],
    CAST([User2].[Balance] AS DECIMAL(10,2)) AS [User2_Balance_C]
FROM [Users]
INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
```

### Пример 2: Автоматическая генерация CAST

**Go:**

```go
// Генерация CAST для всех числовых полей
casts := []string{
    service.GenerateCastWithPrefix("Users", "Balance", "INT"),
    service.GenerateCastWithPrefix("Orders", "Amount", "NUMERIC(10,2)"),
    service.GenerateCastWithPrefix("Products", "Price", "DECIMAL(8,2)"),
}

for _, cast := range casts {
    fmt.Println(cast)
}
```

**Результат:**

```
CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
CAST([Orders].[Amount] AS NUMERIC(10,2)) AS [Orders_Amount_C]
CAST([Products].[Price] AS DECIMAL(8,2)) AS [Products_Price_C]
```

---

## Naming Convention Rules

### Формат: `{SourceName}_{FieldName}[_C]`

| Часть | Описание | Пример |
|-------|----------|--------|
| `SourceName` | Имя таблицы/источника | `Users`, `Orders`, `Products` |
| `FieldName` | Имя поля | `ID`, `Balance`, `Amount` |
| `_C` | Суффикс для CAST полей | `_C` (только если был CAST) |

### Примеры:

- `Users_ID` — поле ID из Users (без CAST)
- `Users_Balance_C` — поле Balance из Users (с CAST)
- `Orders_Amount_C` — поле Amount из Orders (с CAST)
- `Products_Name` — поле Name из Products (без CAST)

---

## Testing

### Unit Tests (Go)

```bash
cd cmd/tdtp-xray/services
go test -v -run TestValidation
```

### Integration Tests (JavaScript)

```bash
cd cmd/tdtp-xray/frontend
npm test validation.test.js
```

---

## Roadmap

- [ ] **v1.0** — Базовая валидация конфликтов и CAST синтаксиса ✅
- [ ] **v1.1** — Интеграция с Visual Designer UI
- [ ] **v1.2** — Автоматическое исправление (auto-fix)
- [ ] **v1.3** — Поддержка сложных JOIN и подзапросов
- [ ] **v1.4** — Валидация типов данных (type checking)
- [ ] **v2.0** — AI-powered suggestions

---

## Contributing

См. [COMPUTED_FIELDS_ANALYSIS.md](../../../docs/tdtp-xray/COMPUTED_FIELDS_ANALYSIS.md) для понимания архитектуры.

## License

См. корневой LICENSE файл проекта.
