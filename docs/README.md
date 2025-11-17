# TDTP Framework Documentation

Полная документация TDTP Framework v1.2.

---

## 📚 Основные руководства

### Для новых пользователей

1. **[../README.md](../README.md)** ⭐ **НАЧНИТЕ ЗДЕСЬ**
   - Обзор фреймворка
   - Быстрый старт
   - Установка
   - Основные концепции

2. **[USER_GUIDE.md](./USER_GUIDE.md)** - CLI утилита tdtpcli
   - Команды и параметры
   - Конфигурация YAML
   - Работа с TDTQL фильтрами
   - Message Brokers интеграция
   - Примеры использования

### Для разработчиков

3. **[DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)** - Руководство разработчика
   - Архитектура фреймворка
   - Настройка тестовой среды
   - Core Modules (Packet, Schema, TDTQL)
   - Database Adapters (SQLite, PostgreSQL, MSSQL, MySQL)
   - Message Brokers (RabbitMQ, MSMQ, Kafka)
   - Production Features (Circuit Breaker, Retry, Audit, Processors)
   - Разработка нового адаптера
   - Best Practices
   - Testing

4. **[SPECIFICATION.md](./SPECIFICATION.md)** - Спецификация TDTP v1.0 & TDTQL
   - XML формат сообщений
   - Типы данных
   - TDTQL язык запросов
   - Протокол обмена
   - Примеры пакетов

---

## 📦 Package-specific документация

Каждый production-ready пакет имеет свой README:

### Resilience & Production Features

- **[pkg/resilience/README.md](../pkg/resilience/README.md)** - Circuit Breaker
  - Три состояния (Closed, Half-Open, Open)
  - Automatic recovery
  - Concurrent call limiting
  - State change callbacks
  - Custom trip logic

- **[pkg/retry/README.md](../pkg/retry/README.md)** - Retry Mechanism
  - Exponential backoff
  - Jitter strategies
  - Context-aware retry
  - Dead Letter Queue (DLQ) support

- **[pkg/audit/README.md](../pkg/audit/README.md)** - Audit Logger
  - File, Database, Console appenders
  - Три уровня (Minimal, Standard, Full)
  - GDPR/HIPAA/SOX compliance
  - Async/Sync modes
  - Query и filter операции

- **[pkg/processors/README.md](../pkg/processors/README.md)** - Data Processors
  - FieldMasker (PII protection)
  - FieldValidator (data validation)
  - FieldNormalizer (data normalization)
  - Processor chains

- **[pkg/sync/README.md](../pkg/sync/README.md)** - Incremental Sync
  - StateManager with checkpoint tracking
  - Timestamp/sequence-based sync
  - Batch processing
  - Recovery mechanisms

### Data Conversion

- **[pkg/xlsx/README.md](../pkg/xlsx/README.md)** - XLSX Converter 🍒
  - TDTP → Excel export
  - Excel → TDTP import
  - Type preservation
  - Business value для non-technical users

### Database Adapters

- **[pkg/adapters/sqlite/README.md](../pkg/adapters/sqlite/README.md)** - SQLite
- **[pkg/adapters/postgres/README.md](../pkg/adapters/postgres/README.md)** - PostgreSQL
- **[pkg/adapters/mysql/README.md](../pkg/adapters/mysql/README.md)** - MySQL
- **[pkg/adapters/mssql/README.md](../pkg/adapters/mssql/README.md)** - MS SQL Server

---

## 💡 Примеры использования

Полные production-ready примеры:

**[examples/README.md](../examples/README.md)** - Каталог всех примеров

**Рекомендуемые примеры:**

1. **[examples/01-basic-export/](../examples/01-basic-export/)** - Начните здесь
2. **[examples/04-tdtp-xlsx/](../examples/04-tdtp-xlsx/)** - XLSX converter 🍒
3. **[examples/02-rabbitmq-mssql/](../examples/02-rabbitmq-mssql/)** - Production integration
4. **[examples/03-incremental-sync/](../examples/03-incremental-sync/)** - Incremental sync

---

## 🗺️ Roadmap

См. **[ROADMAP.md](../ROADMAP.md)** для:
- Текущий статус (v1.2)
- Запланированные фичи (v1.3, v1.5, v2.0)
- Use cases

---

## 📖 Быстрая навигация

**Я хочу...**

| Задача | Документ |
|--------|----------|
| **Установить фреймворк** | [README.md](../README.md) |
| **Использовать CLI** | [USER_GUIDE.md](./USER_GUIDE.md) |
| **Понять TDTP формат** | [SPECIFICATION.md](./SPECIFICATION.md) |
| **Разрабатывать с фреймворком** | [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md) |
| **Настроить тестовую среду** | [DEVELOPER_GUIDE.md § Настройка тестовой среды](./DEVELOPER_GUIDE.md#настройка-тестовой-среды) |
| **Работать с пакетами** | [DEVELOPER_GUIDE.md § Packet Module](./DEVELOPER_GUIDE.md#packet-module) |
| **Работать с типами данных** | [DEVELOPER_GUIDE.md § Schema Module](./DEVELOPER_GUIDE.md#schema-module) |
| **Использовать TDTQL** | [DEVELOPER_GUIDE.md § TDTQL Module](./DEVELOPER_GUIDE.md#tdtql-module) |
| **Интеграция с БД** | [DEVELOPER_GUIDE.md § Database Adapters](./DEVELOPER_GUIDE.md#database-adapters) |
| **Разработать свой адаптер** | [DEVELOPER_GUIDE.md § Разработка нового адаптера](./DEVELOPER_GUIDE.md#разработка-нового-адаптера) |
| **Circuit Breaker** | [pkg/resilience/README.md](../pkg/resilience/README.md) |
| **Retry mechanism** | [pkg/retry/README.md](../pkg/retry/README.md) |
| **Audit Logging** | [pkg/audit/README.md](../pkg/audit/README.md) |
| **Data Processors** | [pkg/processors/README.md](../pkg/processors/README.md) |
| **Incremental Sync** | [pkg/sync/README.md](../pkg/sync/README.md) |
| **Excel конвертер** | [pkg/xlsx/README.md](../pkg/xlsx/README.md) 🍒 |
| **Примеры кода** | [examples/README.md](../examples/README.md) |

---

## 🔄 История изменений

### v1.2 (17.11.2025) - Current

✅ **Новые фичи:**
- XLSX Converter (Database ↔ Excel) 🍒
- Circuit Breaker для resilience
- Audit Logger для compliance
- Production-ready CLI с всеми v1.2 фичами

✅ **Документация:**
- ✨ Новый DEVELOPER_GUIDE.md (комплексное руководство разработчика)
- Обновлены USER_GUIDE.md и SPECIFICATION.md
- Удалена устаревшая и временная документация
- Исправлены все ссылки на репозиторий

### v1.1 (16.11.2025)

- Retry mechanism с DLQ
- Incremental Sync
- Data processors (Masker, Validator, Normalizer)
- Kafka broker
- Docker Compose generator

### v1.0 (15.11.2025)

- Core modules (Packet, Schema, TDTQL)
- Database adapters (SQLite, PostgreSQL, MSSQL)
- Message brokers (RabbitMQ, MSMQ)
- CLI utility (tdtpcli)

---

## 📋 Структура документации

```
docs/
├── README.md              # Этот файл - навигация по документации
├── DEVELOPER_GUIDE.md     # Руководство разработчика (архитектура, модули, адаптеры)
├── USER_GUIDE.md          # Руководство пользователя CLI
└── SPECIFICATION.md       # Спецификация TDTP v1.0 & TDTQL

Root:
├── README.md              # Главная страница проекта
└── ROADMAP.md             # Дорожная карта развития
```

---

## 📞 Поддержка

**GitHub Issues:** https://github.com/ruslano69/tdtp-framework/issues
**Email:** ruslano69@gmail.com

---

**Версия:** v1.2
**Последнее обновление:** 17.11.2025
