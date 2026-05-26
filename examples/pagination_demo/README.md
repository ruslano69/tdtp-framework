# pagination_demo — Пагинация больших таблиц

Демонстрация автоматической разбивки на части при экспорте таблиц с большим количеством строк.  
Пакеты ограничены 2MB — при превышении данные автоматически делятся на `PartNumber/TotalParts`.

## Что показывает

1. Подключение к SQLite с произвольной таблицей
2. Полный экспорт через `ExportTable` — автоматическая пагинация
3. Вывод статистики по каждой части: номер, строки, размер XML

## Запуск

```bash
# На существующей БД
go run main.go path/to/database.db [table_name]

# Без аргументов — создаёт demo БД на 10 000 строк
go run main.go
```

## Вывод

```
Part 1/3: 1000 rows, 512 KB
Part 2/3: 1000 rows, 510 KB
Part 3/3:  324 rows, 166 KB
Total: 2324 rows across 3 parts
```

## Ключевые API

```go
packets, _ := adapter.ExportTable(ctx, "users")
for _, pkt := range packets {
    fmt.Printf("Part %d/%d: %d rows\n",
        pkt.Header.PartNumber,
        pkt.Header.TotalParts,
        pkt.Header.RecordsInPart)
}
```

## Настройка размера пакета

Максимальный размер задаётся в конфиге (по умолчанию 3.8 MB):
```yaml
export:
  max_packet_size: 2097152  # 2 MB
```

## См. также

- [`examples/01-basic-export`](../01-basic-export) — базовый экспорт с PostgreSQL
- [`examples/03-incremental-sync`](../03-incremental-sync) — инкрементальная синхронизация
