# Changelog

All notable changes to tdtp-framework are documented in this file.

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
| 1.8.0-beta | S3 object storage, `--test` integrity check, `compress_algo` config, Python CLI test suites |
| 1.7.1-beta | Compact v1.3.1, `--compact`/`--to-compact`, `--inspect`, `--listen`, SpecialValues, xZMercury |
| 1.7.0 | kanzi compression, `--fields`, MSMQ, `--packet-size` |
| 1.6.0 | TDTQL `--where`, ETL pipeline, Access adapter, `--hash` |
| 1.3.1 | TDTP protocol v1.3.1 — compact format specification |
| 1.0–1.3 | Core protocol, XML serialization, SQLite/PostgreSQL/MSSQL adapters |
