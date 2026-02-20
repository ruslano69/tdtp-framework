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
**Финальное решение:** 2026-02-20 — Computed fields отклонены (архитектурно неверно)
**Статус:** ✅ Закрыто — не реализуется
