# New TDTQL Operators Guide

**Дата:** 2026-02-20
**Версия:** Visual Designer v1.1

---

## 🎉 Новые операторы

Добавлены критичные операторы из спецификации TDTQL для полного покрытия функциональности.

---

## 1. Pattern Matching (Поиск по паттернам)

### LIKE
Поиск по текстовому паттерну с wildcards.

**Wildcards:**
- `%` — любое количество символов (включая 0)
- `_` — ровно один символ

**Примеры использования:**

| Паттерн | Что находит | SQL эквивалент |
|---------|-------------|----------------|
| `%@example.com` | Email'ы домена example.com | `WHERE email LIKE '%@example.com'` |
| `+7%` | Российские телефоны | `WHERE phone LIKE '+7%'` |
| `%Smith%` | Содержит "Smith" в любом месте | `WHERE name LIKE '%Smith%'` |
| `John_%` | Начинается с "John_" + 1 символ | `WHERE name LIKE 'John_%'` |
| `2024-__-01` | Первое число любого месяца 2024 года | `WHERE date LIKE '2024-__-01'` |

**UI подсказка:**
```
Value: %@example.com
💡 Use % (any chars) and _ (one char) as wildcards
```

### NOT LIKE
Исключает записи, соответствующие паттерну.

**Примеры:**

| Паттерн | Что исключает | SQL эквивалент |
|---------|---------------|----------------|
| `test%` | Тестовые записи | `WHERE username NOT LIKE 'test%'` |
| `%@spam.com` | Email'ы с spam.com | `WHERE email NOT LIKE '%@spam.com'` |
| `tmp_%` | Временные файлы | `WHERE filename NOT LIKE 'tmp_%'` |

---

## 2. List Matching (Списки значений)

### IN
Фильтрация по списку значений (OR для каждого значения).

**Формат:** Значения через запятую (comma-separated).

**Примеры использования:**

| Поле | Значения | Что находит | SQL эквивалент |
|------|----------|-------------|----------------|
| `city` | `Moscow,SPb,Kazan` | Города из списка | `WHERE city IN ('Moscow', 'SPb', 'Kazan')` |
| `status` | `active,pending,new` | Активные статусы | `WHERE status IN ('active', 'pending', 'new')` |
| `priority` | `1,2,3` | Высокие приоритеты | `WHERE priority IN (1, 2, 3)` |
| `department` | `IT,HR,Sales` | Отделы | `WHERE department IN ('IT', 'HR', 'Sales')` |

**UI подсказка:**
```
Value: Moscow,SPb,Kazan
💡 Comma-separated values (e.g., value1,value2,value3)
```

**⚠️ Важно:**
- Пробелы после запятых будут удалены автоматически
- Для строк с запятыми внутри используйте LIKE вместо IN

### NOT IN
Исключает записи из списка значений.

**Примеры:**

| Поле | Значения | Что исключает | SQL эквивалент |
|------|----------|---------------|----------------|
| `status` | `deleted,archived,banned` | Неактивные статусы | `WHERE status NOT IN ('deleted', 'archived', 'banned')` |
| `country` | `US,UK,CA` | Англоязычные страны | `WHERE country NOT IN ('US', 'UK', 'CA')` |

---

## 3. Pagination (Пагинация)

### LIMIT
Ограничивает количество возвращаемых строк.

**UI:** Кнопка 📊 в toolbar таблицы

**Примеры:**

| LIMIT | Применение | SQL |
|-------|-----------|-----|
| `10` | Быстрый preview | `SELECT * FROM users LIMIT 10` |
| `100` | Стандартная пагинация | `SELECT * FROM orders LIMIT 100` |
| `1000` | Большой batch | `SELECT * FROM logs LIMIT 1000` |

### OFFSET
Пропускает N первых строк (для пагинации).

**Примеры:**

| LIMIT | OFFSET | Страница | SQL |
|-------|--------|----------|-----|
| `10` | `0` | 1 (строки 1-10) | `LIMIT 10 OFFSET 0` |
| `10` | `10` | 2 (строки 11-20) | `LIMIT 10 OFFSET 10` |
| `10` | `20` | 3 (строки 21-30) | `LIMIT 10 OFFSET 20` |
| `100` | `500` | 6 (строки 501-600) | `LIMIT 100 OFFSET 500` |

**Quick Presets в UI:**
- 10 rows — для preview
- 100 rows — стандартная страница
- 1000 rows — большой batch
- No limit — без ограничений

---

## 4. Tooltip обновления

### Новые форматы tooltip

**LIKE:**
```
email LIKE '%@company.com' (&)
Click to edit filter
```

**NOT LIKE:**
```
username NOT LIKE 'test%' (&)
Click to edit filter
```

**IN:**
```
city IN (Moscow,SPb,Kazan) (&)
Click to edit filter
```

**NOT IN:**
```
status NOT IN (deleted,archived) (&)
Click to edit filter
```

**LIMIT/OFFSET в toolbar:**
```
Hover на 📊: "LIMIT 100 OFFSET 50"
```

---

## 5. Комбинированные примеры

### Пример 1: Поиск активных пользователей из РФ городов

**Фильтры:**
1. `status = active` (AND)
2. `city IN Moscow,SPb,Kazan,Novosibirsk` (AND)
3. `email NOT LIKE %@spam.com` (AND)

**LIMIT:** 100

**SQL:**
```sql
SELECT * FROM users
WHERE status = 'active'
  AND city IN ('Moscow', 'SPb', 'Kazan', 'Novosibirsk')
  AND email NOT LIKE '%@spam.com'
LIMIT 100
```

### Пример 2: Логи ошибок за сегодня (preview)

**Фильтры:**
1. `level IN ERROR,CRITICAL,FATAL` (OR)
2. `created_at >= 2026-02-20` (AND)
3. `message NOT LIKE %test%` (AND)

**LIMIT:** 10 (preview)

**SQL:**
```sql
SELECT * FROM logs
WHERE level IN ('ERROR', 'CRITICAL', 'FATAL')
  AND created_at >= '2026-02-20'
  AND message NOT LIKE '%test%'
LIMIT 10
```

### Пример 3: Заказы со скидкой (пагинация)

**Фильтры:**
1. `discount > 0` (AND)
2. `status NOT IN cancelled,refunded` (AND)

**LIMIT:** 50
**OFFSET:** 100 (3-я страница)

**SQL:**
```sql
SELECT * FROM orders
WHERE discount > 0
  AND status NOT IN ('cancelled', 'refunded')
LIMIT 50 OFFSET 100
```

---

## 6. TDTQL маппинг

### Операторы → TDTQL

| UI Operator | TDTQL Operator | Description |
|-------------|----------------|-------------|
| `LIKE` | `like` | Pattern matching |
| `NOT_LIKE` | `not_like` | Exclude pattern |
| `IN` | `in` | Match list (OR) |
| `NOT_IN` | `not_in` | Exclude list |

### Пагинация → TDTQL

```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <Filter field="status" operator="eq" value="active"/>
  </Filters>
  <Limit>100</Limit>
  <Offset>50</Offset>
</Query>
```

---

## 7. Best Practices

### LIKE оптимизация

✅ **Эффективно:**
```sql
-- Индекс используется (префикс)
name LIKE 'John%'
```

❌ **Медленно:**
```sql
-- Full table scan (wildcard в начале)
name LIKE '%Smith'
```

### IN vs множественные OR

✅ **Лучше:**
```sql
city IN ('Moscow', 'SPb', 'Kazan')
```

❌ **Хуже:**
```sql
city = 'Moscow' OR city = 'SPb' OR city = 'Kazan'
```

### LIMIT для preview

💡 **Совет:** Всегда используйте LIMIT при тестировании запросов:
- Быстрая проверка структуры данных
- Экономия ресурсов БД
- Предотвращение случайной загрузки миллионов строк

**Рекомендуемые значения:**
- Preview: `LIMIT 10`
- Тестирование: `LIMIT 100`
- Production: `LIMIT 1000` + пагинация

---

## 8. Troubleshooting

### LIKE не находит записи

**Проблема:**
```
email LIKE '@example.com'  ❌
```

**Решение:**
```
email LIKE '%@example.com'  ✅
```

Не забывайте wildcards!

### IN с пробелами

**Проблема:**
```
city IN 'Moscow, SPb, Kazan'  ❌ (пробелы после запятых)
```

**Решение:**
```
city IN 'Moscow,SPb,Kazan'  ✅ (без пробелов)
```

Или пробелы будут удалены автоматически при обработке.

### LIMIT без ORDER BY

⚠️ **Внимание:** Без `ORDER BY` результаты могут быть недетерминированными:

```sql
SELECT * FROM users LIMIT 10  ❌ Каждый раз разные строки
```

**Решение:**
```sql
SELECT * FROM users ORDER BY id LIMIT 10  ✅ Стабильный результат
```

---

## 9. Keyboard Shortcuts (планируется)

| Shortcut | Действие |
|----------|---------|
| `Ctrl+F` | Open filter for selected field |
| `Ctrl+L` | Open LIMIT settings |
| `Esc` | Close modal |

---

## 10. Roadmap

### ✅ Phase 1 (Done)
- LIKE / NOT LIKE
- IN / NOT IN
- LIMIT / OFFSET
- Updated tooltips

### 🚧 Phase 2 (Next)
- [ ] Группировка фильтров (скобки)
- [ ] Visual Query Builder
- [ ] Auto-suggestions для IN

### 💡 Phase 3 (Future)
- [ ] Regex support (REGEXP operator)
- [ ] Case-insensitive ILIKE
- [ ] Saved filter templates

---

## Заключение

Теперь Visual Designer покрывает **~95% TDTQL спецификации** для базовой фильтрации:

✅ Все операторы сравнения
✅ NULL проверки
✅ Empty string проверки
✅ **LIKE / NOT LIKE** (текстовый поиск)
✅ **IN / NOT IN** (списки)
✅ **LIMIT / OFFSET** (пагинация)
✅ Сортировка

⚠️ Осталось:
- Группировка фильтров (вложенные AND/OR)
- Advanced Query Builder

**Документация обновлена:** 2026-02-20
