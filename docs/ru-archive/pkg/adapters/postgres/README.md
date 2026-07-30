# PostgreSQL Adapter - TDTP Framework

PostgreSQL адаптер для двунаправленной интеграции с PostgreSQL 12+.

## Статус

🚧 **В разработке** (v0.9)

**Готово:**
- ✅ `types.go` - маппинг типов PostgreSQL ↔ TDTP
- ✅ `adapter.go` - подключение через pgx connection pool
- 🚧 `export.go` - экспорт таблиц (в процессе)
- 🚧 `import.go` - импорт данных (в процессе)
- 🚧 `integration_test.go` - интеграционные тесты (в процессе)

---

## Возможности

### Поддержка типов данных

**Стандартные типы:**
```
PostgreSQL          TDTP            Обратно
─────────────────────────────────────────────
INTEGER             INTEGER         INTEGER
BIGINT              INTEGER         BIGINT
NUMERIC(18,2)       DECIMAL         NUMERIC(18,2)
VARCHAR(100)        TEXT            VARCHAR(100)
TEXT                TEXT            TEXT
BOOLEAN             BOOLEAN         BOOLEAN
DATE                DATE            DATE
TIMESTAMP           TIMESTAMP       TIMESTAMP
BYTEA               BLOB            BYTEA
```

**Специальные типы PostgreSQL (через subtype):**
```
PostgreSQL          TDTP                        Обратно
───────────────────────────────────────────────────────────
UUID                TEXT (subtype="uuid")       UUID
JSONB               TEXT (subtype="jsonb")      JSONB
JSON                TEXT (subtype="json")       JSON
INET                TEXT (subtype="inet")       INET
CIDR                TEXT (subtype="cidr")       CIDR
MACADDR             TEXT (subtype="macaddr")    MACADDR
INTEGER[]           TEXT (subtype="array")      INTEGER[]
TIMESTAMPTZ         TIMESTAMP (subtype="tz")    TIMESTAMPTZ
SERIAL              INTEGER (subtype="serial")  SERIAL
```

### Миграционные сценарии

**SQLite → PostgreSQL:**
```go
source := sqlite.NewAdapter("app.db")
target := postgres.NewAdapter("postgresql://user:pass@localhost/db")

packets, _ := source.ExportTable("Users")
target.ImportPackets(packets, postgres.StrategyReplace)
// Автоматическая конвертация типов!
```

**PostgreSQL → SQLite (downgrade):**
```go
source := postgres.NewAdapter("postgresql://...")
target := sqlite.NewAdapter("backup.db")

// UUID, JSONB → TEXT при экспорте
packets, _ := source.ExportTable("Users")
target.ImportPackets(packets, sqlite.StrategyReplace)
```

---

## Использование

### Подключение

```go
import "github.com/queuebridge/tdtp/pkg/adapters/postgres"

// Стандартное подключение (schema: public)
adapter, err := postgres.NewAdapter(
    "postgresql://tdtp_user:password@localhost:5432/tdtp_test"
)
defer adapter.Close()

// С указанием схемы
adapter, err := postgres.NewAdapterWithSchema(
    "postgresql://...",
    "myschema",
)
```

### Export (TODO)

```go
// Полный экспорт
packets, err := adapter.ExportTable("Users")

// С фильтрацией
translator := tdtql.NewTranslator()
query, _ := translator.Translate("SELECT * FROM Users WHERE active = true")
packets, err := adapter.ExportTableWithQuery("Users", query, "App", "Server")
```

### Import (TODO)

```go
// Импорт с заменой
err := adapter.ImportPacket(packet, postgres.StrategyReplace)

// Импорт с игнорированием дубликатов
err := adapter.ImportPacket(packet, postgres.StrategyIgnore)

// Атомарный импорт нескольких пакетов
err := adapter.ImportPackets(packets, postgres.StrategyReplace)
```

---

## Установка зависимостей

```bash
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
```

---

## Тестирование

### Docker PostgreSQL (уже запущен!)

```bash
# Проверка контейнера
docker ps | grep postgres

# Подключение для проверки
docker exec -it tdtp-postgres psql -U tdtp_user -d tdtp_test
```

**Connection string для тестов:**
```
postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test
```

### Запуск тестов (когда будут готовы)

```bash
cd pkg/adapters/postgres
go test -v
go test -bench=. -benchmem
```

---

## Технические детали

### Connection Pooling

- **MaxConns:** 10 (максимум подключений)
- **MinConns:** 2 (минимум активных)
- **Driver:** pgx/v5 (современный, быстрый)

### Работа со схемами

```go
// Список всех схем
schemas, _ := adapter.GetSchemas()

// Переключение схемы
adapter.SetSchema("custom_schema")

// Проверка таблицы в текущей схеме
exists, _ := adapter.TableExists("Users")
```

### Обработка идентификаторов

PostgreSQL case-sensitive для quoted identifiers:
```go
// Автоматическое квотирование
QuoteIdentifier("User")   → "User"
QuoteIdentifier("user")   → user
QuoteIdentifier("order")  → "order" (reserved word)
```

---

## Roadmap

### v0.9 (текущая)
- [ ] `export.go` - чтение схемы через information_schema
- [ ] `export.go` - экспорт данных с поддержкой всех типов
- [ ] `import.go` - COPY-based bulk insert
- [ ] `import.go` - работа с sequences
- [ ] `integration_test.go` - полное тестирование
- [ ] Поддержка Arrays (PostgreSQL-specific)
- [ ] Benchmark PostgreSQL vs SQLite

### v1.0 (будущее)
- [ ] Поддержка partitioned tables
- [ ] Поддержка foreign keys и constraints
- [ ] Миграция схем (ALTER TABLE)
- [ ] Инкрементальная синхронизация

---

## Примеры использования

### Миграция с типами PostgreSQL

```go
// Таблица в PostgreSQL с UUID и JSONB
CREATE TABLE users (
    id UUID PRIMARY KEY,
    name VARCHAR(100),
    metadata JSONB,
    ip INET
);

// Экспорт через TDTP
packets, _ := pgAdapter.ExportTable("users")

// В TDTP пакете:
<Schema>
  <Field name="id" type="TEXT" subtype="uuid" key="true"/>
  <Field name="name" type="TEXT" length="100"/>
  <Field name="metadata" type="TEXT" length="-1" subtype="jsonb"/>
  <Field name="ip" type="TEXT" subtype="inet"/>
</Schema>

// Импорт обратно в PostgreSQL
pgAdapter2.ImportPacket(packet, postgres.StrategyReplace)
// → Восстанавливает UUID, JSONB, INET
```

### Кросс-платформенная миграция

```go
// PostgreSQL (source)
pgSrc, _ := postgres.NewAdapter("postgresql://src/db")
packets, _ := pgSrc.ExportTable("products")

// SQLite (target) - автоматическое downgrade типов
sqliteTgt, _ := sqlite.NewAdapter("products.db")
sqliteTgt.ImportPackets(packets, sqlite.StrategyReplace)
// UUID → TEXT, JSONB → TEXT, etc.
```

---

## Известные ограничения

1. **Arrays:** Хранятся как TEXT с subtype="array", точная структура типа не сохраняется
2. **Composite types:** Не поддерживаются (TODO v1.1)
3. **Domains:** Обрабатываются как базовый тип
4. **Enums:** Хранятся как TEXT (TODO v1.1)

---

**PostgreSQL Adapter** - мощный инструмент для enterprise миграций! 🐘

*Часть TDTP Framework v0.9*
