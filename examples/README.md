# TDTP Framework Examples

Практические примеры использования TDTP Framework для различных сценариев интеграции данных.

## Быстрый старт

Каждый пример является самостоятельным проектом с подробной документацией и готовым к запуску кодом.

```bash
# Клонируйте репозиторий
git clone https://github.com/queuebridge/tdtp.git
cd tdtp/examples

# Выберите пример и запустите
cd 01-basic-export
go run main.go
```

## Примеры

### [01. Basic Export](./01-basic-export/)
**Сложность**: ⭐ Начинающий
**Время**: 5 минут

Простейший пример экспорта данных из PostgreSQL в TDTP XML файл.

**Что демонстрирует**:
- Базовое использование адаптеров
- Экспорт таблицы
- Работа с TDTP пакетами

**Когда использовать**:
- Первое знакомство с фреймворком
- Простая миграция данных
- Backup в файлы

```bash
cd 01-basic-export
go run main.go
```

---

### [02. RabbitMQ + MSSQL Integration](./02-rabbitmq-mssql/) 🔥
**Сложность**: ⭐⭐⭐ Продвинутый
**Время**: 20 минут

**Полноценная интеграция MSSQL → RabbitMQ с enterprise-функциями.**

**Что демонстрирует**:
- ✅ MSSQL Adapter - экспорт из SQL Server
- ✅ RabbitMQ Broker - отправка в очередь
- ✅ **Circuit Breaker** - защита от перегрузки
- ✅ **Retry Mechanism** - exponential backoff с jitter
- ✅ **Audit Logger** - полный audit trail (GDPR compliance)
- ✅ **Data Masking** - PII protection (email, phone, cards)
- ✅ **Data Validation** - проверка перед отправкой
- ✅ **Data Normalization** - приведение к стандартному формату

**Когда использовать**:
- Production-ready интеграция с message broker
- Защита от каскадных сбоев
- Compliance требования (GDPR, HIPAA)
- Обработка PII данных

**Архитектура**:
```
MSSQL (OrdersDB) → Export → Normalize → Validate → Mask
                              ↓
                    Circuit Breaker + Retry
                              ↓
                    RabbitMQ (orders-queue)
                              ↓
                          Audit Log
```

```bash
cd 02-rabbitmq-mssql
go run main.go
```

**Prerequisites**: MSSQL, RabbitMQ (Docker Compose файл включен)

---

### [03. Incremental Sync](./03-incremental-sync/)
**Сложность**: ⭐⭐ Средний
**Время**: 15 минут

Инкрементальная синхронизация PostgreSQL → MySQL с checkpoint tracking.

**Что демонстрирует**:
- IncrementalSync с StateManager
- Tracking по timestamp
- Batch processing
- Checkpoint сохранение/восстановление
- Resume after failure

**Когда использовать**:
- Синхронизация больших таблиц
- Репликация между базами
- ETL pipelines с checkpoints
- Снижение нагрузки (только изменения)

**Производительность**:
- Полный export: 10M записей = 2 часа
- Incremental: 10K новых = 30 секунд ⚡
- **200x faster** для больших таблиц

```bash
cd 03-incremental-sync
go run main.go
```

---

### [04. TDTP ↔ XLSX Converter](./04-tdtp-xlsx/) 🍒
**Сложность**: ⭐ Начинающий
**Время**: 5 минут

**Мгновенный профит для бизнеса** - конвертация между базой данных и Excel.

**Что демонстрирует**:
- ✅ TDTP → XLSX export (Database → Excel для анализа)
- ✅ XLSX → TDTP import (Excel → Database загрузка)
- ✅ Type preservation (INTEGER, REAL, BOOLEAN, DATE, etc.)
- ✅ Formatted headers (field types + primary keys)
- ✅ Auto-formatting (numbers, dates, booleans)
- ✅ Round-trip data integrity

**Когда использовать**:
- Business users работают с данными в Excel
- Экспорт отчетов из БД для анализа
- Импорт данных из Excel без SQL знаний
- Master data management в Excel
- Data validation и corrections
- **Любой сценарий где нужен Excel** 📊

**Бизнес-ценность**:
- Не нужно знать SQL - работайте в Excel
- Мгновенный экспорт для анализа
- Bulk loading через Excel
- Знакомый интерфейс для всех
- Zero training required

```bash
cd 04-tdtp-xlsx
go run main.go
# Generates: ./output/orders.xlsx (ready for Excel!)
```

**Пример Excel файла:**
```
order_id (INTEGER) * | customer (TEXT) | product (TEXT) | quantity (INTEGER) | ...
1001                 | ACME Corp       | Laptop         | 5                  | ...
1002                 | Tech Solutions  | Monitor        | 10                 | ...
```

---

### [04. Audit + Data Masking](./04-audit-masking/)
**Сложность**: ⭐⭐ Средний
**Время**: 10 минут

Compliance-focused пример с audit logging и data masking.

**Что демонстрирует**:
- Три уровня аудита (Minimal, Standard, Full)
- PII masking стратегии
- GDPR compliance
- Audit log query/filter
- Retention policies

**Когда использовать**:
- Healthcare (HIPAA)
- Finance (SOX, PCI DSS)
- GDPR compliance
- Data privacy requirements

```bash
cd 04-audit-masking
go run main.go
```

---

### [05. Circuit Breaker](./05-circuit-breaker/)
**Сложность**: ⭐⭐ Средний
**Время**: 10 минут

Защита внешних API от перегрузки с помощью Circuit Breaker.

**Что демонстрирует**:
- Circuit Breaker states (Closed, Half-Open, Open)
- Automatic recovery
- Fallback функции
- State change callbacks
- Concurrent call limiting

**Когда использовать**:
- Интеграция с нестабильными API
- Защита от каскадных сбоев
- Rate limiting
- Graceful degradation

```bash
cd 05-circuit-breaker
go run main.go
```

---

### [06. Complete ETL Pipeline](./06-etl-pipeline/)
**Сложность**: ⭐⭐⭐⭐ Эксперт
**Время**: 30 минут

Production-grade ETL pipeline со всеми компонентами фреймворка.

**Что демонстрирует**:
- Multi-source extraction (PostgreSQL + MongoDB + API)
- Complex transformations
- Data enrichment
- Multiple destinations (MySQL + S3 + Kafka)
- Full audit trail
- Error handling с DLQ
- Monitoring & metrics

**Когда использовать**:
- Enterprise ETL systems
- Data warehousing
- Multi-source integration
- Complex data pipelines

```bash
cd 06-etl-pipeline
go run main.go
```

---

### [08. ETL Pipeline + xzmercury](./08-pipeline-encrypted/) 🔐
**Сложность**: ⭐⭐ Средний
**Время**: 5 минут

Интеграционный пример: embedded xzmercury-mock + `tdtpcli --pipeline` с шифрованием.

**Что демонстрирует**:
- ✅ ETL pipeline (TDTP → SQLite workspace → transform → output)
- ✅ Шифрование AES-256-GCM через xzmercury (bind → encrypt → burn-on-read)
- ✅ Вызов `tdtpcli` как внешнего процесса
- ✅ Нулевые внешние зависимости (mock-сервер встроен)

**Когда использовать**:
- Знакомство с интеграцией пайплайна и xzmercury
- Тестирование pipeline без реального xzmercury
- Шаблон для production (заменить mock на реальный сервер)

```bash
go run ./examples/08-pipeline-encrypted/
```

---

### [09. S3 Pipeline Chain: Extract → Split by Region](./09-s3-pipeline-chain/)
**Сложность:** ⭐ Начальный
**Время:** 3 минуты

Цепочка двух пайплайнов с оркестрирующим shell-скриптом.

**Что демонстрирует:**
- Pipeline 1: PostgreSQL → полный TDTP-файл в S3 (с zstd-сжатием)
- Pipeline 2 (шаблон): S3-файл → фильтрация по региону → отдельный файл в S3
- `run_chain.sh`: получает список уникальных значений из БД, запускает pipeline 2 для каждого через `sed`-подстановку в шаблон

**Когда использовать:**
- Нужно разбить один большой экспорт на файлы по категории (регион, статус, отдел)
- Данные из S3 затем забирают независимые потребители (микросервисы, аналитические задания)
- ETL fan-out без изменения кода фреймворка — только YAML + bash

```bash
cd examples/09-s3-pipeline-chain
bash run_chain.sh
```

---

## Сравнение примеров

| Пример | Сложность | Компоненты | Production-Ready | Use Case |
|--------|-----------|------------|------------------|----------|
| 01-basic-export | ⭐ | Adapter | ❌ | Learning, Simple migration |
| 02-rabbitmq-mssql | ⭐⭐⭐ | Adapter, Broker, Circuit Breaker, Retry, Audit, Processors | ✅ | Message queue integration |
| 03-incremental-sync | ⭐⭐ | Adapter, IncrementalSync, StateManager | ✅ | Database replication |
| 04-tdtp-xlsx 🍒 | ⭐ | XLSX Converter | ✅ | Business reports, Excel integration |
| 04-audit-masking | ⭐⭐ | Audit, Processors | ✅ | Compliance, Data privacy |
| 05-circuit-breaker | ⭐⭐ | Circuit Breaker | ✅ | API resilience |
| 06-etl-pipeline | ⭐⭐⭐⭐ | All components | ✅ | Enterprise ETL |
| 08-pipeline-encrypted | ⭐⭐ | ETL + xzmercury | ✅ | Encrypted pipeline, no external deps |
| 09-s3-pipeline-chain | ⭐ | ETL + S3 + bash | ✅ | S3 fan-out, split by category |

## Основные компоненты

### Adapters
- **Database**: PostgreSQL, MySQL, MSSQL, SQLite
- **Brokers**: RabbitMQ, Kafka
- **Files**: JSON, XML, CSV, TDTP

### Resilience
- **Circuit Breaker**: Защита от каскадных сбоев
- **Retry**: Exponential backoff с jitter
- **DLQ**: Dead Letter Queue для failed messages

### Data Processing
- **FieldMasker**: Email, phone, card masking
- **FieldValidator**: Regex, range, format validation
- **FieldNormalizer**: Email, phone, date normalization

### Audit & Compliance
- **AuditLogger**: File, Database, Console appenders
- **Logging Levels**: Minimal, Standard, Full (GDPR)
- **Query & Filter**: SQL-like audit log queries

### Sync & State
- **IncrementalSync**: Timestamp, Sequence, Version tracking
- **StateManager**: Checkpoint persistence
- **Batch Processing**: Configurable batch sizes

## Быстрый выбор примера

**Я хочу...**

- **Изучить фреймворк** → [01-basic-export](./01-basic-export/)
- **Интеграция с RabbitMQ/MSSQL** → [02-rabbitmq-mssql](./02-rabbitmq-mssql/) 🔥
- **Синхронизировать большие таблицы** → [03-incremental-sync](./03-incremental-sync/)
- **Работать с данными в Excel** → [04-tdtp-xlsx](./04-tdtp-xlsx/) 🍒
- **GDPR compliance** → [04-audit-masking](./04-audit-masking/)
- **Защитить API от сбоев** → [05-circuit-breaker](./05-circuit-breaker/)
- **Полноценный ETL** → [06-etl-pipeline](./06-etl-pipeline/)
- **ETL + шифрование (без внешних зависимостей)** → [08-pipeline-encrypted](./08-pipeline-encrypted/)

## Production Checklist

Перед использованием в production, убедитесь:

- [ ] **Error Handling**: Настроен retry + DLQ
- [ ] **Audit Logging**: Включен audit logger с правильным уровнем
- [ ] **Circuit Breaker**: Настроен для внешних зависимостей
- [ ] **Monitoring**: Подключены метрики (Prometheus/Grafana)
- [ ] **Health Checks**: Реализованы health check endpoints
- [ ] **Graceful Shutdown**: Обработка SIGTERM/SIGINT
- [ ] **Data Validation**: Включены validators для critical fields
- [ ] **Data Masking**: PII данные маскируются (GDPR/HIPAA)
- [ ] **Incremental Sync**: Checkpoint файлы backed up
- [ ] **Resource Limits**: Настроены max connections, timeouts
- [ ] **Security**: Credentials в environment variables
- [ ] **Testing**: Integration tests с real databases
- [ ] **Documentation**: Runbooks для operations team
- [ ] **Alerting**: Alerts на circuit breaker open, DLQ size

## Локальное тестирование

### Docker Compose для всех сервисов

```bash
# Используйте docker-compose-generator из фреймворка
cd tools/docker-compose-generator
go run main.go

# Выберите:
# - PostgreSQL
# - MySQL
# - MSSQL
# - RabbitMQ
# - Kafka

# Запустите сгенерированную конфигурацию
docker-compose up -d
```

### Или вручную

```bash
# PostgreSQL
docker run -d --name postgres \
  -e POSTGRES_PASSWORD=password \
  -p 5432:5432 postgres:14

# MySQL
docker run -d --name mysql \
  -e MYSQL_ROOT_PASSWORD=password \
  -p 3306:3306 mysql:8

# MSSQL
docker run -d --name mssql \
  -e "ACCEPT_EULA=Y" -e "SA_PASSWORD=YourPassword123" \
  -p 1433:1433 mcr.microsoft.com/mssql/server:2019-latest

# RabbitMQ
docker run -d --name rabbitmq \
  -p 5672:5672 -p 15672:15672 \
  rabbitmq:3-management

# Kafka
docker run -d --name kafka \
  -p 9092:9092 -p 9093:9093 \
  apache/kafka:latest
```

## Следующие шаги

1. **Изучите базовые концепции**: Начните с [01-basic-export](./01-basic-export/)
2. **Попробуйте свой use case**: Адаптируйте [02-rabbitmq-mssql](./02-rabbitmq-mssql/)
3. **Production deployment**: Используйте [checklist](#production-checklist)
4. **Читайте документацию**: [TDTP Framework Documentation](../README.md)

## Поддержка

- **Issues**: https://github.com/queuebridge/tdtp/issues
- **Discussions**: https://github.com/queuebridge/tdtp/discussions
- **Documentation**: https://docs.tdtp.dev

## Contributing

Хотите добавить свой пример? Мы приветствуем contributions!

1. Fork repository
2. Create example in `examples/XX-your-example/`
3. Add README.md с подробным описанием
4. Submit pull request

## License

MIT License - see LICENSE file for details
