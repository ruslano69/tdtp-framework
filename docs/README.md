# TDTP Framework Documentation

Полная документация TDTP Framework v1.2.

---

## 📚 Руководства

### Для новых пользователей

1. **[INSTALLATION_GUIDE.md](../INSTALLATION_GUIDE.md)** ⭐ **НАЧНИТЕ ЗДЕСЬ**
   - Системные требования
   - Установка фреймворка
   - Быстрый старт
   - Настройка БД и брокеров
   - Production deployment
   - Troubleshooting

2. **[USER_GUIDE.md](./USER_GUIDE.md)** - CLI утилита
   - Команды tdtpcli
   - Конфигурация YAML
   - Работа с TDTQL фильтрами
   - Message Brokers интеграция
   - Примеры использования

### Для разработчиков

3. **[SPECIFICATION.md](./SPECIFICATION.md)** - Спецификация TDTP v1.0
   - XML формат сообщений
   - Типы данных
   - TDTQL язык запросов
   - Протокол обмена

---

## 🔧 Модули фреймворка

### Core модули

- **[PACKET_MODULE.md](./PACKET_MODULE.md)** - Работа с TDTP пакетами
  - Парсинг XML
  - Генерация Reference/Delta/Request/Response
  - Пагинация (chunks до 3.8MB)
  - QueryContext для stateless паттерна

- **[SCHEMA_MODULE.md](./SCHEMA_MODULE.md)** - Типы данных и валидация
  - DataType поддержка (INTEGER, TEXT, DECIMAL, DATE, etc.)
  - Schema Builder
  - Converter для адаптеров
  - Валидация данных

- **[TDTQL_TRANSLATOR.md](./TDTQL_TRANSLATOR.md)** - Язык запросов
  - SQL → TDTQL трансляция
  - TDTQL Executor (in-memory фильтрация)
  - SQL Generator (TDTQL → SQL оптимизация)
  - Операторы (=, !=, <, >, IN, LIKE, BETWEEN, IS NULL)

### Адаптеры БД

- **[SQLITE_ADAPTER.md](./SQLITE_ADAPTER.md)** - SQLite интеграция
  - Export/Import таблиц
  - Стратегии импорта (REPLACE, IGNORE, COPY, FAIL)
  - TDTQL → SQL оптимизация
  - Benchmarks

### Архитектура

- **[MODULES.md](./MODULES.md)** - Обзор всех модулей
  - Структура проекта
  - Зависимости между модулями
  - Паттерны проектирования

---

## 📦 Package-specific документация

Каждый production-ready пакет имеет свой README:

### Resilience & Production

- **[pkg/resilience/README.md](../pkg/resilience/README.md)** - Circuit Breaker
  - Три состояния (Closed, Half-Open, Open)
  - Automatic recovery
  - Concurrent call limiting
  - State change callbacks

- **[pkg/audit/README.md](../pkg/audit/README.md)** - Audit Logger
  - File, Database, Console appenders
  - Три уровня (Minimal, Standard, Full)
  - GDPR/HIPAA/SOX compliance
  - Async/Sync modes
  - Query и filter операции

### Data Conversion

- **[pkg/xlsx/README.md](../pkg/xlsx/README.md)** - XLSX Converter 🍒
  - TDTP → Excel export
  - Excel → TDTP import
  - Type preservation
  - Business value для non-technical users

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
| **Установить фреймворк** | [INSTALLATION_GUIDE.md](../INSTALLATION_GUIDE.md) |
| **Использовать CLI** | [USER_GUIDE.md](./USER_GUIDE.md) |
| **Понять TDTP формат** | [SPECIFICATION.md](./SPECIFICATION.md) |
| **Работать с пакетами** | [PACKET_MODULE.md](./PACKET_MODULE.md) |
| **Работать с типами данных** | [SCHEMA_MODULE.md](./SCHEMA_MODULE.md) |
| **Использовать TDTQL** | [TDTQL_TRANSLATOR.md](./TDTQL_TRANSLATOR.md) |
| **Интеграция с SQLite** | [SQLITE_ADAPTER.md](./SQLITE_ADAPTER.md) |
| **Circuit Breaker** | [pkg/resilience/README.md](../pkg/resilience/README.md) |
| **Audit Logging** | [pkg/audit/README.md](../pkg/audit/README.md) |
| **Excel конвертер** | [pkg/xlsx/README.md](../pkg/xlsx/README.md) 🍒 |
| **Примеры кода** | [examples/README.md](../examples/README.md) |

---

## 🔄 История изменений

### v1.2 (17.11.2025) - Current

✅ **Новые фичи:**
- XLSX Converter (Database ↔ Excel) 🍒
- Circuit Breaker для resilience
- Audit Logger для compliance
- Production-ready примеры

✅ **Документация:**
- Обновлен INSTALLATION_GUIDE.md
- Удалена устаревшая документация
- Добавлены package-specific READMEs

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

## 📞 Поддержка

**GitHub Issues:** https://github.com/queuebridge/tdtp/issues
**Discussions:** https://github.com/queuebridge/tdtp/discussions
**Email:** support@queuebridge.io

---

**Версия:** v1.2
**Последнее обновление:** 17.11.2025
