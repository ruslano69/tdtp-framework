# TDTP Framework - TDTQL Translator Documentation

## Назначение

TDTQL Translator преобразует SQL запросы в TDTQL фильтры для использования в TDTP протоколе.

**Возможности:**
- Парсинг SQL WHERE, ORDER BY, LIMIT, OFFSET
- Поддержка всех операторов сравнения (=, !=, <, >, <=, >=)
- Специальные операторы (IN, BETWEEN, LIKE, IS NULL)
- Логические операторы (AND, OR, NOT)
- Приоритеты и скобки
- Множественная сортировка

## Архитектура

```
pkg/core/tdtql/
├── lexer.go       # Токенизация SQL
├── ast.go         # AST структуры
├── parser.go      # Парсинг в AST
├── generator.go   # AST → TDTQL
├── translator.go  # Главный API
└── tdtql_test.go  # 15 тестов
```

**Процесс трансляции:**
```
SQL String → Lexer → Tokens → Parser → AST → Generator → TDTQL Query
```

## API

### Основной API

```go
translator := tdtql.NewTranslator()

// Полная трансляция SQL
query, err := translator.Translate(sql)

// Только WHERE часть
filters, err := translator.TranslateWhere("IsActive = 1 AND Balance > 0")

// Получить AST (для отладки)
ast, err := translator.GetAST(sql)

// Валидация SQL
err := translator.ValidateSQL(sql)
```

## Поддерживаемый SQL

### Операторы сравнения

```sql
-- Равенство
field = 'value'
field != 'value'
field <> 'value'

-- Сравнение
field > 100
field >= 100
field < 100
field <= 100

-- LIKE
field LIKE '%pattern%'
field NOT LIKE 'pattern%'
```

**Результат:**
```xml
<Filter field="field" operator="eq|ne|gt|gte|lt|lte|like|not_like" value="..."/>
```

### IN оператор

```sql
field IN ('value1', 'value2', 'value3')
field NOT IN (1, 2, 3)
```

**Результат:**
```xml
<Filter field="field" operator="in" value="value1,value2,value3"/>
<Filter field="field" operator="not_in" value="1,2,3"/>
```

### BETWEEN оператор

```sql
field BETWEEN 10 AND 100
```

**Результат:**
```xml
<Filter field="field" operator="between" value="10" value2="100"/>
```

### IS NULL оператор

```sql
field IS NULL
field IS NOT NULL
```

**Результат:**
```xml
<Filter field="field" operator="is_null"/>
<Filter field="field" operator="is_not_null"/>
```

### Логические операторы

**AND:**
```sql
field1 = 1 AND field2 > 100 AND field3 = 'active'
```

**Результат:**
```xml
<Filters>
  <And>
    <Filter field="field1" operator="eq" value="1"/>
    <Filter field="field2" operator="gt" value="100"/>
    <Filter field="field3" operator="eq" value="active"/>
  </And>
</Filters>
```

**OR:**
```sql
City = 'Moscow' OR City = 'SPb' OR City = 'Kazan'
```

**Результат:**
```xml
<Filters>
  <Or>
    <Filter field="City" operator="eq" value="Moscow"/>
    <Filter field="City" operator="eq" value="SPb"/>
    <Filter field="City" operator="eq" value="Kazan"/>
  </Or>
</Filters>
```

**Комбинация AND/OR:**
```sql
IsActive = 1 AND (Balance > 1000 OR VIP = 1)
```

**Результат:**
```xml
<Filters>
  <And>
    <Filter field="IsActive" operator="eq" value="1"/>
    <Or>
      <Filter field="Balance" operator="gt" value="1000"/>
      <Filter field="VIP" operator="eq" value="1"/>
    </Or>
  </And>
</Filters>
```

### Приоритеты операторов

**NOT > AND > OR**

```sql
-- Скобки изменяют приоритет
field1 = 1 AND field2 = 2 OR field3 = 3
-- = (field1 = 1 AND field2 = 2) OR field3 = 3

field1 = 1 AND (field2 = 2 OR field3 = 3)
-- = field1 = 1 AND (field2 = 2 OR field3 = 3)
```

### ORDER BY

**Простая сортировка:**
```sql
ORDER BY Balance DESC
```

**Результат:**
```xml
<OrderBy field="Balance" direction="DESC"/>
```

**Множественная сортировка:**
```sql
ORDER BY City ASC, Balance DESC, Name ASC
```

**Результат:**
```xml
<OrderBy>
  <Field name="City" direction="ASC"/>
  <Field name="Balance" direction="DESC"/>
  <Field name="Name" direction="ASC"/>
</OrderBy>
```

### LIMIT и OFFSET

```sql
LIMIT 100 OFFSET 50
```

**Результат:**
```xml
<Limit>100</Limit>
<Offset>50</Offset>
```

## Примеры использования

### 1. Простая трансляция

```go
translator := tdtql.NewTranslator()

sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
query, err := translator.Translate(sql)
if err != nil {
    log.Fatal(err)
}

// query готов к использованию в TDTP Request
```

### 2. Сложный запрос

```go
sql := `SELECT * FROM CustTable
    WHERE IsActive = 1
      AND (Balance > 1000 OR Balance < -1000)
      AND (City = 'Moscow' OR City = 'SPb')
    ORDER BY Balance DESC
    LIMIT 100
    OFFSET 0`

query, err := translator.Translate(sql)

// query содержит:
// - Filters с древовидной структурой AND/OR
// - OrderBy
// - Limit и Offset
```

### 3. Интеграция с DataPacket

```go
translator := tdtql.NewTranslator()

// 1. Транслируем SQL
sql := "SELECT * FROM Products WHERE Price BETWEEN 100 AND 1000 ORDER BY Price ASC"
query, err := translator.Translate(sql)
if err != nil {
    log.Fatal(err)
}

// 2. Создаем Request пакет
generator := packet.NewGenerator()
requestPacket, err := generator.GenerateRequest(
    "Products",
    query,
    "ClientApp",
    "ServerApp",
)

// 3. Отправляем через message queue
// ...
```

### 4. Только фильтры

```go
// Если нужно только WHERE без SELECT
whereClause := "Status = 'active' AND CreatedAt > '2025-01-01'"
filters, err := translator.TranslateWhere(whereClause)

// Использование в существующем Query
query := packet.NewQuery()
query.Filters = filters
query.Limit = 50
```

### 5. Валидация SQL

```go
sql := "SELECT * FROM Users WHERE invalid syntax"
err := translator.ValidateSQL(sql)
if err != nil {
    fmt.Printf("SQL ошибка: %v\n", err)
}
```

## Полный пример

```go
package main

import (
    "fmt"
    "log"
    "encoding/xml"
    
    "github.com/queuebridge/tdtp/pkg/core/packet"
    "github.com/queuebridge/tdtp/pkg/core/tdtql"
)

func main() {
    // SQL запрос
    sql := `SELECT * FROM Orders
        WHERE Status IN ('new', 'processing')
          AND Amount > 1000
          AND (Priority = 'high' OR Customer = 'VIP')
        ORDER BY CreatedAt DESC
        LIMIT 50`
    
    // Трансляция
    translator := tdtql.NewTranslator()
    query, err := translator.Translate(sql)
    if err != nil {
        log.Fatal(err)
    }
    
    // Создание Request
    generator := packet.NewGenerator()
    requestPacket, err := generator.GenerateRequest(
        "Orders",
        query,
        "WebApp",
        "OrderService",
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // Сериализация в XML
    xmlData, err := generator.ToXML(requestPacket, true)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Println(string(xmlData))
}
```

## Ограничения

**Не поддерживается в текущей версии:**
- JOIN (LEFT JOIN, INNER JOIN)
- Подзапросы (subqueries)
- Агрегатные функции (COUNT, SUM, AVG, MIN, MAX)
- GROUP BY и HAVING
- DISTINCT
- UNION, INTERSECT, EXCEPT
- Арифметические выражения в условиях
- Функции в условиях (UPPER, LOWER, etc.)

**Планируется в будущих версиях** согласно спецификации TDTP.

## Производительность

- Парсинг простого запроса: <1ms
- Парсинг сложного запроса (10+ условий): ~2ms
- Генерация TDTQL: <1ms

## Тесты

**15 unit-тестов покрывают:**
- Лексер (токенизация)
- Парсер (все операторы)
- Генератор (TDTQL)
- Транслятор (end-to-end)

Запуск тестов:
```bash
go test -v ./pkg/core/tdtql/
```

## Отладка

### Просмотр AST

```go
translator := tdtql.NewTranslator()
ast, err := translator.GetAST(sql)
if err != nil {
    log.Fatal(err)
}

// Исследование структуры AST
fmt.Printf("Table: %s\n", ast.TableName)
fmt.Printf("WHERE: %+v\n", ast.Where)
```

### Просмотр токенов

```go
lexer := tdtql.NewLexer(sql)
tokens := lexer.GetAllTokens()
for _, tok := range tokens {
    fmt.Println(tok)
}
```

## Интеграция с другими модулями

### С Schema модулем

```go
// Валидация что поля из SQL существуют в схеме
validator := schema.NewValidator()

for _, filter := range query.Filters.And.Filters {
    field, err := validator.GetFieldByName(schemaObj, filter.Field)
    if err != nil {
        log.Printf("Field %s not found in schema", filter.Field)
    }
}
```

### С Packet модулем

```go
// Создание Request → Response цикла
translator := tdtql.NewTranslator()
query, _ := translator.Translate(sql)

// Request
requestPkt, _ := generator.GenerateRequest("Table", query, "A", "B")

// ... выполнение на сервере ...

// Response с QueryContext
responsePkt, _ := generator.GenerateResponse(
    "Table",
    requestPkt.Header.MessageID,
    schema,
    resultRows,
    &packet.QueryContext{
        OriginalQuery: *query, // сохраняем оригинальный запрос
        ExecutionResults: packet.ExecutionResults{...},
    },
    "B",
    "A",
)
```

## Следующие шаги

TDTQL Translator готов! Следующий компонент - **TDTQL Executor** для выполнения запросов на данных.
