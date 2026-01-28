# TDTP Framework - Analysis Report

**Date:** 2026-01-28
**Tool:** [funcfinder v1.5.0](https://github.com/ruslano69/funcfinder) - Universal Code Analysis Tool
**Target:** tdtp-framework-main

---

## 1. Project Overview

| Metric              | Value           |
|---------------------|-----------------|
| Total Go files      | 155             |
| Total lines of code | 42,279          |
| Test files          | 43              |
| Total functions     | 1,234           |
| Core functions      | 793             |
| Test functions      | 372             |
| Example functions   | 69              |
| Types/Structs       | 168             |
| Packages            | 14 core + CLI   |

### Architecture Layers

```
CLI (cmd/tdtpcli) ──> Production Layer (resilience, retry, audit, security)
                  ──> Processing Layer (processors, compression, sync, ETL)
                  ──> Core (packet, schema, tdtql)
                  ──> Adapters (sqlite, postgres, mssql, mysql)
                  ──> Brokers (rabbitmq, kafka, msmq)
```

---

## 2. Overall Health Assessment

### Strengths
- Well-structured package architecture with clear separation of concerns
- Comprehensive adapter pattern for 4 database engines
- Full message broker abstraction (RabbitMQ, Kafka, MSMQ)
- Production features: circuit breaker, retry with DLQ, audit logging
- 43 test files covering core packages
- 19+ examples demonstrating real-world usage
- CI/CD: 5 GitHub Actions workflows (CI, lint, security, release, SLSA)

### Issues Found
- **Build:** Network-dependent dependencies (kafka-go, sqlite) fail in restricted environments. Some Go module proxy requests to `storage.googleapis.com` timeout. This is an environment-level issue, not a code defect.
- **Duplication:** Significant copy-paste duplication detected across adapter implementations and between diff/merge modules (see Section 3).

---

## 3. Duplication Analysis

funcfinder identified **93 duplicated function names** across core code.
After deep analysis comparing actual function bodies, the results classify as:

### 3.1 IDENTICAL Duplicates (copy-paste, high-priority refactoring targets)

| Function | Locations | Description |
|----------|-----------|-------------|
| `fieldToFieldDef` | mssql/import.go, mysql/import.go, postgres/import.go (3x) | Converts `packet.Field` to `schema.FieldDef`. Zero DB-specific logic. |
| `rowToArgs` | mssql/import.go, mysql/import.go | Converts row strings to `[]interface{}` args. DB-agnostic. |
| `createTableInTx` | mssql/import.go, mysql/import.go | Executes CREATE TABLE SQL in transaction. |
| `scanRows` | mssql/export.go, mysql/export.go | Scans `*sql.Rows` into `[][]string`. ~30 lines identical. |
| `readRowsWithSQL` | mssql/export.go, mysql/export.go | Executes SQL query and delegates to `scanRows`. |
| `createQueryContextForSQL` | mssql/export.go, mysql/export.go | Builds pagination context with total count. |
| `extractKeyFields` | diff/diff.go, merge/merge.go | Returns key field names from schema. |
| `getFieldIndices` | diff/diff.go, merge/merge.go | Maps field names to indices. |
| `parseRows` | diff/diff.go, merge/merge.go | Parses rows via `parser.GetRowValues`. |
| `estimateRowSize` | packet/generator.go, packet/streaming.go | Estimates row byte size. Comment in code says "дублирует из generator.go". |
| `rowsToData` | packet/generator.go, packet/streaming.go | Converts `[][]string` to `Data` struct. Comment says "дублирует из generator.go". |

**Total: 11 identical function pairs/groups**

### 3.2 NEAR-IDENTICAL Duplicates (1-3 line differences)

| Function | Locations | Difference |
|----------|-----------|------------|
| `stringToValue` | mssql/import.go, mysql/import.go | MySQL returns `int(0/1)` for boolean, MSSQL returns `bool` |
| `valueToString` | mssql/export.go, mysql/export.go | MSSQL adds `rowversion` subtype check for `[]byte` |
| `buildInsertSQL` | mssql/import.go, mysql/import.go | Column quoting: `[col]` vs `` `col` `` |
| `importPacketDataInTx` | mssql/import.go, mysql/import.go | Different upsert method name (`importWithMerge` vs `importWithReplace`) |
| `validateSchemas` | diff/diff.go, merge/merge.go | Diff checks type compatibility, merge doesn't |
| `buildKey` | diff/diff.go, merge/merge.go | Diff supports `CaseSensitive` option |

**Total: 6 near-identical pairs**

### 3.3 Legitimate Interface Implementations (NOT duplicates)

These share names but have different implementations dictated by database/protocol specifics:

| Function | Reason Not to Refactor |
|----------|----------------------|
| `Connect`, `Close`, `Ping` | Each adapter/broker uses different drivers |
| `BeginTx`, `Commit`, `Rollback` | Transaction API differs per DB |
| `buildCreateTableSQL` | SQL syntax differs (type mapping, quoting, table options) |
| `importWithIgnore` | MSSQL has no `INSERT IGNORE` — completely different approach |
| `ExportTable`, `ImportPacket` | Core interface implementations, type-specific optimizations |
| `FormatText` | Diff formats differences; Merge formats merge results |

---

## 4. Refactoring Recommendations

### Priority 1: Trivial Fixes (immediate)

**`estimateRowSize` + `rowsToData` in `pkg/core/packet/streaming.go`**

`StreamingGenerator` embeds `*Generator`. The duplicate methods shadow the embedded ones.
Fix: Delete the `StreamingGenerator` versions — embedding resolves calls automatically.

```
Files: pkg/core/packet/streaming.go:349, streaming.go:368
Impact: -30 lines, zero risk
```

### Priority 2: Extract shared adapter helpers

Create `pkg/adapters/common/` package:

```go
// pkg/adapters/common/import_helpers.go
func FieldToFieldDef(field packet.Field) schema.FieldDef { ... }
func RowToArgs(row []string, schema packet.Schema, converter func(string, packet.Field) interface{}) []interface{} { ... }
func CreateTableInTx(ctx context.Context, tx *sql.Tx, createSQL string) error { ... }

// pkg/adapters/common/export_helpers.go
func ScanRows(rows *sql.Rows, schema packet.Schema, formatter func(interface{}, packet.Field) string) ([][]string, error) { ... }
func ReadRowsWithSQL(ctx context.Context, db *sql.DB, query string, schema packet.Schema, scanner func(...) ([][]string, error)) ([][]string, error) { ... }
```

```
Impact: -200+ lines across 4 adapters
Risk: Low (shared helpers with DB-specific callbacks)
```

### Priority 3: Extract diff/merge shared utilities

Create `pkg/core/packet/helpers.go`:

```go
func ExtractKeyFields(schema packet.Schema) []string { ... }
func GetFieldIndices(schema packet.Schema, names []string) []int { ... }
func ParseRows(rows []packet.Row, parser *packet.Parser) [][]string { ... }
func ValidateSchemas(a, b packet.Schema, checkTypes bool) error { ... }
func BuildKey(row []string, keyIndices []int, caseSensitive bool) string { ... }
```

```
Impact: -80 lines from diff.go and merge.go
Risk: Low (pure utility functions)
```

---

## 5. Duplicate Type Names

| Type Name | Locations | Assessment |
|-----------|-----------|------------|
| `Adapter` | adapters/adapter.go (interface) + 4 concrete adapters | Intentional — interface + implementations |
| `Config` | brokers, processors, resilience, retry | Different contexts, different fields. Namespaced by package. |
| `ExecutionResult` | tdtql/executor.go, etl/executor.go | Different fields. Namespaced by package. |
| `Factory` | adapters/factory.go, processors/factory.go | Different purposes. Namespaced by package. |
| `Generator` | packet/generator.go, tdtql/generator.go | Different purposes. Namespaced by package. |
| `Parser` | packet/parser.go, tdtql/parser.go | Different purposes. Namespaced by package. |
| `IncrementalConfig` | adapters/adapter.go, sync/config.go | Potential overlap — worth investigating |

Most type name collisions are **intentional** and properly namespaced by Go packages.
`IncrementalConfig` appearing in both `pkg/adapters` and `pkg/sync` may represent an unintended split.

---

## 6. funcfinder Scan Statistics

```
Mode: functions + types (--all)
Files scanned: 141 (auto-detected Go source)
Functions found: 1,234
Types/Structs found: 168
.gitignore patterns: respected
Scan time: <1s
```

### funcfinder Output Formats Used

| Format | Purpose |
|--------|---------|
| `--map --json` | Structured function mapping for automated analysis |
| `--struct --map --json` | Type/struct extraction for duplication detection |
| `--all --tree` | Visual project overview with hierarchy |

---

## 7. Summary

The TDTP Framework is a **well-architected, feature-rich** Go project with:
- Clean package separation and interface-based design
- Comprehensive adapter and broker abstraction layers
- Production-ready features (resilience, retry, audit, security)
- Good test coverage (43 test files, benchmarks included)

**Key finding:** ~17 functions contain true code duplication (11 identical + 6 near-identical), primarily concentrated in:
1. **mssql ↔ mysql adapter import/export logic** — these two adapters share the most `database/sql`-compatible code
2. **diff ↔ merge utility functions** — shared schema helpers were copy-pasted
3. **generator ↔ streaming** — acknowledged duplication with Russian comments

Estimated refactoring would eliminate **300+ lines** of duplicated code with low risk, primarily by extracting shared helpers with callback-based customization for database-specific behavior.
