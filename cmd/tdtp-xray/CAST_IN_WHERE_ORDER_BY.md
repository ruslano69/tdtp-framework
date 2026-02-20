# CAST в WHERE и ORDER BY

## 🎯 Проблема: Weak Typing в SQLite

SQLite использует динамическую типизацию — одна колонка может хранить разные типы данных!

### Пример проблемы:

```sql
-- Таблица с varchar колонкой "amount"
CREATE TABLE orders (
    id INTEGER,
    amount TEXT  -- хранит "100", "500", "1000" как строки!
);

INSERT INTO orders VALUES (1, '100');
INSERT INTO orders VALUES (2, '500');
INSERT INTO orders VALUES (3, '1000');
```

#### ❌ БЕЗ CAST (неправильно):

```sql
-- Сортировка как строки:
SELECT * FROM orders ORDER BY amount;

-- Результат:
--  1 | '100'
--  3 | '1000'  ← "1000" идёт раньше "500" (строковая сортировка!)
--  2 | '500'
```

#### ✅ С CAST (правильно):

```sql
-- Сортировка как числа:
SELECT * FROM orders ORDER BY CAST(amount AS REAL);

-- Результат:
--  1 | '100'
--  2 | '500'
--  3 | '1000'  ← правильный порядок!
```

---

## 📋 Типы CAST для SQLite

| Тип | Когда использовать | Пример |
|-----|-------------------|--------|
| **STRING** | Текстовое сравнение/сортировка | `CAST(field AS STRING)` |
| **REAL** | Числа с плавающей точкой | `CAST(amount AS REAL)` |
| **INTEGER** | Целые числа | `CAST(count AS INTEGER)` |
| **NUMERIC** | Decimal/numeric значения | `CAST(price AS NUMERIC)` |
| **BLOB** | Бинарные данные | `CAST(data AS BLOB)` |

---

## 🔧 Использование в Visual Designer

### 1️⃣ CAST в WHERE (фильтры)

**UI:** Filter Builder → "CAST as Type (for WHERE)"

**Пример:**

```
Field: amount (type: varchar)
Operator: >
Value: 100
CAST as Type: REAL  ← добавляем!
```

**Генерируется SQL:**

```sql
WHERE CAST([Orders].[amount] AS REAL) > '100'
```

**Без CAST:**

```sql
WHERE [Orders].[amount] > '100'  -- строковое сравнение!
```

---

### 2️⃣ CAST в ORDER BY (сортировка)

**UI:** Filter Builder → "CAST as Type (for ORDER BY)"

**Пример:**

```
Field: amount (type: varchar)
Sort: ASC
CAST as Type: INTEGER  ← добавляем!
```

**Генерируется SQL:**

```sql
ORDER BY CAST([Orders].[amount] AS INTEGER) ASC
```

**Без CAST:**

```sql
ORDER BY [Orders].[amount] ASC  -- строковая сортировка!
```

---

## 📊 Примеры использования

### Пример 1: Числовая сортировка строковых ID

**Проблема:**

```sql
-- IDs хранятся как strings: "1", "2", "10", "20"
SELECT * FROM users ORDER BY user_id;

-- Результат:
-- "1"
-- "10"  ← раньше чем "2"!
-- "2"
-- "20"
```

**Решение:**

```
Field: user_id
Sort: ASC
CAST as Type: INTEGER
```

**Генерируется:**

```sql
SELECT * FROM users ORDER BY CAST(user_id AS INTEGER) ASC;

-- Результат:
-- 1
-- 2
-- 10
-- 20  ← правильный порядок!
```

---

### Пример 2: Фильтр по числовому значению в varchar поле

**Проблема:**

```sql
-- Balance хранится как varchar: "1000", "500", "100"
SELECT * FROM accounts WHERE balance > '500';

-- Результат: неправильный! (строковое сравнение)
-- "500" > "1000" = false
-- "500" > "500"  = false
-- "500" > "100"  = true
```

**Решение:**

```
Field: balance
Operator: >
Value: 500
CAST as Type: REAL
```

**Генерируется:**

```sql
SELECT * FROM accounts WHERE CAST(balance AS REAL) > '500';

-- Результат: правильный! (числовое сравнение)
-- 1000 > 500 = true
-- 500  > 500 = false
-- 100  > 500 = false
```

---

### Пример 3: Сортировка дат в строковом формате

**Проблема:**

```sql
-- Даты в varchar: "2024-01-15", "2024-02-20", "2024-01-03"
SELECT * FROM events ORDER BY event_date;

-- Результат (строковая сортировка):
-- "2024-01-03"
-- "2024-01-15"
-- "2024-02-20"  ← случайно правильно, но только для ISO формата!
```

**Решение:**

```
Field: event_date
Sort: DESC
CAST as Type: STRING  ← явная строковая сортировка
```

**Генерируется:**

```sql
SELECT * FROM events ORDER BY CAST(event_date AS STRING) DESC;

-- Результат (гарантированно правильный):
-- "2024-02-20"
-- "2024-01-15"
-- "2024-01-03"
```

**Альтернатива (если формат не ISO):**

Используй SQL функции для преобразования:

```sql
ORDER BY CAST(strftime('%s', event_date) AS INTEGER) DESC
```

---

## 🔄 Workflow в Visual Designer

### Шаг 1: Открыть Filter Builder

```
Canvas Design → Field → Click Filter Icon (🔽)
```

### Шаг 2: Настроить фильтр с CAST

```
Operator: >
Value: 100
CAST as Type: REAL  ← выбираем тип!
```

### Шаг 3: Настроить сортировку с CAST

```
Sort: ASC
CAST as Type: INTEGER  ← выбираем тип!
```

### Шаг 4: Apply

```
Click "Apply" → SQL генерируется автоматически!
```

**Результат:**

```sql
SELECT
    [Orders].[id],
    [Orders].[amount]
FROM [Orders]
WHERE CAST([Orders].[amount] AS REAL) > '100'
ORDER BY CAST([Orders].[amount] AS INTEGER) ASC
```

---

## 🎨 UI Компоненты

### Filter Builder Modal

```html
<!-- CAST для WHERE -->
<div>
    <label>CAST as Type (for WHERE):</label>
    <select id="filterCastType">
        <option value="">— No CAST (use original type)</option>
        <option value="STRING">STRING (text comparison)</option>
        <option value="REAL">REAL (floating point)</option>
        <option value="INTEGER">INTEGER (whole number)</option>
        <option value="NUMERIC">NUMERIC (decimal)</option>
        <option value="BLOB">BLOB (binary)</option>
    </select>
    <small>Apply type conversion for comparison (useful for SQLite weak typing)</small>
</div>

<!-- CAST для ORDER BY -->
<div>
    <label>CAST as Type (for ORDER BY):</label>
    <select id="filterSortCast">
        <option value="">— No CAST (use original type)</option>
        <option value="STRING">STRING (text sort)</option>
        <option value="REAL">REAL (numeric sort)</option>
        <option value="INTEGER">INTEGER (integer sort)</option>
        <option value="NUMERIC">NUMERIC (decimal sort)</option>
        <option value="BLOB">BLOB (binary sort)</option>
    </select>
    <small>Apply type conversion for sorting (e.g., sort "10" after "2" with INTEGER cast)</small>
</div>
```

---

## 🔢 Backend Implementation

### Go Structures (app.go)

```go
type FieldDesign struct {
    Name         string           `json:"name"`
    Type         string           `json:"type"`
    Filter       *FilterCondition `json:"filter,omitempty"`
    Sort         string           `json:"sort,omitempty"`      // ASC, DESC, ""
    SortCast     string           `json:"sortCast,omitempty"`  // CAST type for ORDER BY
}

type FilterCondition struct {
    Logic    string `json:"logic"`       // AND, OR
    Operator string `json:"operator"`    // =, >, <, etc.
    Value    string `json:"value"`
    Value2   string `json:"value2,omitempty"`
    CastType string `json:"castType,omitempty"` // CAST type for WHERE
}
```

### SQL Generation (app.go)

```go
// WHERE clause with CAST
fieldExpr := fmt.Sprintf("%s.%s", tableAlias, field.Name)

if filter.CastType != "" {
    fieldExpr = fmt.Sprintf("CAST(%s AS %s)", fieldExpr, filter.CastType)
}

condition := fmt.Sprintf("%s > '%s'", fieldExpr, filter.Value)

// ORDER BY clause with CAST
fieldExpr := fmt.Sprintf("%s.%s", tableAlias, field.Name)

if field.SortCast != "" {
    fieldExpr = fmt.Sprintf("CAST(%s AS %s)", fieldExpr, field.SortCast)
}

orderBy := fmt.Sprintf("%s %s", fieldExpr, field.Sort)
```

---

## 📝 JavaScript Frontend (wizard.js)

### openFilterBuilder()

```javascript
function openFilterBuilder(tableIndex, fieldIndex) {
    const field = canvasDesign.tables[tableIndex].fields[fieldIndex];
    const currentFilter = field.filter || { castType: '' };
    const currentSortCast = field.sortCast || '';

    // ... UI строится с dropdowns для castType и sortCast ...
}
```

### saveFilter()

```javascript
function saveFilter(tableIndex, fieldIndex) {
    const castType = document.getElementById('filterCastType').value;
    const sortCast = document.getElementById('filterSortCast').value;

    // Сохраняем в canvasDesign
    canvasDesign.tables[tableIndex].fields[fieldIndex].filter = {
        operator, value, castType  // ← добавили castType!
    };

    canvasDesign.tables[tableIndex].fields[fieldIndex].sortCast = sortCast;  // ← добавили sortCast!
}
```

---

## ✅ Тестирование

### Test Case 1: Numeric Sort

```javascript
// Данные:
const data = [
    { id: 1, amount: '1000' },
    { id: 2, amount: '500' },
    { id: 3, amount: '100' }
];

// SQL без CAST:
ORDER BY amount ASC
// Результат: "100", "1000", "500" ❌

// SQL с CAST:
ORDER BY CAST(amount AS INTEGER) ASC
// Результат: "100", "500", "1000" ✅
```

### Test Case 2: Numeric Filter

```javascript
// SQL без CAST:
WHERE amount > '500'
// Результат: только "500" > "1000", "100" (строковое сравнение) ❌

// SQL с CAST:
WHERE CAST(amount AS REAL) > '500'
// Результат: "1000" > 500 ✅
```

---

## 🚀 Roadmap

- [x] **v1.0** — CAST в WHERE clause ✅
- [x] **v1.0** — CAST в ORDER BY clause ✅
- [x] **v1.0** — UI для выбора типа CAST ✅
- [ ] **v1.1** — Автоопределение типа (smart suggestions)
- [ ] **v1.2** — Валидация совместимости типов
- [ ] **v1.3** — Поддержка кастомных CAST выражений
- [ ] **v2.0** — Multi-database CAST (MySQL, PostgreSQL, MSSQL)

---

## 📚 References

- [SQLite Type Affinity](https://www.sqlite.org/datatype3.html)
- [SQLite CAST Expression](https://www.sqlite.org/lang_expr.html#castexpr)
- [SQL Server CAST/CONVERT](https://learn.microsoft.com/en-us/sql/t-sql/functions/cast-and-convert-transact-sql)

---

## 🎯 Summary

**Проблема:** SQLite weak typing → неправильная сортировка и фильтрация
**Решение:** CAST в WHERE и ORDER BY
**UI:** Dropdown в Filter Builder
**Результат:** Правильная работа с числами, датами, строками! ✅
