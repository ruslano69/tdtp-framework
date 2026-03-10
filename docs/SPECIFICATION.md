# TDTP Specification

**Table Data Transfer Protocol** - спецификация формата обмена табличными данными через message brokers.

**Версия:** 1.3.1 (базовый протокол v1.0; расширения: v1.2 — compression, v1.3 — encryption, v1.3.1 — compact format / fixed fields / special values)
**Дата:** 10.03.2026
**Статус:** Production Ready

---

## Содержание

1. [Введение](#введение)
2. [Архитектура](#архитектура)
3. [Формат пакетов](#формат-пакетов)
4. [Типы данных](#типы-данных)
5. [TDTQL - Query Language](#tdtql---query-language)
6. [Compact Format v1.3.1](#compact-format-v131)
7. [Примеры](#примеры)

---

## Введение

### Назначение

TDTP (Table Data Transfer Protocol) - это протокол для универсального обмена табличными данными между системами через message brokers (RabbitMQ, MSMQ, Kafka). Протокол разработан для:

- **Синхронизации справочников** между информационными системами
- **Репликации данных** между БД разных типов (SQLite, PostgreSQL, MS SQL)
- **Обмена данными** через очереди сообщений
- **Статистических выгрузок** с фильтрацией и сортировкой

### Ключевые особенности

- ✅ **Самодокументируемость** - каждый пакет содержит полную схему данных
- ✅ **Stateless** - каждое сообщение независимо и содержит весь контекст
- ✅ **Валидация** - строгая типизация с проверкой на уровне схемы
- ✅ **Пагинация** - автоматическое разбиение больших таблиц на части (до 3.8MB)
- ✅ **Фильтрация** - встроенный язык запросов TDTQL
- ✅ **Универсальность** - работает с любыми СУБД и message brokers
- ✅ **Сжатие данных** - опциональное сжатие zstd для больших пакетов (v1.2+)
- ✅ **Шифрование** - AES-256-GCM с UUID-binding через xZMercury (v1.3+)

### Формат данных

- **Контейнер:** XML (UTF-8)
- **Разделитель данных:** Pipe `|` (ASCII 124)
- **Максимальный размер пакета:** 3.8 MB (настраивается)
- **Кодировка:** UTF-8

---

## Архитектура

### Структура пакета

```
DataPacket
├── Header              (обязательный)
│   ├── Type            (reference|delta|request|response|alarm|error)
│   ├── TableName       (имя таблицы)
│   ├── MessageID       (UUID)
│   ├── Timestamp       (ISO 8601)
│   └── Pagination      (PartNumber/TotalParts)
│
├── Schema              (обязательный для data packets)
│   └── Field[]         (описание полей)
│       ├── Name
│       ├── Type        (INTEGER|TEXT|DECIMAL|...)
│       ├── Length/Precision/Scale
│       └── Attributes  (key, nullable, timezone, subtype)
│
├── Data                (обязательный для data packets)
│   ├── compression     (опциональный атрибут: "zstd")  🆕 v1.2
│   └── Row[]           (данные в формате pipe-delimited или сжатые)
│
├── Query               (опциональный, для request/response)
│   ├── Filters         (TDTQL условия)
│   ├── OrderBy         (сортировка)
│   └── Limit/Offset    (пагинация)
│
└── QueryContext        (опциональный, для response)
    └── ExecutionResults (статистика выполнения)
```

### Типы пакетов

| Тип | Назначение | Обязательные элементы |
|-----|------------|-----------------------|
| **reference** | Полная синхронизация справочника | Header, Schema, Data |
| **delta** | Инкрементальное обновление | Header, Schema, Data, Query |
| **request** | Запрос данных | Header, Query, Sender, Recipient |
| **response** | Ответ на запрос | Header, Schema, Data, QueryContext |
| **alarm** | Уведомление мониторинга | Header, AlarmDetails (Severity, Code, Message) |
| **error** | Управляемая ошибка ETL pipeline | Header, Schema, Data (запись в `tdtp_errors`) |

> **alarm vs error:** `alarm` использует нестандартный блок `<AlarmDetails>` — предназначен для систем мониторинга (не совместим с ETL pipeline). `error` — стандартный `DataPacket` с `Schema+Data`, пишется в таблицу `tdtp_errors` и совместим с любым downstream-потребителем. Генерируется автоматически при деградации xZMercury.

---

## Формат пакетов

### Header

Заголовок пакета содержит метаданные о сообщении.

**XML структура:**
```xml
<Header>
  <Type>reference</Type>
  <TableName>users</TableName>
  <MessageID>REF-2025-a1b2c3d4-P1</MessageID>
  <PartNumber>1</PartNumber>
  <TotalParts>3</TotalParts>
  <RecordsInPart>1000</RecordsInPart>
  <Timestamp>2025-11-16T12:00:00Z</Timestamp>
  <Sender>SourceSystem</Sender>
  <Recipient>TargetSystem</Recipient>
  <InReplyTo>REQ-2025-xyz123</InReplyTo>
</Header>
```

**Поля:**

| Поле | Тип | Обязательное | Описание |
|------|-----|--------------|----------|
| Type | enum | ✅ | reference, delta, request, response, alarm |
| TableName | string | ✅ | Имя таблицы/справочника |
| MessageID | UUID | ✅ | Уникальный идентификатор сообщения |
| PartNumber | int | ✅ | Номер части (для пагинации) |
| TotalParts | int | ✅ | Общее количество частей |
| RecordsInPart | int | ⚪ | Количество записей в части |
| Timestamp | ISO8601 | ✅ | Время создания пакета |
| Sender | string | ⚪ | Система-отправитель |
| Recipient | string | ⚪ | Система-получатель |
| InReplyTo | string | ⚪ | ID запроса (для response) |

### Schema

Схема описывает структуру таблицы и типы данных.

**XML структура:**
```xml
<Schema>
  <Field name="id" type="INTEGER" key="true"></Field>
  <Field name="username" type="TEXT" length="100"></Field>
  <Field name="email" type="TEXT" length="255"></Field>
  <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
  <Field name="created_at" type="TIMESTAMP" timezone="UTC"></Field>
  <Field name="user_id" type="TEXT" length="-1" subtype="uuid"></Field>
  <Field name="metadata" type="TEXT" length="-1" subtype="jsonb"></Field>
</Schema>
```

**XML структура (v1.3.1 — с fixed и SpecialValues):** 🆕 v1.3.1
```xml
<Schema>
  <!-- fixed=true: значение не меняется в пределах пакета (compact-оптимизация) -->
  <Field name="dept_id"   type="INTEGER"           fixed="true"></Field>
  <Field name="dept_name" type="TEXT" length="100" fixed="true"></Field>
  <Field name="emp_id"    type="INTEGER"></Field>

  <!-- SpecialValues: маркеры для NULL, Infinity, NaN, NoDate -->
  <Field name="notes" type="TEXT" length="500">
    <SpecialValues>
      <Null marker="[NULL]"/>
    </SpecialValues>
  </Field>

  <Field name="sensor_value" type="REAL">
    <SpecialValues>
      <Infinity    marker="INF"/>
      <NegInfinity marker="-INF"/>
      <NaN         marker="NaN"/>
    </SpecialValues>
  </Field>

  <Field name="graduation_date" type="DATE">
    <SpecialValues>
      <NoDate marker="1900-01-01"/>
    </SpecialValues>
  </Field>
</Schema>
```

**Атрибуты Field:**

| Атрибут | Тип | Применимо к | Default | Описание |
|---------|-----|-------------|---------|----------|
| name | string | все | — | Имя поля (обязательное) |
| type | enum | все | — | Тип данных TDTP (обязательное) |
| length | int | TEXT, BLOB | — | Максимальная длина (-1 = unlimited) |
| precision | int | DECIMAL | — | Общее количество цифр |
| scale | int | DECIMAL | — | Количество цифр после запятой |
| timezone | string | TIMESTAMP, TIME | UTC | Часовой пояс (UTC, Local, +03:00) |
| key | bool | любой | false | Первичный ключ |
| subtype | string | любой | — | Подтип (uuid, jsonb, inet, array) |
| **fixed** | bool | любой | false | 🆕 v1.3.1: значение не меняется в пределах пакета |

**Дочерний элемент `<SpecialValues>`** 🆕 v1.3.1

Задаёт строковые маркеры для значений, которые нельзя выразить стандартно:

| Элемент | Атрибут | Применимо к | Описание |
|---------|---------|-------------|----------|
| `<Null>` | `marker` | TEXT | NULL (отличается от пустой строки `""`) |
| `<Infinity>` | `marker` | REAL, DECIMAL | Положительная бесконечность |
| `<NegInfinity>` | `marker` | REAL, DECIMAL | Отрицательная бесконечность |
| `<NaN>` | `marker` | REAL | Not a Number (0/0, sqrt(-1)) |
| `<NoDate>` | `marker` | DATE, TIMESTAMP | Отсутствие даты (не то же самое, что NULL) |

**Логика декодера для SpecialValues:**
- Если значение совпадает с маркером → применить соответствующее специальное значение
- Для TEXT: пустая строка `||` = `""` (empty string, хранится); маркер `[NULL]` = NULL (не хранится)
- Для DATE: маркер NoDate = sentinel-значение «нет даты», отличное от NULL

### Data

Данные передаются в формате pipe-delimited (разделитель `|`).

**XML структура (без сжатия):**
```xml
<Data>
  <R>1|john_doe|john@example.com|1500.50|2025-01-15 10:30:00</R>
  <R>2|jane_smith|jane@example.com|2300.00|2025-01-16 14:20:00</R>
</Data>
```

**XML структура (со сжатием zstd):** 🆕 v1.2
```xml
<Data compression="zstd">
  <R>KLUv/WBgVKEAAYsBAHNvbWUtY29tcHJlc3NlZC1kYXRhLWhlcmU=</R>
</Data>
```

**Атрибуты Data:**

| Атрибут | Тип | Значения | Описание |
|---------|-----|----------|----------|
| compression | string | `"zstd"` | Алгоритм сжатия (опционально, v1.2+) |
| checksum | string | hex | XXH3 хеш сжатых данных (v1.2+) |
| **compact** | bool | `"true"` | 🆕 v1.3.1: compact format — fixed поля пишутся только при смене значения |

**Сжатие данных (v1.2+):**

При установке атрибута `compression="zstd"`:
- Все строки данных объединяются и сжимаются алгоритмом zstd
- Сжатые данные кодируются в base64
- Результат помещается в единственный элемент `<R>`
- При распаковке данные восстанавливаются в исходный формат pipe-delimited

**Когда использовать сжатие:**
- Для пакетов размером > 1KB (настраивается)
- Для больших таблиц с многими строками
- Для экономии bandwidth при передаче через message brokers
- Типичный коэффициент сжатия: 50-80%

**Правила форматирования:**

- **Разделитель:** Pipe `|` (ASCII 124)
- **Пустое значение:** Пустая строка между разделителями: `field1||field3`
- **NULL:** Отсутствие значения = NULL
- **Escape разделителя:** Backslash escaping для pipe внутри значений:
  - `|` → `\|` (pipe внутри значения)
  - `\` → `\\` (backslash внутри значения)
- **XML entities:** XML специальные символы экранируются автоматически:
  - `<` → `&lt;`
  - `>` → `&gt;`
  - `&` → `&amp;`
  - `"` → `&quot;`
  - `'` → `&apos;`

**Примеры экранирования:**
```xml
<!-- Простое значение -->
<R>value1|value2|value3</R>

<!-- Pipe внутри первого значения -->
<R>path\|to\|file|value2|value3</R>
<!-- Декодируется как: ["path|to|file", "value2", "value3"] -->

<!-- Backslash внутри значения -->
<R>C:\\Windows\\System32|value2</R>
<!-- Декодируется как: ["C:\Windows\System32", "value2"] -->

<!-- Комбинация pipe и backslash -->
<R>C:\\path\|to\|file|value2</R>
<!-- Декодируется как: ["C:\path|to|file", "value2"] -->
```

### Query (TDTQL)

Структура запроса для фильтрации данных.

**XML структура:**
```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <And>
      <Filter field="balance" operator="gte" value="1000"></Filter>
      <Filter field="is_active" operator="eq" value="1"></Filter>
    </And>
  </Filters>
  <OrderBy field="balance" direction="DESC"></OrderBy>
  <Limit>100</Limit>
  <Offset>0</Offset>
</Query>
```

Подробнее см. [TDTQL - Query Language](#tdtql---query-language)

### QueryContext

Контекст выполнения запроса (только для response).

**XML структура:**
```xml
<QueryContext>
  <OriginalQuery language="TDTQL" version="1.0">
    <!-- Копия исходного запроса -->
  </OriginalQuery>
  <ExecutionResults>
    <TotalRecordsInTable>10000</TotalRecordsInTable>
    <RecordsAfterFilters>150</RecordsAfterFilters>
    <RecordsReturned>100</RecordsReturned>
    <MoreDataAvailable>true</MoreDataAvailable>
  </ExecutionResults>
</QueryContext>
```

---

## Типы данных

### Базовые типы

| TDTP Type | Описание | SQL аналоги | Формат в Data |
|-----------|----------|-------------|---------------|
| **INTEGER** | Целое число | INT, BIGINT, SERIAL | `123`, `-456` |
| **REAL** | Число с плавающей точкой | FLOAT, DOUBLE | `123.45`, `-0.001` |
| **DECIMAL** | Точное число | DECIMAL(p,s), NUMERIC | `1234.56` |
| **TEXT** | Строка | VARCHAR, TEXT, NVARCHAR | `Hello World` |
| **BLOB** | Бинарные данные | BLOB, BYTEA, VARBINARY | Base64 encoded |
| **BOOLEAN** | Логический | BOOLEAN, BIT | `0` (false), `1` (true) |
| **DATE** | Дата | DATE | `2025-01-15` (ISO 8601) |
| **TIME** | Время | TIME | `14:30:00` (ISO 8601) |
| **TIMESTAMP** | Дата и время | TIMESTAMP, DATETIME | `2025-01-15 14:30:00` |

### Атрибуты типов

**LENGTH** (для TEXT, BLOB):
- Положительное число: максимальная длина
- `-1`: неограниченная длина (TEXT, JSONB, UUID)

**PRECISION и SCALE** (для DECIMAL):
- `precision`: общее количество значащих цифр
- `scale`: количество цифр после запятой
- Пример: `DECIMAL(12,2)` → `precision="12" scale="2"` → `9999999999.99`

**TIMEZONE** (для TIMESTAMP, TIME):
- `UTC`: время в UTC
- `Local`: локальное время системы
- `+03:00`, `-05:00`: конкретный часовой пояс

**KEY**:
- `true`: поле является первичным ключом
- `false` или отсутствует: обычное поле

**SUBTYPE**:
- `uuid`: UUID/GUID (TEXT length="-1" subtype="uuid")
- `jsonb`: JSON Binary (TEXT length="-1" subtype="jsonb")
- `json`: JSON Text (TEXT length="-1" subtype="json")
- `inet`: IP адрес (TEXT subtype="inet")
- `array`: Массив (TEXT subtype="array")
- `timestamptz`: Timestamp с timezone (TIMESTAMP timezone="UTC" subtype="timestamptz")

### Специальные типы (через subtype)

**UUID:**
```xml
<Field name="user_id" type="TEXT" length="-1" subtype="uuid"></Field>
<R>e5f1c2a3-8d7b-4c9e-a1f0-2b3c4d5e6f7a</R>
```

**JSONB:**
```xml
<Field name="metadata" type="TEXT" length="-1" subtype="jsonb"></Field>
<R>{&quot;key&quot;:&quot;value&quot;,&quot;count&quot;:42}</R>
```

**INET:**
```xml
<Field name="ip_address" type="TEXT" length="-1" subtype="inet"></Field>
<R>192.168.1.100</R>
```

**ARRAY:**
```xml
<Field name="tags" type="TEXT" length="-1" subtype="array"></Field>
<R>{tag1,tag2,tag3}</R>
```

---

## Compact Format v1.3.1

### Проблема

В 1-to-many JOIN паттернах (view-ы, отчёты) многие столбцы повторяют одно и то же значение в каждой строке. В базовом формате v1.0 дублирование приводит к 50–70% overhead.

**Пример дублирования (v1.0):**
```xml
<Data>
  <R>10|Sales|Moscow|101|Ivan Petrov|45000</R>
  <R>10|Sales|Moscow|102|Anna Sidorova|52000</R>
  <R>10|Sales|Moscow|103|Boris Kozlov|48000</R>
</Data>
```

Поля `dept_id`, `dept_name`, `location` повторяются в каждой строке.

### Решение

Три дополняющих механизма v1.3.1:

1. **`fixed="true"`** на Field — объявляет, что поле не меняется в пределах группы
2. **`compact="true"`** на Data — значения fixed полей пишутся только при смене
3. **`<SpecialValues>`** на Field — маркеры для NULL, Infinity, NaN, NoDate

### Fixed Fields

Атрибут `fixed="true"` на `<Field>` сигнализирует процессору, что значение поля постоянно в пределах группы строк.

```xml
<Schema>
  <Field name="dept_id"   type="INTEGER" fixed="true"></Field>   <!-- постоянное -->
  <Field name="dept_name" type="TEXT"    fixed="true"></Field>   <!-- постоянное -->
  <Field name="emp_id"    type="INTEGER"></Field>                <!-- переменное -->
  <Field name="emp_name"  type="TEXT"></Field>                   <!-- переменное -->
</Schema>
```

**Соглашение для SQL view (`_prefix`):**

При создании view используйте префикс `_` для обозначения fixed полей. tdtpcli автоматически обнаруживает их, убирает `_` из имени и устанавливает `fixed="true"`:

```sql
CREATE VIEW dept_employees_report AS
SELECT
  d.dept_id   AS _dept_id,     -- будет: name="dept_id" fixed="true"
  d.dept_name AS _dept_name,   -- будет: name="dept_name" fixed="true"
  d.location  AS _location,    -- будет: name="location" fixed="true"
  e.emp_id,                    -- переменное
  e.full_name
FROM employees e
JOIN departments d ON e.dept_id = d.dept_id
ORDER BY dept_id, emp_id;
```

### Compact Format

При `compact="true"` на `<Data>` значения fixed полей записываются только:
- в первой строке (первая строка группы — **header row**)
- при смене значения fixed поля (начало новой группы)

В остальных строках группы на позициях fixed полей стоят пустые строки (`||`).

**Пример (3 отдела по 5 сотрудников):**
```xml
<Data compact="true">
  <!-- dept 10 — header row: все значения -->
  <R>10|Sales|Moscow|101|Ivan Petrov|45000</R>
  <!-- carry-forward: dept_id/dept_name/location из предыдущей строки -->
  <R>|||102|Anna Sidorova|52000</R>
  <R>|||103|Boris Kozlov|48000</R>
  <R>|||104|Elena Novikova|55000</R>
  <R>|||105|Dmitry Smirnov|49500</R>
  <!-- dept 20 — новая группа: снова все значения -->
  <R>20|Engineering|Saint Petersburg|201|Alice Volkov|72000</R>
  <R>|||202|Charlie Morozov|65000</R>
  <R>|||203|Diana Popova|69000</R>
  <R>|||204|Egor Lebedev|61000</R>
  <R>|||205|Fiona Kuznetsova|78000</R>
</Data>
```

**Алгоритм декодера (carry-forward):**

```
currentFixed = []

для каждой строки:
  для каждой позиции i:
    если поле[i].fixed == true:
      если values[i] != "":
        currentFixed[i] = values[i]   // новое значение → обновить carry
      иначе:
        values[i] = currentFixed[i]   // пропуск → взять из carry
```

**Важно:** декодер не проверяет корректность `fixed="true"` — ответственность на отправителе.

### Порядок обработки (кодирование)

```
1. Определить fixed поля из Schema (или по _prefix, или по --fixed-fields)
2. Для каждой строки:
   - если значение fixed поля = предыдущему → записать ""
   - иначе → записать значение явно
3. Установить compact="true" на <Data>
4. Установить version="1.3.1" на пакете
5. Опционально: сжать данные compression="zstd"
```

**Порядок обработки (декодирование):**

```
1. Распаковать zstd (если compression="zstd")
2. Если compact="true": expand carry-forward → нормализованные строки
3. Обработать <SpecialValues> маркеры
4. Импортировать как обычный набор строк
```

### Комбинация с compression

Оба механизма совместимы:

```xml
<Data compression="zstd" compact="true">
  <R>KLUv/WBgVKEAAesEA...base64-compressed-compact-data...</R>
</Data>
```

### Экономия размера

| Сценарий | v1.0 | v1.3.1 compact | Экономия |
|----------|------|----------------|----------|
| 3 fixed поля × 15 строк | 100% | ~30% | ~70% |
| + zstd compression | 100% | ~10–15% | ~85–90% |

---

## TDTQL - Query Language

**TDTQL** (Table Data Transfer Query Language) - язык запросов для фильтрации и сортировки табличных данных.

### Структура запроса

```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <!-- Условия фильтрации -->
  </Filters>
  <OrderBy>
    <!-- Сортировка -->
  </OrderBy>
  <Limit>100</Limit>
  <Offset>0</Offset>
</Query>
```

### Операторы сравнения

| Operator | Описание | SQL аналог | Пример |
|----------|----------|------------|--------|
| `eq` | Равно | `=` | `<Filter field="age" operator="eq" value="25"/>` |
| `ne` | Не равно | `!=`, `<>` | `<Filter field="status" operator="ne" value="deleted"/>` |
| `gt` | Больше | `>` | `<Filter field="balance" operator="gt" value="1000"/>` |
| `gte` | Больше или равно | `>=` | `<Filter field="age" operator="gte" value="18"/>` |
| `lt` | Меньше | `<` | `<Filter field="price" operator="lt" value="100"/>` |
| `lte` | Меньше или равно | `<=` | `<Filter field="quantity" operator="lte" value="10"/>` |

### Операторы диапазонов и списков

| Operator | Описание | SQL аналог | Пример |
|----------|----------|------------|--------|
| `between` | В диапазоне | `BETWEEN` | `<Filter field="age" operator="between" value="18" value2="65"/>` |
| `in` | В списке | `IN` | `<Filter field="city" operator="in" value="Moscow,SPb,Kazan"/>` |
| `not_in` | Не в списке | `NOT IN` | `<Filter field="status" operator="not_in" value="deleted,archived"/>` |

### Операторы паттернов

| Operator | Описание | SQL аналог | Пример |
|----------|----------|------------|--------|
| `like` | Соответствует паттерну | `LIKE` | `<Filter field="email" operator="like" value="%@example.com"/>` |
| `not_like` | Не соответствует паттерну | `NOT LIKE` | `<Filter field="username" operator="not_like" value="test%"/>` |

Wildcards:
- `%` - любое количество символов
- `_` - один символ

### Операторы NULL

| Operator | Описание | SQL аналог | Пример |
|----------|----------|------------|--------|
| `is_null` | Значение NULL | `IS NULL` | `<Filter field="deleted_at" operator="is_null"/>` |
| `is_not_null` | Значение НЕ NULL | `IS NOT NULL` | `<Filter field="email" operator="is_not_null"/>` |

### Логические операторы

**AND:**
```xml
<Filters>
  <And>
    <Filter field="age" operator="gte" value="18"/>
    <Filter field="is_active" operator="eq" value="1"/>
  </And>
</Filters>
```

**OR:**
```xml
<Filters>
  <Or>
    <Filter field="city" operator="eq" value="Moscow"/>
    <Filter field="city" operator="eq" value="SPb"/>
  </Or>
</Filters>
```

**Вложенные группы:**
```xml
<Filters>
  <And>
    <Filter field="is_active" operator="eq" value="1"/>
    <Or>
      <Filter field="city" operator="eq" value="Moscow"/>
      <Filter field="city" operator="eq" value="SPb"/>
    </Or>
  </And>
</Filters>
```

SQL эквивалент:
```sql
WHERE is_active = 1 AND (city = 'Moscow' OR city = 'SPb')
```

### Сортировка (OrderBy)

**Одиночная:**
```xml
<OrderBy field="balance" direction="DESC"></OrderBy>
```

**Множественная:**
```xml
<OrderBy>
  <Fields>
    <OrderField name="balance" direction="DESC"/>
    <OrderField name="created_at" direction="ASC"/>
  </Fields>
</OrderBy>
```

**Direction:**
- `ASC` - по возрастанию (default)
- `DESC` - по убыванию

### Пагинация

```xml
<Limit>100</Limit>
<Offset>200</Offset>
```

- **Limit** - максимальное количество записей
- **Offset** - пропустить N записей

SQL эквивалент:
```sql
LIMIT 100 OFFSET 200
```

### Полный пример TDTQL

**Запрос:**
```
Найти активных пользователей старше 18 лет с балансом >= 1000,
из Москвы или СПб, отсортировать по балансу (убывание),
вернуть первые 50 записей
```

**TDTQL:**
```xml
<Query language="TDTQL" version="1.0">
  <Filters>
    <And>
      <Filter field="is_active" operator="eq" value="1"/>
      <Filter field="age" operator="gte" value="18"/>
      <Filter field="balance" operator="gte" value="1000"/>
      <Or>
        <Filter field="city" operator="eq" value="Moscow"/>
        <Filter field="city" operator="eq" value="SPb"/>
      </Or>
    </And>
  </Filters>
  <OrderBy field="balance" direction="DESC"></OrderBy>
  <Limit>50</Limit>
  <Offset>0</Offset>
</Query>
```

**SQL эквивалент:**
```sql
SELECT * FROM users
WHERE is_active = 1
  AND age >= 18
  AND balance >= 1000
  AND (city = 'Moscow' OR city = 'SPb')
ORDER BY balance DESC
LIMIT 50 OFFSET 0
```

---

## Примеры

### Reference Packet (Полный справочник)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>users</TableName>
    <MessageID>REF-2025-abc123-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>3</RecordsInPart>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="email" type="TEXT" length="255"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="is_active" type="BOOLEAN"></Field>
    <Field name="created_at" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>1|john_doe|john@example.com|1500.50|1|2025-01-15 10:30:00</R>
    <R>2|jane_smith|jane@example.com|2300.00|1|2025-01-16 14:20:00</R>
    <R>3|bob_jones|bob@example.com|750.25|0|2025-01-17 09:15:00</R>
  </Data>
</DataPacket>
```

### Request Packet (Запрос данных)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>request</Type>
    <TableName>users</TableName>
    <MessageID>REQ-2025-xyz789</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
    <Sender>ClientApp</Sender>
    <Recipient>ServerDB</Recipient>
  </Header>
  <Query language="TDTQL" version="1.0">
    <Filters>
      <And>
        <Filter field="balance" operator="gte" value="1000"></Filter>
        <Filter field="is_active" operator="eq" value="1"></Filter>
      </And>
    </Filters>
    <OrderBy field="balance" direction="DESC"></OrderBy>
    <Limit>100</Limit>
  </Query>
</DataPacket>
```

### Response Packet (Ответ на запрос)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>response</Type>
    <TableName>users</TableName>
    <MessageID>RESP-2025-def456-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>2</RecordsInPart>
    <Timestamp>2025-11-16T12:00:01Z</Timestamp>
    <Sender>ServerDB</Sender>
    <Recipient>ClientApp</Recipient>
    <InReplyTo>REQ-2025-xyz789</InReplyTo>
  </Header>
  <QueryContext>
    <OriginalQuery language="TDTQL" version="1.0">
      <Filters>
        <And>
          <Filter field="balance" operator="gte" value="1000"></Filter>
          <Filter field="is_active" operator="eq" value="1"></Filter>
        </And>
      </Filters>
      <OrderBy field="balance" direction="DESC"></OrderBy>
      <Limit>100</Limit>
    </OriginalQuery>
    <ExecutionResults>
      <TotalRecordsInTable>1000</TotalRecordsInTable>
      <RecordsAfterFilters>2</RecordsAfterFilters>
      <RecordsReturned>2</RecordsReturned>
      <MoreDataAvailable>false</MoreDataAvailable>
    </ExecutionResults>
  </QueryContext>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="is_active" type="BOOLEAN"></Field>
  </Schema>
  <Data>
    <R>2|jane_smith|2300.00|1</R>
    <R>1|john_doe|1500.50|1</R>
  </Data>
</DataPacket>
```

### Delta Packet (Инкрементальное обновление)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>delta</Type>
    <TableName>users</TableName>
    <MessageID>DELTA-2025-ghi012</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1</RecordsInPart>
    <Timestamp>2025-11-16T12:05:00Z</Timestamp>
  </Header>
  <Query language="TDTQL" version="1.0">
    <Filters>
      <And>
        <Filter field="updated_at" operator="gte" value="2025-11-16 12:00:00"></Filter>
      </And>
    </Filters>
  </Query>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="username" type="TEXT" length="100"></Field>
    <Field name="balance" type="DECIMAL" precision="12" scale="2"></Field>
    <Field name="updated_at" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>1|john_doe|1600.00|2025-11-16 12:03:00</R>
  </Data>
</DataPacket>
```

### Alarm Packet (Уведомление об ошибке)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>alarm</Type>
    <TableName>users</TableName>
    <MessageID>ALARM-2025-err404</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <Timestamp>2025-11-16T12:00:00Z</Timestamp>
    <Sender>ServerDB</Sender>
    <Recipient>MonitoringSystem</Recipient>
  </Header>
  <Alarm>
    <Severity>error</Severity>
    <Code>DB_CONNECTION_FAILED</Code>
    <Message>Failed to connect to PostgreSQL database: connection timeout</Message>
    <AffectedRecords>0</AffectedRecords>
  </Alarm>
</DataPacket>
```

### Error Packet (Управляемая ошибка ETL, v1.3+) 🆕

Генерируется автоматически ETL pipeline при деградации xZMercury (encryption enabled, Mercury недоступен).
Пишется в выходной файл вместо незашифрованных данных. Pipeline завершается с exit 0.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>error</Type>
    <TableName>tdtp_errors</TableName>
    <MessageID>ERR-2026-a1b2c3d4-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1</RecordsInPart>
    <Timestamp>2026-02-26T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="package_uuid"  type="TEXT" length="36" key="true"></Field>
    <Field name="pipeline"      type="TEXT" length="255"></Field>
    <Field name="error_code"    type="TEXT" length="64"></Field>
    <Field name="error_message" type="TEXT" length="1000"></Field>
    <Field name="created_at"    type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>550e8400-e29b-41d4-a716-446655440000|employee-dept-report|MERCURY_UNAVAILABLE|connect: connection refused|2026-02-26T10:00:00Z</R>
  </Data>
</DataPacket>
```

**Коды ошибок:**

| Код | Причина |
|-----|---------|
| `MERCURY_UNAVAILABLE` | xZMercury недоступен (таймаут, connection refused) |
| `MERCURY_ERROR` | xZMercury вернул HTTP 5xx |
| `HMAC_VERIFICATION_FAILED` | Подпись ключа не прошла верификацию |
| `KEY_BIND_REJECTED` | xZMercury отклонил запрос (HTTP 403/429) |

---

### Reference Packet в compact-формате (v1.3.1+) 🆕

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.3.1">
  <Header>
    <Type>reference</Type>
    <TableName>dept_employees_report</TableName>
    <MessageID>REF-2026-compact-001-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>10</RecordsInPart>
    <Timestamp>2026-03-10T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <!-- Три fixed поля (_prefix в SQL view → stripped + fixed=true) -->
    <Field name="dept_id"   type="INTEGER"            fixed="true"></Field>
    <Field name="dept_name" type="TEXT" length="100"  fixed="true"></Field>
    <Field name="location"  type="TEXT" length="100"  fixed="true"></Field>
    <!-- Переменные поля -->
    <Field name="emp_id"    type="INTEGER"></Field>
    <Field name="full_name" type="TEXT" length="100"></Field>
    <Field name="salary"    type="DECIMAL" precision="10" scale="2"></Field>
    <!-- SpecialValues: для NULL в TEXT поле -->
    <Field name="notes" type="TEXT" length="500">
      <SpecialValues>
        <Null marker="[NULL]"/>
      </SpecialValues>
    </Field>
  </Schema>
  <Data compact="true">
    <!-- dept 10 — header row: все 7 значений -->
    <R>10|Sales|Moscow|101|Ivan Petrov|45000.00|on probation</R>
    <!-- carry-forward: dept_id/dept_name/location из строки выше -->
    <R>|||102|Anna Sidorova|52000.00|[NULL]</R>
    <R>|||103|Boris Kozlov|48000.00|[NULL]</R>
    <R>|||104|Elena Novikova|55000.00|team lead</R>
    <R>|||105|Dmitry Smirnov|49500.00|[NULL]</R>
    <!-- dept 20 — новая группа: снова все значения -->
    <R>20|Engineering|Saint Petersburg|201|Alice Volkov|72000.00|[NULL]</R>
    <R>|||202|Charlie Morozov|65000.00|[NULL]</R>
    <R>|||203|Diana Popova|69000.00|[NULL]</R>
    <R>|||204|Egor Lebedev|61000.00|[NULL]</R>
    <R>|||205|Fiona Kuznetsova|78000.00|[NULL]</R>
  </Data>
</DataPacket>
```

**Декодированный результат:**

| dept_id | dept_name | location | emp_id | full_name | salary | notes |
|---------|-----------|----------|--------|-----------|--------|-------|
| 10 | Sales | Moscow | 101 | Ivan Petrov | 45000.00 | on probation |
| 10 | Sales | Moscow | 102 | Anna Sidorova | 52000.00 | NULL |
| 10 | Sales | Moscow | 103 | Boris Kozlov | 48000.00 | NULL |
| ... | ... | ... | ... | ... | ... | ... |
| 20 | Engineering | Saint Petersburg | 201 | Alice Volkov | 72000.00 | NULL |

---

### Reference Packet со сжатием (v1.2+) 🆕

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>orders</TableName>
    <MessageID>REF-2025-compressed-001</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1000</RecordsInPart>
    <Timestamp>2025-12-08T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="id" type="INTEGER" key="true"></Field>
    <Field name="customer_id" type="INTEGER"></Field>
    <Field name="product_name" type="TEXT" length="200"></Field>
    <Field name="quantity" type="INTEGER"></Field>
    <Field name="price" type="DECIMAL" precision="10" scale="2"></Field>
    <Field name="order_date" type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data compression="zstd">
    <R>KLUv/WBgUKEAAesEABWsAgBZCwIIbGFy...base64-encoded-compressed-data...</R>
  </Data>
</DataPacket>
```

**Пояснения к сжатию:**

1. **Атрибут compression="zstd"** указывает, что данные сжаты алгоритмом zstd
2. **Один элемент `<R>`** содержит все сжатые данные (вместо множества строк)
3. **Base64 кодирование** обеспечивает безопасную передачу бинарных данных в XML
4. **RecordsInPart=1000** показывает реальное количество записей после распаковки
5. При распаковке получается 1000 строк в обычном pipe-delimited формате

**Преимущества:**
- Размер пакета уменьшается на 50-80%
- Экономия bandwidth при передаче через message brokers
- Автоматическая обработка в TDTP framework (v1.2+)

**Когда используется:**
- Автоматически для пакетов > 1KB (настраивается)
- Особенно эффективно для больших таблиц
- Рекомендуется для передачи через медленные каналы

---

## Версионирование

**Текущая версия:** 1.3.1

**Changelog:**

- **v1.3.1** (10.03.2026) 🆕
  - **Fixed Fields** — атрибут `fixed="true"` на `<Field>`
    - Поле объявляется постоянным в пределах группы строк
    - Конвенция `_fieldname` в SQL view для auto-detect (tdtpcli убирает `_`, ставит `fixed=true`)
  - **Compact Format** — атрибут `compact="true"` на `<Data>`
    - Значения fixed полей записываются только в первой строке группы (header row)
    - Остальные строки группы имеют пустые слоты `||` на позициях fixed полей
    - Смена значения fixed поля инициирует новую группу (carry-forward сбрасывается)
    - Совместим с `compression="zstd"` (порядок: decode zstd → expand compact)
    - Экономия: 50–70% на повторяющихся значениях; до 85–90% в комбинации с zstd
  - **Special Values** — дочерний элемент `<SpecialValues>` на `<Field>`
    - `<Null marker="..."/>` — для TEXT: различает NULL и `""` (empty string)
    - `<Infinity marker="..."/>`, `<NegInfinity marker="..."/>`, `<NaN marker="..."/>` — для REAL/DECIMAL
    - `<NoDate marker="..."/>` — для DATE/TIMESTAMP: sentinel «нет даты», отличный от NULL
  - **Backward compatibility:** reader v1.0 читает пакеты v1.3.1, игнорируя compact/fixed/SpecialValues
  - **Forward compatibility:** reader v1.3.1 читает пакеты v1.0 без изменений

- **v1.3** (26.02.2026) 🆕
  - **Тип пакета `error`** — стандартный DataPacket для фиксации ошибок в ETL pipeline
    - Таблица `tdtp_errors` с полями: `package_uuid`, `pipeline`, `error_code`, `error_message`, `created_at`
    - Генерируется автоматически при деградации xZMercury
    - Совместим со всеми downstream-потребителями (в отличие от `alarm`)
  - **Шифрование AES-256-GCM** через xZMercury (UUID-binding флоу)
    - Бинарный заголовок: `[2B version][1B algo][16B package_uuid][12B nonce][ciphertext]`
    - Ключ получается из xZMercury, НЕ передаётся через CLI
    - HMAC-SHA256 верификация ключа (`MERCURY_SERVER_SECRET`)
    - При недоступности Mercury → error-пакет вместо данных, exit 0
  - **pkg/mercury**: HTTP клиент для xZMercury UUID-binding + burn-on-read флоу
  - **pkg/crypto**: AES-256-GCM шифрование/дешифрование
  - **cmd/xzmercury-mock**: standalone mock-сервер для E2E тестирования
  - **ETL CLI**: флаги `--enc` (override encryption) и `--enc-dev` (локальный ключ, !production)
  - **ResultLog**: статус `completed_with_errors`, поле `package_uuid`

- **v1.2** (08.12.2025)
  - **Поддержка сжатия данных zstd**
    - Атрибут `compression="zstd"` для элемента Data
    - Base64-кодирование сжатых данных
    - Автоматическое сжатие для пакетов > 1KB
    - Коэффициент сжатия: 50-80%
  - Production Features: Circuit Breaker, Retry, Audit, Incremental Sync
  - Data Processors: Compression, Masking, Validation, Normalization
  - XLSX Converter (Database ↔ Excel)
  - Kafka broker integration
  - MySQL adapter

- **v1.0** (16.11.2025)
  - Первый production release
  - Полная реализация Core Modules (Packet, Schema, TDTQL)
  - Адаптеры: SQLite, PostgreSQL, MS SQL Server
  - Message Brokers: RabbitMQ, MSMQ
  - CLI утилита tdtpcli
  - Максимальный размер пакета: 3.8MB
  - Поддержка subtypes: UUID, JSONB, INET, ARRAY

---

## Лицензия

MIT License

Copyright (c) 2025 TDTP Framework

---

## Контакты

- **GitHub:** https://github.com/ruslano69/tdtp-framework
- **Email:** ruslano69@gmail.com
- **Документация:** https://github.com/ruslano69/tdtp-framework/tree/main/docs

---

*Последнее обновление: 26.02.2026*
