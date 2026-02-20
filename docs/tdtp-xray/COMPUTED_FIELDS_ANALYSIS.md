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

### Вариант A: **MVP Computed Fields** (рекомендуется)
- **Сроки:** 3-5 дней
- **Scope:** Phase 1 only (базовые операции)
- **Security:** Whitelist + validation
- **TDTQL:** Frontend-only (пока)
- **Risk:** 🟡 Medium

### Вариант B: **Quick Formulas Library** (быстрее)
- **Сроки:** 1 день
- **Scope:** Предустановленные формулы
- **Security:** ✅ Безопасно (no user input)
- **TDTQL:** N/A
- **Risk:** 🟢 Low

### Вариант C: **Не делать** (консервативный)
- **Сроки:** 0
- **Scope:** —
- **Reasoning:** Сложность не оправдывает user demand
- **Alternative:** Улучшить другие фичи

---

## ❓ Вопросы для финального решения

1. **Есть ли реальные use cases?** (опросить пользователей)
2. **Сколько времени доступно?** (3-5 дней или нет?)
3. **Приоритет безопасности?** (можем ли гарантировать?)
4. **TDTQL roadmap?** (планируется ли поддержка computed fields?)
5. **Альтернатива Quick Formulas достаточна?** (80/20 principle)

---

**Мой vote:** 🟡 **Вариант B** (Quick Formulas) → затем **Вариант A** (MVP) если будет demand

**Обоснование:**
- Quick Formulas покрывают 80% use cases
- Быстро реализовать (1 день vs 5 дней)
- Безопасно (no user input → no injection risk)
- Соберём feedback → поймём нужен ли full MVP

---

**Документация создана:** 2026-02-20
