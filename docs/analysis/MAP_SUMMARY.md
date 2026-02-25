# PROJECT MAP SUMMARY

**Updated:** 2026-02-25
**Tool:** funcfinder v1.6.118 (auto-generated)
**Branch:** claude/test-rdptcli-where-conditions-HQULZ

---

## CODE STATISTICS

### Go Source (production, excl. examples)

| Component | Files | Lines | Functions | Structs/Types | Tests |
|-----------|------:|------:|----------:|--------------:|------:|
| **pkg/adapters** | 27 | 7 790 | 317 | 15+ | 27 |
| **pkg/core** | 21 | 4 552 | 347 | 54 | 153 |
| **pkg/etl** | 7 | 2 382 | 82 | — | 14 |
| **pkg/processors** | 8 | 1 406 | 96 | — | 24 |
| **pkg/resilience** | 3 | 692 | 54 | — | 13 |
| **pkg/xlsx** | 1 | 298 | 7 | — | 0 |
| **cmd/tdtp-xray** | 8 | 4 442 | 255 | 55 | 0 |
| **cmd/tdtpcli** | 18 | 4 508 | 94 | — | 10 |
| **cmd/tdtpserve** | 3 | 927 | 19 | — | 0 |
| **TOTAL** | **125** | **32 850** | **~1 271** | **~125** | **241** |

### Other languages

| Language | Files | Functions |
|----------|------:|----------:|
| Python (bindings/) | 32 | 209 |
| JavaScript (wizard.js, validation.js) | 2 | ~130 |
| C# (libcs/) | 1 | — |

---

## PACKAGE DEPENDENCY GRAPH (internal)

Most-imported internal packages (by import count across all Go files):

```
pkg/core/packet       98 imports  ← universal data carrier
pkg/adapters          46 imports  ← DB I/O
pkg/core/schema       31 imports  ← type system
pkg/core/tdtql        16 imports  ← query language
pkg/processors        11 imports  ← transforms
pkg/etl                6 imports  ← pipeline runner
pkg/adapters/base      6 imports  ← shared DB logic
pkg/brokers            5 imports  ← message queues
pkg/audit              4 imports  ← logging
pkg/sync               3 imports  ← replication helpers
pkg/xlsx               3 imports  ← Excel I/O
```

---

## COMPONENT BREAKDOWN

### pkg/core — Framework Core (4 552 lines)

**Subpackages:**

| Subpackage | Key Types | Key Functions |
|------------|-----------|---------------|
| `schema` | `Builder`, `TypedValue`, `FieldDef`, `Converter`, `Validator` | ParseValue, Validate, Convert |
| `tdtql` | `Parser`, `Lexer`, `SQLGenerator`, `FilterEngine`, `Executor`, `Translator` | Parse, GenerateSQL, Execute, Filter |
| `tdtql/ast` | `SelectStatement`, `BinaryExpression`, `ComparisonExpression`, `InExpression`, `BetweenExpression`, `IsNullExpression`, `OrderByClause` | — (AST nodes) |
| `packet` | `Query`, `Filters`, `Filter`, `LogicalGroup`, `OrderBy`, `OrderField` | — |

**TDTQL AST** — full SQL subset implemented:
- Binary ops: `AND`, `OR`
- Comparisons: `=`, `!=`, `<`, `>`, `<=`, `>=`
- Special: `IN`, `BETWEEN`, `IS NULL`, `IS NOT NULL`
- Ordering: `ORDER BY` with `ASC`/`DESC`

---

### pkg/adapters — Database Adapters (7 790 lines)

**Interfaces:**

```
Adapter (adapter.go)          ← main interface
├── IncrementalConfig         ← incremental sync config
├── SSLConfig                 ← TLS params
├── Tx                        ← transaction handle
└── ViewInfo                  ← view metadata

base/StandardSQLAdapter       ← shared SQL implementation
base/MSSQLAdapter             ← MSSQL-specific
base/ImportHelper             ← bulk insert helpers
    ├── TableManager
    ├── DataInserter
    └── TransactionManager
```

**Implemented adapters:** mssql, postgres, sqlite, mysql, clickhouse, oracle, redis, rabbitmq, kafka

---

### pkg/etl — Pipeline Runner (2 382 lines)

Key functions:
- `loader.go` — source loading, multi-part support, TDTP decompression
- `exporter.go` — output writing (TDTP, XLSX, DB)
- `workspace.go` — `:memory:` SQLite for transform execution
- `config.go` — ETL YAML config parsing

Type mapping (workspace):
```
INTEGER → INTEGER
REAL/DECIMAL → REAL
DATE/DATETIME → TEXT
BOOLEAN → INTEGER (0/1)
BLOB → BLOB
```

---

### cmd/tdtp-xray — Visual ETL Designer (4 442 lines Go + JS)

**Go backend (app.go, services/):**

| Service | Structs | Key Functions |
|---------|---------|---------------|
| `App` (app.go) | 50+ structs | GenerateSQL, PreviewTransform, SaveToRepository, LoadFromRepository, ValidateTransformationSQL |
| `ConnectionService` | 1 | TestConnection, GetTables, GetViews, QuickTest |
| `ValidationService` | 1 | ValidateTransformationSQL, findColumnConflicts, SuggestMultiSourcePrefixes, GenerateCastWithPrefix |
| `PreviewService` | 1 | PreviewQuery, PreviewMockSource, PreviewTDTPSource, EstimateRowCount |
| `MetadataService` | 1 | GetTableSchema, InferTDTPSchema, getPrimaryKeys |
| `SourceService` | 3 | LoadMockSource, ValidateRealSource, InferSchemaFromTable |
| `TDTPService` | 2 | TestTDTPFile, collectAllParts, decompressPacket |

**JavaScript frontend (wizard.js ~4 500 lines, validation.js):**

The wizard implements a 7-step pipeline builder:

| Step | Description | Key JS functions |
|------|-------------|-----------------|
| Step 1 | Pipeline name/config | validatePipelineName, loadConfigurationFile, saveConfigurationFile |
| Step 2 | Source management | renderSourceList, testConnection, saveSourceForm, generateDSN |
| Step 3 | Canvas/JOIN designer | addTableToCanvas, renderCanvas, createTableCard, openFilterBuilder, cycleSortState, resetFieldSort, openSelectCastDialog |
| Step 4 | Transform SQL preview | previewTransform, useGeneratedSQL |
| Step 5 | Output config | loadOutputFormData, onOutputTypeChange |
| Step 6 | Settings | setDefaultSettings, saveStep6 |
| Step 7 | YAML generation | renderConfigSummary, generateAndShowYAML, saveYAMLToFile |

**Repository (SQLite `configs.db`):**
- `SaveToRepository`, `ListRepositoryConfigs`, `LoadFromRepository`, `DeleteFromRepository`
- Filter by name, technology flags (`us_pg`, `us_mssql`, etc.), AND/OR logic

---

### cmd/tdtpcli — CLI Tool (4 508 lines)

**Commands:**

| Command file | Description |
|---|---|
| `html.go` | NEW: TDTP → HTML viewer (ConvertTDTPToHTML, renderHTML, openInBrowser) |
| `export.go` | Export DB → TDTP |
| `import.go` | Import TDTP → DB |
| `broker.go` | RabbitMQ/Kafka integration |
| `pipeline.go` | Run ETL pipeline |
| `diff.go` | Compare two TDTP files |
| `merge.go` | Merge TDTP files |
| `sync.go` | Incremental sync |
| `xlsx.go` | TDTP ↔ XLSX |
| `list.go` | List TDTP contents |

**Key flags (new):**
- `--limit N` — first N rows (negative = last N, tail mode)
- `--row N` — single row view
- `-h` — short alias for `--help`

---

### pkg/resilience (692 lines)

- `circuit_breaker.go` — circuit breaker pattern
- Protects external connections (DB, broker) from cascade failures

---

## ARCHITECTURE PRINCIPLES

1. **Single source of truth** — `pkg/core/packet` is the universal data carrier between all components
2. **Adapter pattern** — all DB engines implement `Adapter` interface; callers are agnostic
3. **ETL separation** — source loading, transform (SQLite workspace), output export are independent stages
4. **UI layer isolation** — `cmd/tdtp-xray` is a self-contained Wails app; it uses `pkg/etl` and `pkg/adapters` but adds its own service layer for UI-specific logic
5. **Type preservation chain** — DB types → TDTP schema types → SQLite in-memory types → output types

---

## NEW SINCE LAST ANALYSIS (2026-02-20)

| Area | Change |
|------|--------|
| `cmd/tdtpcli/commands/html.go` | NEW — HTML viewer command (+478 lines) |
| `cmd/tdtp-xray/services/validation_service.go` | NEW — SQL validation service (+242 lines) |
| `cmd/tdtp-xray/frontend/src/scripts/validation.js` | NEW — realtime SQL validation in UI (+343 lines) |
| `cmd/tdtp-xray/app.go` | +833 lines: repository, validation API, CAST support |
| `cmd/tdtp-xray/frontend/src/scripts/wizard.js` | +1 251 lines: ORDER BY, IS NULL ops, field toolbar, CAST in WHERE |
| `pkg/adapters/mssql/hex.go` | Optimized: timestamp→hex 3.33x faster, zero allocs |
| `pkg/core/tdtql/sql_generator.go` | IS NULL / IS NOT NULL operators |
| `pkg/etl/loader.go` | +116 lines: TDTP file source support |
| `pkg/etl/exporter.go` | +42 lines: XLSX output |
| `docs/` | 9 old doc files removed (~9 000 lines), DEVELOPER_GUIDE rewritten |
| **funcfinder** | Updated v1.5.0 → **v1.6.118** (--struct extract fix, --dir . fix, auto-version) |

---

## TEST COVERAGE SNAPSHOT

| Package | Test funcs | Notes |
|---------|----------:|-------|
| pkg/core | 153 | Best coverage in project |
| pkg/processors | 24 | — |
| pkg/adapters | 27 | Integration (require DB) |
| pkg/resilience | 13 | — |
| pkg/etl | 14 | — |
| cmd/tdtpcli | 10 | — |
| cmd/tdtp-xray | 0 | UI — no automated tests |
| pkg/xlsx | 0 | Missing |

**Total test functions: 241**

---
