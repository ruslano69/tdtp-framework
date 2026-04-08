# Adapters Benchmarks

Database adapters performance comparison.

## Файлы

| Файл | Описание |
|------|----------|
| `pkg/adapters/sqlite/benchmark_test.go` | SQLite performance |
| `pkg/adapters/strategy_benchmark_test.go` | Import strategy comparison |
| `pkg/adapters/factory_benchmark_test.go` | Adapter factory overhead |
| `pkg/adapters/database_comparison_benchmark_test.go` | Cross-DB comparison |

## Запуск

```bash
go test -bench=. -benchmem ./pkg/adapters/...
```

## Тестируемые операции

- **Export**: SELECT → XML
- **Import**: INSERT batch performance
- **Factory**: Adapter creation overhead
- **Strategy**: REPLACE vs IGNORE vs COPY performance