# ETL Benchmarks

ETL pipeline and Kafka spool performance.

## Файлы

| Файл | Описание |
|------|----------|
| `pkg/etl/kafka_spool_bench_test.go` | Spool buffer performance |
| `pkg/etl/kafka_spool_real_bench_test.go` | Real end-to-end ETL |

## Запуск

```bash
# Spool only (без реальной Kafka)
go test -bench=. -benchmem ./pkg/etl/...

# Real ETL (требуется Kafka + SQLite)
go test -bench=. -benchmem ./pkg/etl/... -tags=!nokafka
```

## Метрики

- **SpoolThroughput**: буферизация перед Kafka
- **ETLPipeline**: DB → Kafka → Spool → DB latency
- **100K rows**: полное время для 100K записей