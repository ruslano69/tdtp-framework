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

### ✅ Adapters: SQLite Adapter (NEW!)
- Подключение к SQLite БД
- Export: БД → TDTP пакеты
- Import: TDTP пакеты → БД
- Автоматический маппинг типов
- 3 стратегии импорта (REPLACE/IGNORE/FAIL)
- Автоматическое создание таблиц
- Транзакции для множественных операций

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

### ~~v0.5~~ ✅ Завершено (NEW!)
- [x] SQLite Adapter
- [x] Маппинг типов SQLite ↔ TDTP
- [x] Export: БД → TDTP
- [x] Import: TDTP → БД
- [x] Автоматическое создание таблиц

### v0.6 (следующее)
- [ ] Integration тесты для SQLite
- [ ] ExportTableWithQuery через TDTQL
- [ ] Инкрементальный sync (по timestamp)
- [ ] CLI утилита (tdtpcli)

### v0.7 (планируется)
- [ ] PostgreSQL adapter
- [ ] MS SQL Server adapter
- [ ] Schema migration (ALTER TABLE)

### v1.0 (планируется)
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

**Статус:** Beta (v0.5) - Core + SQLite Adapter Complete!  
**Последнее обновление:** 14.11.2025
