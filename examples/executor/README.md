# executor — TDTQL In-Memory Executor

Пример фильтрации данных через TDTQL без базы данных.  
Демонстрирует трансляцию SQL → TDTQL и выполнение запросов в памяти.

## Что показывает

1. Трансляция SQL-запроса в TDTQL (`tdtql.Translator`)
2. Выполнение TDTQL фильтра на срезе строк (`tdtql.Executor`)
3. Несколько вариантов запросов: фильтры, ORDER BY, LIMIT

## Запуск

```bash
go run main.go
```

## Ключевые API

```go
// SQL → TDTQL
translator := tdtql.NewTranslator()
query, _ := translator.Translate(`SELECT * FROM CustTable
    WHERE IsActive = 1 AND Balance > 50000 AND (City = 'Moscow' OR City = 'SPb')
    ORDER BY Balance DESC LIMIT 3`)

// Выполнение фильтра на rows [][]string
executor := tdtql.NewExecutor()
result, _ := executor.Execute(query, rows, schemaObj)
```

## Применение

Полезно когда данные уже загружены как TDTP пакет и нужно отфильтровать строки  
без обращения к БД — например при обработке входящих пакетов в consumer.

## См. также

- [`examples/basic`](../basic) — создание Request пакетов с TDTQL фильтрами
- [`examples/xml_filter_demo`](../xml_filter_demo) — фильтрация через SQLite адаптер
