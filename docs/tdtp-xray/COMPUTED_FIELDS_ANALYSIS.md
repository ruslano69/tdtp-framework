# Computed Fields — Killer Feature Analysis

**Дата:** 2026-02-20
**Статус:** 💭 Proposal & Analysis

---

## 🎯 Что такое Computed Fields?

**Вычисляемые поля** — это динамически вычисляемые колонки на основе выражений (expressions), функций и других полей.

### Примеры:

```sql
-- E-commerce
full_name = first_name || ' ' || last_name
discount_price = price * (1 - discount / 100)
total_with_tax = total * 1.20

-- HR / CRM
age = TIMESTAMPDIFF(YEAR, birth_date, CURDATE())
years_in_company = YEAR(NOW()) - YEAR(hire_date)
is_adult = age >= 18

-- Analytics
conversion_rate = (orders / visits) * 100
avg_order_value = revenue / orders
customer_segment = CASE
    WHEN revenue > 100000 THEN 'Enterprise'
    WHEN revenue > 10000 THEN 'Business'
    ELSE 'Individual'
END
```

---

## ✅ ЗА (Преимущества)

### 1. **Бизнес-логика без кода** 🚀
- Non-technical пользователи создают сложные вычисления
- Не нужно править SQL вручную
- Visual Expression Builder

**Impact:** ⭐⭐⭐⭐⭐

### 2. **Переиспользование формул** ♻️
- Создал формулу один раз → используется везде
- DRY принцип
- Saved formula library

**Impact:** ⭐⭐⭐⭐

### 3. **Быстрая аналитика** 📊
- On-the-fly вычисления
- A/B testing metrics
- KPI dashboards
- Preview данных без изменения БД

**Impact:** ⭐⭐⭐⭐⭐

### 4. **Конкурентное преимущество** 🏆

| Feature | Visual Designer | pgAdmin | DBeaver | MySQL Workbench | Power BI |
|---------|----------------|---------|---------|-----------------|----------|
| Computed Fields | 🚧 Proposal | ❌ | ⚠️ Limited | ❌ | ✅ |
| Expression Builder | 🚧 | ❌ | ❌ | ❌ | ✅ |
| Live Preview | 🚧 | ❌ | ✅ | ❌ | ✅ |

**Impact:** ⭐⭐⭐⭐

### 5. **Real-world Use Cases** 💼

#### E-commerce:
```javascript
{
  name: 'discount_price',
  expression: 'price * (1 - discount / 100)',
  type: 'number'
}
// Показывает цену со скидкой для каждого товара
```

#### HR / Recruiting:
```javascript
{
  name: 'candidate_score',
  expression: '(experience_years * 10) + (education_level * 5)',
  type: 'number'
}
// Рейтинг кандидата на основе опыта и образования
```

#### Logistics:
```javascript
{
  name: 'delivery_days',
  expression: 'DATEDIFF(delivered_at, ordered_at)',
  type: 'number'
}
// Время доставки в днях
```

**Impact:** ⭐⭐⭐⭐⭐

### 6. **Не изменяет БД** ✅
- Только визуальный слой
- БД остаётся неизменной
- Безопасно для production

**Impact:** ⭐⭐⭐⭐⭐

---

## ❌ ПРОТИВ (Риски и сложности)

### 1. **Сложность реализации** ⚠️

#### UI Challenges:
- ✅ Expression Builder (dropdown для функций)
- ✅ Field picker (autocomplete)
- ✅ Syntax validation (real-time)
- ✅ Type checking (number + string = ?)
- ✅ Live preview (first 10 rows)

**Оценка сложности:** 🔴 High (3-5 дней MVP)

#### Backend Challenges:
- ✅ Safe expression parsing
- ✅ SQL injection prevention ⚠️⚠️⚠️
- ✅ Multi-DB support (PostgreSQL, MySQL, SQLite, SQL Server)
- ✅ Type inference
- ✅ Error handling

**Оценка сложности:** 🔴 High (2-3 дня)

### 2. **🔴 БЕЗОПАСНОСТЬ (КРИТИЧНО!)**

#### SQL Injection Risk:
```javascript
// ❌ ОПАСНО - user input напрямую в SQL
expression: "'; DROP TABLE users; --"

// ✅ БЕЗОПАСНО - whitelist подход
allowedFunctions: ['CONCAT', 'UPPER', 'LOWER', 'ROUND', 'ABS']
allowedOperators: ['+', '-', '*', '/', '||']
allowedFields: ['first_name', 'last_name', 'price', ...] // только существующие
```

#### Решения:
1. **Whitelist only** — разрешены только определённые функции
2. **AST parsing** — парсим в дерево, валидируем каждый узел
3. **No raw SQL** — генерируем SELECT через параметризованные запросы
4. **Sandbox execution** — ограничение прав, timeout
5. **Input sanitization** — очистка от опасных символов

**Risk Level:** 🔴🔴🔴 Critical (но решаемо!)

### 3. **Производительность** ⚡

#### Проблемы:
- Вычисления на каждой строке (N операций)
- Сложные формулы → медленный query
- Нет индексов на computed fields

#### Примерный impact:
```sql
-- 10,000 rows × простая формула (concat)
SELECT first_name || ' ' || last_name as full_name FROM users;
-- ~50-100ms ✅

-- 10,000 rows × сложная формула (CASE, math)
SELECT CASE WHEN ... THEN ... END as segment FROM users;
-- ~200-500ms ⚠️

-- 1,000,000 rows × любая формула
-- ~5-30 секунд 🔴 (нужен LIMIT!)
```

#### Решения:
- **LIMIT по умолчанию** для preview (10-100 rows)
- **Warning** если результат > 1000 rows
- **Estimated cost** индикатор
- **Async execution** с прогресс-баром

**Risk Level:** 🟡 Medium (решаемо через LIMIT)

### 4. **Сопровождаемость** 🛠️

#### Проблемы:
- Circular dependencies (A зависит от B, B от A)
- Breaking changes (переименовали поле → формулы сломались)
- Debugging сложных формул
- Version control (как сохранять историю формул?)

#### Решения:
- **Dependency graph** — показывать зависимости
- **Validation** — проверка циклических ссылок
- **Test mode** — preview без сохранения
- **Formula history** — git-like версионность

**Risk Level:** 🟡 Medium

### 5. **TDTQL совместимость** ❓

#### Вопрос: Поддерживает ли TDTQL computed fields?

**Результат проверки:**
- ❌ В текущей документации TDTQL НЕТ упоминания computed fields
- ⚠️ Упоминаются только в контексте SQL Server (read-only computed columns)
- ❓ Нет спецификации XML формата для выражений

#### Варианты:
1. **Расширить TDTQL spec** (новая фича)
2. **Local-only** (только в Visual Designer, не сохраняется в TDTQL)
3. **Альтернатива:** Использовать Views (но это изменяет БД)

**Risk Level:** 🟡 Medium (нужно согласование)

### 6. **Scope Creep** 📈

#### Пользователи захотят:
- Подзапросы (subqueries) 🔴
- Агрегации (SUM, AVG) 🔴
- Window functions (ROW_NUMBER) 🔴
- Joins в формулах 🔴
- Custom JavaScript expressions 🔴🔴🔴

**Где остановиться?**

**Risk Level:** 🟡 Medium (нужны чёткие границы MVP)

---

## 💡 MVP Proposal — Минимальная версия

### Phase 1: Basic Computed Fields (MVP)

#### Scope (что РАЗРЕШЕНО):

**1. Арифметика:**
- `+`, `-`, `*`, `/`, `%` (модуло)

**2. String операции:**
- `||` (concat) или `CONCAT(field1, ' ', field2)`
- `UPPER(field)`, `LOWER(field)`, `TRIM(field)`
- `LENGTH(field)`, `SUBSTR(field, start, len)`

**3. Math функции:**
- `ROUND(value, decimals)`
- `ABS(value)`
- `CEIL(value)`, `FLOOR(value)`

**4. Type casting:**
- `CAST(field AS INTEGER)`
- `CAST(field AS TEXT)`

**5. Ссылки:**
- Только поля **из той же таблицы**
- Format: `field_name` или `table.field_name`

**6. Константы:**
- Числа: `42`, `3.14`
- Строки: `'Hello'`, `'World'`

#### Что НЕ РАЗРЕШЕНО (для безопасности):
- ❌ Subqueries
- ❌ Агрегации (SUM, COUNT, AVG)
- ❌ Window functions
- ❌ Joins / Cross-table refs
- ❌ CASE WHEN (пока, в Phase 2)
- ❌ Date functions (пока, в Phase 2)
- ❌ Любые функции вне whitelist

---

### UI Design (MVP)

#### 1. Add Computed Field Button:
```
┌─────────────────────────────────────┐
│ Table: users                        │
│ ┌─────────────────────────────────┐ │
│ │ [+] Add Computed Field          │ │
│ └─────────────────────────────────┘ │
│                                     │
│ Fields:                             │
│ ☑ id           (PK)                 │
│ ☑ first_name   [filter]             │
│ ☑ last_name    [filter]             │
│ ☑ price        [filter]             │
│ 🧮 full_name    (computed)  [edit]  │
│ 🧮 total_price  (computed)  [edit]  │
└─────────────────────────────────────┘
```

#### 2. Expression Builder Modal:
```
┌───────────────────────────────────────────────────┐
│ Add Computed Field                                │
├───────────────────────────────────────────────────┤
│ Field Name:                                       │
│ [full_name                                    ]   │
│                                                   │
│ Expression:                                       │
│ [first_name || ' ' || last_name               ]   │
│                                                   │
│ 💡 Available:                                     │
│ - Fields: [first_name▾] [last_name▾] [age▾]      │
│ - Functions: [CONCAT▾] [UPPER▾] [LOWER▾]          │
│ - Operators: [+] [-] [*] [/] [||]                │
│                                                   │
│ ✅ Syntax: Valid                                  │
│ 🔍 Type: text (inferred)                          │
│                                                   │
│ Preview (first 10 rows):                          │
│ ┌─────────────┬──────────┬──────────────┐         │
│ │ first_name  │ last_name│ full_name    │         │
│ ├─────────────┼──────────┼──────────────┤         │
│ │ John        │ Doe      │ John Doe     │         │
│ │ Jane        │ Smith    │ Jane Smith   │         │
│ └─────────────┴──────────┴──────────────┘         │
│                                                   │
│ [Preview] [Save] [Cancel]                         │
└───────────────────────────────────────────────────┘
```

#### 3. Visual Indicators:
- 🧮 icon для computed fields
- Gray italic text (отличие от обычных полей)
- Tooltip: "Computed: first_name || ' ' || last_name"
- [Edit] кнопка для изменения формулы

---

### Implementation Plan

#### Frontend (wizard.js):

**1. Data Structure:**
```javascript
{
  sourceName: 'users',
  fields: [
    { name: 'first_name', type: 'text', visible: true },
    { name: 'last_name', type: 'text', visible: true },
    // Computed field
    {
      name: 'full_name',
      type: 'text',
      visible: true,
      isComputed: true,
      expression: "first_name || ' ' || last_name",
      dependencies: ['first_name', 'last_name']
    }
  ]
}
```

**2. Functions:**
```javascript
function addComputedField(tableIndex) { ... }
function openExpressionBuilder(tableIndex, fieldIndex) { ... }
function validateExpression(expr, availableFields) { ... }
function previewComputedField(tableIndex, expression) { ... }
```

**3. Expression Parser (simple):**
```javascript
function parseExpression(expr) {
  // Tokenize
  // Validate against whitelist
  // Check field references exist
  // Infer type
  return { valid: true, type: 'text', dependencies: [...] }
}
```

#### Backend (если нужно):

**Option 1: Frontend-only (проще)**
- Computed fields только для визуального preview
- Генерируем `SELECT ..., (expression) AS computed_field FROM ...`
- Не сохраняется в TDTQL XML (пока)

**Option 2: Backend support**
- Добавить поддержку в GenerateSQL()
- Сохранять в TDTQL как extension:
```xml
<Field name="full_name" computed="true">
  <Expression>first_name || ' ' || last_name</Expression>
</Field>
```

---

## 🎯 Рекомендация

### ✅ **СТОИТ ДЕЛАТЬ**, если:

1. ✅ **MVP scope чёткий** — только базовые операции (Phase 1)
2. ✅ **Безопасность приоритет** — whitelist, AST parsing, sandbox
3. ✅ **Frontend-only** (для начала) — не меняем TDTQL spec сразу
4. ✅ **Preview-focused** — помогает анализировать данные, не меняет БД
5. ✅ **User demand** — есть реальные use cases

### ❌ **НЕ СТОИТ ДЕЛАТЬ**, если:

1. ❌ Нет времени на **качественную реализацию** (спешка = баги)
2. ❌ **Безопасность** не можем гарантировать
3. ❌ Use cases **не оправдывают сложность**
4. ❌ TDTQL spec **против расширений**

---

## 🚦 Альтернативы (проще реализовать)

### 1. **Alias Fields** ⭐
Просто переименование:
```
user_email → "Email Address"
created_at → "Registration Date"
```

**Сложность:** 🟢 Low (1 час)
**Impact:** ⭐⭐

### 2. **Format Templates** ⭐⭐
Форматирование без вычислений:
```
phone: +7 (XXX) XXX-XX-XX
date: DD.MM.YYYY
price: $XXX.XX USD
```

**Сложность:** 🟡 Medium (3 часа)
**Impact:** ⭐⭐⭐

### 3. **Quick Formulas Library** ⭐⭐⭐
Предустановленные формулы (без ручного ввода):
```
[✓] Full Name (first + last)
[✓] Age from birth_date
[✓] Days since registration
[✓] Price with 20% tax
```

**Сложность:** 🟡 Medium (1 день)
**Impact:** ⭐⭐⭐⭐

---

## 📊 Финальная матрица решений

| Критерий | Weight | Score (1-5) | Weighted |
|----------|--------|-------------|----------|
| **User Value** | 30% | ⭐⭐⭐⭐⭐ (5) | 1.5 |
| **Implementation Cost** | 25% | ⭐⭐ (2) | 0.5 |
| **Security Risk** | 20% | ⭐⭐⭐ (3) | 0.6 |
| **TDTQL Compatibility** | 15% | ⭐⭐ (2) | 0.3 |
| **Maintenance** | 10% | ⭐⭐⭐ (3) | 0.3 |
| **Total** | 100% | — | **3.2/5** |

**Интерпретация:**
- 4.0+ = 🟢 Go ahead!
- 3.0-4.0 = 🟡 Consider carefully (наш случай)
- <3.0 = 🔴 Don't do it

---

## 🎬 Итоговое решение

### ✅ **ПРИНЯТО: Вариант C — НЕ ДЕЛАТЬ** (архитектурно верно)

**Обоснование:**

#### 🏗️ TDTP Architecture Philosophy

В архитектуре TDTP вычисляемые поля **намеренно НЕ предусмотрены** по следующим причинам:

**1. Разделение ответственности (Separation of Concerns):**

```
┌─────────────┐     ┌─────────┐     ┌─────────────┐
│   Source    │ →   │   ETL   │ →   │   Target    │
│   (Views)   │     │Pipeline │     │   System    │
└─────────────┘     └─────────┘     └─────────────┘
      ↓                  ↓                 ↓
 Сырые данные     Трансформация      Готовые данные
 + SQL views      Вычисления         для потребления
 Бизнес-логика    Агрегации
```

**2. Views отвечают за бизнес-логику:**

```sql
-- ❌ НЕПРАВИЛЬНО: вычисления в UI
SELECT quantity, reserve FROM warehouse;
-- UI вычисляет: available = quantity - reserve

-- ✅ ПРАВИЛЬНО: вычисления в view
CREATE VIEW warehouse_stock AS
SELECT
    quantity,
    reserve,
    quantity - reserve AS available_stock  -- бизнес-логика здесь!
FROM warehouse;

-- UI просто показывает:
SELECT * FROM warehouse_stock;
```

**⚡ Ключевой момент: TDTP не знает о вычислениях!**

При экспорте view в TDTP, вычисляемые поля выглядят как обычные:

```xml
<!-- TDTP Export из warehouse_stock view -->
<Source name="warehouse_stock" type="view">
  <Field name="quantity" type="int"/>
  <Field name="reserve" type="int"/>
  <Field name="available_stock" type="int"/>  <!-- выглядит как обычное поле! -->
</Source>
```

**Это правильная абстракция!** TDTP работает на уровне **результирующей схемы**, а не реализации.

Visual Designer видит просто три поля и не знает (и не должен знать!) что `available_stock` вычисляемое:
```
warehouse_stock:
  ☑ quantity
  ☑ reserve
  ☑ available_stock  ← не знает что computed, просто поле!
```

**3. ETL Pipeline для трансформаций:**

```
Source View (сырые данные):
┌──────────┬─────────┐
│ quantity │ reserve │
├──────────┼─────────┤
│ 100      │ 20      │
│ 50       │ 10      │
└──────────┴─────────┘

      ↓ ETL трансформация

Target System (готовые данные):
┌──────────┬─────────┬──────────────────┐
│ quantity │ reserve │ available_stock  │
├──────────┼─────────┼──────────────────┤
│ 100      │ 20      │ 80               │
│ 50       │ 10      │ 40               │
└──────────┴─────────┴──────────────────┘
```

**4. Проблемы если добавить computed fields в UI:**

❌ **Бизнес-логика размазывается:**
- Часть в views
- Часть в ETL
- Часть в UI ← НЕТ!

❌ **Нет single source of truth:**
- Формула `available = quantity - reserve` живёт в UI
- Другие системы не знают об этой логике
- Дублирование логики в разных местах

❌ **Сложность поддержки:**
- Изменилась формула → нужно менять в UI
- Нет версионности
- Нет тестирования бизнес-логики

✅ **Правильный путь:**
- **Views содержат ВСЕ нужные поля** (включая вычисляемые)
- **Visual Designer только показывает** что есть
- **ETL делает трансформации** между системами
- **Бизнес-логика в одном месте** (SQL view/stored proc)

---

### ⚙️ НО добавляем CAST() в ETL для миграций

**Разделение ответственности:**
- **Computed Fields** = бизнес-логика (формулы, правила) → остаются в SQL views ✅
- **CAST()** = утилитарная трансформация (конвертация типов) → добавляем в ETL ✅

**Зачем CAST() в ETL:**
- Разные СУБД имеют разные типы данных (SQL Server `money` ≠ PostgreSQL `numeric`)
- При миграции между системами нужна конвертация типов
- Автоматический суффикс `_C` показывает что это cast field
- Упрощает миграцию систем с ограничениями типов

#### Примеры использования CAST():

**1. Миграция SQL Server → PostgreSQL:**
```yaml
# etl/sqlserver_to_postgres.yaml
name: migrate_orders

source:
  type: sqlserver
  connection: source_db
  query: "SELECT order_id, order_date, amount, description FROM orders"

transformation:
  sql: |
    SELECT
      order_id,
      CAST(order_date AS timestamp) AS order_date_C,  -- datetime → timestamp
      CAST(amount AS numeric(10,2)) AS amount_C,      -- money → numeric
      CAST(description AS text) AS description        -- nvarchar → text (auto)
    FROM source_data

target:
  type: postgresql
  connection: target_db
  table: orders_target
```

**Результат в Target DB:**
```sql
-- PostgreSQL
CREATE TABLE orders_target (
  order_id INT,
  order_date_C TIMESTAMP,  -- cast!
  amount_C NUMERIC,        -- cast!
  description TEXT         -- auto
);
```

**2. Миграция Oracle → MySQL:**
```yaml
# etl/oracle_to_mysql.yaml
name: migrate_employees

source:
  type: oracle
  connection: oracle_db
  query: "SELECT emp_id, salary, hire_date, notes FROM employees"

transformation:
  sql: |
    SELECT
      CAST(emp_id AS int) AS emp_id_C,              -- NUMBER → int
      CAST(salary AS decimal(10,2)) AS salary_C,    -- NUMBER(10,2) → decimal
      hire_date,                                     -- DATE остаётся
      CAST(notes AS text) AS notes_C                -- CLOB → text
    FROM source_data

target:
  type: mysql
  connection: mysql_db
  table: employees_target
```

**3. Упрощение работы с legacy системами:**
```yaml
# etl/legacy_normalization.yaml
name: normalize_legacy_types

source:
  type: postgresql  # или любая БД
  connection: legacy_db
  query: |
    SELECT
      id,          -- varchar(10) но хранит числа!
      amount,      -- varchar(20) но хранит decimals!
      created_at   -- varchar(30) но хранит даты!
    FROM legacy_data

transformation:
  sql: |
    SELECT
      -- Исправляем типы:
      CAST(id AS int) AS id_C,                      -- varchar → int
      CAST(amount AS decimal(10,2)) AS amount_C,    -- varchar → decimal
      CAST(created_at AS timestamp) AS created_at_C -- varchar → timestamp
    FROM source_data

target:
  type: postgresql
  connection: normalized_db
  table: normalized_data
```

**Результат в Target DB:**
```sql
CREATE TABLE normalized_data (
  id_C INT,               -- ✅ можем фильтровать по числу
  amount_C DECIMAL,       -- ✅ можем суммировать
  created_at_C TIMESTAMP  -- ✅ можем сортировать по дате
);
```

#### Преимущества суффикса `_C`:

✅ **Явность:**
- Видно что поле прошло CAST
- Понятно что это не оригинальное поле из source

✅ **Без конфликтов:**
- Оригинальное поле: `order_date` (datetime)
- Cast поле: `order_date_C` (timestamp)
- Можно оставить оба если нужно!

✅ **Совместимость:**
```sql
-- В target системе можно выбрать что использовать:
SELECT order_date_C FROM orders;  -- используем cast версию

-- Или маппинг на оригинальное имя:
CREATE VIEW orders_view AS
SELECT
    order_id,
    order_date_C AS order_date,  -- alias убирает суффикс
    amount_C AS amount
FROM orders_target;
```

✅ **Упрощение миграции:**
- Не нужно вручную писать CAST в каждом запросе
- ETL автоматически генерирует правильные типы
- Visual Designer видит правильные типы для фильтрации

#### Отличие от Computed Fields:

| Аспект | Computed Fields ❌ | CAST() ✅ |
|--------|-------------------|-----------|
| **Назначение** | Бизнес-логика (формулы) | Утилитарная трансформация (типы) |
| **Примеры** | `price * (1 - discount/100)` | `CAST(price AS decimal)` |
| **Где живёт** | SQL view (правильно) <br>или UI (неправильно) | ETL pipeline |
| **Реализация** | НЕ добавляем в UI/ETL | ✅ Добавляем в ETL |
| **Когда использовать** | Создать SQL view с формулой | Миграция между СУБД |
| **Риски** | Дублирование логики | Минимальные (просто конверсия типов) |

---

### 🔥 Multi-Source Naming Convention (критический раздел!)

**ПРОБЛЕМА:** При объединении данных из нескольких источников возникают **конфликты имён**!

#### ❌ Пример проблемы:

```sql
-- До CAST: Объединение Users + User2
SELECT
    [Users].[ID],        -- ID #1
    [Users].[Name],      -- Name #1
    [Users].[Balance],   -- Balance #1
    [User2].[ID],        -- ID #2 - КОНФЛИКТ!
    [User2].[Name],      -- Name #2 - КОНФЛИКТ!
    [User2].[Balance]    -- Balance #2 - КОНФЛИКТ!
FROM [Users]
INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
WHERE [Users].[Balance] > 0
```

**Результат:**
```
ERROR: duplicate column names 'ID', 'Name', 'Balance'
```

#### ❌ Пример синтаксической ошибки CAST:

```sql
-- НЕПРАВИЛЬНО: запятая не на месте!
CAST([Users].[Balance] AS INT) AS , [Users].[Balance_C]
                                  ↑ ошибка!
```

#### ✅ РЕШЕНИЕ: Префикс источника + суффикс CAST

**Формат:** `{SourceName}_{FieldName}[_C]`

Где:
- **SourceName** = имя таблицы/источника (Users, User2, Orders...)
- **FieldName** = оригинальное имя поля (ID, Name, Balance...)
- **_C** = суффикс CAST (только если был CAST)

**Примеры:**

| Источник | Поле | Операция | Результирующее имя | Описание |
|----------|------|----------|-------------------|----------|
| Users | ID | нет CAST | `Users_ID` | Простой префикс |
| Users | Balance | CAST → INT | `Users_Balance_C` | Префикс + суффикс |
| User2 | ID | нет CAST | `User2_ID` | Префикс |
| User2 | Balance | CAST → DECIMAL | `User2_Balance_C` | Префикс + суффикс |
| Orders | Amount | CAST → NUMERIC | `Orders_Amount_C` | Префикс + суффикс |

#### ✅ Правильный SQL с CAST и префиксами:

```sql
SELECT
    -- Source 1: Users (префикс: Users_)
    [Users].[ID] AS [Users_ID],
    [Users].[Name] AS [Users_Name],
    [Users].[City] AS [Users_City],
    CAST([Users].[Balance] AS INT) AS [Users_Balance_C],     -- CAST!
    [Users].[IsActive] AS [Users_IsActive],
    [Users].[RegisteredAt] AS [Users_RegisteredAt],

    -- Source 2: User2 (префикс: User2_)
    [User2].[ID] AS [User2_ID],
    [User2].[Name] AS [User2_Name],
    [User2].[Email] AS [User2_Email],
    CAST([User2].[Balance] AS DECIMAL(10,2)) AS [User2_Balance_C]  -- CAST!

FROM [Users]
INNER JOIN [User2] ON [Users].[ID] = [User2].[ID]
WHERE
    [Users].[Balance] > 0
    AND [Users].[IsActive] = 1
```

**Результат (без конфликтов!):**
```
Users_ID | Users_Name | Users_Balance_C | User2_ID | User2_Name | User2_Balance_C
---------|------------|-----------------|----------|------------|----------------
   1     |   Alice    |      1000       |    1     |   Alice    |     1000.00
   2     |   Bob      |       500       |    2     |   Bobby    |      500.50
```

#### 📋 YAML Config для Multi-Source ETL:

```yaml
# etl/multi_source_pipeline.yaml
name: merge_users_data

sources:
  - name: Users
    type: sqlserver
    connection: db1
    query: "SELECT ID, Name, City, Balance, IsActive, RegisteredAt FROM Users"

  - name: User2
    type: sqlserver
    connection: db2
    query: "SELECT ID, Name, Email, Balance FROM User2"

transformation:
  sql: |
    SELECT
      -- Source 1: Users (префикс Users_)
      u1.ID AS Users_ID,
      u1.Name AS Users_Name,
      u1.City AS Users_City,
      CAST(u1.Balance AS INT) AS Users_Balance_C,
      u1.IsActive AS Users_IsActive,
      u1.RegisteredAt AS Users_RegisteredAt,

      -- Source 2: User2 (префикс User2_)
      u2.ID AS User2_ID,
      u2.Name AS User2_Name,
      u2.Email AS User2_Email,
      CAST(u2.Balance AS DECIMAL(10,2)) AS User2_Balance_C

    FROM Users u1
    INNER JOIN User2 u2 ON u1.ID = u2.ID
    WHERE
      u1.Balance > 0
      AND u1.IsActive = 1

target:
  type: postgresql
  connection: target_db
  table: merged_users
```

#### 📊 TDTP Schema для Multi-Source результата:

```xml
<!-- tdtp/merged_users.xml -->
<Source name="merged_users">
  <!-- Users columns -->
  <Field name="Users_ID" type="int" source="Users"/>
  <Field name="Users_Name" type="varchar" source="Users"/>
  <Field name="Users_City" type="varchar" source="Users"/>
  <Field name="Users_Balance_C" type="int" source="Users" cast="true"/>
  <Field name="Users_IsActive" type="bit" source="Users"/>
  <Field name="Users_RegisteredAt" type="datetime" source="Users"/>

  <!-- User2 columns -->
  <Field name="User2_ID" type="int" source="User2"/>
  <Field name="User2_Name" type="varchar" source="User2"/>
  <Field name="User2_Email" type="varchar" source="User2"/>
  <Field name="User2_Balance_C" type="decimal" source="User2" cast="true"/>
</Source>
```

#### 🎯 Ключевые правила для Multi-Source:

##### 1️⃣ Всегда используй префиксы источников:

```sql
-- ❌ BAD: Конфликт имён!
SELECT ID, Name FROM Users
UNION
SELECT ID, Name FROM User2
-- ERROR: duplicate column names!

-- ✅ GOOD: Уникальные имена!
SELECT
  ID AS Users_ID,
  Name AS Users_Name
FROM Users
UNION
SELECT
  ID AS User2_ID,
  Name AS User2_Name
FROM User2
```

##### 2️⃣ CAST с префиксом и суффиксом:

```sql
-- Формула: {Source}_{Field}_C
CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
CAST([User2].[Balance] AS DECIMAL) AS [User2_Balance_C]
CAST([Orders].[Amount] AS NUMERIC) AS [Orders_Amount_C]
```

##### 3️⃣ WHERE условия с оригинальными типами:

```sql
-- ✅ Фильтруем ДО CAST (эффективнее!)
WHERE
    [Users].[Balance] > 0           -- оригинальный тип
    AND [Users].[IsActive] = 1      -- оригинальный тип

-- Не нужно:
WHERE Users_Balance_C > 0  -- ❌ поле ещё не существует!
```

##### 4️⃣ Атрибут source в TDTP Schema (опционально):

```xml
<!-- Указываем откуда пришло поле -->
<Field name="Users_Balance_C" type="int" source="Users" cast="true"/>
<Field name="User2_Balance_C" type="decimal" source="User2" cast="true"/>
```

**Преимущества:**
- ✅ Трассировка: видно из какого источника поле
- ✅ Документация: понятна структура merge
- ✅ Валидация: можно проверить что все источники учтены

#### 📈 Сравнение подходов:

| Подход | Пример | Конфликты? | CAST? | Читаемость |
|--------|--------|------------|-------|------------|
| **Без префиксов** | `ID`, `Name` | ❌ ДА | ❌ | ⭐⭐⭐⭐⭐ |
| **Только префикс** | `Users_ID`, `User2_ID` | ✅ НЕТ | ❌ | ⭐⭐⭐⭐ |
| **Префикс + суффикс _C** | `Users_Balance_C`, `User2_Balance_C` | ✅ НЕТ | ✅ ДА | ⭐⭐⭐⭐⭐ |

**Вывод:** Используй **префикс + суффикс _C** для максимальной ясности!

---

#### ✅ ПРАВИЛЬНАЯ архитектура: ETL Config (YAML) + SQL Transformation

**ВАЖНО:** Трансформация живёт в **SQL коде**, который хранится в **YAML конфиге**!

```yaml
# etl/pipeline_config.yaml
name: migrate_orders_sqlserver_to_postgres

source:
  type: sqlserver
  connection: source_db
  query: |
    SELECT
      order_id,
      order_date,
      amount,
      quantity,
      notes
    FROM orders

transformation:
  # Трансформация — это SQL код!
  sql: |
    SELECT
      order_id,
      -- SQL Server datetime → PostgreSQL timestamp
      CAST(order_date AS timestamp) AS order_date_C,
      -- SQL Server money → PostgreSQL numeric
      CAST(amount AS numeric(10,2)) AS amount_C,
      -- SQL Server smallint → PostgreSQL int
      CAST(quantity AS int) AS quantity_C,
      -- SQL Server nvarchar → PostgreSQL text
      CAST(notes AS text) AS notes_C
    FROM source_data

target:
  type: postgresql
  connection: target_db
  table: orders_target
  mode: replace  # или append, upsert
```

**Ключевые моменты:**
- ✅ **YAML** — конфигурация ETL (где читать, куда писать)
- ✅ **SQL** — трансформация (CAST и другие операции)
- ✅ **Суффикс `_C`** — прямо в SQL (AS order_date_C)
- ✅ Трансформация выполняется **В БД** (эффективно!)

**Альтернатива: Python pandas/polars:**
```yaml
# etl/pipeline_config.yaml (если используем Python)
transformation:
  type: python
  script: |
    import pandas as pd

    # Конвертация типов в pandas
    df['order_date_C'] = pd.to_datetime(df['order_date'])
    df['amount_C'] = df['amount'].astype('float64')
    df['quantity_C'] = df['quantity'].astype('int32')
    df['notes_C'] = df['notes'].astype('str')
```

**Результат:**
- ✅ Конвертация типов в SQL/Python
- ✅ Явность через суффикс `_C`
- ✅ Версионность (YAML в git)
- ✅ Документация встроена в YAML

---

### 🔑 Принцип абстракции TDTP

**ВАЖНО:** TDTP спецификацию **НЕ ТРОГАЕМ** при использовании computed fields!

#### Как это работает:

```
┌─────────────────────────────────────────────────────┐
│  1. SQL Layer (реализация):                        │
├─────────────────────────────────────────────────────┤
│  CREATE VIEW warehouse_stock AS                     │
│  SELECT                                             │
│      quantity,                                      │
│      reserve,                                       │
│      quantity - reserve AS available_stock  ← вычисл│
│  FROM warehouse;                                    │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  2. TDTP Layer (абстракция):                        │
├─────────────────────────────────────────────────────┤
│  <Source name="warehouse_stock">                    │
│    <Field name="quantity" type="int"/>              │
│    <Field name="reserve" type="int"/>               │
│    <Field name="available_stock" type="int"/>       │
│  </Source>                                          │
│                                                     │
│  ☑ available_stock выглядит как ОБЫЧНОЕ поле!      │
│  ☑ TDTP не знает (и не должен знать) о вычислениях │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  3. Visual Designer (UI):                           │
├─────────────────────────────────────────────────────┤
│  warehouse_stock:                                   │
│    ☑ quantity         [filter]                      │
│    ☑ reserve          [filter]                      │
│    ☑ available_stock  [filter]  ← просто поле!     │
│                                                     │
│  ☑ UI видит три обычных поля                       │
│  ☑ Может фильтровать, сортировать любое из них     │
└─────────────────────────────────────────────────────┘
```

#### Преимущества этого подхода:

✅ **Separation of Concerns:**
- SQL знает о вычислениях (CREATE VIEW)
- TDTP знает о схеме (Field name/type)
- UI знает о визуализации (filters/sort)

✅ **Гибкость реализации:**
TDTP не зависит от того, как реализовано поле:
- Обычная колонка в таблице
- Computed column в SQL Server
- Virtual column в MySQL
- Generated column в PostgreSQL
- View с expression
- Materialized view

Для TDTP это **просто поле** → одинаковый XML!

✅ **Изменения не ломают контракт:**
```sql
-- Было: обычная колонка
CREATE TABLE warehouse (
    quantity INT,
    reserve INT,
    available_stock INT  -- дублирование, нужно синхронизировать
);

-- Стало: view с вычислением
CREATE VIEW warehouse_stock AS
SELECT quantity, reserve,
       quantity - reserve AS available_stock  -- автоматически!
FROM warehouse;

-- TDTP XML НЕ МЕНЯЕТСЯ! Тот же контракт.
```

✅ **Single Source of Truth остаётся в SQL:**
- Формула `available_stock = quantity - reserve` живёт ТОЛЬКО в SQL
- TDTP экспортирует результат
- UI показывает результат
- Все системы видят одинаковую логику

---

### Альтернативные варианты (отклонены):

### ~~Вариант A: MVP Computed Fields~~ ❌
- **Причина отклонения:** Нарушает архитектуру TDTP
- **Правильное решение:** Создать view с нужными полями

### ~~Вариант B: Quick Formulas Library~~ ❌
- **Причина отклонения:** Та же проблема — логика в UI
- **Правильное решение:** Использовать ETL pipeline

---

## ❓ Вопросы для финального решения

1. **Есть ли реальные use cases?** (опросить пользователей)
2. **Сколько времени доступно?** (3-5 дней или нет?)
3. **Приоритет безопасности?** (можем ли гарантировать?)
4. **TDTQL roadmap?** (планируется ли поддержка computed fields?)
5. **Альтернатива Quick Formulas достаточна?** (80/20 principle)

---

## ✅ Правильный подход — примеры

### Пример 1: E-commerce — скидка

**❌ Неправильно (computed field в UI):**
```javascript
// Visual Designer вычисляет:
discount_price = price * (1 - discount / 100)
```

**✅ Правильно (view с бизнес-логикой):**
```sql
CREATE VIEW products_with_pricing AS
SELECT
    product_id,
    name,
    price,
    discount,
    -- Бизнес-логика здесь:
    ROUND(price * (1 - discount / 100.0), 2) AS discount_price,
    -- Дополнительные вычисления:
    ROUND(price * (1 - discount / 100.0) * 1.20, 2) AS price_with_tax
FROM products;

-- Visual Designer просто показывает:
SELECT * FROM products_with_pricing;
```

### Пример 2: Склад — свободный остаток

**❌ Неправильно:**
```javascript
// UI вычисляет:
available_stock = quantity - reserve
```

**✅ Правильно:**
```sql
CREATE VIEW warehouse_stock AS
SELECT
    warehouse_id,
    product_id,
    quantity,
    reserve,
    -- Бизнес-логика:
    quantity - reserve AS available_stock,
    -- Дополнительная логика:
    CASE
        WHEN quantity - reserve <= 0 THEN 'Out of Stock'
        WHEN quantity - reserve < 10 THEN 'Low Stock'
        ELSE 'In Stock'
    END AS stock_status
FROM warehouse;
```

**TDTP Export (автоматический):**
```xml
<Source name="warehouse_stock" type="view">
  <Field name="warehouse_id" type="int"/>
  <Field name="product_id" type="int"/>
  <Field name="quantity" type="int"/>
  <Field name="reserve" type="int"/>
  <Field name="available_stock" type="int"/>      <!-- computed! -->
  <Field name="stock_status" type="varchar"/>     <!-- computed! -->
</Source>
```

**Visual Designer видит:**
```
warehouse_stock (6 fields):
  ☑ warehouse_id    [filter]
  ☑ product_id      [filter]
  ☑ quantity        [filter]
  ☑ reserve         [filter]
  ☑ available_stock [filter]  ← работает как обычное поле!
  ☑ stock_status    [filter]  ← работает как обычное поле!
```

**Можно фильтровать computed fields:**
```sql
-- Пользователь в UI выбирает:
available_stock > 0 AND stock_status = 'In Stock'

-- Visual Designer генерирует корректный SQL:
SELECT * FROM warehouse_stock
WHERE available_stock > 0
  AND stock_status = 'In Stock';
```

### Пример 3: HR — возраст сотрудников

**❌ Неправильно:**
```javascript
// UI вычисляет:
age = YEAR(NOW()) - YEAR(birth_date)
```

**✅ Правильно:**
```sql
CREATE VIEW employees_with_age AS
SELECT
    employee_id,
    first_name,
    last_name,
    birth_date,
    hire_date,
    -- Бизнес-логика:
    TIMESTAMPDIFF(YEAR, birth_date, CURDATE()) AS age,
    TIMESTAMPDIFF(YEAR, hire_date, CURDATE()) AS years_in_company,
    -- Категоризация:
    CASE
        WHEN TIMESTAMPDIFF(YEAR, birth_date, CURDATE()) < 30 THEN 'Junior'
        WHEN TIMESTAMPDIFF(YEAR, birth_date, CURDATE()) < 45 THEN 'Mid'
        ELSE 'Senior'
    END AS age_category
FROM employees;
```

### Пример 4: Эволюция схемы (миграция таблица → view)

**Сценарий:** Было дублирование данных, переходим на вычисляемое поле

**Шаг 1: Было (плохо) — дублирование:**
```sql
-- Таблица с дублированием
CREATE TABLE products (
    id INT PRIMARY KEY,
    price DECIMAL(10,2),
    discount INT,
    discount_price DECIMAL(10,2)  -- дублирование! нужна синхронизация
);

-- При INSERT нужно вычислять вручную:
INSERT INTO products (id, price, discount, discount_price)
VALUES (1, 1000, 20, 800);  -- 800 = 1000 * (1 - 20/100)

-- Проблема: может рассинхронизироваться!
UPDATE products SET discount = 30 WHERE id = 1;
-- ❌ Забыли обновить discount_price!
```

**TDTP Export (было):**
```xml
<Source name="products">
  <Field name="id" type="int"/>
  <Field name="price" type="decimal"/>
  <Field name="discount" type="int"/>
  <Field name="discount_price" type="decimal"/>  <!-- дубль! -->
</Source>
```

**Шаг 2: Стало (хорошо) — computed field в view:**
```sql
-- 1. Удаляем дублирующую колонку
ALTER TABLE products DROP COLUMN discount_price;

-- 2. Создаём view с вычислением
CREATE VIEW products_pricing AS
SELECT
    id,
    price,
    discount,
    ROUND(price * (1 - discount / 100.0), 2) AS discount_price  -- автоматически!
FROM products;

-- 3. Используем view вместо таблицы
SELECT * FROM products_pricing;
```

**TDTP Export (стало):**
```xml
<!-- ☑ ТОТ ЖЕ XML! Контракт не изменился! -->
<Source name="products_pricing" type="view">
  <Field name="id" type="int"/>
  <Field name="price" type="decimal"/>
  <Field name="discount" type="int"/>
  <Field name="discount_price" type="decimal"/>  <!-- теперь computed! -->
</Source>
```

**Результат:**
- ✅ TDTP XML не изменился (совместимость!)
- ✅ Visual Designer работает как раньше
- ✅ Нет дублирования (single source of truth)
- ✅ Автоматическая синхронизация
- ✅ Невозможна рассинхронизация

---

### Пример 5: ETL Pipeline трансформация

**Сценарий:** Синхронизация складов между системами

```sql
-- 1. Source View (сырые данные):
CREATE VIEW source_warehouse AS
SELECT
    product_id,
    quantity,
    reserve
FROM warehouse_raw;

-- 2. ETL Transformation (в target систему):
INSERT INTO target_warehouse (product_id, quantity, reserve, available, status)
SELECT
    product_id,
    quantity,
    reserve,
    -- Вычисления в ETL:
    quantity - reserve AS available,
    CASE
        WHEN quantity - reserve > 0 THEN 'available'
        ELSE 'out_of_stock'
    END AS status
FROM source_warehouse;

-- 3. Visual Designer показывает результат:
SELECT * FROM target_warehouse;
```

---

## 📚 Best Practices

### ✅ DO (Правильно):

1. **Вся бизнес-логика в views:**
   ```sql
   CREATE VIEW sales_metrics AS
   SELECT
       order_id,
       revenue,
       cost,
       revenue - cost AS profit,
       (revenue - cost) / revenue * 100 AS profit_margin
   FROM orders;
   ```

2. **ETL для трансформаций:**
   ```python
   # ETL script
   df['available_stock'] = df['quantity'] - df['reserve']
   df['stock_status'] = df['available_stock'].apply(lambda x:
       'In Stock' if x > 10 else 'Low Stock'
   )
   ```

3. **Visual Designer для визуализации:**
   ```javascript
   // Просто показываем что есть:
   SELECT * FROM sales_metrics;
   // Фильтруем:
   WHERE profit_margin > 20;
   ```

### ❌ DON'T (Неправильно):

1. **Не добавлять вычисления в UI:**
   ```javascript
   // ❌ Плохо:
   computed_field = field1 + field2
   ```

2. **Не дублировать бизнес-логику:**
   ```sql
   -- ❌ Логика в двух местах:
   -- 1) В view: available = quantity - reserve
   -- 2) В UI: available = quantity - reserve
   ```

3. **Не хранить формулы в UI state:**
   ```javascript
   // ❌ Плохо:
   formulas: {
       'profit': 'revenue - cost',
       'margin': 'profit / revenue * 100'
   }
   ```

---

## 🎯 Итоговые рекомендации

### Для разработчиков Visual Designer:

✅ **Focus на:**
- Фильтрация (LIKE, IN, BETWEEN) ← сделано ✅
- Сортировка (ORDER BY) ← сделано ✅
- Пагинация (LIMIT/OFFSET) ← сделано ✅
- Joins (визуальные связи)
- Grouping (GROUP BY) — если нужно
- Экспорт результатов

❌ **Не делать:**
- Computed fields в UI
- Expression builders
- Формулы и вычисления
- Агрегации вне SQL

### Для пользователей Visual Designer:

✅ **Если нужно вычисляемое поле:**
1. Создай view с нужной логикой
2. Добавь view как Source в TDTP
3. Используй в Visual Designer

✅ **Если нужна трансформация данных:**
1. Используй ETL pipeline
2. Настрой маппинг полей
3. Целевая система получит готовые данные

---

## 📊 Финальное решение

### 🎯 **COMPUTED FIELDS — НЕ ДЕЛАТЬ**

**Причина:** Архитектурно неправильно

**Альтернатива:**
1. Views для бизнес-логики
2. ETL для трансформаций
3. Visual Designer для визуализации

**Преимущества этого подхода:**
- ✅ Single source of truth (логика в views)
- ✅ Переиспользование (views доступны везде)
- ✅ Тестируемость (SQL views можно тестировать)
- ✅ Версионность (views в migration scripts)
- ✅ Безопасность (нет user input → нет injection)
- ✅ Производительность (views могут быть materialized)

---

**Документация создана:** 2026-02-20
**Финальное решение:**
- 2026-02-20 — Computed fields отклонены (архитектурно неверно)
- 2026-02-20 — CAST() в ETL одобрен (утилитарная трансформация)

**Статус:**
- ❌ Computed Fields в UI/ETL — НЕ реализуется (используй SQL views)
- ✅ CAST() трансформации в ETL — К реализации (с суффиксом `_C`)

---

## 📝 Краткая шпаргалка

### Когда использовать что:

| Задача | Решение | Где реализовать |
|--------|---------|-----------------|
| Вычислить скидку: `price * (1 - discount/100)` | SQL View | CREATE VIEW products_pricing AS ... |
| Вычислить возраст: `YEAR(NOW()) - YEAR(birth_date)` | SQL View | CREATE VIEW employees_with_age AS ... |
| Конвертировать `datetime` → `timestamp` | CAST() в ETL | `<Cast field="date" to="timestamp" as="date_C"/>` |
| Конвертировать `money` → `numeric` | CAST() в ETL | `<Cast field="amount" to="numeric" as="amount_C"/>` |
| Конвертировать `VARCHAR` → `INT` (legacy) | CAST() в ETL | `<Cast field="id" to="int" as="id_C"/>` |
| Показать данные в UI | Visual Designer | SELECT * FROM view_name |
| Фильтровать/сортировать | Visual Designer | Используй готовые поля из view |

### Золотое правило:

```
┌──────────────────────────────────────────────────────┐
│  Бизнес-логика (формулы)   → SQL Views              │
│  Конвертация типов          → ETL CAST()            │
│  Визуализация               → Visual Designer        │
└──────────────────────────────────────────────────────┘
```

**НЕ ПУТАТЬ:**
- **Computed Field** = бизнес-правило (`discount_price = price * 0.8`)
- **CAST Field** = технический тип (`datetime` → `timestamp`)

**Один делает логику, другой меняет упаковку!**

---

## 🏗️ Архитектура CAST(): Где что живёт?

### ❓ Вопрос 1: Нужно ли что-то менять в Visual Designer?

**Ответ: НЕТ! ❌ Ничего менять не нужно!**

Visual Designer работает с **результатом** ETL pipeline, а не с процессом трансформации.

```
┌─────────────────────────────────────────────────────┐
│  ETL Pipeline (выполняется ДО Visual Designer)      │
├─────────────────────────────────────────────────────┤
│  Source DB → ETL Tool → Target DB                   │
│  (SQL Server)  (трансформация)  (PostgreSQL)        │
│                                                     │
│  order_date (datetime) ──CAST──→ order_date_C (timestamp) │
│  amount (money)        ──CAST──→ amount_C (numeric)       │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  Visual Designer (работает с результатом)           │
├─────────────────────────────────────────────────────┤
│  Читает TDTP XML для Target DB:                    │
│                                                     │
│  <Source name="orders_target">                      │
│    <Field name="order_id" type="int"/>              │
│    <Field name="order_date_C" type="timestamp"/>    │
│    <Field name="amount_C" type="numeric"/>          │
│  </Source>                                          │
│                                                     │
│  ☑ Видит уже трансформированные поля               │
│  ☑ Работает с ними как с обычными полями           │
│  ☑ НЕ знает о процессе трансформации               │
└─────────────────────────────────────────────────────┘
```

**Visual Designer не изменяется!** Он просто видит поля с суффиксом `_C` как обычные поля.

---

### ❓ Вопрос 2: Нужна ли секция `<Transformation>` в TDTP XML?

**Ответ: ЗАВИСИТ от контекста!**

#### Сценарий A: TDTP для Visual Designer (после ETL)

```xml
<!-- ❌ НЕТ секции <Transformation>! -->
<Source name="orders_target" connection="postgres_db">
  <Field name="order_id" type="int"/>
  <Field name="order_date_C" type="timestamp"/>  <!-- уже трансформировано! -->
  <Field name="amount_C" type="numeric"/>        <!-- уже трансформировано! -->
</Source>
```

**Почему НЕТ `<Transformation>`:**
- ETL уже выполнен
- Данные уже лежат в Target DB
- TDTP описывает **результирующую схему**
- Visual Designer работает с готовыми данными

**Суффикс `_C` — это просто соглашение об именовании** в target БД!

---

#### Сценарий B: Конфигурация ETL Pipeline (до выполнения)

```xml
<!-- ✅ ДА, секция <Transformation> нужна! -->
<Pipeline name="migrate_orders">
  <!-- 1. Source: откуда берём -->
  <Source name="orders" system="sqlserver" connection="source_db">
    <Field name="order_date" type="datetime"/>
    <Field name="amount" type="money"/>
  </Source>

  <!-- 2. Transformation: ЧТО ДЕЛАТЬ -->
  <Transformation>
    <Cast field="order_date" from="datetime" to="timestamp" as="order_date_C"/>
    <Cast field="amount" from="money" to="numeric(10,2)" as="amount_C"/>
  </Transformation>

  <!-- 3. Target: куда записываем -->
  <Target name="orders_target" system="postgresql" connection="target_db">
    <Field name="order_date_C" type="timestamp"/>
    <Field name="amount_C" type="numeric"/>
  </Target>
</Pipeline>
```

**Почему ДА `<Transformation>`:**
- Это **конфигурация для ETL инструмента**
- Описывает **ЧТО делать** при миграции
- Используется **до** выполнения ETL
- Генерирует код для трансформации

---

### 📂 Разделение файлов:

```
project/
├── etl/
│   └── pipeline_config.yaml         ← ETL Config (YAML + SQL)
│       source: ...
│       transformation:
│         sql: "SELECT CAST(...) AS field_C FROM ..."
│       target: ...
│
├── tdtp/
│   └── target_schema.xml            ← TDTP Schema для Visual Designer
│       <Source name="orders_target">
│         <Field name="order_date_C" type="timestamp"/>
│       </Source>
│
└── visual-designer/
    └── wizard.html                  ← Читает target_schema.xml
```

**Два разных файла, два разных назначения!**
- **ETL Config (YAML)** — содержит SQL трансформацию
- **TDTP Schema (XML)** — описывает результат (без SQL!)

---

### 🔄 Workflow полной миграции:

```
┌─────────────────────────────────────────────────────┐
│  Шаг 1: Создаём ETL конфигурацию (YAML + SQL)      │
├─────────────────────────────────────────────────────┤
│  File: etl/pipeline_config.yaml                     │
│                                                     │
│  source:                                            │
│    query: "SELECT order_id, order_date, amount..."  │
│  transformation:                                    │
│    sql: |                                           │
│      SELECT CAST(order_date AS timestamp) AS order_date_C, │
│             CAST(amount AS numeric) AS amount_C     │
│      FROM source_data                               │
│  target:                                            │
│    table: orders_target                             │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  Шаг 2: Запускаем ETL инструмент                   │
├─────────────────────────────────────────────────────┤
│  $ etl-tool run etl/pipeline_config.yaml            │
│                                                     │
│  Processing...                                      │
│  ✅ Executing SQL transformation                    │
│  ✅ Cast order_date → order_date_C (timestamp)      │
│  ✅ Cast amount → amount_C (numeric)                │
│  ✅ Migrated 10,000 rows to PostgreSQL              │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  Шаг 3: Генерируем TDTP схему для Target DB        │
├─────────────────────────────────────────────────────┤
│  $ tdtp-export --connection postgres_db \           │
│                --output tdtp/target_schema.xml      │
│                                                     │
│  Generated:                                         │
│  <Source name="orders_target">                      │
│    <Field name="order_date_C" type="timestamp"/>    │
│    <Field name="amount_C" type="numeric"/>          │
│  </Source>                                          │
│                                                     │
│  ☑ НЕТ <Transformation> — данные уже трансформированы! │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  Шаг 4: Visual Designer использует Target схему    │
├─────────────────────────────────────────────────────┤
│  Открываем: visual-designer/wizard.html             │
│  Загружаем: tdtp/target_schema.xml                  │
│                                                     │
│  Видим:                                             │
│  ☑ order_date_C (timestamp)  ← обычное поле!       │
│  ☑ amount_C (numeric)        ← обычное поле!       │
│                                                     │
│  Можем фильтровать, сортировать, визуализировать   │
└─────────────────────────────────────────────────────┘
```

---

### 💡 Ключевые выводы:

#### 1️⃣ Visual Designer НЕ НУЖНО МЕНЯТЬ

```javascript
// ❌ НЕ НУЖНО добавлять:
if (field.name.endsWith('_C')) {
  // special handling...
}

// ✅ Работает как есть:
fields.forEach(field => {
  renderField(field);  // order_date_C — обычное поле!
});
```

**Суффикс `_C` — это просто имя поля**, ничего особенного!

---

#### 2️⃣ Трансформация живёт в SQL, который хранится в YAML

| Файл | Назначение | Содержит SQL? |
|------|------------|---------------|
| `etl/pipeline_config.yaml` | ETL Config | ✅ ДА (SQL трансформация) |
| `tdtp/target_schema.xml` | TDTP Schema | ❌ НЕТ (только результат) |

**ВАЖНО:** Нет никакого XML элемента `<Transformation>`!
Трансформация = **SQL код** в **YAML параметре** `transformation.sql`!

---

#### 3️⃣ TDTP описывает результат, а не процесс

```
ETL Config (YAML + SQL):         TDTP Schema (результат):
┌────────────────────────┐       ┌──────────────────────┐
│ transformation:        │       │ <Source>             │
│   sql: |               │       │   <Field             │
│     SELECT CAST(date   │       │     name="date_C"    │
│       AS timestamp)    │  →→→  │     type="timestamp" │
│       AS date_C        │       │   />                 │
│     FROM source_data   │       │ </Source>            │
└────────────────────────┘       └──────────────────────┘
  SQL трансформация              Что получилось
  (в YAML параметре)             (в TDTP XML)
```

**TDTP не знает (и не должен знать) о SQL трансформации!**

---

#### 4️⃣ Суффикс `_C` — это соглашение, а не магия

```sql
-- В Target DB просто есть колонка с таким именем:
CREATE TABLE orders_target (
    order_id INT,
    order_date_C TIMESTAMP,  -- просто имя колонки!
    amount_C NUMERIC         -- просто имя колонки!
);

-- Visual Designer видит:
SELECT order_date_C FROM orders_target;  -- обычное поле!
```

**Суффикс помогает понять происхождение** (было cast), но технически это просто имя!

---

### 🎯 Финальный ответ на вопросы:

**Q1: Что нужно добавить в Visual Designer?**
**A1: НИЧЕГО! ❌**
- Visual Designer работает с результатом ETL
- Поля с суффиксом `_C` — обычные поля
- Не нужно никакой специальной обработки

**Q2: Где живёт трансформация (CAST)?**
**A2: В SQL коде, который хранится в YAML конфиге!**
- **ETL Config** (`pipeline_config.yaml`) — содержит `transformation.sql`
- **TDTP Schema** (`target_schema.xml`) — НЕ содержит SQL (только результат)

**Q3: Нужно ли добавлять XML элемент `<Transformation>` в TDTP?**
**A3: НЕТ! ❌ Такого элемента вообще нет!**
- Трансформация = **SQL код** в **YAML файле**
- TDTP XML содержит только **результирующую схему**
- SQL не попадает в TDTP!

**Q4: Как избежать конфликтов имён при Multi-Source ETL?**
**A4: Используй префикс источника + суффикс _C!**
- **Формат:** `{SourceName}_{FieldName}[_C]`
- **Пример:** `Users_Balance_C`, `User2_Balance_C`
- **Подробнее:** См. раздел "Multi-Source Naming Convention" выше

---

## 🛡️ Валидация конфликтов имён (Implementation)

### Автоматическая проверка дубликатов колонок

При генерации ETL конфигурации нужно валидировать конфликты имён!

#### ✅ Validator псевдокод:

```python
# etl_validator.py
def validate_column_names(transformation_sql: str) -> List[str]:
    """
    Проверяет SQL на дублирующиеся имена колонок в SELECT.

    Returns:
        List[str]: Список конфликтующих имён (пустой если всё ОК)
    """
    # Извлекаем колонки из SELECT
    columns = extract_select_columns(transformation_sql)

    # Проверяем дубликаты
    duplicates = []
    seen = {}

    for col in columns:
        col_name = col.alias or col.name  # берём alias если есть, иначе имя

        if col_name in seen:
            duplicates.append({
                'column': col_name,
                'first_source': seen[col_name],
                'second_source': col.table
            })
        else:
            seen[col_name] = col.table

    return duplicates


def suggest_prefixes(conflicts: List[dict]) -> List[str]:
    """
    Генерирует рекомендации по исправлению конфликтов.

    Example output:
        [
            "Column 'ID' conflicts between 'Users' and 'User2'",
            "  Suggestion: Use 'Users_ID' and 'User2_ID'",
            "  SQL: [Users].[ID] AS [Users_ID], [User2].[ID] AS [User2_ID]"
        ]
    """
    suggestions = []

    for conflict in conflicts:
        col = conflict['column']
        src1 = conflict['first_source']
        src2 = conflict['second_source']

        suggestions.append(
            f"❌ Column '{col}' conflicts between '{src1}' and '{src2}'"
        )
        suggestions.append(
            f"   ✅ Suggestion: Use '{src1}_{col}' and '{src2}_{col}'"
        )
        suggestions.append(
            f"   SQL: [{src1}].[{col}] AS [{src1}_{col}], "
            f"[{src2}].[{col}] AS [{src2}_{col}]"
        )

    return suggestions


# Пример использования:
yaml_config = load_yaml("etl/pipeline_config.yaml")
sql = yaml_config['transformation']['sql']

conflicts = validate_column_names(sql)

if conflicts:
    print("⚠️ Column name conflicts detected!")
    suggestions = suggest_prefixes(conflicts)
    for suggestion in suggestions:
        print(suggestion)

    # FAIL: блокируем выполнение ETL
    raise ValueError("Fix column conflicts before running ETL!")
else:
    print("✅ No column conflicts. Safe to run ETL!")
```

#### 📋 Пример вывода валидатора:

```bash
$ etl-tool validate etl/multi_source_pipeline.yaml

⚠️ Column name conflicts detected!

❌ Column 'ID' conflicts between 'Users' and 'User2'
   ✅ Suggestion: Use 'Users_ID' and 'User2_ID'
   SQL: [Users].[ID] AS [Users_ID], [User2].[ID] AS [User2_ID]

❌ Column 'Name' conflicts between 'Users' and 'User2'
   ✅ Suggestion: Use 'Users_Name' and 'User2_Name'
   SQL: [Users].[Name] AS [Users_Name], [User2].[Name] AS [User2_Name]

❌ Column 'Balance' conflicts between 'Users' and 'User2'
   ✅ Suggestion: Use 'Users_Balance_C' and 'User2_Balance_C' (with CAST)
   SQL: CAST([Users].[Balance] AS INT) AS [Users_Balance_C],
        CAST([User2].[Balance] AS DECIMAL) AS [User2_Balance_C]

❌ ETL validation FAILED! Fix conflicts before running.
```

#### ✅ После исправления:

```bash
$ etl-tool validate etl/multi_source_pipeline.yaml

✅ No column conflicts detected
✅ All column names are unique
✅ CAST syntax is correct
✅ Safe to run ETL!

$ etl-tool run etl/multi_source_pipeline.yaml
Processing...
✅ Migrated 10,000 rows successfully!
```

### 🔍 Проверка синтаксиса CAST

Дополнительная валидация для корректности SQL:

```python
def validate_cast_syntax(sql: str) -> List[str]:
    """
    Проверяет синтаксис CAST() выражений.

    Типичные ошибки:
    - CAST(...) AS , field_C  ← запятая не на месте
    - CAST(...) AS            ← нет имени alias
    - CAST(... AS type        ← нет закрывающей скобки
    """
    errors = []

    # Regex для поиска CAST выражений
    cast_pattern = r'CAST\s*\([^)]+\)\s*AS\s*([,\s]|$)'

    matches = re.finditer(cast_pattern, sql, re.IGNORECASE)

    for match in matches:
        if match.group(1).startswith(','):
            errors.append({
                'type': 'syntax_error',
                'message': 'Missing alias after CAST() AS',
                'position': match.start(),
                'fix': 'Add column alias: CAST(...) AS [column_name]'
            })

    return errors


# Пример:
sql = """
SELECT
    CAST([Users].[Balance] AS INT) AS , [Users].[Balance_C]
FROM Users
"""

errors = validate_cast_syntax(sql)

if errors:
    print("❌ CAST syntax errors:")
    for err in errors:
        print(f"   {err['message']}")
        print(f"   Fix: {err['fix']}")
```

**Вывод:**
```
❌ CAST syntax errors:
   Missing alias after CAST() AS
   Fix: Add column alias: CAST(...) AS [column_name]

   Correct syntax:
   CAST([Users].[Balance] AS INT) AS [Users_Balance_C]
```

### 📊 CI/CD Integration

Добавить валидацию в CI pipeline:

```yaml
# .github/workflows/validate-etl.yml
name: Validate ETL Configs

on:
  pull_request:
    paths:
      - 'etl/**/*.yaml'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2

      - name: Validate ETL configs
        run: |
          for config in etl/**/*.yaml; do
            echo "Validating $config..."
            etl-tool validate "$config"
          done

      - name: Check for column conflicts
        run: |
          python scripts/validate_column_names.py

      - name: Check CAST syntax
        run: |
          python scripts/validate_cast_syntax.py
```

**Результат:** Автоматическое блокирование PR если есть конфликты имён!

---

## 📝 Checklist для Multi-Source ETL

При создании multi-source ETL конфигурации, проверь:

- [ ] **Префиксы источников** — все колонки имеют префикс `{SourceName}_`
- [ ] **Суффикс _C для CAST** — все CAST используют суффикс `_C`
- [ ] **Нет дубликатов** — все имена колонок уникальны
- [ ] **Синтаксис CAST** — нет запятых перед alias
- [ ] **WHERE до CAST** — фильтры используют оригинальные типы
- [ ] **TDTP Schema** — содержит атрибут `source="..."` (опционально)
- [ ] **Валидация пройдена** — `etl-tool validate` успешно
- [ ] **Тесты** — проверены JOIN условия и типы данных

---

---
