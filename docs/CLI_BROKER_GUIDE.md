# TDTP CLI - Message Broker Integration

Руководство по работе с брокерами сообщений в TDTP CLI v1.2.

## Поддерживаемые брокеры

✅ **RabbitMQ** - AMQP message broker
✅ **MSMQ** - Microsoft Message Queuing
✅ **Kafka** - Distributed event streaming platform

---

## Конфигурация

### RabbitMQ

**Базовая конфигурация:**
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

**С TLS и сжатием данных:** 🆕
```yaml
broker:
  type: rabbitmq
  host: rabbitmq.example.com
  port: 5671
  user: producer
  password: secret
  queue: tdtp_queue
  vhost: /production
  # TLS настройки
  tls:
    enabled: true
    ca_cert: /path/to/ca.crt
    client_cert: /path/to/client.crt
    client_key: /path/to/client.key
    skip_verify: false

# Настройки экспорта
export:
  compress: true        # Включить zstd сжатие
  compress_level: 3     # Уровень сжатия (1-22, по умолчанию 3)
```

### MSMQ

```yaml
broker:
  type: msmq
  queue: .\Private$\tdtp_queue

# Настройки экспорта
export:
  compress: true
  compress_level: 3
```

### Kafka

```yaml
broker:
  type: kafka
  host: localhost
  port: 9092
  queue: tdtp_topic  # topic name

# Настройки экспорта
export:
  compress: true
  compress_level: 5   # Для Kafka можно использовать более высокий уровень
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

## Сжатие данных (Data Compression) 🆕

TDTP CLI v1.2 поддерживает автоматическое сжатие данных при экспорте в брокеры с использованием алгоритма **zstd**.

### Преимущества

✅ **Экономия bandwidth**: Снижение сетевой нагрузки в 3-7 раз
✅ **Меньше хранилища**: Уменьшение размера сообщений в очереди
✅ **Быстрая передача**: Меньше данных = быстрее отправка
✅ **Низкая стоимость**: Экономия на трафике в облачных средах

### Конфигурация

```yaml
export:
  compress: true        # Включить сжатие (по умолчанию false)
  compress_level: 3     # Уровень сжатия 1-22 (по умолчанию 3)
```

**Рекомендуемые уровни:**
- **Level 1-3**: Быстрое сжатие, коэффициент 3-5x (для real-time систем)
- **Level 4-9**: Баланс скорости и сжатия, коэффициент 5-7x (рекомендуется)
- **Level 10-22**: Максимальное сжатие, коэффициент 7-10x (для архивации)

### Пример использования

**Конфигурация (config.rabbitmq.yaml):**
```yaml
database:
  type: mssql
  host: localhost
  dbname: Production

broker:
  type: rabbitmq
  host: rabbitmq.example.com
  port: 5672
  queue: data_export

export:
  compress: true
  compress_level: 3
```

**Команда:**
```bash
tdtpcli --export-broker 'Employees' \
  --where "Department = 'Sales'" \
  --config config.rabbitmq.yaml
```

**Вывод:**
```
Compression enabled from config (level: 3)
Exporting table 'Employees' to broker...
Applying filters...
✓ Exported 2 packet(s)
Compressing data (level 3)...
  → Compressed: 1880684 → 256476 bytes (ratio: 7.33x)
  → Compressed: 244709 → 36376 bytes (ratio: 6.73x)
✓ Data compressed with zstd
Sending to queue 'data_export'...
✓ Sent packet 1/2
✓ Sent packet 2/2
✓ Export to broker complete!
```

### Статистика производительности

**Реальные результаты тестирования:**

| Размер данных | Без сжатия | Со сжатием (level 3) | Коэффициент | Экономия |
|--------------|-----------|---------------------|------------|---------|
| 5.5 KB       | 5540 B    | 1484 B              | 3.73x      | 73%     |
| 1.8 MB       | 1880684 B | 256476 B            | 7.33x      | 86%     |
| 245 KB       | 244709 B  | 36376 B             | 6.73x      | 85%     |

**Выводы:**
- Чем больше данных, тем лучше сжатие (7.33x vs 3.73x)
- Экономия bandwidth: 73-86%
- Средний коэффициент сжатия: **5-7x**

### Технические детали

**Алгоритм:** zstd (Zstandard) от Facebook
**Кодирование:** base64 (для безопасной передачи в XML)
**XML атрибут:** `compression="zstd"` в элементе `<Data>`
**Автоматическая распаковка:** При импорте данные автоматически распаковываются

**Пример сжатого TDTP пакета:**
```xml
<DataPacket>
  <Header>
    <MessageID>EXP-2024-001</MessageID>
    <Timestamp>2024-12-24T10:30:00Z</Timestamp>
  </Header>
  <Data compression="zstd">
    <R>KLUv/WBgUKEAAesEABWsAgBZCwIIbGFy...base64-encoded-compressed-data...</R>
  </Data>
</DataPacket>
```

### Когда использовать сжатие

**✅ Рекомендуется:**
- Большие таблицы (> 100 KB)
- Ограниченная пропускная способность сети
- Платный трафик (облачные провайдеры)
- Долгосрочное хранение в очередях

**❌ Не рекомендуется:**
- Очень маленькие пакеты (< 1 KB) - overhead > выгода
- CPU-ограниченные системы
- Real-time критичные системы (уровень 1-3 можно)

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

**Без сжатия:**
- RabbitMQ: ~5,000 msg/sec
- MSMQ: ~1,000 msg/sec
- Kafka: ~10,000 msg/sec

**Со сжатием (level 3):** 🆕
- RabbitMQ: ~3,500 msg/sec (небольшое снижение throughput)
- MSMQ: ~800 msg/sec
- Kafka: ~8,000 msg/sec

**Но:**
- **Bandwidth снижается в 5-7 раз** (главное преимущество!)
- **Время передачи** по сети уменьшается благодаря меньшему размеру
- **Общее время end-to-end** часто улучшается несмотря на overhead сжатия

**Пример:**
```
Без сжатия:  1.8 MB × 2 пакета = 3.6 MB → ~3.6 сек при 1 MB/sec сети
Со сжатием:  256 KB × 2 пакета = 512 KB → ~0.5 сек при 1 MB/sec сети

Выигрыш: 7x быстрее передача данных!
```

### Влияние сжатия на производительность

| Метрика | Без сжатия | Со сжатием (level 3) | Изменение |
|---------|-----------|---------------------|-----------|
| CPU usage | Low | Medium (+20-30%) | ⚠️ Увеличение |
| Network bandwidth | High | Low (-80-86%) | ✅ Снижение |
| Message size | Large | Small (-73-86%) | ✅ Снижение |
| Throughput (msg/sec) | Higher | Lower (-20-30%) | ⚠️ Снижение |
| End-to-end time | Slower | **Faster** (3-7x) | ✅ Улучшение |
| Storage cost | High | Low (-80-86%) | ✅ Снижение |

**Рекомендация:**
- **Быстрая сеть (10+ Gbps):** Сжатие опционально
- **Средняя сеть (100 Mbps - 1 Gbps):** Сжатие level 3 рекомендуется
- **Медленная сеть (< 100 Mbps):** Сжатие level 5-9 обязательно
- **Облачная среда с платным трафиком:** Сжатие всегда включено!

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

**С сжатием для экономии места в очереди:**
```bash
# Экспорт больших объемов данных со сжатием
tdtpcli --export-broker large_events \
  --where "created_at >= '2024-01-01'" \
  --config broker_with_compression.yaml

# Результат:
# - Меньше места в RabbitMQ/Kafka
# - Быстрее передача через сеть
# - Меньше затрат на облачный трафик
```

---

## Интеграция с другими фичами

### Со сжатием данных 🆕

```bash
# Экспорт больших таблиц со сжатием
tdtpcli --export-broker large_orders \
  --where "order_date >= '2024-01-01'" \
  --config rabbitmq_compressed.yaml

# config.rabbitmq_compressed.yaml:
# export:
#   compress: true
#   compress_level: 5

# Результат: 85% экономии bandwidth!
```

### Со сжатием + маскированием (compliance-ready) 🆕

```bash
# Production-ready: PII masking + compression
tdtpcli --export-broker customers \
  --mask email,phone,ssn \
  --config secure_broker.yaml

# config.secure_broker.yaml:
# broker:
#   type: rabbitmq
#   tls:
#     enabled: true
# export:
#   compress: true      # Экономия bandwidth
#   compress_level: 3
# processors:
#   masking:
#     enabled: true     # GDPR compliance

# = Безопасность + Производительность!
```

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

### Полный production stack 🆕

```bash
# Все фичи вместе: Circuit Breaker + Retry + Audit + Compression + TLS
tdtpcli --export-broker critical_data \
  --where "status = 'active'" \
  --config production.yaml

# production.yaml:
# database:
#   type: postgres
#   host: db.production.com
#
# broker:
#   type: rabbitmq
#   host: rabbitmq.production.com
#   port: 5671
#   tls:
#     enabled: true
#     ca_cert: /certs/ca.crt
#
# export:
#   compress: true
#   compress_level: 3
#
# resilience:
#   circuit_breaker:
#     enabled: true
#     threshold: 5
#   retry:
#     enabled: true
#     max_attempts: 3
#
# audit:
#   enabled: true
#   level: full

# = Production-ready система!
```

---

## Заключение

TDTP CLI v1.2 предоставляет полную интеграцию с брокерами сообщений:

✅ **Поддержка**: RabbitMQ, MSMQ, Kafka
✅ **Надежность**: Circuit Breaker, Retry, Audit
✅ **Безопасность**: TLS encryption, PII masking
✅ **Гибкость**: TDTQL фильтрация, стратегии импорта
✅ **Производительность**: Сжатие данных zstd (3-7x экономия bandwidth) 🆕
✅ **Production-ready**: Полный набор enterprise фич

### Ключевые преимущества сжатия 🆕

- 🚀 **Экономия bandwidth**: 73-86% снижение сетевого трафика
- 💰 **Снижение затрат**: Меньше платы за облачный трафик
- ⚡ **Быстрее передача**: В 5-7 раз меньше данных через сеть
- 💾 **Меньше хранилища**: Экономия места в очередях брокеров
- 🔧 **Простая настройка**: Просто добавить `export: compress: true` в конфиг

### Реальные результаты

```
Таблица 4,500 записей:
  Без сжатия:  2.1 MB → ~2.1 сек передачи (1 MB/sec сеть)
  Со сжатием:  293 KB → ~0.3 сек передачи (1 MB/sec сеть)

  Выигрыш: 7x быстрее! 86% экономии трафика!
```

**Рекомендация для production:**
- Всегда используйте `compress: true` для экспорта в брокеры
- Level 3 - оптимальный баланс скорости и сжатия
- Включайте TLS для безопасности
- Настройте Circuit Breaker и Retry для надежности

Для получения дополнительной информации см. [CLI_v1.2_FEATURES.md](CLI_v1.2_FEATURES.md).
