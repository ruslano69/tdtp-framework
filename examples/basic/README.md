# Basic — Core Packet API

Минимальный пример работы с пакетами TDTP напрямую через Go API (без БД и брокеров).

## Что показывает

1. **Reference** — создание пакета с таблицей клиентов, запись в XML
2. **Parse** — чтение и разбор XML обратно в структуры
3. **Request** — создание запроса с TDTQL фильтрами (`IsActive=1 AND Balance>1000`)
4. **Response** — ответ на запрос с `QueryContext` (статистика выполнения)

## Запуск

```bash
go run main.go
```

Файлы сохраняются во `/tmp/`: `reference_part_1.xml`, `request.xml`, `response.xml`.

## Ключевые API

```go
generator := packet.NewGenerator()

// Reference
packets, _ := generator.GenerateReference("CustTable", schema, rows)

// Request с фильтром
query := packet.NewQuery()
query.Filters = &packet.Filters{And: &packet.LogicalGroup{...}}
requestPkt, _ := generator.GenerateRequest("CustTable", query, "SystemA", "SystemB")

// Response с QueryContext
responsePkts, _ := generator.GenerateResponse("CustTable", requestPkt.Header.MessageID,
    schema, rows, queryContext, "SystemB", "SystemA")

// Парсинг
parser := packet.NewParser()
pkt, _ := parser.ParseFile("file.xml")
values := parser.GetRowValues(pkt.Data.Rows[0])
```

## См. также

- [`examples/read-tdtp`](../read-tdtp) — чтение произвольного TDTP файла
- [`examples/executor`](../executor) — фильтрация данных через TDTQL in-memory
