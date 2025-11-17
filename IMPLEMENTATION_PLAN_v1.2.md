# TDTP Framework v1.2 - Implementation Plan
**Дата:** 16.11.2025
**Цель:** MS SQL Server Adapter + MSMQ/RabbitMQ Integration

## ⚠️ КРИТИЧЕСКИ ВАЖНО!

### Окружения разработки vs Production

**Development (локально):**
- Docker контейнер: SQL Server 2019/2022
- Все современные функции доступны

**Production:**
- SQL Server 2012
- Ограниченный набор функций

**ПРОБЛЕМА:** Код, работающий в Docker, может НЕ работать в production!

**РЕШЕНИЕ:**
1. **Обязательно** читать: [docs/MSSQL_DEV_VS_PROD.md](docs/MSSQL_DEV_VS_PROD.md)
2. Использовать `docker-compose.mssql.yml` для тестирования на SQL Server 2012 compatibility mode
3. **ВСЕГДА** тестировать на `mssql-prod-sim` контейнере перед коммитом

### Обратная совместимость: MS SQL Server 2012+

- **Минимальная версия:** SQL Server 2012 (11.x)
- **Поддержка:** 2012, 2014, 2016, 2017, 2019, 2022
- **Документация:**
  - [docs/MSSQL_2012_COMPATIBILITY.md](docs/MSSQL_2012_COMPATIBILITY.md) - Типы данных и функции
  - [docs/MSSQL_DEV_VS_PROD.md](docs/MSSQL_DEV_VS_PROD.md) - **ОБЯЗАТЕЛЬНО К ПРОЧТЕНИЮ!**

## 🎯 Use Case

**Задача:** Экспорт данных из MS SQL Server в облачную СЭД через очереди сообщений

**Архитектура:**
```
MS SQL Server (on-premise)
    ↓ export
[TDTP Adapter]
    ↓ TDTP XML packets
[Message Broker]
    ├→ MSMQ (локальный) → Облачная СЭД
    └→ RabbitMQ (удаленный) → Другие системы
```

## 📋 Этапы реализации

### Этап 1: MS SQL Server Adapter (2-3 недели)

#### Задачи:

1. **Настройка драйвера MS SQL**
   - Драйвер: `github.com/denisenkom/go-mssqldb` (стабильный, зрелый)
   - Connection string поддержка (SQL Auth + Windows Auth)
   - Connection pool настройка
   - **SQL Server 2012+ compatibility mode**
   - Определение версии сервера для feature detection

2. **Маппинг типов MS SQL ↔ TDTP (SQL Server 2012+ compatible)**
   ```go
   MS SQL Type           TDTP Type        Особенности
   ─────────────────────────────────────────────────────
   INT, BIGINT          INTEGER
   DECIMAL, NUMERIC     DECIMAL          precision, scale
   VARCHAR, NVARCHAR    TEXT             length, Unicode (используем NVARCHAR)
   CHAR, NCHAR          TEXT             fixed length
   BIT                  BOOLEAN
   DATE                 DATE             (SQL Server 2008+)
   DATETIME, DATETIME2  TIMESTAMP        DATETIME2 preferred (точность 100ns)
   UNIQUEIDENTIFIER     TEXT(36)         UUID as string
   VARBINARY, IMAGE     BLOB             Binary data
   XML                  TEXT             XML as string
   MONEY                DECIMAL(19,4)    Fixed precision
   TEXT, NTEXT          TEXT             (deprecated, но работает)

   ❌ JSON               -                SQL Server 2016+ only (не поддерживается в 2012)
   ```

   **Важно:**
   - Используем NVARCHAR вместо VARCHAR для Unicode
   - DATETIME2 вместо DATETIME (лучшая точность)
   - JSON не поддерживается в SQL Server 2012 - хранится как NVARCHAR(MAX)

3. **Adapter Implementation**
   ```
   pkg/adapters/mssql/
   ├── adapter.go        # Connection, lifecycle
   ├── types.go          # Type mapping MS SQL ↔ TDTP
   ├── export.go         # Export: MS SQL → TDTP
   ├── import.go         # Import: TDTP → MS SQL
   ├── integration_test.go
   └── doc.go
   ```

4. **Специфичные возможности MS SQL (SQL Server 2012+ compatible)**
   - Schema support (dbo, custom schemas)
   - Catalog/Database support
   - **Bulk INSERT / Table-Valued Parameters** для производительности
   - **MERGE statement** для UPSERT (SQL Server 2008+) ✅
   - **OFFSET/FETCH** для пагинации (SQL Server 2012+) ✅
   - Transaction isolation levels
   - **Feature detection** - определение версии сервера для условного использования функций

   **Ограничения SQL Server 2012:**
   - ❌ JSON функции (появились в 2016)
   - ❌ STRING_SPLIT (появилась в 2016)
   - ❌ STRING_AGG (появилась в 2017)
   - ✅ MERGE, OFFSET/FETCH, IIF, TRY_CONVERT работают

5. **Export функциональность**
   ```go
   // Export table with schema
   func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)

   // Export with custom schema
   func (a *Adapter) ExportTableFromSchema(ctx context.Context, schema, table string) ([]*packet.DataPacket, error)

   // Export with TDTQL query (with SQL optimization)
   func (a *Adapter) ExportTableWithQuery(ctx context.Context, table string, query *tdtql.Query) ([]*packet.DataPacket, error)
   ```

6. **Import функциональность**
   ```go
   // Import with strategies
   func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy Strategy) error

   // Bulk import for performance
   func (a *Adapter) ImportPackets(ctx context.Context, pkts []*packet.DataPacket, strategy Strategy) error

   // MERGE statement for UPSERT
   func (a *Adapter) executeUpsert(ctx context.Context, table string, schema packet.Schema, rows [][]string) error
   ```

#### Deliverables:
- ✅ Полнофункциональный MS SQL Server Adapter
- ✅ Интеграционные тесты
- ✅ Документация (MSSQL_ADAPTER.md)
- ✅ Примеры использования
- ✅ Обновленный CLI (поддержка MS SQL)

---

### Этап 2: MSMQ Integration (2-3 недели)

#### Задачи:

1. **MSMQ Wrapper для Go**

   **Проблема:** Go не имеет native MSMQ библиотеки (MSMQ - Windows-only API)

   **Решения:**

   **Вариант A: CGO + COM Interop** (рекомендую для Windows)
   ```go
   // Через syscall/dll
   import "syscall"

   // Вызов MSMQ COM API
   mqSend := syscall.NewLazyDLL("mqrt.dll")
   ```

   **Вариант B: C# Wrapper + gRPC/HTTP** (кроссплатформенно)
   ```
   C# Service (MSMQ Wrapper)
       ↕ gRPC/HTTP
   Go Producer/Consumer
   ```

   **Вариант C: PowerShell wrapper** (простой)
   ```go
   // Вызов PowerShell скриптов для отправки/получения
   cmd := exec.Command("powershell", "-File", "send_to_msmq.ps1", args...)
   ```

2. **MSMQ Producer Implementation**
   ```
   pkg/brokers/msmq/
   ├── producer.go       # Отправка TDTP пакетов в MSMQ
   ├── consumer.go       # Получение пакетов из MSMQ
   ├── config.go         # Конфигурация MSMQ
   ├── msmq_wrapper.go   # Wrapper для MSMQ API
   └── doc.go
   ```

3. **Producer API**
   ```go
   package msmq

   type Producer struct {
       queuePath string // .\private$\tdtp или FormatName:...
   }

   func NewProducer(queuePath string) (*Producer, error)

   // Отправка одного пакета
   func (p *Producer) Send(pkt *packet.DataPacket) error

   // Отправка множественных пакетов
   func (p *Producer) SendBatch(pkts []*packet.DataPacket) error

   // Отправка с гарантией доставки
   func (p *Producer) SendTransactional(pkt *packet.DataPacket) error
   ```

4. **Consumer API**
   ```go
   type Consumer struct {
       queuePath string
   }

   func NewConsumer(queuePath string) (*Consumer, error)

   // Получение пакета (blocking)
   func (c *Consumer) Receive() (*packet.DataPacket, error)

   // Получение с timeout
   func (c *Consumer) ReceiveTimeout(timeout time.Duration) (*packet.DataPacket, error)

   // Callback-based consumer
   func (c *Consumer) Subscribe(handler func(*packet.DataPacket) error) error
   ```

5. **MSMQ Configuration**
   ```yaml
   msmq:
     queue_path: ".\private$\tdtp_export"
     transactional: true
     timeout: 30s
     max_retries: 3
     dead_letter_queue: ".\private$\tdtp_dlq"
   ```

6. **Интеграция с MS SQL Adapter**
   ```go
   // Example: Export и отправка в MSMQ
   func ExportToMSMQ(adapter *mssql.Adapter, producer *msmq.Producer, tableName string) error {
       ctx := context.Background()

       // Export from MS SQL
       packets, err := adapter.ExportTable(ctx, tableName)
       if err != nil {
           return err
       }

       // Send to MSMQ
       for _, pkt := range packets {
           if err := producer.Send(pkt); err != nil {
               return err
           }
       }

       return nil
   }
   ```

#### Deliverables:
- ✅ MSMQ Producer/Consumer
- ✅ MSMQ Wrapper (syscall или C# service)
- ✅ Интеграционные тесты
- ✅ Документация (MSMQ_INTEGRATION.md)
- ✅ Примеры: MS SQL → MSMQ

---

### Этап 3: RabbitMQ Integration (1-2 недели)

#### Задачи:

1. **RabbitMQ Client**
   - Библиотека: `github.com/rabbitmq/amqp091-go`
   - Connection management
   - Channel pooling
   - Auto-reconnect

2. **RabbitMQ Producer/Consumer**
   ```
   pkg/brokers/rabbitmq/
   ├── producer.go       # Отправка TDTP в RabbitMQ
   ├── consumer.go       # Получение из RabbitMQ
   ├── config.go         # Конфигурация (exchange, queue, routing key)
   └── doc.go
   ```

3. **Producer API**
   ```go
   package rabbitmq

   type Producer struct {
       conn     *amqp.Connection
       exchange string
   }

   func NewProducer(amqpURL, exchange string) (*Producer, error)

   // Отправка в exchange с routing key
   func (p *Producer) Send(pkt *packet.DataPacket, routingKey string) error

   // Отправка batch
   func (p *Producer) SendBatch(pkts []*packet.DataPacket, routingKey string) error

   // Publisher confirms для гарантии
   func (p *Producer) SendWithConfirm(pkt *packet.DataPacket, routingKey string) error
   ```

4. **Consumer API**
   ```go
   type Consumer struct {
       conn  *amqp.Connection
       queue string
   }

   func NewConsumer(amqpURL, queue string) (*Consumer, error)

   // Subscribe с обработчиком
   func (c *Consumer) Subscribe(handler func(*packet.DataPacket) error) error

   // Manual ACK support
   func (c *Consumer) SubscribeManualAck(handler func(*packet.DataPacket, func() error) error) error
   ```

5. **RabbitMQ Configuration**
   ```yaml
   rabbitmq:
     url: "amqp://user:pass@localhost:5672/"
     exchange: "tdtp_export"
     exchange_type: "topic"  # direct, topic, fanout
     queue: "tdtp_queue"
     routing_key: "table.*"
     durable: true
     auto_ack: false
     prefetch_count: 10
   ```

6. **Интеграция с MS SQL**
   ```go
   // Example: Export и отправка в RabbitMQ
   func ExportToRabbitMQ(adapter *mssql.Adapter, producer *rabbitmq.Producer, tableName string) error {
       ctx := context.Background()

       packets, err := adapter.ExportTable(ctx, tableName)
       if err != nil {
           return err
       }

       for _, pkt := range packets {
           routingKey := fmt.Sprintf("table.%s", tableName)
           if err := producer.Send(pkt, routingKey); err != nil {
               return err
           }
       }

       return nil
   }
   ```

#### Deliverables:
- ✅ RabbitMQ Producer/Consumer
- ✅ Connection management с reconnect
- ✅ Интеграционные тесты
- ✅ Документация (RABBITMQ_INTEGRATION.md)
- ✅ Примеры: MS SQL → RabbitMQ

---

### Этап 4: CLI Extensions & Integration Examples (1 неделя)

#### CLI Extensions

1. **Новые команды для brokers**
   ```bash
   # Export и отправка в MSMQ
   tdtpcli export Users --to-msmq --queue ".\private$\tdtp_export"

   # Export и отправка в RabbitMQ
   tdtpcli export Orders --to-rabbitmq --exchange "tdtp" --routing-key "orders"

   # Получение из MSMQ и импорт
   tdtpcli import --from-msmq --queue ".\private$\tdtp_import"

   # Получение из RabbitMQ и импорт
   tdtpcli import --from-rabbitmq --queue "tdtp_queue"
   ```

2. **Конфигурация brokers в config.yaml**
   ```yaml
   database:
     type: mssql
     host: localhost
     port: 1433
     user: sa
     password: YourPassword
     dbname: Production
     schema: dbo

   msmq:
     enabled: true
     export_queue: ".\private$\tdtp_export"
     import_queue: ".\private$\tdtp_import"
     transactional: true

   rabbitmq:
     enabled: true
     url: "amqp://user:pass@remote-server:5672/"
     exchange: "tdtp_export"
     queue: "tdtp_queue"
     routing_key_prefix: "table"
   ```

#### Integration Examples

1. **Example: MS SQL → MSMQ → Import**
   ```
   examples/mssql_to_msmq/
   ├── main.go           # Full pipeline example
   ├── config.yaml       # Configuration
   └── README.md         # Пошаговая инструкция
   ```

2. **Example: MS SQL → RabbitMQ → Import**
   ```
   examples/mssql_to_rabbitmq/
   ├── producer/         # Export и send to RabbitMQ
   ├── consumer/         # Receive from RabbitMQ и import
   └── README.md
   ```

3. **Example: Real-world СЭД Integration**
   ```
   examples/sed_integration/
   ├── exporter/         # MS SQL → TDTP → MSMQ
   ├── sed_consumer/     # MSMQ → СЭД API
   ├── scheduler/        # Periodic export (cron-like)
   └── README.md
   ```

#### Документация

1. **MSSQL_ADAPTER.md** - Полная документация MS SQL Adapter
2. **MSMQ_INTEGRATION.md** - MSMQ Producer/Consumer
3. **RABBITMQ_INTEGRATION.md** - RabbitMQ Producer/Consumer
4. **SED_INTEGRATION_GUIDE.md** - Интеграция с облачной СЭД
5. **DEPLOYMENT.md** - Production deployment guide

---

## 📊 Timeline

### Общая продолжительность: 6-8 недель

**Week 1-2: MS SQL Server Adapter**
- Настройка драйвера
- Маппинг типов
- Export/Import реализация
- Базовые тесты

**Week 3: MS SQL Advanced Features**
- Bulk operations
- MERGE для UPSERT
- Integration тесты
- Документация

**Week 4-5: MSMQ Integration**
- MSMQ Wrapper (syscall или C# service)
- Producer/Consumer
- Интеграция с MS SQL
- Тесты

**Week 6: RabbitMQ Integration**
- RabbitMQ Producer/Consumer
- Connection management
- Интеграция с MS SQL
- Тесты

**Week 7: CLI & Examples**
- CLI расширения
- Integration examples
- СЭД integration guide
- Документация

**Week 8: Testing & Polish**
- End-to-end тесты
- Performance benchmarks
- Bug fixes
- Final documentation

---

## 🎯 Приоритеты

### P0 (Critical)
1. ✅ MS SQL Server Adapter - критичен для экспорта
2. ✅ MSMQ Producer - критичен для отправки в СЭД

### P1 (High)
3. ✅ MSMQ Consumer - для тестирования и обратного импорта
4. ✅ RabbitMQ Producer - для удаленного брокера

### P2 (Medium)
5. ✅ RabbitMQ Consumer - для полноценной интеграции
6. ✅ CLI Extensions - для удобства использования

### P3 (Nice to have)
7. ✅ Integration Examples
8. ✅ СЭД Guide

---

## 🛠️ Technical Decisions

### MS SQL Driver
**Выбор:** `github.com/denisenkom/go-mssqldb`
**Причины:**
- Стабильный и зрелый
- Широко используется
- Хорошая документация
- Active maintenance

### MSMQ Wrapper
**Выбор:** Syscall + COM Interop
**Причины:**
- Native интеграция
- Лучшая производительность
- Меньше зависимостей
- Fallback: PowerShell wrapper

**Альтернатива (если syscall не работает):**
- C# Service + gRPC для кроссплатформенности

### RabbitMQ Client
**Выбор:** `github.com/rabbitmq/amqp091-go`
**Причины:**
- Официальная библиотека
- AMQP 0.9.1 protocol
- Stable and maintained

---

## 📦 Deliverables Summary

### Code
1. `pkg/adapters/mssql/` - MS SQL Server Adapter
2. `pkg/brokers/msmq/` - MSMQ Integration
3. `pkg/brokers/rabbitmq/` - RabbitMQ Integration
4. `cmd/tdtpcli/` - CLI extensions

### Examples
1. `examples/mssql/` - MS SQL basic usage
2. `examples/mssql_to_msmq/` - MS SQL → MSMQ
3. `examples/mssql_to_rabbitmq/` - MS SQL → RabbitMQ
4. `examples/sed_integration/` - Real-world СЭД integration

### Documentation
1. `MSSQL_ADAPTER.md` - MS SQL documentation
2. `MSMQ_INTEGRATION.md` - MSMQ guide
3. `RABBITMQ_INTEGRATION.md` - RabbitMQ guide
4. `SED_INTEGRATION_GUIDE.md` - СЭД integration
5. `DEPLOYMENT.md` - Production deployment

### Tests
1. Unit tests for all modules
2. Integration tests (MS SQL, MSMQ, RabbitMQ)
3. End-to-end pipeline tests
4. Benchmarks (MS SQL vs PostgreSQL vs SQLite)

---

## 🚀 Getting Started

### Prerequisites

**MS SQL Server:**
```bash
# Docker (для тестирования)
docker run -e "ACCEPT_EULA=Y" -e "SA_PASSWORD=YourPassword123" \
  -p 1433:1433 --name mssql \
  mcr.microsoft.com/mssql/server:2019-latest
```

**MSMQ (Windows):**
```powershell
# Enable MSMQ feature
Enable-WindowsOptionalFeature -Online -FeatureName MSMQ-Server -All

# Create queue
New-MsmqQueue -Name "tdtp_export" -Transactional
```

**RabbitMQ:**
```bash
# Docker
docker run -d --name rabbitmq \
  -p 5672:5672 -p 15672:15672 \
  rabbitmq:3-management
```

### Development Order

**Phase 1: MS SQL Adapter (2-3 weeks)**
```bash
# Step 1: Setup
go get github.com/denisenkom/go-mssqldb

# Step 2: Implement
# - types.go
# - adapter.go
# - export.go
# - import.go

# Step 3: Test
go test ./pkg/adapters/mssql/...
```

**Phase 2: MSMQ Integration (2-3 weeks)**
```bash
# Step 1: MSMQ Wrapper
# - Implement syscall wrapper
# - Or create C# service

# Step 2: Producer/Consumer
go test ./pkg/brokers/msmq/...

# Step 3: Integration
# - Connect MS SQL + MSMQ
```

**Phase 3: RabbitMQ Integration (1-2 weeks)**
```bash
# Step 1: Setup
go get github.com/rabbitmq/amqp091-go

# Step 2: Implement
go test ./pkg/brokers/rabbitmq/...
```

---

## 💡 Success Criteria

### MS SQL Adapter
- ✅ Подключение к MS SQL Server
- ✅ Export таблиц в TDTP формат
- ✅ Import TDTP пакетов в MS SQL
- ✅ Поддержка всех основных типов MS SQL
- ✅ Bulk operations для производительности
- ✅ Integration тесты проходят

### MSMQ Integration
- ✅ Отправка TDTP пакетов в MSMQ
- ✅ Получение пакетов из MSMQ
- ✅ Transactional support
- ✅ Dead letter queue handling
- ✅ End-to-end тест: MS SQL → MSMQ → Import

### RabbitMQ Integration
- ✅ Отправка в RabbitMQ с routing
- ✅ Consumer с auto-ack/manual-ack
- ✅ Publisher confirms
- ✅ Connection recovery
- ✅ End-to-end тест: MS SQL → RabbitMQ → Import

### Integration
- ✅ CLI команды работают
- ✅ Config файлы поддерживаются
- ✅ Examples запускаются
- ✅ Документация complete

---

## 📝 Next Steps

**Immediate (Week 1):**
1. Создать структуру `pkg/adapters/mssql/`
2. Настроить MS SQL драйвер
3. Реализовать базовое подключение
4. Начать маппинг типов

**Short-term (Week 2-3):**
1. Реализовать Export/Import
2. Добавить bulk operations
3. Написать integration тесты

**Mid-term (Week 4-6):**
1. MSMQ integration
2. RabbitMQ integration
3. CLI extensions

**Long-term (Week 7-8):**
1. Integration examples
2. СЭД integration guide
3. Final testing and documentation

---

## 📚 Важная документация

**КРИТИЧНО - Обязательно к прочтению перед началом разработки:**

### 1. **[MSSQL_DEV_VS_PROD.md](docs/MSSQL_DEV_VS_PROD.md)** ⚠️ САМОЕ ВАЖНОЕ!
   - **Проблема:** Docker (SQL Server 2019+) vs Production (SQL Server 2012)
   - **Решение:** Compatibility mode testing
   - **Forbidden functions list** (JSON, STRING_SPLIT, STRING_AGG)
   - **Allowed functions list** (OFFSET/FETCH, MERGE, IIF)
   - **Docker Compose** для тестирования на SQL Server 2012 mode
   - **Pre-commit workflow** для проверки совместимости

### 2. **[MSSQL_2012_COMPATIBILITY.md](docs/MSSQL_2012_COMPATIBILITY.md)**
   - Поддерживаемые типы данных
   - Ограничения SQL Server 2012
   - Рекомендации по type mapping
   - Feature detection strategy
   - Workarounds для устаревших функций

**Ключевые моменты:**
- ✅ OFFSET/FETCH работает в SQL Server 2012+
- ✅ MERGE работает для UPSERT
- ❌ JSON функции НЕ работают (SQL Server 2016+)
- ❌ STRING_SPLIT НЕ работает (SQL Server 2016+)
- ❌ STRING_AGG НЕ работает (SQL Server 2017+)
- Используем NVARCHAR для Unicode
- Используем DATETIME2 для лучшей точности
- **Feature detection** для условного использования функций
- **ВСЕГДА тестировать на prod simulation** перед коммитом

---

## 🚀 Быстрый старт для разработки

### Первые шаги ПЕРЕД написанием кода:

1. **Прочитать документацию (30 минут):**
   - ⚠️ [docs/MSSQL_DEV_VS_PROD.md](docs/MSSQL_DEV_VS_PROD.md) - КРИТИЧНО!
   - [docs/MSSQL_2012_COMPATIBILITY.md](docs/MSSQL_2012_COMPATIBILITY.md)

2. **Настроить окружение:**
   ```bash
   # Запустить prod simulation контейнер
   docker-compose -f docker-compose.mssql.yml up -d mssql-prod-sim

   # Проверить что контейнер работает
   docker-compose -f docker-compose.mssql.yml ps

   # Подключиться и проверить compatibility level
   docker exec -it tdtp-mssql-prod-sim /opt/mssql-tools/bin/sqlcmd \
       -S localhost -U sa -P 'ProdPassword123!' \
       -Q "SELECT name, compatibility_level FROM sys.databases WHERE name='ProdSimDB'"

   # Должен вернуть: ProdSimDB | 110 (SQL Server 2012)
   ```

3. **Создать базовую структуру:**
   ```bash
   mkdir -p pkg/adapters/mssql
   cd pkg/adapters/mssql
   ```

4. **Начать с feature detection:**
   - Создать `adapter.go` с определением версии сервера
   - Создать `types.go` с маппингом типов SQL Server 2012+
   - Добавить тесты на compatibility level 110

---

**Готов начать реализацию?** 🚀

Предлагаю начать с MS SQL Server Adapter, так как это foundation для всей вашей интеграции.

**КРИТИЧНО:** Всегда тестировать на `mssql-prod-sim` (SQL Server 2012 compatibility) перед коммитом!
