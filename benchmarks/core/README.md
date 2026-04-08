# Core Benchmarks

XML parsing и compression бенчмарки.

## Файлы

| Файл | Описание |
|------|----------|
| `pkg/core/packet/parser_bench_test.go` | XML parsing speed, decompression |

## Запуск

```bash
go test -bench=. -benchmem ./pkg/core/packet/...
```

## Метрики

- **ParseSpeed**: скорость парсинга XML
- **Allocation**: память на строку
- **Decompression**: zstd vs kanzi vs none