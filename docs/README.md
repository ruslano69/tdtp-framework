# TDTP Framework Documentation

Полная документация TDTP Framework v1.3.

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
   - ETL Pipeline (`--pipeline`, `--enc`, `--enc-dev`)
   - Шифрование AES-256-GCM через xZMercury
   - Конфигурация YAML
   - Работа с TDTQL фильтрами
   - Message Brokers интеграция
   - Примеры использования

### Для разработчиков

3. **[ETL_PIPELINE.md](./ETL_PIPELINE.md)** - ETL Pipeline сценарии 🆕
   - Справочник конфигурации YAML
   - Сценарии: TDTP JOIN, PostgreSQL→TDTP, шифрование, Redis оркестрация
   - Graceful degradation при отказе xZMercury
   - CLI флаги, exit codes

4. **[DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md)** - Руководство разработчика
   - Архитектура фреймворка
   - Настройка тестовой среды
   - Core Modules (Packet, Schema, TDTQL)
   - Database Adapters (SQLite, PostgreSQL, MSSQL, MySQL)
   - Message Brokers (RabbitMQ, MSMQ, Kafka)
   - Production Features (Circuit Breaker, Retry, Audit, Processors)
   - Security: Encryption v1.3 (pkg/mercury, pkg/crypto, xzmercury-mock)
   - Разработка нового адаптера
   - Best Practices
   - Testing

5. **[DEPLOYMENT.md](./DEPLOYMENT.md)** - Развёртывание системы оркестрации
   - Карта сервисов и зависимости между ними
   - Минимальная локальная установка (dev)
   - Продакшн-развёртывание (Redis, TLS, LDAP)
   - Порядок запуска сервисов
   - Air-gap / offline cert
   - Audit log: text/JSON/syslog форматы

6. **[SPECIFICATION.md](./SPECIFICATION.md)** - Спецификация TDTP v1.0 & TDTQL
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
5. **[examples/encryption-test/](../examples/encryption-test/)** - ETL с шифрованием 🆕

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
| **Запустить ETL pipeline** | [ETL_PIPELINE.md](./ETL_PIPELINE.md) 🆕 |
| **Шифрование через xZMercury** | [ETL_PIPELINE.md § Сценарий 3](./ETL_PIPELINE.md#сценарий-3-шифрованный-вывод-через-xzmercury) 🆕 |
| **Развернуть оркестрацию** | [DEPLOYMENT.md](./DEPLOYMENT.md) |
| **LDAP auth в оркестраторе** | [DEPLOYMENT.md § LDAP auth](./DEPLOYMENT.md#ldap-auth) |
| **Air-gap / offline cert** | [DEPLOYMENT.md § Air-gapped](./DEPLOYMENT.md#air-gapped-environments) |
| **Audit log JSON/syslog** | [DEPLOYMENT.md § Audit log](./DEPLOYMENT.md#audit-log-tdtpcli) |
| **Понять TDTP формат** | [SPECIFICATION.md](./SPECIFICATION.md) |
| **Разрабатывать с фреймворком** | [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md) |
| **Настроить тестовую среду** | [DEVELOPER_GUIDE.md § Настройка тестовой среды](./DEVELOPER_GUIDE.md#настройка-тестовой-среды) |
| **Работать с пакетами** | [DEVELOPER_GUIDE.md § Packet Module](./DEVELOPER_GUIDE.md#packet-module) |
| **Работать с типами данных** | [DEVELOPER_GUIDE.md § Schema Module](./DEVELOPER_GUIDE.md#schema-module) |
| **Использовать TDTQL** | [DEVELOPER_GUIDE.md § TDTQL Module](./DEVELOPER_GUIDE.md#tdtql-module) |
| **Интеграция с БД** | [DEVELOPER_GUIDE.md § Database Adapters](./DEVELOPER_GUIDE.md#database-adapters) |
| **Разработать свой адаптер** | [DEVELOPER_GUIDE.md § Разработка нового адаптера](./DEVELOPER_GUIDE.md#разработка-нового-адаптера) |
| **pkg/mercury, pkg/crypto** | [DEVELOPER_GUIDE.md § Security Encryption](./DEVELOPER_GUIDE.md#security-encryption-v13) 🆕 |
| **Circuit Breaker** | [pkg/resilience/README.md](../pkg/resilience/README.md) |
| **Retry mechanism** | [pkg/retry/README.md](../pkg/retry/README.md) |
| **Audit Logging** | [pkg/audit/README.md](../pkg/audit/README.md) |
| **Data Processors** | [pkg/processors/README.md](../pkg/processors/README.md) |
| **Incremental Sync** | [pkg/sync/README.md](../pkg/sync/README.md) |
| **Excel конвертер** | [pkg/xlsx/README.md](../pkg/xlsx/README.md) 🍒 |
| **Примеры кода** | [examples/README.md](../examples/README.md) |

---

## 🔄 История изменений

### v1.3 (26.02.2026) - Current 🆕

✅ **Новые фичи:**
- AES-256-GCM шифрование через xZMercury (UUID-binding флоу)
- Тип пакета `error` для управляемых ошибок ETL pipeline
- pkg/mercury — HTTP клиент, HMAC верификация, DevClient для dev-сборок
- pkg/crypto — AES-256-GCM с бинарным заголовком
- cmd/xzmercury-mock — standalone mock-сервер для тестирования
- `--enc` / `--enc-dev` флаги для tdtpcli
- ResultLog: статус `completed_with_errors`, поле `package_uuid`
- Graceful degradation: error-пакет при недоступности xZMercury, exit 0

✅ **Документация:**
- Новый ETL_PIPELINE.md — полное руководство с 5 сценариями
- Обновлены SPECIFICATION.md (v1.3), USER_GUIDE.md, DEVELOPER_GUIDE.md

### v1.2 (17.11.2025)

✅ **Новые фичи:**
- XLSX Converter (Database ↔ Excel) 🍒
- Circuit Breaker для resilience
- Audit Logger для compliance
- Production-ready CLI с всеми v1.2 фичами

✅ **Документация:**
- ✨ Новый DEVELOPER_GUIDE.md (комплексное руководство разработчика)
- Обновлены USER_GUIDE.md и SPECIFICATION.md

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
├── ETL_PIPELINE.md        # ETL Pipeline — сценарии и примеры 🆕
├── USER_GUIDE.md          # Руководство пользователя CLI
└── SPECIFICATION.md       # Спецификация TDTP v1.0-v1.3 & TDTQL

Root:
├── README.md              # Главная страница проекта
└── ROADMAP.md             # Дорожная карта развития
```

---

## 📞 Поддержка

**GitHub Issues:** https://github.com/ruslano69/tdtp-framework/issues
**Email:** ruslano69@gmail.com

---

**Версия:** v1.3
**Последнее обновление:** 26.02.2026
