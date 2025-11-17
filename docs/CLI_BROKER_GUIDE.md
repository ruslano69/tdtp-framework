# TDTP CLI - Message Broker Integration

Руководство по работе с брокерами сообщений в TDTP CLI v1.2.

## Поддерживаемые брокеры

✅ **RabbitMQ** - AMQP message broker
✅ **MSMQ** - Microsoft Message Queuing
✅ **Kafka** - Distributed event streaming platform

---

## Конфигурация

### RabbitMQ

```yaml
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_queue
  vhost: /
```

### MSMQ

```yaml
broker:
  type: msmq
  queue: .\Private$\tdtp_queue
```

### Kafka

```yaml
broker:
  type: kafka
  host: localhost
  port: 9092
  queue: tdtp_topic  # topic name
```

---

## Команды

### Экспорт в брокер

Экспортирует данные таблицы напрямую в очередь брокера.

```bash
tdtpcli --export-broker <table> --config broker.yaml
```

**Примеры:**

```bash
# Экспорт всей таблицы в RabbitMQ
tdtpcli --export-broker orders --config rabbitmq.yaml

# Экспорт с фильтром
tdtpcli --export-broker customers \
  --where "status = active" \
  --config rabbitmq.yaml

# Экспорт в MSMQ
tdtpcli --export-broker products --config msmq.yaml

# Экспорт в Kafka
tdtpcli --export-broker events --config kafka.yaml
```

**Что происходит:**
1. Данные экспортируются из БД
2. Конвертируются в TDTP XML
3. Отправляются в очередь брокера
4. Каждый packet = одно сообщение

### Импорт из брокера

Импортирует данные из очереди брокера в базу данных.

```bash
tdtpcli --import-broker --config broker.yaml --strategy <strategy>
```

**Примеры:**

```bash
# Импорт из RabbitMQ
tdtpcli --import-broker --config rabbitmq.yaml --strategy replace

# Импорт из MSMQ
tdtpcli --import-broker --config msmq.yaml --strategy ignore

# Импорт из Kafka
tdtpcli --import-broker --config kafka.yaml --strategy fail
```

**Что происходит:**
1. Читаются сообщения из очереди
2. Парсятся TDTP XML пакеты
3. Данные импортируются в БД согласно стратегии
4. Обработка до 100 сообщений (защита от бесконечного цикла)

---

## Стратегии импорта

При импорте из брокера доступны все стандартные стратегии:

- `--strategy replace` - Обновление существующих записей (по умолчанию)
- `--strategy ignore` - Игнорирование дубликатов
- `--strategy fail` - Прерывание при дубликатах
- `--strategy copy` - Копирование с новыми ключами

---

## Production Features

Broker операции автоматически получают все production features:

### Circuit Breaker

Защита от сбоев брокера:

```yaml
resilience:
  circuit_breaker:
    enabled: true
    threshold: 5        # Открыть после 5 ошибок
    timeout: 60         # Закрыть через 60 секунд
```

**Поведение:**
- При недоступности брокера → Circuit Breaker открывается
- Последующие попытки → быстрый fail
- После timeout → Half-Open (пробная попытка)

### Retry Mechanism

Автоматические повторы при временных сбоях:

```yaml
resilience:
  retry:
    enabled: true
    max_attempts: 3
    strategy: exponential
    initial_wait_ms: 1000
```

**Применяется к:**
- Connection errors
- Network timeouts
- Temporary broker unavailability

### Audit Logging

Все broker операции логируются:

```log
[2024-11-17 16:00:00] [SUCCESS] EXPORT table=orders broker=rabbitmq queue=tdtp_queue
[2024-11-17 16:00:05] [SUCCESS] IMPORT broker=rabbitmq queue=tdtp_queue strategy=replace
[2024-11-17 16:00:10] [FAILURE] EXPORT table=products broker=rabbitmq error="connection refused"
```

---

## Примеры использования

### Сценарий 1: Async data transfer

```bash
# Server A: Export to RabbitMQ
tdtpcli --config serverA.yaml \
  --export-broker orders \
  --where "date = CURRENT_DATE"

# Server B: Import from RabbitMQ
tdtpcli --config serverB.yaml \
  --import-broker \
  --strategy replace
```

### Сценарий 2: Event streaming

```bash
# Publish events to Kafka
tdtpcli --config kafka.yaml \
  --export-broker user_events \
  --where "event_time > '2024-11-17 00:00:00'"

# Consume events from Kafka
tdtpcli --config kafka_consumer.yaml \
  --import-broker \
  --strategy ignore
```

### Сценарий 3: Cross-platform integration

```bash
# Windows: Export to MSMQ
tdtpcli --config msmq.yaml \
  --export-broker transactions

# Linux: Import from RabbitMQ bridge
tdtpcli --config rabbitmq.yaml \
  --import-broker \
  --strategy copy
```

### Сценарий 4: Batch processing

```bash
#!/bin/bash
# batch_export_to_broker.sh

TABLES="orders customers products transactions"

for TABLE in $TABLES; do
  echo "Exporting $TABLE to RabbitMQ..."

  tdtpcli --export-broker "$TABLE" \
    --config rabbitmq.yaml \
    --where "updated_at >= CURRENT_DATE - 1"

  if [ $? -eq 0 ]; then
    echo "✓ $TABLE exported"
  else
    echo "✗ $TABLE export failed"
  fi
done
```

### Сценарий 5: Data pipeline

```bash
#!/bin/bash
# etl_pipeline_with_broker.sh

# Step 1: Export from source DB to RabbitMQ
tdtpcli --config source.yaml \
  --export-broker transactions \
  --where "date = CURRENT_DATE - 1"

# Step 2: Process in intermediate service (RabbitMQ consumer)
# ... external processing ...

# Step 3: Import from RabbitMQ to target DB
tdtpcli --config target.yaml \
  --import-broker \
  --strategy replace
```

---

## Мониторинг

### Проверка очереди RabbitMQ

```bash
# RabbitMQ Management CLI
rabbitmqadmin list queues name messages

# Проверить количество сообщений в очереди
rabbitmqadmin get queue=tdtp_queue count=10
```

### Проверка MSMQ

```powershell
# PowerShell
Get-MsmqQueue -Name tdtp_queue | Select-Object MessageCount
```

### Проверка Kafka

```bash
# Kafka CLI
kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic tdtp_topic \
  --from-beginning \
  --max-messages 1
```

---

## Troubleshooting

### Connection refused

```
Error: failed to connect to broker: connection refused
```

**Решение:**
1. Проверить что брокер запущен
2. Проверить host и port в конфигурации
3. Проверить firewall правила
4. Увеличить timeout в конфигурации

### Authentication failed

```
Error: failed to connect to broker: authentication failed
```

**Решение:**
1. Проверить user/password в конфигурации
2. Проверить права доступа к очереди
3. Для RabbitMQ проверить vhost

### Queue not found

```
Error: queue 'tdtp_queue' does not exist
```

**Решение:**
1. Создать очередь вручную
2. Для RabbitMQ: включить автосоздание очередей
3. Проверить имя очереди в конфигурации

### Circuit Breaker open

```
Error: circuit breaker is open
```

**Решение:**
1. Дождаться timeout (60s по умолчанию)
2. Проверить доступность брокера
3. Увеличить threshold в config.yaml

---

## Производительность

### Batch size

TDTP CLI отправляет каждый packet как отдельное сообщение.
Для контроля размера используйте:

```bash
# Экспорт с лимитом
tdtpcli --export-broker large_table \
  --limit 10000 \
  --config broker.yaml
```

### Throughput

Типичная производительность:
- RabbitMQ: ~5,000 msg/sec
- MSMQ: ~1,000 msg/sec
- Kafka: ~10,000 msg/sec

### Memory usage

Для больших таблиц используйте фильтрацию:

```bash
# Экспорт по частям
for day in {1..30}; do
  tdtpcli --export-broker events \
    --where "date = CURRENT_DATE - $day" \
    --config broker.yaml
done
```

---

## Интеграция с другими фичами

### С маскированием данных

```bash
# Экспорт с маскированием PII перед отправкой в брокер
tdtpcli --export-broker customers \
  --mask email,phone,ssn \
  --config rabbitmq.yaml
```

### С фильтрацией (TDTQL)

```bash
# Экспорт только активных пользователей
tdtpcli --export-broker users \
  --where "status = active AND created_at > '2024-01-01'" \
  --order-by "created_at DESC" \
  --limit 1000 \
  --config kafka.yaml
```

### С валидацией

```bash
# Импорт с валидацией данных
tdtpcli --import-broker \
  --validate rules.yaml \
  --strategy fail \
  --config rabbitmq.yaml
```

---

## Заключение

TDTP CLI v1.2 предоставляет полную интеграцию с брокерами сообщений:

✅ **Поддержка**: RabbitMQ, MSMQ, Kafka
✅ **Надежность**: Circuit Breaker, Retry, Audit
✅ **Безопасность**: PII masking
✅ **Гибкость**: TDTQL фильтрация, стратегии импорта

Для получения дополнительной информации см. [CLI_v1.2_FEATURES.md](CLI_v1.2_FEATURES.md).
