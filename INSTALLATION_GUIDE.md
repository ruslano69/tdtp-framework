# TDTP Framework v1.2 - Installation Guide

**Table Data Transfer Protocol** - фреймворк для универсального обмена табличными данными.

**Версия:** 1.2
**Дата:** 17.11.2025
**Статус:** Production Ready

---

## 📋 Содержание

1. [Системные требования](#системные-требования)
2. [Установка](#установка)
3. [Быстрый старт](#быстрый-старт)
4. [Проверка установки](#проверка-установки)
5. [Настройка адаптеров БД](#настройка-адаптеров-бд)
6. [Message Brokers](#message-brokers)
7. [Примеры использования](#примеры-использования)
8. [Production Deployment](#production-deployment)
9. [Troubleshooting](#troubleshooting)

---

## 🖥️ Системные требования

### Минимальные требования

- **Go:** 1.21 или выше (рекомендуется 1.22+)
- **Память:** 512 MB RAM
- **Диск:** 100 MB свободного места

### Опциональные зависимости

**Базы данных (на выбор):**
- SQLite 3.x (встроенная)
- PostgreSQL 12+ (рекомендуется 14+)
- MS SQL Server 2012+ (рекомендуется 2019+)
- MySQL 8.0+

**Message Brokers (опционально):**
- RabbitMQ 3.8+
- MSMQ (Windows)
- Kafka 2.8+

---

## 📦 Установка

### Метод 1: Клонирование репозитория (рекомендуется)

```bash
# Клонируем репозиторий
git clone https://github.com/queuebridge/tdtp.git
cd tdtp

# Устанавливаем зависимости
go mod download
go mod tidy

# Проверяем установку
go test ./pkg/core/...
```

### Метод 2: Использование как Go модуля

```bash
# Создайте ваш проект
mkdir my-tdtp-project
cd my-tdtp-project
go mod init my-project

# Добавьте TDTP как зависимость
go get github.com/queuebridge/tdtp
```

### Метод 3: CLI утилита

```bash
# Сборка CLI
cd cmd/tdtpcli
go build -o tdtpcli

# Установка в систему
sudo mv tdtpcli /usr/local/bin/

# Проверка
tdtpcli --version
```

---

## 🚀 Быстрый старт

### 1. Запустите первый пример

```bash
# Базовый экспорт данных
cd examples/01-basic-export
go run main.go

# Результат: данные экспортированы в TDTP XML файл
```

### 2. Попробуйте XLSX Converter 🍒

```bash
# Database ↔ Excel конвертер (instant business value!)
cd examples/04-tdtp-xlsx
go run main.go

# Результат: создан файл ./output/orders.xlsx
# Откройте его в Excel!
```

### 3. Production-ready пример с RabbitMQ

```bash
# Запустите RabbitMQ в Docker
docker run -d --name rabbitmq \
  -p 5672:5672 -p 15672:15672 \
  rabbitmq:3-management

# Запустите пример
cd examples/02-rabbitmq-mssql
go run main.go
```

---

## ✅ Проверка установки

### Тест 1: Core модули

```bash
# Все базовые модули
go test ./pkg/core/packet -v
go test ./pkg/core/schema -v
go test ./pkg/core/tdtql -v

# Ожидаемый результат: PASS для всех тестов
```

### Тест 2: Адаптеры БД

```bash
# SQLite (без внешних зависимостей)
go test ./pkg/adapters/sqlite -v

# PostgreSQL (требует running instance)
export POSTGRES_DSN="postgres://user:password@localhost/testdb"
go test ./pkg/adapters/postgres -v

# MS SQL (требует running instance)
export MSSQL_DSN="sqlserver://sa:Password123@localhost:1433?database=testdb"
go test ./pkg/adapters/mssql -v
```

### Тест 3: Resilience & Production компоненты

```bash
# CircuitBreaker
go test ./pkg/resilience -v

# AuditLogger
go test ./pkg/audit -v

# Retry mechanism
go test ./pkg/retry -v

# XLSX Converter
go test ./pkg/xlsx -v
```

---

## 🗄️ Настройка адаптеров БД

### SQLite (рекомендуется для начала)

**Установка драйвера:**

```bash
# Pure Go драйвер (без CGO)
go get modernc.org/sqlite
```

**Использование:**

```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    cfg := adapters.Config{
        Type: "sqlite",
        DSN:  "./database.db",
    }

    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Готов к работе!
}
```

### PostgreSQL

**Установка:**

```bash
# Драйвер устанавливается автоматически через go.mod
go get github.com/jackc/pgx/v5
```

**Docker setup:**

```bash
docker run -d --name postgres \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=tdtp \
  -p 5432:5432 \
  postgres:14
```

**Использование:**

```go
cfg := adapters.Config{
    Type: "postgres",
    DSN:  "postgres://user:password@localhost:5432/tdtp",
}

adapter, err := adapters.New(ctx, cfg)
```

**DSN Format:**
```
postgres://username:password@hostname:port/database?sslmode=disable
```

### MS SQL Server

**Установка:**

```bash
go get github.com/microsoft/go-mssqldb
```

**Docker setup:**

```bash
docker run -d --name mssql \
  -e "ACCEPT_EULA=Y" \
  -e "SA_PASSWORD=YourPassword123" \
  -p 1433:1433 \
  mcr.microsoft.com/mssql/server:2019-latest
```

**Использование:**

```go
cfg := adapters.Config{
    Type: "mssql",
    DSN:  "sqlserver://sa:YourPassword123@localhost:1433?database=tdtp",
}

adapter, err := adapters.New(ctx, cfg)
```

**DSN Format:**
```
sqlserver://username:password@hostname:port?database=dbname
```

---

## 📨 Message Brokers

### RabbitMQ

**Установка:**

```bash
# Docker (рекомендуется)
docker run -d --name rabbitmq \
  -p 5672:5672 \
  -p 15672:15672 \
  rabbitmq:3-management

# Management UI: http://localhost:15672
# Логин: guest / guest
```

**Использование:**

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

broker := brokers.NewRabbitMQ("amqp://guest:guest@localhost:5672/")
err := broker.Connect()
defer broker.Close()

// Публикация
err = broker.Publish(ctx, "my-queue", packet)

// Подписка
err = broker.Subscribe(ctx, "my-queue", func(pkt *packet.DataPacket) error {
    // Обработка пакета
    return nil
})
```

### Kafka

**Установка:**

```bash
# Docker
docker run -d --name kafka \
  -p 9092:9092 \
  apache/kafka:latest
```

**Использование:**

```go
import "github.com/queuebridge/tdtp/pkg/brokers"

broker := brokers.NewKafka([]string{"localhost:9092"})
err := broker.Connect()
defer broker.Close()

// Публикация
err = broker.Publish(ctx, "my-topic", packet)
```

---

## 💡 Примеры использования

### Пример 1: Экспорт данных в XLSX 🍒

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    "github.com/queuebridge/tdtp/pkg/xlsx"
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
)

func main() {
    ctx := context.Background()

    // Подключаемся к PostgreSQL
    cfg := adapters.Config{
        Type: "postgres",
        DSN:  "postgres://user:password@localhost/mydb",
    }
    adapter, _ := adapters.New(ctx, cfg)
    defer adapter.Close(ctx)

    // Экспортируем таблицу
    packets, _ := adapter.ExportTable(ctx, "orders")

    // Конвертируем в Excel
    xlsx.ToXLSX(packets[0], "./orders.xlsx", "Orders")

    // Готово! Открывайте в Excel
}
```

### Пример 2: Импорт из Excel в БД

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    "github.com/queuebridge/tdtp/pkg/xlsx"
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
)

func main() {
    ctx := context.Background()

    // Читаем Excel
    packet, _ := xlsx.FromXLSX("./data.xlsx", "Sheet1")

    // Подключаемся к БД
    cfg := adapters.Config{
        Type: "postgres",
        DSN:  "postgres://user:password@localhost/mydb",
    }
    adapter, _ := adapters.New(ctx, cfg)
    defer adapter.Close(ctx)

    // Импортируем (заменяем существующие)
    adapter.ImportPacket(ctx, packet, adapters.StrategyReplace)
}
```

### Пример 3: Интеграция с Circuit Breaker + Audit

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/resilience"
    "github.com/queuebridge/tdtp/pkg/audit"
)

func main() {
    ctx := context.Background()

    // Circuit Breaker для защиты от сбоев
    cb := resilience.NewCircuitBreaker(resilience.Config{
        MaxFailures:    5,
        ResetTimeout:   30 * time.Second,
        MaxConcurrent:  100,
    })

    // Audit Logger для compliance
    logger := audit.NewAuditLogger()
    logger.AddAppender(audit.NewFileAppender("./audit.log"))
    logger.SetLevel(audit.LevelStandard)

    // Используйте в вашей интеграции
    err := cb.Execute(ctx, func() error {
        // Ваша операция
        logger.Info("Operation started", map[string]interface{}{
            "operation": "data_export",
        })

        // ... экспорт данных ...

        return nil
    })
}
```

**Полные примеры:** См. директорию [`examples/`](./examples/README.md)

---

## 🏭 Production Deployment

### Checklist перед production

- [ ] **Environment Variables:** Все credentials в env vars, не в коде
- [ ] **Circuit Breaker:** Настроен для всех внешних зависимостей
- [ ] **Audit Logger:** Включен с правильным уровнем (Standard/Full)
- [ ] **Retry Mechanism:** Настроен с exponential backoff
- [ ] **Connection Pooling:** Настроены max connections для БД
- [ ] **Timeouts:** Установлены разумные таouts для всех операций
- [ ] **Health Checks:** Реализованы /health endpoints
- [ ] **Monitoring:** Подключены метрики (Prometheus/Grafana)
- [ ] **Logging:** Structured logging (JSON format)
- [ ] **Graceful Shutdown:** Обработка SIGTERM/SIGINT
- [ ] **Data Validation:** Включены validators
- [ ] **Data Masking:** PII данные маскируются (GDPR)
- [ ] **Testing:** Integration tests с реальными БД

### Docker Deployment

**Dockerfile:**

```dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /tdtp-app ./cmd/myapp

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /tdtp-app /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/tdtp-app"]
```

**Docker Compose:**

```yaml
version: '3.8'

services:
  app:
    build: .
    environment:
      - POSTGRES_DSN=postgres://user:pass@postgres:5432/tdtp
      - RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/
    depends_on:
      - postgres
      - rabbitmq

  postgres:
    image: postgres:14
    environment:
      POSTGRES_PASSWORD: password
      POSTGRES_DB: tdtp
    volumes:
      - postgres-data:/var/lib/postgresql/data

  rabbitmq:
    image: rabbitmq:3-management
    ports:
      - "15672:15672"

volumes:
  postgres-data:
```

### Environment Variables

```bash
# Database
export POSTGRES_DSN="postgres://user:password@localhost:5432/tdtp"
export MSSQL_DSN="sqlserver://sa:Password123@localhost:1433?database=tdtp"

# Message Brokers
export RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
export KAFKA_BROKERS="localhost:9092"

# Application
export LOG_LEVEL="info"
export AUDIT_LEVEL="standard"
export CIRCUIT_BREAKER_ENABLED="true"

# Security
export TLS_ENABLED="true"
export TLS_CERT_PATH="/etc/certs/cert.pem"
export TLS_KEY_PATH="/etc/certs/key.pem"
```

---

## 🔧 Troubleshooting

### Проблема: "driver not found"

**Решение:**

```bash
# Убедитесь что драйвер импортирован
import _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
import _ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
import _ "github.com/queuebridge/tdtp/pkg/adapters/mssql"

# Проверьте go.mod
go mod tidy
```

### Проблема: "connection refused" (PostgreSQL/MSSQL)

**Решение:**

```bash
# Проверьте что БД запущена
docker ps | grep postgres

# Проверьте DSN
echo $POSTGRES_DSN

# Проверьте доступность порта
telnet localhost 5432
```

### Проблема: "circuit breaker open"

**Решение:**

```bash
# Circuit Breaker открылся из-за множественных ошибок
# Проверьте логи audit logger

# Настройте параметры:
cb := resilience.NewCircuitBreaker(resilience.Config{
    MaxFailures:    10,        // Увеличьте порог
    ResetTimeout:   60 * time.Second,  // Увеличьте timeout
})
```

### Проблема: "Excel file corrupted" (XLSX)

**Решение:**

```bash
# Проверьте версию excelize
go list -m github.com/xuri/excelize/v2
# Должна быть v2.8.0+

# Переустановите
go get -u github.com/xuri/excelize/v2
```

### Проблема: "out of memory" при больших данных

**Решение:**

```go
// Используйте пагинацию для больших таблиц
query := &tdtql.Query{
    Limit:  ref(1000),  // Batch по 1000 строк
    Offset: ref(0),
}

for {
    packets, err := adapter.ExportTableWithQuery(ctx, "large_table", query)
    if len(packets) == 0 {
        break
    }

    // Обработка batch

    *query.Offset += 1000
}
```

### Проблема: Низкая производительность импорта

**Решение:**

```go
// Используйте транзакции для batch импорта
tx, _ := adapter.BeginTx(ctx)
defer tx.Rollback(ctx)

for _, packet := range packets {
    tx.ImportPacket(ctx, packet, adapters.StrategyReplace)
}

tx.Commit(ctx)

// Для PostgreSQL используйте COPY strategy
adapter.ImportPacket(ctx, packet, adapters.StrategyCopy)
```

---

## 📚 Дополнительные ресурсы

### Документация

- **[USER_GUIDE.md](./docs/USER_GUIDE.md)** - полное руководство по CLI
- **[SPECIFICATION.md](./docs/SPECIFICATION.md)** - спецификация TDTP v1.0
- **[PACKET_MODULE.md](./docs/PACKET_MODULE.md)** - работа с пакетами
- **[SCHEMA_MODULE.md](./docs/SCHEMA_MODULE.md)** - типы и валидация
- **[TDTQL_TRANSLATOR.md](./docs/TDTQL_TRANSLATOR.md)** - язык запросов

### Package-specific READMEs

- **[pkg/resilience/README.md](./pkg/resilience/README.md)** - Circuit Breaker
- **[pkg/audit/README.md](./pkg/audit/README.md)** - Audit Logger
- **[pkg/xlsx/README.md](./pkg/xlsx/README.md)** - XLSX Converter 🍒

### Examples

- **[examples/README.md](./examples/README.md)** - все примеры с описанием
- **[examples/04-tdtp-xlsx/](./examples/04-tdtp-xlsx/)** - XLSX converter
- **[examples/02-rabbitmq-mssql/](./examples/02-rabbitmq-mssql/)** - RabbitMQ integration

---

## 📞 Поддержка

**GitHub Issues:** https://github.com/queuebridge/tdtp/issues
**Discussions:** https://github.com/queuebridge/tdtp/discussions
**Email:** support@queuebridge.io

---

## 📄 Лицензия

MIT License - see [LICENSE](./LICENSE) file for details

---

**Приятной работы с TDTP Framework! 🚀**

**Версия:** v1.2
**Последнее обновление:** 17.11.2025
**Статус:** Production Ready
