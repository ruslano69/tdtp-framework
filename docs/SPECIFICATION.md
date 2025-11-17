# TDTP v1.0 Specification

**Table Data Transfer Protocol** - спецификация формата обмена табличными данными через message brokers.

**Версия:** 1.0
**Дата:** 16.11.2025
**Статус:** Production Ready

---

## Содержание

1. [Введение](#введение)
2. [Архитектура](#архитектура)
3. [Формат пакетов](#формат-пакетов)
4. [Типы данных](#типы-данных)
5. [TDTQL - Query Language](#tdtql---query-language)
6. [Примеры](#примеры)

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
│   ├── Type            (reference|delta|request|response|alarm)
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
│   └── Row[]           (данные в формате pipe-delimited)
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
| **alarm** | Уведомление об ошибке | Header, Severity, Code, Message |

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

**Атрибуты Field:**

| Атрибут | Тип | Применимо к | Описание |
|---------|-----|-------------|----------|
| name | string | все | Имя поля (обязательное) |
| type | enum | все | Тип данных TDTP (обязательное) |
| length | int | TEXT, BLOB | Максимальная длина (-1 = unlimited) |
| precision | int | DECIMAL | Общее количество цифр |
| scale | int | DECIMAL | Количество цифр после запятой |
| timezone | string | TIMESTAMP, TIME | Часовой пояс (UTC, Local, +03:00) |
| key | bool | любой | Первичный ключ |
| subtype | string | любой | Подтип (uuid, jsonb, inet, array) |

### Data

Данные передаются в формате pipe-delimited (разделитель `|`).

**XML структура:**
```xml
<Data>
  <R>1|john_doe|john@example.com|1500.50|2025-01-15 10:30:00</R>
  <R>2|jane_smith|jane@example.com|2300.00|2025-01-16 14:20:00</R>
</Data>
```

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

---

## Версионирование

**Текущая версия:** 1.0

**Changelog:**

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

*Последнее обновление: 16.11.2025*
