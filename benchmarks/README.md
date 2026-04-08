# Benchmarks Overview

Систематизированные бенчмарки проекта по категориям.

## Структура

| Папка | Что тестирует | Файлы |
|-------|---------------|-------|
| `core/` | XML parsing, compression | parser_bench_test.go |
| `brokers/` | Kafka, RabbitMQ, MSMQ performance | kafka_bench_test.go |
| `adapters/` | SQLite, PostgreSQL, MSSQL, MySQL comparison | strategy_benchmark_test.go, factory_benchmark_test.go, database_comparison_benchmark_test.go, sqlite/benchmark_test.go |
| `etl/` | ETL pipeline, Kafka spool | kafka_spool_bench_test.go, kafka_spool_real_bench_test.go |
| `tools/` | Standalone bench tools | bench_raw/, export_benchmark_py.py |

## Запуск

### Makefile (рекомендуется)

```bash
# Все бенчмарки
make bench-all

# Конкретная категория
make bench-core      # XML parsing, compression
make bench-kafka     # Kafka performance  
make bench-adapters  # DB comparison
make bench-etl       # ETL pipeline

# Специфичные
make bench-parser    # Parser only
make bench-db        # SQLite benchmark
```

### Go тесты напрямую

```bash
go test -bench=. ./pkg/core/packet/...
go test -bench=. ./pkg/brokers/...
go test -bench=. ./pkg/adapters/...
go test -bench=. ./pkg/etl/...
```

## Требования

- **Kafka benches**: Kafka на localhost:9092
- **DB benches**: Соответствующие БД должны быть доступны
- **Real ETL bench**: Kafka + SQLite

## Результаты

Результаты сохраняются в:
- stdout (бинарный вывод Go bench)
- Для детального анализа: `go test -bench=. -benchmem > results.txt`