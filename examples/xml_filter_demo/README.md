# xml_filter_demo — Фильтрация через адаптер SQLite

Пример полного цикла: импорт данных в SQLite → экспорт с TDTQL фильтром → XML.

## Что показывает

1. Создание схемы через `schema.Builder`
2. Импорт TDTP пакета в SQLite (`adapter.ImportPacket`)
3. Экспорт с фильтром `city='Moscow' AND is_active=1` и `ORDER BY age ASC`
4. Сериализация результата в XML файл

## Запуск

```bash
go run main.go
# Создаёт filtered_export.xml в текущей директории
```

## Ключевые API

```go
// Импорт пакета в БД
adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)

// Экспорт с TDTQL фильтром
query := packet.Query{
    Filters: &packet.Filters{
        And: &packet.LogicalGroup{
            Filters: []packet.Filter{
                {Field: "city", Operator: "eq", Value: "Moscow"},
                {Field: "is_active", Operator: "eq", Value: "1"},
            },
        },
    },
    OrderBy: &packet.OrderBy{Field: "age", Direction: "ASC"},
}
packets, _ := adapter.ExportTableWithQuery(ctx, "Users", &query, "Sender", "Recipient")
```

## См. также

- [`examples/executor`](../executor) — фильтрация in-memory без БД
- [`examples/01-basic-export`](../01-basic-export) — полный экспорт из PostgreSQL
