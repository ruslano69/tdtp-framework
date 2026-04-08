# Brokers Benchmarks

Message brokers (Kafka, RabbitMQ, MSMQ) performance.

## Файлы

| Файл | Описание |
|------|----------|
| `pkg/brokers/kafka_bench_test.go` | Kafka publish/consume, SendBatch, parallel compress |

## Запуск

```bash
# Требуется Kafka на localhost:9092
go test -bench=. -benchmem ./pkg/brokers/... -tags=!nokafka
```

## Метрики

- **PublishThroughput**: сообщений в секунду
- **ConsumeLatency**: задержка при чтении
- **CompressionRatio**: kanzi vs zstd vs none
- **SendBatch**: batch отправка vs одиночные