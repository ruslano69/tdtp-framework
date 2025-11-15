# TDTP Framework

**Table Data Transfer Protocol** - фреймворк для универсального обмена табличными данными через message brokers.

## 🎯 Цели проекта

- **Универсальность** - работа с любыми таблицами и СУБД
- **Прозрачность** - самодокументируемые XML сообщения
- **Надежность** - stateless паттерн, валидация, пагинация
- **Безопасность** - TLS, аутентификация, audit trail
- **Удобство** - простое API, понятная структура

## 📦 Что реализовано (v0.5)

### ✅ Core: Packet Module
- XML парсер с валидацией
- Генератор для всех типов сообщений
- Автоматическое разбиение на части
- QueryContext для stateless паттерна

### ✅ Core: Schema Module
- Валидация всех типов данных TDTP
- Конвертер строковых значений
- Проверка соответствия данных схеме
- Builder для создания схем

### ✅ Core: TDTQL Translator
- SQL парсер (WHERE, ORDER BY, LIMIT, OFFSET)
- Поддержка всех операторов
- Логические операторы с приоритетами
- Генерация древовидных фильтров

### ✅ Core: TDTQL Executor
- Фильтрация данных по TDTQL запросам
- Все операторы (=, !=, <, >, IN, BETWEEN, LIKE, IS NULL)
- Логические группы (AND/OR) с вложенностью
- Сортировка (одиночная и множественная)
- Пагинация (LIMIT/OFFSET)
- Статистика выполнения
- QueryContext для Response

### ✅ Adapters: Universal Adapter Interface (v1.0)
- **Двухуровневая архитектура** (Level 1: Interface, Level 2: Implementations)
- **Фабрика адаптеров** с автоматической регистрацией
- **Унифицированный API** для всех БД
- **Context-aware** операции (context.Context)
- **Стратегии импорта**: REPLACE, IGNORE, FAIL, COPY

### ✅ Adapters: SQLite Adapter
- Подключение к SQLite БД
- Export: БД → TDTP пакеты
- Import: TDTP пакеты → БД
- Автоматический маппинг типов
- Автоматическое создание таблиц
- Транзакции для множественных операций

### ✅ Adapters: PostgreSQL Adapter
- Подключение через pgx/v5 connection pool
- Export с поддержкой schemas
- Import с COPY (высокая производительность)
- Специальные типы: UUID, JSONB, JSON, INET, ARRAY
- ON CONFLICT для стратегий импорта

## 🏗️ Архитектура

```
tdtp-framework/
├─ pkg/core/packet/      ✅ Парсинг/генерация пакетов
├─ pkg/core/schema/      ✅ Валидация типов и схем  
├─ pkg/core/tdtql/       ✅ Translator + Executor
├─ pkg/core/validator/   ⏳ Расширенная валидация
├─ pkg/core/security/    ⏳ TLS, auth, audit
├─ pkg/adapters/sqlite/  ✅ SQLite adapter (NEW!)
├─ pkg/adapters/postgres ⏳ PostgreSQL adapter
├─ pkg/adapters/mssql    ⏳ MS SQL Server adapter
├─ pkg/brokers/          ⏳ Интеграция с брокерами
├─ cmd/tdtpcli/          ⏳ CLI утилита
├─ examples/basic/       ✅ Packet примеры
├─ examples/schema/      ✅ Schema примеры
├─ examples/tdtql/       ✅ Translator примеры
├─ examples/executor/    ✅ Executor примеры
└─ examples/sqlite/      ✅ SQLite adapter примеры (NEW!)
```

## 🚀 Быстрый старт

### Установка

```bash
git clone https://github.com/queuebridge/tdtp
cd tdtp-framework
go mod tidy
```

### Использование

```go
import "github.com/queuebridge/tdtp/pkg/core/packet"

// Создание схемы
schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "ID", Type: "INTEGER", Key: true},
        {Name: "Name", Type: "TEXT", Length: 200},
        {Name: "Balance", Type: "DECIMAL"},
    },
}

// Подготовка данных
rows := [][]string{
    {"1", "Company A", "150000.50"},
    {"2", "Company B", "250000.00"},
}

// Генерация пакета
generator := packet.NewGenerator()
packets, err := generator.GenerateReference("Companies", schema, rows)

// Сохранение
generator.WriteToFile(packets[0], "reference.xml")

// Парсинг
parser := packet.NewParser()
pkt, err := parser.ParseFile("reference.xml")
```

### Использование адаптеров (v1.0)

```go
import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    _ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"   // Регистрация
    _ "github.com/queuebridge/tdtp/pkg/adapters/postgres" // Регистрация
)

func main() {
    ctx := context.Background()

    // Создаем адаптер через фабрику
    cfg := adapters.Config{
        Type: "sqlite",  // или "postgres"
        DSN:  "database.db",
    }

    adapter, err := adapters.New(ctx, cfg)
    if err != nil {
        panic(err)
    }
    defer adapter.Close(ctx)

    // Export: БД → TDTP
    packets, err := adapter.ExportTable(ctx, "users")

    // Import: TDTP → БД
    err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)

    // Транзакции
    tx, _ := adapter.BeginTx(ctx)
    // ... операции ...
    tx.Commit(ctx)
}
```

### Запуск примера

```bash
cd examples/basic
go run main.go
```

## 📚 Документация

- [Packet Module](docs/PACKET_MODULE.md) - парсинг и генерация пакетов
- [Schema Module](docs/SCHEMA_MODULE.md) - валидация типов и схем
- [TDTQL Translator](docs/TDTQL_TRANSLATOR.md) - трансляция SQL → TDTQL
- [SQLite Adapter](docs/SQLITE_ADAPTER.md) - интеграция с SQLite **(NEW!)**
- [Техническое задание](docs/SPECIFICATION.md) - полная спецификация TDTP/TDTQL

## 🧪 Тестирование

```bash
# Запуск всех тестов
go test ./...

# С покрытием
go test -cover ./...

# Verbose
go test -v ./pkg/core/packet/
```

## 📋 Roadmap

### ~~v0.1~~ ✅ Завершено
- [x] Packet module

### ~~v0.2~~ ✅ Завершено  
- [x] Schema module
- [x] Builder, Converter, Validator

### ~~v0.3~~ ✅ Завершено
- [x] TDTQL Translator (SQL → TDTQL)
- [x] Lexer, Parser, AST, Generator
- [x] Поддержка всех операторов

### ~~v0.4~~ ✅ Завершено
- [x] TDTQL Executor
- [x] Фильтрация in-memory данных
- [x] Сортировка и пагинация
- [x] QueryContext для Response

### ~~v0.5~~ ✅ Завершено
- [x] SQLite Adapter
- [x] Маппинг типов SQLite ↔ TDTP
- [x] Export: БД → TDTP
- [x] Import: TDTP → БД
- [x] Автоматическое создание таблиц

### ~~v0.6~~ ✅ Завершено
- [x] Integration тесты для SQLite
- [x] ExportTableWithQuery через TDTQL
- [x] In-memory фильтрация

### ~~v0.7~~ ✅ Завершено
- [x] TDTQL → SQL трансляция для оптимизации
- [x] SQL-level фильтрация (WHERE/ORDER BY/LIMIT)

### ~~v0.8~~ ✅ Завершено
- [x] SQLite benchmark тесты
- [x] Поддержка subtypes

### ~~v0.9~~ ✅ Завершено
- [x] PostgreSQL adapter
- [x] UUID, JSONB, JSON, INET типы
- [x] COPY для bulk import

### ~~v1.0~~ ✅ Завершено (NEW!)
- [x] **Двухуровневая архитектура адаптеров**
- [x] **Фабрика адаптеров с регистрацией**
- [x] **Context-aware API**
- [x] **Унифицированные стратегии импорта**
- [x] **Обновленные интеграционные тесты**
- [x] **Примеры использования фабрики**

### v1.1 (следующее)
- [ ] Оптимизация производительности
- [ ] Расширенная документация
- [ ] CLI утилита (tdtpcli)

### v1.5 (планируется)
- [ ] MS SQL Server adapter
- [ ] Schema migration (ALTER TABLE)
- [ ] Incremental sync

### v2.0 (планируется)
- [ ] RabbitMQ broker integration
- [ ] Kafka broker integration
- [ ] Python bindings
- [ ] Docker образ
- [ ] Production документация

## 🤝 Вклад в проект

Проект находится в активной разработке. Приветствуются:
- Баг-репорты
- Предложения по улучшению
- Pull requests

## 📄 Лицензия

MIT

## 📞 Контакты

- GitHub: https://github.com/queuebridge/tdtp
- Email: support@queuebridge.io

---

**Статус:** v1.0 - Universal Adapter Architecture Complete!
**Последнее обновление:** 15.11.2025
