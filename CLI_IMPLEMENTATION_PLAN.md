# TDTP CLI Implementation Plan v1.2

**Цель:** Реализация полнофункциональной CLI утилиты `tdtpcli` с интеграцией всех v1.2 фич.

**Статус:** 📋 План готов к реализации
**Дата:** 17.11.2025

---

## 🔍 Текущее состояние

### ✅ Что есть:
- **Документация:** `docs/USER_GUIDE.md` - полное описание интерфейса CLI
- **Все пакеты v1.2:**
  - `pkg/adapters/` - SQLite, PostgreSQL, MSSQL, MySQL
  - `pkg/brokers/` - RabbitMQ, MSMQ, Kafka
  - `pkg/xlsx/` 🍒 - XLSX Converter
  - `pkg/audit/` - AuditLogger
  - `pkg/resilience/` - CircuitBreaker
  - `pkg/retry/` - Retry mechanism
  - `pkg/sync/` - IncrementalSync
  - `pkg/processors/` - FieldMasker, FieldValidator, FieldNormalizer
  - `pkg/core/` - packet, schema, tdtql

### ❌ Что отсутствует:
- **Реализация CLI:** Директория `cmd/tdtpcli/` не существует
- **Код утилиты:** Нет main.go, config.go, commands.go

---

## 🎯 Цели реализации

### Phase 1: Базовая функциональность (MVP)
✅ Основные команды экспорта/импорта
✅ Поддержка всех адаптеров БД
✅ YAML конфигурация

### Phase 2: Advanced Features
✅ TDTQL фильтры (--where, --order-by, --limit, --offset)
✅ Message Brokers интеграция
✅ XLSX конвертер 🍒

### Phase 3: Production Features
✅ Circuit Breaker интеграция
✅ Audit Logger
✅ Retry mechanism
✅ Data processors (masking, validation, normalization)
✅ Incremental Sync

---

## 📁 Структура проекта

```
cmd/tdtpcli/
├── main.go              # Entry point, CLI parser
├── config.go            # YAML config parser
├── commands/
│   ├── list.go          # --list command
│   ├── export.go        # --export command
│   ├── import.go        # --import command
│   ├── broker.go        # --export-broker, --import-broker
│   ├── xlsx.go          # --to-xlsx, --from-xlsx 🍒
│   └── sync.go          # --sync-incremental
├── flags.go             # Command-line flags
├── adapters.go          # Adapter factory wrapper
├── processors.go        # Data processor integration
└── utils.go             # Helper functions
```

---

## 🚀 Implementation Roadmap

### **Milestone 1: Core CLI (Week 1)**

#### 1.1 Project Structure
- [ ] Создать `cmd/tdtpcli/` директорию
- [ ] Создать `main.go` с базовым CLI parser
- [ ] Создать `config.go` для YAML конфигов
- [ ] Создать `flags.go` для command-line flags

**Files to create:**
```
cmd/tdtpcli/main.go
cmd/tdtpcli/config.go
cmd/tdtpcli/flags.go
```

#### 1.2 Config Management
- [ ] YAML parser (database, broker settings)
- [ ] Config validation
- [ ] Config generators (--create-config-sqlite, --create-config-pg, --create-config-mssql)
- [ ] Environment variables override

**Config structure:**
```yaml
database:
  type: postgres | sqlite | mssql | mysql
  dsn: "..." # or host/port/user/password

audit:
  enabled: true
  level: standard | minimal | full
  file: ./audit.log

circuit_breaker:
  enabled: true
  max_failures: 5
  reset_timeout: 30s

retry:
  enabled: true
  strategy: exponential
  max_attempts: 3
```

#### 1.3 Basic Commands
- [ ] `--help` - help message
- [ ] `--version` - version info
- [ ] `--list` - list tables
- [ ] `--export <table>` - export to file
- [ ] `--import <file>` - import from file

**Implementation:**
```go
// cmd/tdtpcli/commands/list.go
func ListTables(ctx context.Context, adapter adapters.Adapter) error

// cmd/tdtpcli/commands/export.go
func ExportTable(ctx context.Context, adapter adapters.Adapter,
                 table string, output string, query *tdtql.Query) error

// cmd/tdtpcli/commands/import.go
func ImportFile(ctx context.Context, adapter adapters.Adapter,
                file string, strategy adapters.ImportStrategy) error
```

---

### **Milestone 2: TDTQL Integration (Week 1)**

#### 2.1 TDTQL Flags
- [ ] `--where "condition"` - filter conditions
- [ ] `--order-by "field DESC"` - sorting
- [ ] `--limit N` - limit rows
- [ ] `--offset N` - skip rows

**Example usage:**
```bash
tdtpcli --export users \
  --where "is_active = 1 AND age > 18" \
  --order-by "created_at DESC" \
  --limit 1000 \
  --output users.xml
```

#### 2.2 TDTQL Parser Integration
- [ ] Parse --where to TDTQL filters
- [ ] Parse --order-by to TDTQL sorting
- [ ] Pass Query to ExportTableWithQuery()
- [ ] Display QueryContext statistics

---

### **Milestone 3: XLSX Converter 🍒 (Week 2)**

#### 3.1 XLSX Commands
- [ ] `--to-xlsx <input.xml> <output.xlsx>` - TDTP XML → Excel
- [ ] `--from-xlsx <input.xlsx> <output.xml>` - Excel → TDTP XML
- [ ] `--export-xlsx <table> <output.xlsx>` - Database → Excel (direct)
- [ ] `--import-xlsx <input.xlsx> <table>` - Excel → Database (direct)
- [ ] `--sheet <name>` - specify Excel sheet name

**Implementation:**
```go
// cmd/tdtpcli/commands/xlsx.go
func ToXLSX(inputXML, outputXLSX, sheetName string) error
func FromXLSX(inputXLSX, outputXML, sheetName string) error
func ExportToXLSX(ctx context.Context, adapter adapters.Adapter,
                  table, outputXLSX, sheetName string) error
func ImportFromXLSX(ctx context.Context, adapter adapters.Adapter,
                    inputXLSX, sheetName string, strategy adapters.ImportStrategy) error
```

**Example usage:**
```bash
# Database → Excel (instant business value!)
tdtpcli --export-xlsx orders orders.xlsx

# Excel → Database
tdtpcli --import-xlsx data.xlsx --table customers

# TDTP XML → Excel
tdtpcli --to-xlsx users.xml users.xlsx

# Excel → TDTP XML
tdtpcli --from-xlsx data.xlsx data.xml
```

---

### **Milestone 4: Message Brokers (Week 2)**

#### 4.1 Broker Commands
- [ ] `--export-broker <table> <queue>` - export to message broker
- [ ] `--import-broker <queue>` - import from message broker
- [ ] `--broker-type rabbitmq|kafka|msmq` - broker type
- [ ] `--broker-url <url>` - broker connection string

**Implementation:**
```go
// cmd/tdtpcli/commands/broker.go
func ExportToBroker(ctx context.Context, adapter adapters.Adapter,
                    broker brokers.Broker, table, queue string) error
func ImportFromBroker(ctx context.Context, adapter adapters.Adapter,
                      broker brokers.Broker, queue string) error
```

**Example usage:**
```bash
# Export to RabbitMQ
tdtpcli --export-broker orders tdtp-orders-queue \
  --broker-type rabbitmq \
  --broker-url "amqp://guest:guest@localhost:5672/"

# Import from Kafka
tdtpcli --import-broker customers-topic \
  --broker-type kafka \
  --broker-url "localhost:9092"
```

---

### **Milestone 5: Production Features (Week 3)**

#### 5.1 Circuit Breaker Integration
- [ ] `--circuit-breaker` flag to enable
- [ ] Configure via config.yaml
- [ ] Wrap all external calls (DB, broker)
- [ ] Display circuit state in output

**Config:**
```yaml
circuit_breaker:
  enabled: true
  max_failures: 5
  reset_timeout: 30s
  max_concurrent: 100
```

**Implementation:**
```go
// Wrap adapter calls with circuit breaker
cb := resilience.NewCircuitBreaker(config.CircuitBreaker)
err := cb.Execute(ctx, func() error {
    return adapter.ExportTable(ctx, table)
})
```

#### 5.2 Audit Logger Integration
- [ ] `--audit` flag to enable
- [ ] Configure via config.yaml
- [ ] Log all operations (export, import, convert)
- [ ] Support multiple appenders (file, database, console)

**Config:**
```yaml
audit:
  enabled: true
  level: standard  # minimal | standard | full
  appenders:
    - type: file
      path: ./audit.log
      rotation:
        max_size_mb: 100
        max_backups: 5
    - type: console
```

**Implementation:**
```go
logger := audit.NewAuditLogger()
logger.AddAppender(audit.NewFileAppender(config.Audit.File))
logger.SetLevel(audit.LevelStandard)

// Log operations
logger.Info("Export started", map[string]interface{}{
    "table": table,
    "user": os.Getenv("USER"),
})
```

#### 5.3 Retry Mechanism
- [ ] `--retry` flag to enable
- [ ] Configure via config.yaml
- [ ] Retry on transient errors
- [ ] Exponential backoff with jitter

**Config:**
```yaml
retry:
  enabled: true
  strategy: exponential  # constant | linear | exponential
  max_attempts: 3
  initial_delay: 1s
  max_delay: 30s
  jitter: true
```

**Implementation:**
```go
retrier := retry.NewRetry(config.Retry)
err := retrier.Do(ctx, func() error {
    return adapter.ImportPacket(ctx, packet, strategy)
})
```

---

### **Milestone 6: Data Processors (Week 3)**

#### 6.1 Processor Flags
- [ ] `--mask <field>` - mask PII fields
- [ ] `--validate <field:rule>` - validate fields
- [ ] `--normalize <field:type>` - normalize fields
- [ ] `--processors <file.yaml>` - load processor config

**Processor config:**
```yaml
processors:
  - type: mask
    fields:
      - email
      - phone
      - credit_card
    strategy: partial  # full | partial | hash

  - type: validate
    rules:
      - field: email
        pattern: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
      - field: age
        min: 0
        max: 150

  - type: normalize
    fields:
      - field: phone
        type: phone
      - field: email
        type: email
```

**Implementation:**
```go
// cmd/tdtpcli/processors.go
func ApplyProcessors(packet *packet.DataPacket, config ProcessorConfig) error {
    chain := processors.NewChain()

    for _, proc := range config.Processors {
        switch proc.Type {
        case "mask":
            chain.Add(processors.NewFieldMasker(proc.Fields, proc.Strategy))
        case "validate":
            chain.Add(processors.NewFieldValidator(proc.Rules))
        case "normalize":
            chain.Add(processors.NewFieldNormalizer(proc.Fields))
        }
    }

    return chain.Process(packet)
}
```

**Example usage:**
```bash
# Export with PII masking
tdtpcli --export customers \
  --mask email \
  --mask phone \
  --output customers_masked.xml

# Import with validation
tdtpcli --import data.xml \
  --validate "email:^.*@.*\\..*$" \
  --validate "age:0-150"
```

---

### **Milestone 7: Incremental Sync (Week 4)**

#### 7.1 Incremental Sync Command
- [ ] `--sync-incremental <table>` - incremental export
- [ ] `--tracking-field <field>` - timestamp/sequence field
- [ ] `--checkpoint-file <path>` - state persistence
- [ ] `--batch-size <N>` - batch processing size

**Config:**
```yaml
incremental_sync:
  tracking_strategy: timestamp  # timestamp | sequence | version
  tracking_field: updated_at
  checkpoint_file: ./.sync_state.json
  batch_size: 1000
```

**Implementation:**
```go
// cmd/tdtpcli/commands/sync.go
func SyncIncremental(ctx context.Context, adapter adapters.Adapter,
                     table string, config sync.IncrementalConfig) error {
    stateManager := sync.NewStateManager(config.CheckpointFile)
    state, _ := stateManager.Load(table)

    packets, newState, err := adapter.ExportTableIncremental(ctx, table, config, state)
    if err != nil {
        return err
    }

    // Process packets...

    return stateManager.Save(table, newState)
}
```

**Example usage:**
```bash
# Initial sync
tdtpcli --sync-incremental orders \
  --tracking-field updated_at \
  --checkpoint-file .sync_orders.json \
  --output orders_incremental.xml

# Resume from checkpoint (only new/modified rows)
tdtpcli --sync-incremental orders \
  --checkpoint-file .sync_orders.json \
  --output orders_delta.xml
```

---

### **Milestone 8: Advanced Features (Week 4)**

#### 8.1 Import Strategies
- [ ] `--strategy replace|ignore|fail|copy` - import strategy
- [ ] Default: replace

**Example:**
```bash
# Replace existing rows
tdtpcli --import data.xml --strategy replace

# Ignore conflicts
tdtpcli --import data.xml --strategy ignore

# Fail on conflicts
tdtpcli --import data.xml --strategy fail

# PostgreSQL COPY (bulk insert)
tdtpcli --import data.xml --strategy copy
```

#### 8.2 Output Formats
- [ ] `--format xml|json|csv` - output format (future)
- [ ] `--pretty` - pretty-print XML
- [ ] `--compress` - gzip compression

#### 8.3 Batch Operations
- [ ] `--batch <file>` - batch commands from file
- [ ] Multiple tables in one command

**batch.yaml:**
```yaml
operations:
  - export:
      table: customers
      output: customers.xml
      where: "country = 'US'"

  - export:
      table: orders
      output: orders.xml
      where: "created_at > '2024-01-01'"

  - to-xlsx:
      input: orders.xml
      output: orders.xlsx
```

---

## 📋 Command Reference

### Complete Command List

```bash
# General
tdtpcli --help
tdtpcli --version
tdtpcli --config config.yaml

# Config generation
tdtpcli --create-config-sqlite
tdtpcli --create-config-pg
tdtpcli --create-config-mssql
tdtpcli --create-config-mysql

# Database operations
tdtpcli --list
tdtpcli --export <table> [--output file.xml]
tdtpcli --import <file.xml> [--strategy replace|ignore|fail|copy]

# TDTQL filters
tdtpcli --export <table> --where "condition" --order-by "field" --limit N --offset N

# XLSX operations 🍒
tdtpcli --to-xlsx input.xml output.xlsx [--sheet SheetName]
tdtpcli --from-xlsx input.xlsx output.xml [--sheet SheetName]
tdtpcli --export-xlsx <table> output.xlsx
tdtpcli --import-xlsx input.xlsx --table <table>

# Message brokers
tdtpcli --export-broker <table> <queue> --broker-type rabbitmq --broker-url <url>
tdtpcli --import-broker <queue> --broker-type rabbitmq --broker-url <url>

# Incremental sync
tdtpcli --sync-incremental <table> --tracking-field <field> --checkpoint-file <path>

# Data processing
tdtpcli --export <table> --mask <field> --validate <field:rule> --normalize <field:type>
tdtpcli --export <table> --processors config.yaml

# Production features
tdtpcli --export <table> --circuit-breaker --audit --retry

# Batch operations
tdtpcli --batch operations.yaml
```

---

## 🧪 Testing Strategy

### Unit Tests
```
cmd/tdtpcli/
├── config_test.go       # Config parsing tests
├── commands/
│   ├── export_test.go   # Export command tests
│   ├── import_test.go   # Import command tests
│   ├── xlsx_test.go     # XLSX conversion tests
│   └── broker_test.go   # Broker integration tests
```

### Integration Tests
```
tests/cli/
├── basic_test.go        # Basic export/import
├── tdtql_test.go        # TDTQL filters
├── xlsx_test.go         # XLSX conversion
├── broker_test.go       # Message brokers
├── processors_test.go   # Data processors
└── incremental_test.go  # Incremental sync
```

### E2E Tests
- Docker Compose with PostgreSQL, MySQL, MSSQL, RabbitMQ, Kafka
- Full workflow tests
- Performance benchmarks

---

## 📊 Success Criteria

### Phase 1 (MVP)
- ✅ CLI builds successfully
- ✅ All database adapters work
- ✅ Export/import with TDTQL filters
- ✅ YAML config management

### Phase 2 (Advanced)
- ✅ XLSX converter integrated 🍒
- ✅ Message brokers work (RabbitMQ, Kafka)
- ✅ Test coverage > 70%

### Phase 3 (Production)
- ✅ Circuit Breaker protects external calls
- ✅ Audit Logger tracks all operations
- ✅ Retry mechanism handles transient errors
- ✅ Data processors (mask, validate, normalize)
- ✅ Incremental sync with checkpoint tracking
- ✅ Full documentation with examples
- ✅ Performance: 10K rows/sec export
- ✅ Test coverage > 80%

---

## 📦 Dependencies

```go
// go.mod additions for CLI
require (
    github.com/spf13/cobra v1.8.0         // CLI framework
    github.com/spf13/viper v1.18.0        // Config management
    gopkg.in/yaml.v3 v3.0.1               // YAML parsing
    github.com/fatih/color v1.16.0        // Colored output
    github.com/schollz/progressbar/v3 v3.14.1  // Progress bars
)
```

---

## 🚢 Delivery Timeline

**Total: 4 weeks**

- **Week 1:** Milestones 1-2 (Core CLI + TDTQL)
- **Week 2:** Milestones 3-4 (XLSX + Brokers)
- **Week 3:** Milestones 5-6 (Production + Processors)
- **Week 4:** Milestones 7-8 (Incremental Sync + Testing)

---

## 📝 Documentation Updates

После реализации обновить:
- [x] `docs/USER_GUIDE.md` - полное руководство (уже есть)
- [ ] `INSTALLATION_GUIDE.md` - добавить CLI section
- [ ] `README.md` - добавить CLI examples
- [ ] `examples/cli/` - примеры использования CLI
- [ ] `cmd/tdtpcli/README.md` - developer guide

---

## 🎯 Priority Features

### Must Have (Phase 1-2)
1. ✅ Basic export/import
2. ✅ All database adapters
3. ✅ TDTQL filters
4. ✅ XLSX converter 🍒

### Should Have (Phase 3)
5. ✅ Circuit Breaker
6. ✅ Audit Logger
7. ✅ Retry mechanism
8. ✅ Message brokers

### Nice to Have (Phase 4)
9. ✅ Data processors
10. ✅ Incremental sync
11. ⏳ Batch operations
12. ⏳ Output formats (JSON, CSV)

---

## 📞 Next Steps

1. **Review this plan** with team
2. **Create GitHub issues** for each milestone
3. **Start with Milestone 1** (Core CLI)
4. **Iterative development** with weekly demos

---

**Plan Status:** ✅ Ready for Review
**Author:** Claude
**Date:** 17.11.2025
**Version:** 1.0
