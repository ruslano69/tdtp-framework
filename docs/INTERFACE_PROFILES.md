# Интерфейсные профили TDTP Framework

**Назначение**: Детальные спецификации всех ключевых интерфейсов фреймворка для продуктивного анализа

---

## 1. Adapter Interface Profile

### Базовый контракт

```go
// Файл: pkg/adapters/adapter.go
type Adapter interface {
    // Lifecycle methods
    Connect(ctx context.Context, cfg Config) error
    Close(ctx context.Context) error
    Ping(ctx context.Context) error

    // Export methods
    ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error)
    ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query,
                         sender, recipient string) ([]*packet.DataPacket, error)
    ExportTableIncremental(ctx context.Context, tableName string,
                          incrementalConfig IncrementalConfig) ([]*packet.DataPacket, string, error)

    // Import methods
    ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy ImportStrategy) error
    ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy ImportStrategy) error

    // Schema methods
    GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error)
    GetTableNames(ctx context.Context) ([]string, error)
    TableExists(ctx context.Context, tableName string) (bool, error)

    // Transaction methods
    BeginTx(ctx context.Context) (Tx, error)

    // Metadata methods
    GetDatabaseVersion(ctx context.Context) (string, error)
    GetDatabaseType() string
}
```

### Матрица реализаций

| Метод | SQLite | PostgreSQL | MS SQL Server | MySQL | Сложность |
|-------|--------|------------|---------------|-------|-----------|
| **Connect** | ✅ modernc.org/sqlite | ✅ pgx/v5 pool | ✅ go-mssqldb | ✅ go-sql-driver/mysql | Средняя |
| **ExportTable** | ✅ Простая | ✅ Schema-aware | ✅ Read-only поля | ✅ Стандартная | Высокая |
| **ExportTableWithQuery** | ✅ TDTQL→SQL | ✅ TDTQL→SQL | ✅ OFFSET/FETCH | ✅ TDTQL→SQL | Высокая |
| **ExportTableIncremental** | ❌ Not implemented | ✅ WHERE > lastValue | ❌ Not implemented | ❌ Not implemented | Высокая |
| **ImportPacket** | ✅ OR REPLACE/IGNORE | ✅ ON CONFLICT | ✅ MERGE | ✅ ON DUPLICATE KEY | Высокая |
| **ImportPackets** | ✅ Транзакции | ✅ Транзакции | ✅ Транзакции | ✅ Транзакции | Средняя |
| **GetTableSchema** | ✅ PRAGMA | ✅ information_schema | ✅ INFORMATION_SCHEMA | ✅ INFORMATION_SCHEMA | Средняя |

### Dependency Graph

```
Adapter
  ├─> Config (DSN, Schema, Timeout, SSL)
  ├─> packet.DataPacket (TDTP протокол)
  ├─> packet.Schema (определение таблицы)
  ├─> packet.Query (TDTQL запросы)
  ├─> sync.IncrementalConfig (инкрементальная синхронизация)
  └─> ImportStrategy (REPLACE/IGNORE/FAIL/COPY)
```

### Метод ExportTable - Детальный профиль

**Цель**: Экспорт всей таблицы в TDTP пакеты

**Алгоритм** (общий для всех адаптеров):

```
1. GetTableSchema(tableName) → packet.Schema
2. readAllRows(tableName, schema) → [][]string
3. packet.Generator.GenerateReference(tableName, schema, rows) → []*packet.DataPacket
```

**Различия между адаптерами**:

| Аспект | SQLite | PostgreSQL | MS SQL Server | MySQL |
|--------|--------|------------|---------------|-------|
| **Получение схемы** | `PRAGMA table_info()` | `information_schema.columns` + PK query | `INFORMATION_SCHEMA` + COLUMNPROPERTY | `INFORMATION_SCHEMA.COLUMNS` |
| **Чтение данных** | `SELECT * FROM table` | `SELECT * FROM schema.table` | `SELECT * FROM [schema].[table]` | `SELECT * FROM table` |
| **Сканирование строк** | `sql.NullString` | `rows.Values()` (pgx) | `interface{}` scanner | `sql.NullString` |
| **Особенности** | Нет схем | Поддержка схем (public/custom) | Read-only поля (timestamp, computed, identity) | Auto-increment |

**Файлы реализации**:
- SQLite: `pkg/adapters/sqlite/export.go:67-83`
- PostgreSQL: `pkg/adapters/postgres/export.go:134-186`
- MS SQL Server: `pkg/adapters/mssql/export.go:234-236`
- MySQL: `pkg/adapters/mysql/export.go` (аналогично)

### Метод ExportTableWithQuery - Детальный профиль

**Цель**: Экспорт с фильтрацией через TDTQL

**Алгоритм оптимизации** (все адаптеры):

```
1. GetTableSchema(tableName) → schema
2. Попытка TDTQL → SQL оптимизации:
   a. tdtql.NewSQLGenerator()
   b. if CanTranslateToSQL(query):
      - GenerateSQL(tableName, query) → SQL string
      - Адаптация SQL под СУБД (PostgreSQL - schema, MSSQL - OFFSET/FETCH)
      - readRowsWithSQL(sql, schema) → [][]string
      - createQueryContextForSQL() → QueryContext
      - GenerateResponse(...) → []*packet.DataPacket
   c. else:
      - Fallback на in-memory фильтрацию
3. Fallback путь (если SQL не удался):
   - ExportTable() → все данные
   - tdtql.NewExecutor().Execute(query, rows, schema) → фильтрованные данные
   - GenerateResponse(...) → []*packet.DataPacket
```

**SQL адаптация по СУБД**:

| СУБД | Адаптация | Пример |
|------|-----------|--------|
| SQLite | Стандартный SQL | `SELECT * FROM users WHERE age > 18 LIMIT 10 OFFSET 5` |
| PostgreSQL | Schema prefix | `SELECT * FROM public.users WHERE age > 18 LIMIT 10 OFFSET 5` |
| MS SQL Server | OFFSET/FETCH | `SELECT * FROM [dbo].[users] WHERE [age] > 18 OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY` |
| MySQL | Стандартный SQL | `SELECT * FROM users WHERE age > 18 LIMIT 10 OFFSET 5` |

**Производительность**:

| Путь | Скорость | Память | Когда использовать |
|------|---------|--------|-------------------|
| **SQL оптимизация** | ⚡ Быстро | 💾 Мало | Простые фильтры (WHERE, ORDER BY, LIMIT) |
| **In-memory фильтрация** | 🐌 Медленно | 💾💾💾 Много | Сложные запросы, агрегация |

### Метод ImportPacket - Детальный профиль

**Цель**: Импорт одного TDTP пакета в БД

**Алгоритм** (общий):

```
1. TableExists(tableName) → bool
2. if !exists:
   - createTableFromSchema(tableName, packet.Schema)
3. Выбор стратегии импорта:
   - REPLACE → UPSERT (INSERT OR REPLACE / ON CONFLICT UPDATE / MERGE)
   - IGNORE  → Пропуск дубликатов (INSERT OR IGNORE / ON CONFLICT DO NOTHING)
   - FAIL    → Обычный INSERT (ошибка при дубликатах)
   - COPY    → Bulk insert (PostgreSQL COPY, MSSQL BULK INSERT)
4. insertRows(tableName, schema, rows, strategy)
```

**Стратегии по СУБД**:

| Стратегия | SQLite | PostgreSQL | MS SQL Server | MySQL |
|-----------|--------|------------|---------------|-------|
| **REPLACE** | `INSERT OR REPLACE` | `INSERT ... ON CONFLICT DO UPDATE` | `MERGE` | `INSERT ... ON DUPLICATE KEY UPDATE` |
| **IGNORE** | `INSERT OR IGNORE` | `INSERT ... ON CONFLICT DO NOTHING` | `MERGE` (skip) | `INSERT IGNORE` |
| **FAIL** | `INSERT` | `INSERT` | `INSERT` | `INSERT` |
| **COPY** | ❌ Fallback на FAIL | ✅ `COPY FROM` | ✅ `BULK INSERT` | ✅ `LOAD DATA INFILE` |

**Особенности**:

- **PostgreSQL**: Требует указания conflict_target (PK колонки) для ON CONFLICT
- **MS SQL Server**: MERGE требует IDENTITY_INSERT ON для identity колонок
- **MySQL**: ON DUPLICATE KEY UPDATE работает только с UNIQUE индексами

---

## 2. Processor Interface Profile

### Базовый контракт

```go
// Файл: pkg/processors/processor.go
type Processor interface {
    Name() string
    Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error)
}

type PreProcessor interface {
    Processor  // Выполняется ПЕРЕД генерацией TDTP пакета (при экспорте)
}

type PostProcessor interface {
    Processor  // Выполняется ПОСЛЕ парсинга TDTP пакета (при импорте)
}
```

### Матрица реализаций

| Процессор | Type | Входные данные | Выходные данные | Use Case |
|-----------|------|---------------|-----------------|----------|
| **FieldMasker** | PreProcessor | PII данные | Маскированные данные | Защита персональных данных при экспорте |
| **FieldNormalizer** | Pre/PostProcessor | Разнородные форматы | Нормализованные форматы | Приведение к стандарту |
| **FieldValidator** | Pre/PostProcessor | Любые данные | Валидные данные (или ошибка) | Проверка корректности |
| **Compression** | Pre/PostProcessor | TDTP пакеты | Сжатые TDTP пакеты | Уменьшение размера |
| **ProcessorChain** | Meta | Данные | Последовательно обработанные данные | Композиция процессоров |

### FieldMasker - Детальный профиль

**Цель**: Маскирование чувствительных данных (PII)

**Паттерны маскирования**:

```go
type MaskPattern string

const (
    MaskPartial      MaskPattern = "partial"       // j***@example.com
    MaskMiddle       MaskPattern = "middle"        // +1 (555) XXX-X567
    MaskStars        MaskPattern = "stars"         // **** ****
    MaskFirst2Last2  MaskPattern = "first2_last2"  // 12** **78
)
```

**Примеры маскирования**:

| Тип данных | Исходное | Паттерн | Результат |
|------------|----------|---------|-----------|
| Email | `john.doe@example.com` | `partial` | `j***@example.com` |
| Phone | `+1 (555) 123-4567` | `middle` | `+1 (555) XXX-X567` |
| Card | `1234 5678 9012 3456` | `stars` | `**** **** **** 3456` |
| Passport | `1234 567890` | `first2_last2` | `12** **90` |

**Алгоритм**:

```
1. Поиск индексов полей для маскирования по именам
2. Для каждой строки:
   - Создание копии строки
   - Применение паттерна маскирования к указанным полям
3. Возврат обработанных данных
```

**Конфигурация**:

```go
masker := NewFieldMasker(map[string]MaskPattern{
    "email":    MaskPartial,
    "phone":    MaskMiddle,
    "card_number": MaskStars,
    "passport": MaskFirst2Last2,
})
```

**Регулярные выражения** (предкомпилированные):
- Email: `^([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})$`
- Phone: `^\+?\d{1,3}[\s.-]?\(?\d{2,4}\)?[\s.-]?\d{2,4}[\s.-]?\d{2,4}[\s.-]?\d{0,4}$`
- Passport: `^(\d{4})\s*(\d{6})$`

**Файл**: `pkg/processors/field_masker.go`

### FieldNormalizer - Детальный профиль

**Цель**: Приведение данных к единому формату

**Правила нормализации**:

```go
type NormalizeRule string

const (
    NormalizePhone      NormalizeRule = "phone"       // → 79991234567
    NormalizeEmail      NormalizeRule = "email"       // → lowercase
    NormalizeWhitespace NormalizeRule = "whitespace"  // → убрать лишние пробелы
    NormalizeUpperCase  NormalizeRule = "uppercase"   // → UPPER
    NormalizeLowerCase  NormalizeRule = "lowercase"   // → lower
    NormalizeDate       NormalizeRule = "date"        // → YYYY-MM-DD
)
```

**Примеры нормализации**:

| Правило | Исходное | Результат |
|---------|----------|-----------|
| `phone` | `+7 (999) 123-45-67` | `79991234567` |
| `email` | `John.Doe@Example.COM` | `john.doe@example.com` |
| `whitespace` | `  Hello   World  ` | `Hello World` |
| `date` | `25.12.2025` | `2025-12-25` |
| `uppercase` | `hello` | `HELLO` |

**Алгоритм нормализации телефона**:

```
1. Удалить все символы кроме цифр и +: "+7 (999) 123-45-67" → "+79991234567"
2. Если начинается с 8, заменить на 7: "89991234567" → "79991234567"
3. Если начинается с +7, убрать +: "+79991234567" → "79991234567"
4. Если 11 цифр и первая 7 - оставить как есть
```

**Алгоритм нормализации даты**:

```
1. Regex match: DD.MM.YYYY или DD/MM/YYYY или DD-MM-YYYY
2. Парсинг компонентов: день, месяц, год
3. Форматирование: YYYY-MM-DD
```

**Файл**: `pkg/processors/field_normalizer.go`

### FieldValidator - Детальный профиль

**Цель**: Валидация корректности данных

**Типы валидации**:

```go
type ValidationRule string

const (
    ValidateRegex    ValidationRule = "regex"     // Регулярное выражение
    ValidateRange    ValidationRule = "range"     // Числовой диапазон (min-max)
    ValidateEnum     ValidationRule = "enum"      // Список допустимых значений
    ValidateRequired ValidationRule = "required"  // Обязательное поле
    ValidateLength   ValidationRule = "length"    // Длина строки (min-max)
    ValidateEmail    ValidationRule = "email"     // Email формат
    ValidatePhone    ValidationRule = "phone"     // Телефон формат
    ValidateURL      ValidationRule = "url"       // URL формат
    ValidateDate     ValidationRule = "date"      // Дата YYYY-MM-DD
)
```

**Примеры валидации**:

| Правило | Параметр | Валидное | Невалидное |
|---------|----------|----------|------------|
| `regex` | `^\d{4}$` | `1234` | `12345` |
| `range` | `18-65` | `25` | `70` |
| `enum` | `active,inactive,pending` | `active` | `deleted` |
| `required` | - | `value` | `` (пустая строка) |
| `length` | `5-10` | `hello` | `hi` |
| `email` | - | `test@example.com` | `invalid-email` |
| `phone` | - | `+79991234567` | `123` |

**Конфигурация**:

```go
validator := NewFieldValidator(map[string][]FieldValidationRule{
    "age": {
        {Type: ValidateRequired, ErrMsg: "Age is required"},
        {Type: ValidateRange, Param: "18-65", ErrMsg: "Age must be 18-65"},
    },
    "email": {
        {Type: ValidateRequired},
        {Type: ValidateEmail, ErrMsg: "Invalid email format"},
    },
    "status": {
        {Type: ValidateEnum, Param: "active,inactive,pending"},
    },
}, stopOnFirstError)
```

**Режимы обработки ошибок**:

- `stopOnFirstError = true`: Остановиться на первой ошибке
- `stopOnFirstError = false`: Собрать все ошибки и вернуть в конце

**Файл**: `pkg/processors/field_validator.go`

### ProcessorChain - Детальный профиль

**Цель**: Последовательное выполнение нескольких процессоров

**Паттерн**: Chain of Responsibility

**Алгоритм**:

```
1. data = inputData
2. for each processor in chain:
   - data, err = processor.Process(ctx, data, schema)
   - if err != nil:
     - return nil, fmt.Errorf("processor %s failed: %w", processor.Name(), err)
3. return data, nil
```

**Пример использования**:

```go
chain := NewProcessorChain()
chain.Add(NewFieldMasker(maskConfig))       // 1. Маскирование PII
chain.Add(NewFieldNormalizer(normalizeConfig))  // 2. Нормализация форматов
chain.Add(NewFieldValidator(validateConfig))    // 3. Валидация

processedData, err := chain.Process(ctx, rawData, schema)
```

**Порядок важен!**

Правильный порядок:
1. **Normalizer** → приведение к стандарту
2. **Validator** → проверка корректности
3. **Masker** → маскирование (перед экспортом)

Неправильный порядок:
1. Masker → Validator ❌ (валидатор не сможет проверить замаскированные данные)

**Файл**: `pkg/processors/chain.go`

---

## 3. MessageBroker Interface Profile

### Базовый контракт

```go
// Файл: pkg/brokers/broker.go
type MessageBroker interface {
    Connect(ctx context.Context) error
    Close() error
    Send(ctx context.Context, message []byte) error
    Receive(ctx context.Context) ([]byte, error)
    Ping(ctx context.Context) error
    GetBrokerType() string
}
```

### Матрица реализаций

| Брокер | Протокол | Транспорт | Надежность | Производительность | Особенности |
|--------|----------|-----------|------------|-------------------|-------------|
| **RabbitMQ** | AMQP 0-9-1 | TCP/TLS | 🟢 Высокая | 🟡 Средняя | Exchanges, Routing, TTL |
| **MSMQ** | MSMQ | Windows IPC | 🟢 Высокая | 🟡 Средняя | Только Windows |
| **Kafka** | Kafka protocol | TCP | 🟢 Высокая | 🟢 Очень высокая | Partitions, Consumer Groups |

### RabbitMQ - Детальный профиль

**Компоненты**:

```
Producer → Exchange → Queue → Consumer
           ↓ (routing key)
```

**Конфигурация**:

```go
type Config struct {
    Type       string  // "rabbitmq"
    Host       string  // "localhost"
    Port       int     // 5672 (AMQP), 5671 (AMQPS)
    User       string  // "guest"
    Password   string  // "guest"
    Queue      string  // "tdtp_export"
    VHost      string  // "/" (default)
    UseTLS     bool    // true для amqps://
    Exchange   string  // "" (default exchange) или "tdtp.exchange"
    RoutingKey string  // "export.tdtp" (если пустой, используется Queue)

    // Параметры очереди (должны совпадать с существующей!)
    Durable    bool    // true - очередь переживает перезапуск
    AutoDelete bool    // true - удаляется когда нет consumers
    Exclusive  bool    // true - доступна только одному соединению
}
```

**DSN формат**:

```
amqp://user:password@host:port/vhost
amqps://user:password@host:port/vhost  (TLS)
```

**Send алгоритм**:

```
1. amqp.Dial(dsn) → connection
2. connection.Channel() → channel
3. channel.QueueDeclare(queue, durable, autoDelete, exclusive, ...) → queue
4. channel.Publish(exchange, routingKey, mandatory=false, immediate=false, msg)
```

**Receive алгоритм**:

```
1. channel.Consume(queue, consumer="", autoAck=false, ...) → deliveries
2. msg := <-deliveries (блокирующий прием)
3. msg.Ack(multiple=false) (manual acknowledgment)
```

**Особенности**:
- Manual ACK для надежной доставки (сообщение не удаляется пока не подтверждено)
- Поддержка TLS (amqps://)
- Exchanges для маршрутизации (direct, fanout, topic, headers)

**Файл**: `pkg/brokers/rabbitmq.go`

### Kafka - Детальный профиль

**Компоненты**:

```
Producer → Topic (Partition 0, 1, 2, ...) → Consumer Group
                                              ├─ Consumer 1
                                              └─ Consumer 2
```

**Конфигурация**:

```go
type Config struct {
    Type          string   // "kafka"
    Brokers       []string // ["localhost:9092", "localhost:9093"]
    Topic         string   // "tdtp-export"
    ConsumerGroup string   // "tdtp-consumer-group" (default)
}
```

**Send алгоритм** (Producer):

```
1. sarama.NewSyncProducer(brokers, config) → producer
2. producer.SendMessage(&sarama.ProducerMessage{
     Topic: topic,
     Value: sarama.ByteEncoder(message),
   }) → partition, offset, error
```

**Receive алгоритм** (Consumer Group):

```
1. sarama.NewConsumerGroup(brokers, consumerGroup, config) → consumerGroup
2. consumerGroup.Consume(ctx, []string{topic}, handler)
3. handler.ConsumeClaim() → обработка сообщений
4. session.MarkMessage(msg, "") (commit offset)
```

**Особенности**:
- Partitions для параллелизма
- Consumer Groups для балансировки нагрузки
- Offset management (автоматический commit или ручной)
- Очень высокая пропускная способность (millions msg/sec)

**Файл**: `pkg/brokers/kafka.go`

### MSMQ - Детальный профиль

**Компоненты**:

```
Sender → Queue Path (.\\private$\\queue_name) → Receiver
```

**Конфигурация**:

```go
type Config struct {
    Type      string  // "msmq"
    QueuePath string  // ".\\private$\\tdtp_export" (локальная)
                      // "MACHINE\\private$\\queue_name" (удаленная)
}
```

**Особенности**:
- **Только Windows** (использует .NET interop)
- Локальные очереди: `.\private$\queue_name`
- Сетевые очереди: `MACHINE\private$\queue_name`
- Транзакционные очереди

**Файл**: `pkg/brokers/msmq.go`

---

## 4. Supporting Modules Profiles

### Diff Module (pkg/diff/)

**Цель**: Сравнение двух TDTP пакетов и выявление изменений

**Интерфейс**:

```go
type DiffResult struct {
    Added    [][]string  // Строки добавленные в новый пакет
    Removed  [][]string  // Строки удаленные из старого пакета
    Modified []ModifiedRow  // Строки измененные
    Unchanged [][]string  // Строки без изменений

    Statistics DiffStats
}

type ModifiedRow struct {
    OldRow []string
    NewRow []string
    ChangedFields []int  // Индексы измененных полей
}

type DiffStats struct {
    TotalRows      int
    AddedRows      int
    RemovedRows    int
    ModifiedRows   int
    UnchangedRows  int
}

func Compare(oldPacket, newPacket *packet.DataPacket, keyFields []string,
             ignoreFields []string) (*DiffResult, error)
```

**Алгоритм**:

```
1. validateSchemas([oldPacket.Schema, newPacket.Schema])
2. Построение индексов ключевых полей
3. Построение map[key]row для старых строк
4. Для каждой строки в новом пакете:
   - buildKey(row, keyIndices) → key
   - if key in oldMap:
     - compareRows(oldRow, newRow, ignoreFields)
     - if changed: Modified
     - else: Unchanged
   - else: Added
5. Для каждой строки в старом пакете не найденной в новом: Removed
6. Подсчет статистики
```

**Файл**: `pkg/diff/diff.go`

### Merge Module (pkg/merge/)

**Цель**: Объединение нескольких TDTP пакетов по стратегии

**Стратегии**:

```go
type MergeStrategy string

const (
    StrategyUnion        MergeStrategy = "union"         // Все уникальные строки
    StrategyIntersection MergeStrategy = "intersection"  // Только общие строки
    StrategyLeftPriority MergeStrategy = "left_priority" // Конфликты = первый пакет
    StrategyRightPriority MergeStrategy = "right_priority" // Конфликты = последний пакет
    StrategyAppend       MergeStrategy = "append"        // Без дедупликации
)
```

**Интерфейс**:

```go
type MergeResult struct {
    Schema packet.Schema
    Rows   [][]string

    Statistics MergeStats
}

type MergeStats struct {
    TotalInputRows    int
    TotalOutputRows   int
    DuplicatesRemoved int
}

func Merge(packets []*packet.DataPacket, strategy MergeStrategy,
           keyFields []string) (*MergeResult, error)
```

**Файл**: `pkg/merge/merge.go`

### ETL Module (pkg/etl/)

**Цель**: Сложные ETL конвейеры с трансформациями

**Конфигурация**:

```yaml
sources:
  - name: source1
    type: postgres
    dsn: "postgresql://..."
    tables: [users, orders]

transformations:
  - sql: |
      SELECT u.id, u.name, COUNT(o.id) as order_count
      FROM users u
      LEFT JOIN orders o ON u.id = o.user_id
      GROUP BY u.id, u.name

output:
  type: tdtp  # или rabbitmq, kafka
  destination: output.tdtp
```

**Файл**: `pkg/etl/config.go`, `pkg/etl/executor.go`

---

## 5. Cross-cutting Concerns

### Resilience (Circuit Breaker)

**Файл**: `pkg/resilience/circuit_breaker.go`

**Состояния**:

```
Closed (нормальная работа)
   ↓ (failures ≥ threshold)
Open (запрос немедленно отклоняется)
   ↓ (timeout истек)
Half-Open (пробный запрос)
   ↓ (success) / ↓ (failure)
Closed            Open
```

### Retry + DLQ

**Файл**: `pkg/retry/retry.go`, `pkg/retry/dlq.go`

**Backoff стратегии**:
- Constant: фиксированная задержка
- Linear: линейное увеличение
- Exponential: экспоненциальное увеличение + jitter

**DLQ**: Сообщения которые не удалось обработать после N попыток сохраняются в Dead Letter Queue для ручного анализа

### Security (SQL Validation)

**Файл**: `pkg/security/validator.go`

**Защита от SQL injection**:
- Safe mode: только SELECT/WITH разрешены
- Forbidden keywords: DROP, DELETE, UPDATE, ALTER, TRUNCATE
- Comment blocking: блокировка -- и /* */
- Multiple statement prevention: блокировка ;

### Audit Logging

**Файл**: `pkg/audit/logger.go`

**Режимы**:
- Async: неблокирующая запись (буферизация)
- Sync: синхронная запись (надежность)

**Appenders**:
- File: запись в файл
- Database: запись в БД

---

## Заключение

Данный документ предоставляет детальные спецификации всех ключевых интерфейсов TDTP Framework для:

1. **Быстрого понимания** контрактов и зависимостей
2. **Продуктивного анализа** при добавлении новых адаптеров/процессоров
3. **Рефакторинга** дублирующегося кода
4. **Тестирования** через четкие спецификации поведения

**Для дальнейшего изучения см**:
- `ARCHITECTURE_ANALYSIS.md` - Анализ дублирования и рекомендации по рефакторингу
- Исходные файлы в `pkg/` директории
