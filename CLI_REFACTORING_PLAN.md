# TDTP CLI Refactoring Plan v1.2

**Статус:** 📋 План рефакторинга существующей CLI утилиты
**Дата:** 17.11.2025

---

## 🔍 Анализ текущей реализации

### ✅ Что уже работает (v1.0)

**Базовая функциональность:**
- ✅ `--list` - список таблиц
- ✅ `--export <table>` - экспорт в TDTP XML
- ✅ `--import <file>` - импорт из TDTP XML
- ✅ `--output <file>` - сохранение в файл

**TDTQL фильтры:**
- ✅ `--where "condition"` - WHERE clause
- ✅ `--order-by "field DESC"` - ORDER BY
- ✅ `--limit N` - LIMIT rows
- ✅ `--offset N` - OFFSET rows

**Message Brokers:**
- ✅ `--export-broker <table>` - экспорт в очередь
- ✅ `--import-broker` - импорт из очереди
- ✅ Поддержка RabbitMQ, MSMQ

**Config Management:**
- ✅ `--config <path>` - путь к конфигу
- ✅ `--create-config-pg/sl/ms/my/mi` - генерация конфигов
- ✅ YAML config parser

**Адаптеры:**
- ✅ PostgreSQL (postgres)
- ✅ SQLite (sqlite)
- ✅ MS SQL Server (mssql)
- 🚧 MySQL (mysql) - under development
- 🚧 Miranda SQL (miranda) - under development

### ❌ Чего не хватает (v1.2 features)

**1. XLSX Converter 🍒** - отсутствует полностью
- ❌ `--to-xlsx` - TDTP XML → Excel
- ❌ `--from-xlsx` - Excel → TDTP XML
- ❌ `--export-xlsx` - Database → Excel (direct)
- ❌ `--import-xlsx` - Excel → Database (direct)

**2. Production Features** - отсутствуют полностью
- ❌ Circuit Breaker интеграция
- ❌ Audit Logger
- ❌ Retry mechanism
- ❌ Import strategies (--strategy replace|ignore|fail|copy)

**3. Data Processors** - отсутствуют полностью
- ❌ `--mask <field>` - PII masking
- ❌ `--validate <field:rule>` - validation
- ❌ `--normalize <field:type>` - normalization
- ❌ `--processors <config.yaml>` - processor config

**4. Incremental Sync** - отсутствует полностью
- ❌ `--sync-incremental <table>`
- ❌ `--tracking-field <field>`
- ❌ `--checkpoint-file <path>`

---

## 🎯 План рефакторинга

### Phase 1: Код структура (Week 1)

#### 1.1 Рефакторинг main.go
**Текущая проблема:** Все в одном файле (~700 строк)

**Решение:** Разделить на модули

```
cmd/tdtpcli/
├── main.go              # Entry point (только flag parsing + routing)
├── config.go            # ✅ Уже есть
├── commands/
│   ├── list.go          # handleListTables()
│   ├── export.go        # handleExportTable()
│   ├── import.go        # handleImportFile()
│   ├── broker.go        # handleExportBroker(), handleImportBroker()
│   ├── xlsx.go          # NEW: XLSX commands
│   └── sync.go          # NEW: Incremental sync
├── flags.go             # Flag definitions
├── query.go             # buildTDTQLQuery(), parseSimpleWhere()
├── help.go              # printHelp()
└── utils.go             # Helper functions
```

**Задачи:**
- [ ] Создать структуру директорий
- [ ] Переместить функции в соответствующие модули
- [ ] Упростить main.go до ~100 строк
- [ ] Добавить package-level документацию

#### 1.2 Улучшение обработки ошибок
**Текущая проблема:** `os.Exit(1)` везде

**Решение:** Вернуть ошибки из функций

```go
// Before
func handleExportTable(...) {
    if err != nil {
        fmt.Fprintf(os.Stderr, "❌ Error\n")
        os.Exit(1)
    }
}

// After
func handleExportTable(...) error {
    if err != nil {
        return fmt.Errorf("export failed: %w", err)
    }
    return nil
}

// main.go
func main() {
    if err := run(); err != nil {
        fmt.Fprintf(os.Stderr, "❌ %v\n", err)
        os.Exit(1)
    }
}
```

---

### Phase 2: XLSX Converter 🍒 (Week 1-2)

#### 2.1 Новые команды

**cmd/tdtpcli/commands/xlsx.go:**

```go
package commands

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/xlsx"
    "github.com/queuebridge/tdtp/pkg/adapters"
)

// ToXLSX converts TDTP XML to Excel
func ToXLSX(inputXML, outputXLSX, sheetName string) error {
    // 1. Read TDTP XML file
    // 2. Parse with packet.Parser
    // 3. Convert to XLSX with xlsx.ToXLSX()
    // 4. Success message
}

// FromXLSX converts Excel to TDTP XML
func FromXLSX(inputXLSX, outputXML, sheetName string) error {
    // 1. Read Excel file with xlsx.FromXLSX()
    // 2. Marshal to XML
    // 3. Write to file
    // 4. Success message
}

// ExportToXLSX exports database table directly to Excel
func ExportToXLSX(ctx context.Context, adapter adapters.Adapter,
                  table, outputXLSX, sheetName string, query *packet.Query) error {
    // 1. Export table with adapter.ExportTable()
    // 2. Convert packets to XLSX with xlsx.ToXLSX()
    // 3. Success message with row count
}

// ImportFromXLSX imports Excel file directly to database
func ImportFromXLSX(ctx context.Context, adapter adapters.Adapter,
                    inputXLSX, sheetName string, strategy adapters.ImportStrategy) error {
    // 1. Read Excel with xlsx.FromXLSX()
    // 2. Import packet with adapter.ImportPacket()
    // 3. Success message with row count
}
```

**main.go additions:**

```go
// Flags
toXLSX := flag.String("to-xlsx", "", "Convert TDTP XML to Excel: input.xml")
fromXLSX := flag.String("from-xlsx", "", "Convert Excel to TDTP XML: input.xlsx")
exportXLSX := flag.String("export-xlsx", "", "Export table directly to Excel")
importXLSX := flag.String("import-xlsx", "", "Import Excel file to database")
sheetName := flag.String("sheet", "Sheet1", "Excel sheet name")

// Command routing
if *toXLSX != "" {
    handleToXLSX(*toXLSX, *output, *sheetName)
} else if *fromXLSX != "" {
    handleFromXLSX(*fromXLSX, *output, *sheetName)
} else if *exportXLSX != "" {
    handleExportXLSX(ctx, adapter, *exportXLSX, *output, *sheetName, query)
} else if *importXLSX != "" {
    handleImportXLSX(ctx, adapter, *importXLSX, *sheetName, strategy)
}
```

**help.go updates:**

```go
fmt.Println("XLSX Converter 🍒:")
fmt.Println("  --to-xlsx <input.xml>     Convert TDTP XML to Excel")
fmt.Println("  --from-xlsx <input.xlsx>  Convert Excel to TDTP XML")
fmt.Println("  --export-xlsx <table>     Export database table to Excel")
fmt.Println("  --import-xlsx <file.xlsx> Import Excel file to database")
fmt.Println("  --sheet <name>            Excel sheet name (default: Sheet1)")
fmt.Println()
fmt.Println("Examples:")
fmt.Println("  # Database → Excel (instant business value!)")
fmt.Println("  tdtpcli --export-xlsx orders --output orders.xlsx")
fmt.Println()
fmt.Println("  # Excel → Database")
fmt.Println("  tdtpcli --import-xlsx data.xlsx --table customers")
fmt.Println()
fmt.Println("  # TDTP XML → Excel")
fmt.Println("  tdtpcli --to-xlsx users.xml --output users.xlsx")
fmt.Println()
fmt.Println("  # Excel → TDTP XML")
fmt.Println("  tdtpcli --from-xlsx data.xlsx --output data.xml")
```

---

### Phase 3: Import Strategies (Week 2)

#### 3.1 Strategy Flag

**Текущая проблема:** Всегда используется `StrategyReplace`

**Решение:** Добавить флаг `--strategy`

```go
// flags.go
strategy := flag.String("strategy", "replace",
    "Import strategy: replace|ignore|fail|copy")

// import.go
func parseStrategy(s string) (adapters.ImportStrategy, error) {
    switch strings.ToLower(s) {
    case "replace":
        return adapters.StrategyReplace, nil
    case "ignore":
        return adapters.StrategyIgnore, nil
    case "fail":
        return adapters.StrategyFail, nil
    case "copy":
        return adapters.StrategyCopy, nil
    default:
        return "", fmt.Errorf("unknown strategy: %s", s)
    }
}

func HandleImport(ctx context.Context, adapter adapters.Adapter,
                  filename string, strategyStr string) error {
    strategy, err := parseStrategy(strategyStr)
    if err != nil {
        return err
    }

    // ... existing import logic with strategy
    return adapter.ImportPacket(ctx, pkt, strategy)
}
```

**Обновить:** `handleImportFile()`, `handleImportBroker()`, `handleImportXLSX()`

---

### Phase 4: Production Features (Week 2-3)

#### 4.1 Circuit Breaker

**config.yaml additions:**

```yaml
circuit_breaker:
  enabled: true
  max_failures: 5
  reset_timeout: 30s
  max_concurrent: 100
```

**cmd/tdtpcli/circuit.go:**

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/resilience"
)

// WrapWithCircuitBreaker wraps adapter calls with circuit breaker
func WrapWithCircuitBreaker(cfg CircuitBreakerConfig) *resilience.CircuitBreaker {
    if !cfg.Enabled {
        return nil
    }

    return resilience.NewCircuitBreaker(resilience.Config{
        MaxFailures:   cfg.MaxFailures,
        ResetTimeout:  cfg.ResetTimeout,
        MaxConcurrent: cfg.MaxConcurrent,
    })
}

// ExecuteWithCircuitBreaker executes function with circuit breaker protection
func ExecuteWithCircuitBreaker(ctx context.Context, cb *resilience.CircuitBreaker,
                                fn func() error) error {
    if cb == nil {
        return fn()
    }

    return cb.Execute(ctx, fn)
}
```

**Usage in commands:**

```go
// export.go
func HandleExport(ctx context.Context, adapter adapters.Adapter, cb *resilience.CircuitBreaker, ...) error {
    var packets []*packet.DataPacket

    err := ExecuteWithCircuitBreaker(ctx, cb, func() error {
        var err error
        packets, err = adapter.ExportTable(ctx, tableName)
        return err
    })

    if err != nil {
        return fmt.Errorf("export failed: %w", err)
    }

    // ... rest of export logic
}
```

#### 4.2 Audit Logger

**config.yaml additions:**

```yaml
audit:
  enabled: true
  level: standard  # minimal | standard | full
  file: ./audit.log
  rotation:
    max_size_mb: 100
    max_backups: 5
```

**cmd/tdtpcli/audit.go:**

```go
package main

import (
    "os"
    "github.com/queuebridge/tdtp/pkg/audit"
)

func InitAuditLogger(cfg AuditConfig) *audit.AuditLogger {
    if !cfg.Enabled {
        return nil
    }

    logger := audit.NewAuditLogger()

    // File appender
    if cfg.File != "" {
        appender := audit.NewFileAppender(cfg.File)
        if cfg.Rotation.MaxSizeMB > 0 {
            appender.SetRotation(cfg.Rotation.MaxSizeMB, cfg.Rotation.MaxBackups)
        }
        logger.AddAppender(appender)
    }

    // Console appender (for verbose mode)
    logger.AddAppender(audit.NewConsoleAppender())

    // Set level
    switch cfg.Level {
    case "minimal":
        logger.SetLevel(audit.LevelMinimal)
    case "standard":
        logger.SetLevel(audit.LevelStandard)
    case "full":
        logger.SetLevel(audit.LevelFull)
    }

    return logger
}

func LogOperation(logger *audit.AuditLogger, operation string, data map[string]interface{}) {
    if logger == nil {
        return
    }

    data["user"] = os.Getenv("USER")
    data["operation"] = operation

    logger.Info(operation, data)
}
```

**Usage in commands:**

```go
// export.go
func HandleExport(ctx context.Context, adapter adapters.Adapter,
                  logger *audit.AuditLogger, ...) error {
    LogOperation(logger, "export_started", map[string]interface{}{
        "table": tableName,
        "filters": query != nil,
    })

    // ... export logic

    LogOperation(logger, "export_completed", map[string]interface{}{
        "table": tableName,
        "rows": totalRows,
        "packets": len(packets),
    })

    return nil
}
```

#### 4.3 Retry Mechanism

**config.yaml additions:**

```yaml
retry:
  enabled: true
  strategy: exponential  # constant | linear | exponential
  max_attempts: 3
  initial_delay: 1s
  max_delay: 30s
  jitter: true
```

**cmd/tdtpcli/retry.go:**

```go
package main

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/retry"
)

func InitRetry(cfg RetryConfig) *retry.Retry {
    if !cfg.Enabled {
        return nil
    }

    var strategy retry.BackoffStrategy
    switch cfg.Strategy {
    case "constant":
        strategy = retry.NewConstantBackoff(cfg.InitialDelay)
    case "linear":
        strategy = retry.NewLinearBackoff(cfg.InitialDelay, cfg.MaxDelay)
    case "exponential":
        strategy = retry.NewExponentialBackoff(cfg.InitialDelay, cfg.MaxDelay)
    }

    return retry.NewRetry(retry.Config{
        MaxAttempts:     cfg.MaxAttempts,
        BackoffStrategy: strategy,
        Jitter:          cfg.Jitter,
    })
}

func ExecuteWithRetry(ctx context.Context, r *retry.Retry, fn func() error) error {
    if r == nil {
        return fn()
    }

    return r.Do(ctx, fn)
}
```

**Usage in commands:**

```go
// import.go
func HandleImport(ctx context.Context, adapter adapters.Adapter,
                  retrier *retry.Retry, ...) error {
    return ExecuteWithRetry(ctx, retrier, func() error {
        return adapter.ImportPacket(ctx, pkt, strategy)
    })
}
```

---

### Phase 5: Data Processors (Week 3)

#### 5.1 Processor Flags

```go
// flags.go
mask := flag.String("mask", "", "Mask PII fields (comma-separated): email,phone,card")
validate := flag.String("validate", "", "Validate fields: field1:rule1,field2:rule2")
normalize := flag.String("normalize", "", "Normalize fields: field1:type1,field2:type2")
processorsConfig := flag.String("processors", "", "Processors config file")
```

#### 5.2 Processor Integration

**cmd/tdtpcli/processors.go:**

```go
package main

import (
    "strings"
    "github.com/queuebridge/tdtp/pkg/processors"
    "github.com/queuebridge/tdtp/pkg/core/packet"
)

func BuildProcessorChain(maskFields, validateRules, normalizeRules string) *processors.Chain {
    chain := processors.NewChain()

    // Masking
    if maskFields != "" {
        fields := strings.Split(maskFields, ",")
        masker := processors.NewFieldMasker(map[string]processors.MaskStrategy{})
        for _, field := range fields {
            field = strings.TrimSpace(field)
            masker.AddField(field, processors.MaskStrategyPartial)
        }
        chain.Add(masker)
    }

    // Validation
    if validateRules != "" {
        rules := parseValidationRules(validateRules)
        validator := processors.NewFieldValidator(rules)
        chain.Add(validator)
    }

    // Normalization
    if normalizeRules != "" {
        rules := parseNormalizationRules(normalizeRules)
        normalizer := processors.NewFieldNormalizer(rules)
        chain.Add(normalizer)
    }

    return chain
}

func ApplyProcessors(packets []*packet.DataPacket, chain *processors.Chain) error {
    if chain == nil || chain.IsEmpty() {
        return nil
    }

    for _, pkt := range packets {
        if err := chain.Process(pkt); err != nil {
            return err
        }
    }

    return nil
}

func parseValidationRules(rules string) []processors.ValidationRule {
    // Parse "email:^.*@.*$,age:0-150"
    // Return []ValidationRule
}

func parseNormalizationRules(rules string) []processors.NormalizationRule {
    // Parse "phone:phone,email:email"
    // Return []NormalizationRule
}
```

**Usage in export:**

```go
// export.go
func HandleExport(ctx context.Context, adapter adapters.Adapter,
                  processorChain *processors.Chain, ...) error {
    // 1. Export
    packets, err := adapter.ExportTable(ctx, tableName)
    if err != nil {
        return err
    }

    // 2. Apply processors
    if err := ApplyProcessors(packets, processorChain); err != nil {
        return fmt.Errorf("processor failed: %w", err)
    }

    // 3. Write output
    // ...
}
```

**help.go additions:**

```go
fmt.Println("Data Processing:")
fmt.Println("  --mask <fields>           Mask PII fields (comma-separated)")
fmt.Println("  --validate <rules>        Validate fields: field:rule,...")
fmt.Println("  --normalize <rules>       Normalize fields: field:type,...")
fmt.Println("  --processors <file.yaml>  Load processors from config")
fmt.Println()
fmt.Println("Examples:")
fmt.Println("  # Export with PII masking")
fmt.Println("  tdtpcli --export users --mask email,phone,credit_card")
fmt.Println()
fmt.Println("  # Import with validation")
fmt.Println("  tdtpcli --import data.xml --validate \"email:^.*@.*$,age:0-150\"")
```

---

### Phase 6: Incremental Sync (Week 4)

#### 6.1 Sync Command

**cmd/tdtpcli/commands/sync.go:**

```go
package commands

import (
    "context"
    "github.com/queuebridge/tdtp/pkg/adapters"
    "github.com/queuebridge/tdtp/pkg/sync"
)

func SyncIncremental(ctx context.Context, adapter adapters.Adapter,
                     table, trackingField, checkpointFile string,
                     batchSize int, output string) error {
    // 1. Load state
    stateManager := sync.NewStateManager(checkpointFile)
    state, err := stateManager.Load(table)
    if err != nil && !os.IsNotExist(err) {
        return err
    }

    if state == nil {
        fmt.Println("📝 Starting initial sync (no checkpoint found)")
    } else {
        fmt.Printf("📝 Resuming from checkpoint: %s = %v\n",
                   trackingField, state.LastValue)
    }

    // 2. Configure incremental sync
    config := sync.IncrementalConfig{
        TrackingStrategy: sync.StrategyTimestamp,
        TrackingField:    trackingField,
        BatchSize:        batchSize,
    }

    // 3. Export incrementally
    fmt.Printf("🔄 Syncing table: %s...\n", table)

    packets, newState, err := adapter.ExportTableIncremental(ctx, table, config, state)
    if err != nil {
        return fmt.Errorf("incremental export failed: %w", err)
    }

    if len(packets) == 0 {
        fmt.Println("✅ No new data to sync")
        return nil
    }

    totalRows := 0
    for _, pkt := range packets {
        totalRows += len(pkt.Data.Rows)
    }

    // 4. Write to file
    if err := WritePackets(packets, output); err != nil {
        return err
    }

    // 5. Save checkpoint
    if err := stateManager.Save(table, newState); err != nil {
        return fmt.Errorf("failed to save checkpoint: %w", err)
    }

    fmt.Printf("✅ Synced %d new/modified rows\n", totalRows)
    fmt.Printf("📝 Checkpoint saved: %s = %v\n", trackingField, newState.LastValue)

    return nil
}
```

**flags.go additions:**

```go
syncIncremental := flag.String("sync-incremental", "", "Incremental sync table")
trackingField := flag.String("tracking-field", "updated_at", "Tracking field for sync")
checkpointFile := flag.String("checkpoint-file", "./.sync_state.json", "Checkpoint file")
batchSize := flag.Int("batch-size", 1000, "Batch size for sync")
```

**help.go additions:**

```go
fmt.Println("Incremental Sync:")
fmt.Println("  --sync-incremental <table>  Incremental sync (only changes)")
fmt.Println("  --tracking-field <field>    Timestamp/sequence field (default: updated_at)")
fmt.Println("  --checkpoint-file <path>    Checkpoint file (default: ./.sync_state.json)")
fmt.Println("  --batch-size <N>            Batch size (default: 1000)")
fmt.Println()
fmt.Println("Examples:")
fmt.Println("  # Initial sync")
fmt.Println("  tdtpcli --sync-incremental orders \\")
fmt.Println("    --tracking-field updated_at \\")
fmt.Println("    --output orders_sync.xml")
fmt.Println()
fmt.Println("  # Resume (only new/modified rows)")
fmt.Println("  tdtpcli --sync-incremental orders \\")
fmt.Println("    --output orders_delta.xml")
fmt.Println("  # 200x faster for large tables!")
```

---

### Phase 7: Config Structure Updates

#### 7.1 Extended Config

**config.go:**

```go
type Config struct {
    Database       DatabaseConfig       `yaml:"database"`
    Broker         BrokerConfig         `yaml:"broker"`
    CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker"` // NEW
    Audit          AuditConfig          `yaml:"audit"`           // NEW
    Retry          RetryConfig          `yaml:"retry"`           // NEW
}

type CircuitBreakerConfig struct {
    Enabled       bool          `yaml:"enabled"`
    MaxFailures   int           `yaml:"max_failures"`
    ResetTimeout  time.Duration `yaml:"reset_timeout"`
    MaxConcurrent int           `yaml:"max_concurrent"`
}

type AuditConfig struct {
    Enabled  bool           `yaml:"enabled"`
    Level    string         `yaml:"level"`
    File     string         `yaml:"file"`
    Rotation RotationConfig `yaml:"rotation"`
}

type RotationConfig struct {
    MaxSizeMB  int `yaml:"max_size_mb"`
    MaxBackups int `yaml:"max_backups"`
}

type RetryConfig struct {
    Enabled      bool          `yaml:"enabled"`
    Strategy     string        `yaml:"strategy"`
    MaxAttempts  int           `yaml:"max_attempts"`
    InitialDelay time.Duration `yaml:"initial_delay"`
    MaxDelay     time.Duration `yaml:"max_delay"`
    Jitter       bool          `yaml:"jitter"`
}
```

#### 7.2 Config Template Updates

**CreateConfigTemplate() updates:**

```yaml
# Production features
circuit_breaker:
  enabled: false
  max_failures: 5
  reset_timeout: 30s
  max_concurrent: 100

audit:
  enabled: false
  level: standard  # minimal | standard | full
  file: ./audit.log
  rotation:
    max_size_mb: 100
    max_backups: 5

retry:
  enabled: false
  strategy: exponential  # constant | linear | exponential
  max_attempts: 3
  initial_delay: 1s
  max_delay: 30s
  jitter: true
```

---

## 📊 Приоритеты реализации

### Must Have (Weeks 1-2)
1. ✅ **Рефакторинг структуры** - разделение на модули
2. ✅ **XLSX Converter** 🍒 - 4 команды (instant business value!)
3. ✅ **Import strategies** - --strategy flag

### Should Have (Week 3)
4. ✅ **Circuit Breaker** - production resilience
5. ✅ **Audit Logger** - compliance tracking
6. ✅ **Retry mechanism** - transient error handling

### Nice to Have (Week 4)
7. ✅ **Data Processors** - mask, validate, normalize
8. ✅ **Incremental Sync** - 200x faster for large tables

---

## 🧪 Testing Strategy

### Unit Tests

```
cmd/tdtpcli/
├── commands/
│   ├── export_test.go
│   ├── import_test.go
│   ├── xlsx_test.go
│   ├── broker_test.go
│   └── sync_test.go
├── processors_test.go
├── query_test.go
└── config_test.go
```

### Integration Tests

```bash
# Test XLSX converter
go test -v ./cmd/tdtpcli/commands -run TestXLSXConversion

# Test incremental sync
go test -v ./cmd/tdtpcli/commands -run TestIncrementalSync

# Test processors
go test -v ./cmd/tdtpcli -run TestProcessors
```

---

## 📋 Complete Command Reference (After Refactoring)

```bash
# Basic commands
tdtpcli --help
tdtpcli --version
tdtpcli --config config.yaml
tdtpcli --list

# Export/Import
tdtpcli --export users --output users.xml
tdtpcli --import users.xml --strategy replace

# TDTQL filters
tdtpcli --export users --where "age > 18" --order-by "created_at DESC" --limit 100

# XLSX Converter 🍒
tdtpcli --to-xlsx users.xml --output users.xlsx --sheet Users
tdtpcli --from-xlsx data.xlsx --output data.xml --sheet Sheet1
tdtpcli --export-xlsx orders --output orders.xlsx
tdtpcli --import-xlsx data.xlsx --strategy replace

# Message brokers
tdtpcli --export-broker orders --where "status = 'pending'"
tdtpcli --import-broker

# Incremental sync
tdtpcli --sync-incremental orders --tracking-field updated_at --output orders_delta.xml

# Data processing
tdtpcli --export users --mask email,phone --output users_masked.xml
tdtpcli --import data.xml --validate "email:^.*@.*$,age:0-150"
tdtpcli --export orders --normalize "phone:phone,email:email"

# Production features (via config.yaml)
# - Circuit Breaker: enabled in config
# - Audit Logger: enabled in config
# - Retry: enabled in config
```

---

## 🚀 Implementation Steps

### Week 1: Structure + XLSX
1. ✅ Рефакторинг: разделить main.go на модули
2. ✅ Реализовать XLSX команды (--to-xlsx, --from-xlsx, --export-xlsx, --import-xlsx)
3. ✅ Добавить --strategy flag
4. ✅ Unit tests для XLSX

### Week 2: Production Features
5. ✅ Circuit Breaker интеграция
6. ✅ Audit Logger интеграция
7. ✅ Retry mechanism интеграция
8. ✅ Обновить config.yaml template

### Week 3: Processors
9. ✅ Реализовать --mask, --validate, --normalize flags
10. ✅ Processor chain integration
11. ✅ Unit tests для processors

### Week 4: Incremental Sync
12. ✅ Реализовать --sync-incremental command
13. ✅ Checkpoint management
14. ✅ Integration tests
15. ✅ Documentation updates

---

## 📝 Documentation Updates

После рефакторинга обновить:
- [ ] `docs/USER_GUIDE.md` - добавить новые команды
- [ ] `INSTALLATION_GUIDE.md` - обновить CLI section
- [ ] `README.md` - обновить CLI examples
- [ ] `cmd/tdtpcli/README.md` - developer guide

---

## ✅ Success Criteria

**После рефакторинга CLI должна:**
- ✅ Поддерживать все v1.2 features
- ✅ Иметь модульную структуру кода
- ✅ Покрытие тестами > 70%
- ✅ Полная документация с примерами
- ✅ Обратная совместимость (все старые команды работают)

---

**Status:** 📋 Ready for Implementation
**Estimated time:** 4 weeks
**Priority:** High (missing key v1.2 features)
