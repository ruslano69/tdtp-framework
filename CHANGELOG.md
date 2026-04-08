# Changelog

All notable changes to tdtp-framework are documented in this file.

## [1.9.1] — 2026-04-07

### Fixed

- **PostgreSQL TIME type** (`pkg/adapters/postgres/types.go`, `pkg/adapters/base/type_converter.go`):
  - PostgreSQL `time without time zone` column now exports correctly as `08:00:00` instead of
    failing with "invalid timestamp format, expected RFC3339".
  - Root cause: `time` type was mapped to `TIMESTAMP` with subtype `"time"`, but converter
    didn't handle the subtype during validation.
  - Fix: Added `Subtype` field to `schema.FieldDef`, updated all `FieldDef` creation sites
    to copy subtype from `packet.Field`, and modified `parseTimestamp` to check for
    `subtype == "time"` and delegate to new `parseTime` function.
  - Added `pgtype.Time` handler in `DBValueToString` for PostgreSQL driver.

- **Test data reproducibility** (`scripts/create_postgres_test_db.py`):
  - Added `random.seed(42)` for deterministic test data generation.
  - Updated expected values in `tests/cli/test_postgres.py`: `ACTIVE_USERS=73`, `USERS_BALANCE_GT_5000=53`.

### Added

- **35/35 PostgreSQL CLI integration tests pass**:
  - All tests in `tests/cli/test_postgres.py` now pass with deterministic data.
  - Coverage: basic export, TDTQL filters, compression (zstd/kanzi/hash), export/import roundtrip,
    file integrity, edge cases, compact format.

---

## [1.9.0] — 2026-04-06

### Message Broker — Production Release

Kafka broker graduates from `[BETA]` to production-ready.
Full pipeline (DB → Kafka → DB, DB → Kafka → files) benchmarked at **50 000 rows in ~7s**
over localhost with 5 packets; traffic reduced 4× with kanzi vs uncompressed.

#### Export (`--export-broker`)

- **Parallel compress + serialize**: all packets processed in concurrent goroutines
  (`sync.WaitGroup`); each goroutine owns its own `packet.NewGenerator()` instance.
  kanzi: 6.7s → 5.1s (1.3×) on 100k rows.
- **`SendBatch`** (`pkg/brokers/kafka.go`): all serialized packets sent in a single
  `WriteMessages` call — one network roundtrip instead of N sequential sends.
  kafka-go `BatchTimeout` lowered from default 1s to 5ms (eliminates per-packet 1s wait).
  kafka-go `BatchBytes` raised to 100 MB (was 1 MB — caused "Message Size Too Large" on kanzi packets).

#### Import (`--import-broker`)

- **Parallel decompression**: all raw packets buffered first (receive is inherently serial),
  then packets 2…N decompressed in parallel goroutines; results assembled in order.
  ACK: single `CommitLast()` after all processing — for Kafka this commits the highest
  offset, implicitly covering all previous offsets.
- **`--output` mode**: instead of importing to DB, saves decompressed packets as
  `base_part_N_of_Total.tdtp.xml` files compatible with `--import` multi-part convention.
- **`--raw` flag** (`--import-broker --raw --output`): saves queue messages verbatim
  without any parse, decompress, or validation. Peeks the first message header to read
  `TotalParts` for correct `_part_N_of_Total` naming. No DB connection required.

#### Broker Configuration (Kafka)

- `brokers` (list) and `consumer_group` YAML fields added to `BrokerConfig`.
- `StartOffset: kafka.FirstOffset` (was `LastOffset`) — fixes race where reader
  positioned after messages sent during consumer group rebalance.

#### Performance (50k rows, 5 packets, localhost)

| Mode | Export | Import→files | Traffic |
|------|--------|-------------|---------|
| No compression | 3.4s | 3.8s | 7.2 MB |
| zstd level 3 | 3.5s | 3.9s | 2.4 MB (3×) |
| kanzi level 6 | 3.6s | 3.9s | 1.8 MB (4×) |

Import time dominated by receive + XML re-serialize; decompression parallelism eliminates
its contribution entirely at 5 packets.

### Changed

- `--listen` flag: removed `[BETA]` label. Streaming consumer is production-ready for Kafka.
- **`--import-broker` atomicity** (`cmd/tdtpcli/commands/broker.go`): multi-part imports now
  use a single `ImportPackets` transaction by default — all-or-nothing, mirrors `--import`
  (file) behaviour. Previously each part was committed with a separate `ImportPacket` call,
  leaving the table partially updated on failure.
- **`--keep` flag** (`--import-broker --keep`): opt-in streaming mode — each packet is
  received, decompressed, and committed immediately without buffering the full batch in
  memory. On failure, successfully committed parts remain in the table for analysis.
  Implemented in `importBrokerKeep()` as a separate code path (no full-batch buffer).
- Help (`help_full.txt`, `help_short.txt`): broker section expanded with `--raw`,
  `--output` multi-part naming, `--keep` semantics, parallel processing notes, kanzi
  traffic comparison.

### Fixed

- **`--fields` bracket-quoting** (`cmd/tdtpcli/main.go`): `splitCommaSeparated` now parses
  `[Field Name]` syntax for field names containing spaces or commas, matching the
  bracket-quoting already supported in `--where` (TDTQL lexer).
  - `--fields "id,[Birth Date],status"` → `["id", "Birth Date", "status"]`
  - `--fields "[First, Last],[Birth Date]"` → `["First, Last", "Birth Date"]`
  - Same parser used for `--key-fields`, `--ignore-fields`, `--fixed-fields`.
- **SELECT projection quoting** (`pkg/core/tdtql/sql_generator.go`): field names in
  `query.Fields` were joined bare into `SELECT f1, f2 FROM ...` — a name like `Birth Date`
  produced invalid SQL. Now each field passes through `quoteFieldName()` (same function
  already used for WHERE and ORDER BY), producing `SELECT "Birth Date", id FROM ...`.
  MSSQL/MySQL dialect adapters convert ANSI double-quotes downstream as before.

---

## [1.8.2] — 2026-04-05

### Performance

#### Import pipeline — 2× speedup (1.55s → 0.77s, 100k rows × 7 fields, SQLite)

- **Streaming import** (`cmd/tdtpcli/commands/import.go`): parts processed one at a time —
  read → parse → insert → release. Previously all parts were buffered in memory
  simultaneously before any inserts began. Memory usage is now constant regardless
  of part count; GC pauses during insertion eliminated.

- **`GetRowValues` fast path** (`pkg/core/packet/parser.go`): rows without escape
  characters (`\|`, `\\`, `\n`) — the vast majority of real data — are split via
  index scan returning subslices of the original string with zero per-field
  allocations. Benchmark: `simple_10_fields` 409 ns/11 allocs → 150 ns/1 alloc (2.7×);
  `many_fields_100` 5034 ns/105 allocs → 1079 ns/1 alloc (4.7×).

- **Parser/Converter singletons** (`pkg/adapters/base/import_helper.go`,
  `pkg/adapters/postgres/import.go`, `pkg/adapters/mssql/import.go`):
  `packet.NewParser()` and `schema.NewConverter()` were allocated on every single row
  in all adapters. Both structs are stateless (`{}`); replaced with package-level
  singletons. Eliminates ~2 allocs × 100k rows per import.

- **`PrepareContext` for SQLite batch INSERT** (`pkg/adapters/sqlite/import.go`):
  the 994-parameter INSERT query was re-parsed by SQLite on every batch call
  (~700 calls for 100k rows). Now prepared once; reused for all full batches.
  Args slice reused across batches. Raw benchmark: 1043 ms → 433 ms (2.4×).

#### Misc

- **`help.go` refactor**: ~100 `fmt.Println` calls replaced with two embedded text
  files (`help_short.txt`, `help_full.txt`) via `//go:embed`. Version injected via
  `strings.ReplaceAll("{VERSION}", version)` at runtime.

### Infrastructure

- **Pre-commit hook** (`.git/hooks/pre-commit`): runs `gofmt`, `golint`, `go vet`
  on staged `.go` files before every commit. `gofmt` and `go vet` are blocking;
  `golint` is advisory.

---

## [1.8.1-beta] — 2026-04-02

### Added

#### Field Name Sanitizer (`--translit`, `--clear`)
- `pkg/sanitize` — new package with `ApplyToSchema()` single entry point
  - `--clear`: symbol map replacement (`%` → `_pct_`, `$` → `_usd_`, `&` → `_and_`, `@` → `_at_`, `#` → `_xh_`, `?` → `_is_`, `~` → `_not_`, spaces/dots/dashes → `_`; consecutive `__` collapsed)
  - `--translit`: non-ASCII transliteration via `github.com/mozillazg/go-unidecode v0.2.0` (Cyrillic, European diacritics)
  - Combined mode: `--translit` runs first, then `--clear`
  - Applied **only on `--import`** — `--export` always preserves original field names (source of truth)
- `cmd/tdtpcli/flags.go`: `--translit` and `--clear` CLI flags
- `cmd/tdtpcli/commands/import.go`: `SanitizeClear` / `SanitizeTranslit` options, applied after field whitelist
- `pkg/etl/config.go`: `SanitizeFieldsConfig` struct; `sanitize.translit/clear` keys in ETL source YAML
- `pkg/etl/processor.go`: per-source sanitization in `populateWorkspace`
- `pkg/core/packet/types.go`: `OriginalName string` runtime field on `packet.Field` (never serialized)
- DB column comments preserving original names:
  - PostgreSQL: `COMMENT ON COLUMN t.col IS 'original: ...'`
  - MySQL: inline `COMMENT 'original: ...'` in column definition
- Test XMLs: `tests/sanitize/` — `access_fields.tdtp.xml`, `cyrillic_fields.tdtp.xml`, `exotic_mixed.tdtp.xml`, `safe_import.tdtp.xml`
- `pkg/sanitize/fieldname_test.go` — 7 unit tests covering all sanitizer modes

#### TDTQL: Bracket-Quoted Identifiers
- `pkg/core/tdtql/lexer.go`: support for `[Field Name]` syntax (MSSQL/Access style)
  - `[` token now reads to `]` and emits `TokenIdent` with the inner name (brackets stripped)
  - Fixes: `--where "[Termination Date] = '1753-01-01'"` — was "parse error: expected field name, got 1"
- `pkg/core/tdtql/sql_generator.go`: `quoteFieldName()` helper
  - Names with non-safe chars → ANSI `"field name"` in generated SQL
  - Applied in `generateFilterCondition`, `generateOrderByClause`, `generateReversedOrderByClause`
- `pkg/adapters/base/sql_adapter.go`: `MSSQLAdapter.AdaptSQL` now converts ANSI-quoted `"field"` → `[field]`
  - `StandardSQLAdapter` MySQL mode: existing `ReplaceAll("\"", "`")` handles ANSI → backtick conversion

### Fixed
- `pkg/brokers/kafka_stub.go`: removed unused `config Config` field; added doc comments to all exported methods (revive lint)
- `pkg/processors/compression_test.go`: removed trailing blank line (gofmt)
- `.git/hooks/pre-commit`: `golangci-lint run --tags` → `--build-tags` (golangci-lint v2 rename)

### Documentation
- `docs/USER_GUIDE.md`: added `--test` command section, `--translit`/`--clear` section, bracket-quoted WHERE section, parallel export note, pre-import workflow `--inspect → --test → --import`
- `AGENTS.md`: added `--test` workflow, `--import --translit/--clear` skills, bracket-quoted `--where` examples
- `cmd/tdtpcli/help.go`: bracket-quoted `--where` examples, `--test`/`--inspect` pre-import workflow in EXAMPLES section

### Tests
- `tests/cli/test_sqlite.py`: added `complex_fields` table (column names with spaces and special chars); T2.8 and T2.9 tests for bracket-quoted `--where` on this table

---

## [1.8.0-beta] — 2026-03-31

### Added

#### Object Storage (S3)
- `pkg/storage` — ObjectStorage interface, factory, and S3 driver (`aws-sdk-go-v2`)
- `--output s3://bucket/key` on export — upload multi-part TDTP directly to S3
- `--import s3://bucket/key` — download + auto-discover all `_part_N_of_M` siblings from S3
- `--inspect s3://bucket/key` — inspect packet metadata from S3 in-memory (no temp file)
- `--to-xlsx / --export-xlsx --output s3://` — XLSX output directly to S3
- ETL pipeline source type `tdtp-s3`: load compressed multi-part TDTP sets from S3 into workspace
- Compatible with SeaweedFS, MinIO, Ceph RGW, AWS S3 (path-style and virtual-hosted)
- Build tag `nos3` to exclude driver and drop `aws-sdk-go-v2` dependency

#### File Integrity (`--test`)
- `--test <file>` — dry-run integrity check of TDTP files (no database required)
  - Multi-part file discovery: auto-resolves `_part_N_of_M` siblings from any part path
  - Missing part detection: reports which parts are absent before validating
  - Batch consistency: all parts must share the same `InReplyTo` UUID and `TableName`
  - Row count validation: actual `<R>` count vs `RecordsInPart` header field
  - XXH3 checksum validation for files exported with `--hash`
  - Decompression integrity: dry decompress in memory for zstd and kanzi files
  - Duplicate `MessageID` detection across parts

#### Compression
- `compress_algo` YAML config field in `ExportConfig` — set default algorithm in config file
  - Flag `--compress-algo` takes precedence over config file value
  - Example: `compress_algo: kanzi` in config enables kanzi without CLI flags

#### CLI Integration Tests
- `tests/cli/test_sqlite.py` — 31 integration tests for SQLite source
  - T1: Basic Export (3 tests) — row counts, field projection, `--list`
  - T2: TDTQL Filters (7 tests) — WHERE, AND, IN, ORDER BY, LIMIT, OFFSET, tail mode
  - T3: Compression (6 tests) — zstd levels, kanzi, hash, corrupt file detection
  - T4: Export/Import Roundtrip (5 tests) — data identity, strategies (replace/ignore), field subset
  - T5: File Integrity (3 tests) — `--test` on plain/compressed/checksum files, `--inspect`
  - T6: Edge Cases (3 tests) — empty result, nonexistent table/file error handling
  - T7: Compact Format (4 tests) — protocol v1.3.1, compact+compress pipeline, `--to-compact`, roundtrip
- `tests/cli/test_postgres.py` — 32 integration tests for PostgreSQL source
  - Same T1–T7 structure; T4 roundtrip imports into same PG database
  - Preflight check: `pg_isready` + row count verification + auto-setup via `create_postgres_test_db.py`
  - Dynamic WHERE assertions: expected counts queried live via psql subprocess
  - Run a single group: `python3 tests/cli/test_postgres.py T3`

#### Kanzi Compression (from v1.7.x)
- `--compress-algo kanzi` — kanzi-go compression alongside existing zstd
- Compression levels 6–7 for kanzi (6× ratio vs raw, vs 3× for zstd level 3)
- `pkg/python/libtdtp` — multi-algorithm support in Python bindings compress/decompress paths
- Build tag `nokafka` for offline builds without Kafka dependency

#### S3 + Pipeline Features
- `examples/09-s3-pipeline-chain` — extract → split by region pipeline example
- ETL `output.type: tdtp` with S3 output
- Smart Failover in ETL — fallback delivery channel with circuit breaker
- `--fast` flag to skip SpecialValues detection on export

### Changed
- `CreateSampleConfig` includes `CompressAlgo: "zstd"` in default template
- `--test` is an early-exit command: no database connection required
- `commandWasSpecified()` updated to include `--test`

### Performance (from v1.7.x)
- Parallel packet processing for file/S3 export
- Skip `GetRowCount` in TDTQL export when no LIMIT is set
- Single-pass XML escaping with schema-aware escape mask
- Manual `bufio` writer replacing `xml.MarshalIndent` in data section
- `strconv` replacing `fmt.Sprintf` in hot data conversion path
- DATE/DATETIME scanned as string in SQLite (skip `modernc.parseTime`)
- PostgreSQL full-export benchmark infrastructure (`cmd/bench_raw`)

---

## [1.7.1-beta] — 2025 Q4

### Added
- `--compact` — TDTP v1.3.1 compact format on export (fixed fields written once per group)
- `--to-compact <file>` — convert existing TDTP v1.x file to compact v1.3.1 in-place
- `--compact-tail` — tail + carry attributes for streaming support
- `--fields <col1,col2>` — column projection on export and import
- `--inspect <file>` — YAML metadata summary of a TDTP file or S3 object
- `--listen` — streaming consumer daemon (v1.7.1-beta)
- `--where` flag repeatable — multiple conditions combined with AND
- `--where` supports `IN (...)` operator
- `--limit` with negative value — tail mode (last N rows)
- `--list` accepts optional glob pattern for table name filtering
- `--validate` and `--normalize` YAML-based processors
- `FieldValidator` with `on_error` strategy: fail / filter / warn
- SpecialValues v1.3.1: `[NULL]`, `NaN`, `INF`, `-INF`, `0000-00-00` markers
  - Auto-detected on export; correctly restored to NULL/native on import
  - Excel data-integrity traps handled automatically (BIGINT, dates pre-1900, formula strings)
- RabbitMQ: flexible queue config, TLS skip-verify, passive declare
- MSMQ broker support (`queue_path` config field)
- xZMercury AES-256-GCM encryption layer for pipeline output
- `tdtpserve` — standalone HTTP encrypted TDTP data viewer
- Python bindings: `J_ExportAll`, `read_pandas` / `write_pandas`, zstd+XXH3 support
- C# .NET 3.5 P/Invoke wrapper for `libtdtp.dll`
- Redis result publisher for pipeline state reporting

### Fixed
- `RecordsInPart=0` in `ExecuteRawQuery` and `workspace.ExecuteSQL`
- rawRows regression: `ImportPacket` importing nothing after fast-path optimization
- Compact format auto-expansion at parser boundary (broker, ETL importer, diff/merge, HTML, XLSX)
- `--fields` projection applied to `<Schema>` and `<R>` in MSSQL export
- `StrategyReplace` = full table swap (TRUNCATE + INSERT), not UPSERT
- `StrategyCopy` = full replace; other strategies = UPSERT accumulate
- Batch-aware broker import — match by batchID, nack foreign packets
- Compression: `SetRows(GetRows())` clearing `rawRows` fixed
- DATE type detection and rowversion filtering in MSSQL adapter
- Scientific notation handling in DECIMAL parser

---

## [1.6.0] — 2025 Q3

### Added
- `--where` TDTQL filter with SQL-to-TDTQL translation
- `pkg/cliquery` — WHERE/fields parsing with unit tests
- PostgreSQL `--fields` projection in `ExportTableWithQuery`
- `pkg/etl` — ETL pipeline with workspace, smart failover, processor chain (mask → normalize → compact → compress → encrypt → hash)
- MS Access adapter (ODBC, 32-bit, Windows-1251, ADOX schema via VBScript)
- kanzi-go compression (direct dependency)
- `--packet-size` flag
- `--hash` flag — XXH3 checksum embedded in packet header
- Pagination: `ExportTableWithQuery` with Limit/Offset/MoreDataAvailable
- TDTP HTML viewer (`--to-html`)
- TDTP XLSX export/import (`--to-xlsx`, `--export-xlsx`)
- Zero Trust encryption layer (xZMercury)

---

## Version History Summary

| Version | Highlights |
|---------|-----------|
| 1.9.1 | PostgreSQL TIME type fix, test data reproducibility (seed=42), 35/35 tests pass |
| 1.9.0 | Kafka production-ready, parallel compress/decompress, `--raw`, `SendBatch`, `--output` multi-part save |
| 1.8.2 | 2× import speedup, streaming import, `PrepareContext`, embedded help files |
| 1.8.1-beta | `--translit`/`--clear` sanitization, bracket-quoted identifiers, ETL sanitize |
| 1.8.0-beta | S3 object storage, `--test` integrity check, `compress_algo` config, Python CLI test suites |
| 1.7.1-beta | Compact v1.3.1, `--compact`/`--to-compact`, `--inspect`, `--listen`, SpecialValues, xZMercury |
| 1.7.0 | kanzi compression, `--fields`, MSMQ, `--packet-size` |
| 1.6.0 | TDTQL `--where`, ETL pipeline, Access adapter, `--hash` |
| 1.3.1 | TDTP protocol v1.3.1 — compact format specification |
| 1.0–1.3 | Core protocol, XML serialization, SQLite/PostgreSQL/MSSQL adapters |
