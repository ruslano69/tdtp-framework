# RabbitMQ + MSSQL Integration Example

Комплексный пример интеграции MSSQL и RabbitMQ с полным набором enterprise-функций.

## Сценарий

Экспорт заказов из MSSQL базы данных в RabbitMQ для дальнейшей обработки другими системами.

**Требования:**
- Маскирование PII данных (email, phone, card numbers)
- Audit logging всех операций (GDPR compliance)
- Circuit Breaker для защиты от перегрузки RabbitMQ
- Retry mechanism с exponential backoff
- Data validation перед отправкой

## Архитектура

```
MSSQL (OrdersDB)
    ↓
  Export (with audit)
    ↓
  Data Processing:
    - Normalization (email, phone)
    - Validation (email format, positive numbers)
    - Masking (PII protection)
    ↓
  Circuit Breaker + Retry
    ↓
  RabbitMQ (orders-queue)
```

## Компоненты

### 1. Audit Logger
- **File Appender**: Постоянное хранение audit trail
- **Console Appender**: Real-time мониторинг
- **Level**: Standard (без sensitive data)
- **Mode**: Async (high performance)

### 2. MSSQL Adapter
- Подключение к SQL Server
- Query execution
- Error handling

### 3. Data Processors
- **FieldNormalizer**: Email lowercase, phone международный формат
- **FieldValidator**: Email regex, положительные числа
- **FieldMasker**: Email partial, phone middle, card first2last2

### 4. Circuit Breaker
- **MaxFailures**: 5 (открывается после 5 ошибок)
- **Timeout**: 30s (восстановление через 30 секунд)
- **SuccessThreshold**: 2 (закрывается после 2 успехов)
- **State change callbacks**: Логирование изменений состояния

### 5. Retry Mechanism
- **MaxAttempts**: 3
- **Backoff**: Exponential (1s, 2s, 4s)
- **Jitter**: Enabled (предотвращает thundering herd)
- **OnRetry callback**: Логирование попыток

## Запуск

### Предварительные требования

1. **MSSQL Server**:
```bash
docker run -e "ACCEPT_EULA=Y" -e "SA_PASSWORD=YourPassword123" \
  -p 1433:1433 --name mssql \
  -d mcr.microsoft.com/mssql/server:2019-latest
```

2. **RabbitMQ**:
```bash
docker run -d --hostname rabbitmq --name rabbitmq \
  -p 5672:5672 -p 15672:15672 \
  rabbitmq:3-management
```

3. **Создать базу данных**:
```sql
CREATE DATABASE OrdersDB;
GO

USE OrdersDB;
GO

CREATE TABLE orders (
    order_id VARCHAR(50) PRIMARY KEY,
    customer_email VARCHAR(255),
    customer_phone VARCHAR(50),
    billing_card VARCHAR(50),
    order_total DECIMAL(10,2),
    created_at DATETIME DEFAULT GETDATE()
);

INSERT INTO orders VALUES
('ORD-001', 'john.doe@company.com', '+1-555-123-4567', '4532-1234-5678-9010', 150.00, GETDATE()),
('ORD-002', 'jane.smith@example.com', '+1-555-987-6543', '5412-9876-5432-1098', 75.50, GETDATE()),
('ORD-003', 'bob.wilson@test.org', '+1-555-456-7890', '3782-8224-6310-005', 225.75, GETDATE());
```

### Запуск примера

```bash
cd examples/02-rabbitmq-mssql
go run main.go
```

## Пример вывода

```
=== RabbitMQ + MSSQL Integration Example ===
📊 Connecting to MSSQL: OrdersDB
🐰 Connecting to RabbitMQ

--- Step 1: Export from MSSQL ---
Query: SELECT * FROM orders (last 3 records)
  • Order: ORD-001, Email: john.doe@company.com, Total: 150.00
  • Order: ORD-002, Email: jane.smith@example.com, Total: 75.50
  • Order: ORD-003, Email: bob.wilson@test.org, Total: 225.75
Exported 3 records from MSSQL

--- Step 2: Apply Data Masking ---
  • Order: ORD-001, Masked Email: joh***@company.com, Masked Card: 45******9010
  • Order: ORD-002, Masked Email: jan***@example.com, Masked Card: 54******1098
  • Order: ORD-003, Masked Email: bob***@test.org, Masked Card: 37******0005
Masked 3 records

--- Step 3: Send to RabbitMQ with Protection ---
Sending order ORD-001 (1/3)...
✓ Order ORD-001 sent successfully
Sending order ORD-002 (2/3)...
🔄 Retry attempt 1: temporary network error
✓ Order ORD-002 sent successfully
Sending order ORD-003 (3/3)...
✓ Order ORD-003 sent successfully
✓ Successfully sent to RabbitMQ

--- Circuit Breaker Statistics ---
State: closed
Total Requests: 3
Total Successes: 3
Total Failures: 0
Consecutive Successes: 3
Consecutive Failures: 0
Max Running Calls: 1

=== Integration Complete ===
```

## Конфигурация

### Environment Variables

```bash
# MSSQL
export MSSQL_HOST=localhost
export MSSQL_PORT=1433
export MSSQL_USER=sa
export MSSQL_PASSWORD=YourPassword123
export MSSQL_DATABASE=OrdersDB

# RabbitMQ
export RABBITMQ_URL=amqp://guest:guest@localhost:5672/
export RABBITMQ_QUEUE=orders-queue

# Audit
export AUDIT_LOG_PATH=./logs/rabbitmq-mssql.log
export AUDIT_LOG_LEVEL=standard
```

### Config File (config.yaml)

```yaml
mssql:
  host: localhost
  port: 1433
  user: sa
  password: YourPassword123
  database: OrdersDB

rabbitmq:
  url: amqp://guest:guest@localhost:5672/
  queue: orders-queue
  exchange: orders-exchange
  routing_key: orders.new

circuit_breaker:
  max_failures: 5
  timeout: 30s
  success_threshold: 2

retry:
  max_attempts: 3
  initial_delay: 1s
  max_delay: 10s
  multiplier: 2.0
  jitter: true

processors:
  - type: normalizer
    fields:
      customer_email: email
      customer_phone: phone
  - type: validator
    fields:
      customer_email: email
      order_total: positive_number
  - type: masker
    fields:
      customer_email: email
      customer_phone: phone
      billing_card: first2last2

audit:
  enabled: true
  level: standard
  file:
    path: ./logs/rabbitmq-mssql.log
    max_size: 50
    max_backups: 10
  async: true
  buffer_size: 1000
```

## Production Considerations

### 1. Error Handling

```go
// Dead Letter Queue для failed messages
dlqConfig := retry.DLQConfig{
    Enabled:         true,
    StoragePath:     "./dlq",
    MaxSize:         1000,
    RetentionPeriod: 7 * 24 * time.Hour,
}

dlq, _ := retry.NewDLQ(dlqConfig)
defer dlq.Close()

// При ошибке сохраняем в DLQ
if err := sendToRabbitMQ(record); err != nil {
    dlq.Add(retry.FailedMessage{
        Data:      record,
        Error:     err.Error(),
        Timestamp: time.Now(),
        Attempt:   3,
    })
}
```

### 2. Monitoring

```go
// Prometheus metrics
cbStats := circuitBreaker.Stats()
metrics.Gauge("circuit_breaker_state", float64(cbStats.State))
metrics.Counter("circuit_breaker_successes", float64(cbStats.Counts.TotalSuccesses))
metrics.Counter("circuit_breaker_failures", float64(cbStats.Counts.TotalFailures))
```

### 3. Graceful Shutdown

```go
// Signal handling
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

go func() {
    <-sigChan
    log.Println("Shutting down gracefully...")

    // Close connections
    auditLogger.Close()
    mssqlAdapter.Close()
    rabbitMQ.Close()

    os.Exit(0)
}()
```

### 4. Health Checks

```go
// Health check endpoint
http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    health := map[string]string{
        "status":         "healthy",
        "mssql":          mssqlAdapter.Ping(),
        "rabbitmq":       rabbitMQ.Ping(),
        "circuit_breaker": circuitBreaker.State().String(),
    }
    json.NewEncoder(w).Encode(health)
})
```

## Troubleshooting

### Circuit Breaker открыт

```
⚡ Circuit Breaker [rabbitmq]: closed → open
```

**Решение**: Проверьте доступность RabbitMQ. Circuit breaker автоматически восстановится через 30 секунд.

### Retry exhausted

```
🔄 Retry attempt 3: connection refused
❌ Failed to send order ORD-002: max attempts exceeded
```

**Решение**: Проверьте сетевое подключение к RabbitMQ. Сообщение сохранено в DLQ для повторной обработки.

### Validation errors

```
⚠️  Processing error for order ORD-005: invalid email format
```

**Решение**: Проверьте данные в MSSQL. Добавьте data cleaning processor.

## См. также

- [Circuit Breaker Documentation](../../pkg/resilience/README.md)
- [Audit Logger Documentation](../../pkg/audit/README.md)
- [Retry Mechanism Documentation](../../pkg/retry/README.md)
- [Data Processors](../../pkg/processor/README.md)
