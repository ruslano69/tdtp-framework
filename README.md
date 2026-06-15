# TDTP Framework

**Table Data Transfer Protocol** — a framework for universal tabular data exchange across databases and message brokers.

---

## Why TDTP?

### For AI agents and data engineers:

**Explore any database in minutes — no documentation needed:**

```bash
# 1. What tables exist?
tdtpcli --list --config my-db.yaml

# 2. What is the table structure? (types, keys, FKs)
tdtpcli --inspect-table orders

# 3. What does the data look like? (latest record)
tdtpcli --export orders --limit -1
```

**Result:** AI understands the structure of ANY database (Navision 2003, MSSQL, PostgreSQL, Access...) and can build ETL automatically.

---

### For enterprise integrations:

**Full cycle: discovery → ETL → synchronization → encryption**

| Layer | What it does | Command |
|-------|-------------|---------|
| Discovery | Understand any DB structure | `--list`, `--inspect-table` |
| Transfer | Extract → Transform → Load | `--pipeline etl.yaml` |
| Sync | Event-driven distributed sync | `--export-broker` / `--import-broker` |
| Protection | Zero Trust encryption (burn-on-read keys) | `--enc` + xZMercury + AES-256-GCM |

---

## Quick Start

### 1. Install and try

```bash
# Download binary for your OS
# Windows: tdtpcli.exe
# Linux/Mac: tdtpcli

# Create config for your database
tdtpcli --create-config-pg > config.yaml
# or for MSSQL, MySQL, SQLite, Access

# Try it
tdtpcli --list --config config.yaml
```

### 2. What is inside a TDTP file

Every TDTP.xml is a **self-contained packet** with all information inside:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">

  <Header>
    <Type>reference</Type>
    <TableName>orders</TableName>
    <MessageID>REF-2026-a1b2c3d4-P1</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>2</RecordsInPart>
    <Timestamp>2026-04-07T10:30:00Z</Timestamp>
    <Sender>erp-prod</Sender>
    <Recipient>analytics</Recipient>
  </Header>

  <Query language="TDTQL" version="1.0">
    <Filters>
      <Filter field="status" operator="eq" value="active"/>
    </Filters>
    <OrderBy field="created_at" direction="DESC"/>
    <Limit>1000</Limit>
  </Query>

  <Schema>
    <Field name="order_id"     type="INTEGER"  key="true"/>
    <Field name="customer_id"  type="INTEGER"/>
    <Field name="total_amount" type="REAL"/>
    <Field name="status"       type="TEXT"     length="20"/>
    <Field name="created_at"   type="DATETIME"/>
  </Schema>

  <Data>
    <R>1|42|1299.99|active|2026-04-07T10:30:00Z</R>
    <R>2|18|550.00|active|2026-04-07T11:45:00Z</R>
  </Data>

</DataPacket>
```

**No external documentation needed** — schema, query context and metadata are all inside the packet.

### 3. Performance

| Scenario | Time | Speed |
|----------|------|-------|
| Export 100K rows → kanzi | 0.7 s | ~70 MB/s |
| 50K rows via Kafka → PostgreSQL | 7 s | ~7K rows/s |
| kanzi level 6 compression | — | **4×** denser than raw |

### 4. Real-world examples

**Travel Agency** — 3 nodes, event-driven sync:
- Central → Branch: countries, tours, guides
- Branch → Central: clients, sales
- Airline → Central: flights, bookings

[Examples in examples/](/examples)

---

## What's Implemented (v1.9.6)

### Core Modules

#### Packet Module
- XML parser with TDTP v1.0 validation
- Generator for all message types (Reference, Delta, Response, Request)
- Automatic splitting into parts (pagination up to 3.8 MB)
- zstd compression support:
  - `CompressionOptions` configuration (enabled, level, minSize, algorithm)
  - Automatic compression on packet generation (threshold 1 KB)
  - Automatic decompression on parsing
  - XML attribute `compression="zstd"` for identifying compressed data
- **Kanzi compression** (`--compress-algo kanzi`):
  - 4× denser than raw, 30% denser than zstd level 3
  - Levels 6–7 (default: 6)
  - Ideal for large text tables, archive exports
- **XXH3 checksum** (`--hash`):
  - Integrity verification embedded in packet header
  - Validated by `--test` before import
- **`--fast` flag**: skip NULL/NaN/Inf detection for maximum throughput
- **`--packet-size`**: configurable max broker packet size (default ~1.9MB, use 8 for kanzi)
- **Compact format (v1.3.1)**:
  - Fixed fields written once per packet header, omitted from each row (`fixed="true"` in schema)
  - `RowsToCompactData` / `ExpandCompactRows` for encode/decode
  - Auto-detection of fixed fields: explicit list → `_`-prefix convention → data analysis (all-same values)
  - CLI: `--compact` on export, `--to-compact` for post-hoc conversion of existing files
- **`--test` command**: file integrity verification (no DB needed)
  - Multi-part file discovery and consistency checks
  - Row count validation vs header
  - XXH3 checksum verification (for `--hash` files)
  - Dry-run decompression (zstd/kanzi)
  - Duplicate `MessageID` detection
- `QueryContext` for stateless pattern
- Subtype support (UUID, JSONB, TIMESTAMPTZ)

#### Schema Module
- Validation of all TDTP data types
- Universal Converter for all adapters
- Data-to-schema conformance checking
- Builder API for schema construction

#### TDTQL Module
- Translator: SQL → TDTQL (WHERE, ORDER BY, LIMIT, OFFSET)
- Executor: in-memory data filtering
- SQL Generator: TDTQL → SQL optimization
- All operators (`=`, `!=`, `<`, `>`, `>=`, `<=`, `IN`, `BETWEEN`, `LIKE`, `IS NULL`)
- Logical groups (AND/OR) with nesting
- Sorting (single and multiple fields)
- Pagination with `QueryContext` statistics

---

### Database Adapters

#### Universal Interface
- Two-tier architecture (Interface + Implementations)
- Adapter factory with automatic registration
- Context-aware operations (`context.Context`)
- Import strategies: `REPLACE`, `IGNORE`, `FAIL`, `COPY`
- `ExportTable` / `ExportTableWithQuery`
- `ImportPacket` with transaction support

#### SQLite Adapter
- Connection via `modernc.org/sqlite`
- Export/Import with automatic type mapping
- TDTQL → SQL optimization at the DB level
- Automatic table creation
- Benchmark tests (10K+ rows/sec)

#### PostgreSQL Adapter
- Connection via `pgx/v5` connection pool
- Export with schema support (public/custom)
- Import with COPY for bulk operations
- Special types: UUID, JSONB, JSON, INET, ARRAY, NUMERIC
- `ON CONFLICT` for import strategies
- TDTQL → SQL optimization with safe schema substitution

#### MS SQL Server Adapter
- Connection via `github.com/microsoft/go-mssqldb`
- Export with parameterized queries
- `IDENTITY_INSERT` for importing key fields
- Support for NVARCHAR, UNIQUEIDENTIFIER, DATETIME2
- Compatible with MS SQL 2012+

#### MySQL Adapter
- Connection via `go-sql-driver/mysql`
- Export/Import with full MySQL type mapping (INT, DECIMAL, VARCHAR, DATETIME, TINYINT, BLOB, etc.)
- TDTQL → SQL push-down: WHERE, ORDER BY, LIMIT/OFFSET, IN, bracket-quoted identifiers
- All import strategies: replace (INSERT ... ON DUPLICATE KEY UPDATE), ignore (INSERT IGNORE), fail, copy
- Bracket-quoted table/field names with spaces and `$` (NAV/BC/ERP style)
- Compact format, compression (zstd/kanzi), hash verification
- Roundtrip MySQL → MySQL and MySQL → SQLite verified: **58/58 CLI integration tests pass**
- Docker-based test environment: `docker compose up -d mysql`

#### MS Access Adapter (Windows)
- Connection via ODBC (`alexbrainman/odbc`)
- Schema via ADOX COM for exact Access catalog types (Text, Number, Date/Time, etc.)
- Fallback to sample-row type inference when ADOX unavailable
- `SELECT * FROM [TableName]` with bracket-quoted names
- Windows-only (build tag `windows`)
- Supports `.mdb` and `.accdb` files

---

### Message Brokers

#### RabbitMQ
- Publish/Consume TDTP packets
- Manual ACK for reliable delivery
- Queue parameters (durable, auto_delete, exclusive)
- Tested with PostgreSQL adapter

#### MSMQ (Windows)
- Windows Message Queue integration
- Transactional queues support
- Tested with MS SQL adapter

#### Kafka (Production-Ready since v1.9.0)
- High-throughput message streaming
- Producer/Consumer with manual commit
- Configurable partitioning and consumer groups
- Stats and offset management (replay capability)
- **`SendBatch`**: all packets sent in a single network roundtrip
- **Parallel compress + serialize**: concurrent goroutines with per-goroutine generators
- **Parallel decompression** on import: packets 2…N decompressed concurrently
- **`--output` mode**: save received packets as `base_part_N_of_Total.tdtp.xml` files
- **`--raw` flag**: save broker messages verbatim (no parse/decompress/validate)
- **`--keep` flag**: non-atomic mode — import each part immediately as it arrives
- **Atomic multi-part import** (default): all parts in one transaction — all-or-nothing
- `StartOffset: kafka.FirstOffset` — fixes race during consumer group rebalance
- `BatchBytes` raised to 100 MB, `BatchTimeout` lowered to 5ms
- Tested with PostgreSQL adapter

#### Performance (50k rows, 5 packets, localhost)

| Mode | Export | Import→files | Traffic |
|------|--------|-------------|---------|
| No compression | 3.4s | 3.8s | 7.2 MB |
| zstd level 3 | 3.5s | 3.9s | 2.4 MB (3×) |
| kanzi level 6 | 3.6s | 3.9s | 1.8 MB (4×) |

---

### Resilience & Production Features

#### CircuitBreaker (`pkg/resilience`)
- Three states: Closed, Half-Open, Open
- Automatic recovery with configurable timeout
- Concurrent call limiting
- Success threshold for recovery
- State change callbacks
- Custom trip logic
- Circuit Breaker groups
- 13 comprehensive tests

#### AuditLogger (`pkg/audit`)
- Multiple appenders: File, Database, Console
- Three logging levels: Minimal, Standard, Full (GDPR compliance)
- Async/Sync modes with configurable buffering
- File rotation with size limits and backups
- Database storage with SQL support (batch inserts)
- Query, filter, and cleanup operations
- Builder pattern for fluent entry creation
- Thread-safe concurrent operations
- GDPR/HIPAA/SOX compliance features
- 17 comprehensive tests

#### Retry Mechanism (`pkg/retry`)
- Three backoff strategies: Constant, Linear, Exponential
- Jitter support to prevent thundering herd
- Configurable retryable errors
- Context-aware cancellation
- `OnRetry` callbacks for monitoring
- Dead Letter Queue (DLQ) support
- 20 comprehensive tests

#### IncrementalSync (`pkg/sync`)
- `StateManager` with checkpoint tracking
- Three tracking strategies: Timestamp, Sequence, Version
- Batch processing with configurable sizes
- Resume from last checkpoint
- 200x faster for large tables

---

### Data Processors (`pkg/processors`)

- **CompressionProcessor**: zstd compression/decompression (levels 1–22, default 3)
  - Automatic base64 encoding for safe transport
  - Multi-threaded processing (up to 4 cores)
  - Compression threshold (default 1 KB)
  - Compression statistics (ratio, time)
  - Integration with packet generator/parser
- **FieldMasker**: Email, phone, card masking (GDPR/PII)
- **FieldValidator**: Regex, range, format validation
- **FieldNormalizer**: Email, phone, date normalization
- **Processor chain**: Processor chains for complex transformations

---

### XLSX Converter (`pkg/xlsx`)

- TDTP → XLSX export (Database → Excel for business analysis)
- XLSX → TDTP import (Excel → Database bulk loading)
- Type preservation (INTEGER, REAL, BOOLEAN, DATE, DATETIME, etc.)
- Formatted headers with field types and primary keys
- Auto-formatting (numbers, dates, booleans)
- Business-friendly interface (no SQL knowledge required)
- Round-trip data integrity
- **Excel data-integrity traps handled automatically (v1.3.1+):**
  - BIGINT > 15 digits → string cell (IEEE-754 float64 precision loss prevention)
  - NaN / ±Inf → blank cell (Excel has no numeric NaN/Inf; text breaks `=SUM()`)
  - Dates < 1900-01-01 → ISO text string (Excel serial starts at Jan 1, 1900)
  - 1900 leap-year bug compensation on import (phantom serial 60 = Feb 29, 1900)
  - Formula injection prevention via `SetCellStr` (`=`, `+`, `-`, `@` stored as literal text)
  - Error cells (`#N/A`, `#DIV/0!`, `#NUM!`, etc.) → canonical NULL on import
  - All imported strings trimmed (Excel trailing-space formatting artifact)

---

### HTML Viewer (`pkg/html`)

- TDTP → HTML conversion for quick browser-based data preview
- Row range support (`--row 100-500`)
- Tail-mode preview (`--limit -50` — last 50 rows)
- Combined range and limit
- Open in browser with a single command (`--open`)

---

### CSV Converter (`pkg/csv`)

- TDTP → CSV conversion for interoperability with data tools and pipelines
- Encrypted input (`.tdtp.enc`) auto-decrypted before conversion
- v1.4 integrity gate applied automatically

---

### SVG Support (`pkg/svg`)

- SVG → TDTP: parse SVG into tabular rows (element tag, attributes, hierarchy)
- TDTP → SVG: reconstruct SVG from rows with full round-trip fidelity
- Namespace-aware: `xlink:href`, `inkscape:label` and other URI-namespaced attributes
  preserved correctly across encode/decode cycles
- Designed for SVG data pipelines, diff, and batch transformations

---

### Diff & Merge (`pkg/diff`)

- Compare two TDTP files (added / modified / deleted)
- Configurable key fields (`--key-fields`)
- Field exclusion from comparison (`--ignore-fields`)
- Case-sensitive/insensitive comparison
- Five merge strategies: union, intersection, left, right, append
- Detailed conflict report (`--show-conflicts`)

---

### ETL Pipeline Processor (`pkg/etl`)

Multi-Database ETL with 4-level security.

**Key capabilities:**
- Multiple sources: PostgreSQL, MS SQL Server, MySQL, SQLite
- Parallel loading: all sources loaded simultaneously
- SQLite `:memory:` workspace: fast JOIN operations without disk I/O
- SQL transformations: full SQL power for data processing
- Multiple outputs: TDTP XML, RabbitMQ, Kafka
- 4-level security: READ-ONLY by default, protection against accidental data modification
- Detailed statistics: execution time, row counts, errors

**ETL Components:**
- `Loader` (`pkg/etl/loader.go`): parallel loading from sources
- `Workspace` (`pkg/etl/workspace.go`): SQLite `:memory:` management for JOINs
- `Executor` (`pkg/etl/executor.go`): SQL transformation execution
- `Exporter` (`pkg/etl/exporter.go`): export to TDTP/RabbitMQ/Kafka
- `Processor` (`pkg/etl/processor.go`): main ETL orchestrator

**Security (4 levels):**
1. **Code level**: `SQLValidator` blocks forbidden operations (INSERT, UPDATE, DELETE, DROP)
2. **OS level**: `IsAdmin()` checks administrator privileges for unsafe mode
3. **CLI level**: READ-ONLY by default, `--unsafe` requires explicit flag
4. **SQL level**: only SELECT/WITH in safe mode, all operations in unsafe

**Operating modes:**
- **Safe mode** (default): SELECT/WITH only, no admin rights required
- **Unsafe mode** (`--unsafe`): all SQL operations, requires administrator privileges

**Configuration example:**

```yaml
name: "Multi-DB Report"
sources:
  - name: pg_users
    type: postgres
    dsn: "postgres://localhost/db1"
    table_alias: users
    query: "SELECT * FROM users WHERE active = true"

  - name: mssql_orders
    type: mssql
    dsn: "server=localhost;database=orders;user id=sa"
    table_alias: orders
    query: "SELECT * FROM orders WHERE year = 2024"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "report"
  sql: |
    SELECT
      u.username,
      COUNT(o.order_id) as total_orders,
      SUM(o.amount) as total_spent
    FROM users u
    LEFT JOIN orders o ON u.user_id = o.user_id
    GROUP BY u.username
    ORDER BY total_spent DESC

output:
  type: TDTP
  tdtp:
    destination: "report.xml"
    compress: true
```

Documentation: [`docs/ETL_PIPELINE.md`](docs/ETL_PIPELINE.md)

---

### Object Storage (`pkg/storage`)

S3-compatible object storage support (AWS S3, SeaweedFS, MinIO, Ceph).

**Supported operations for all CLI commands:**

| Command | S3 input | S3 output |
|---------|----------|-----------|
| `--export` | — | `--output s3://bucket/key.xml` |
| `--import` | `--import s3://bucket/key.xml` | — |
| `--inspect` | `--inspect s3://bucket/key.xml` | — |
| `--to-xlsx` | `--to-xlsx s3://bucket/in.xml` | `--output s3://bucket/out.xlsx` |
| `--export-xlsx` | — | `--output s3://bucket/out.xlsx` |
| ETL `tdtp-s3` source | DSN `s3://bucket/key.xml` | — |

**Multi-part sets**: when importing `s3://bucket/table.tdtp.xml`, all parts
`table.tdtp_part_1_of_N.xml … table.tdtp_part_N_of_N.xml` are discovered
automatically via prefix listing — same as local file discovery.

**All export flags work transparently with S3:**
- `--compress`, `--compress-level`, `--hash` — zstd + XXH3 checksum
- `--where`, `--fields`, `--limit`, `--order-by` — filtering before upload
- `--compact` — v1.3.1 compact format

**Configuration (`config.yaml`):**

```yaml
storage:
  type: s3
  s3:
    endpoint: ""           # empty = AWS S3; set for SeaweedFS/MinIO/Ceph
    region: us-east-1
    bucket: my-bucket
    access_key: AKIAIOSFODNN7EXAMPLE
    secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    disable_ssl: false     # set true for local HTTP endpoints
```

The `s3://bucket/key` URI in `--output` / `--import` overrides the `bucket` field in config,
so one config can address multiple buckets.

**ETL pipeline source `tdtp-s3`:**

```yaml
sources:
  - name: users_archive
    type: tdtp-s3
    dsn: "s3://my-bucket/exports/users.tdtp.xml"
    multi_part: true       # auto-discover all _part_N_of_M files
    s3:
      endpoint: "http://seaweedfs:8333"
      region: us-east-1
      access_key: my_key
      secret_key: my_secret
      disable_ssl: true
```

**Build tags:** the S3 driver is included by default; exclude with `-tags nos3` to
drop the `aws-sdk-go-v2` dependency for minimal builds.

---

### Encryption & Zero Trust (`pkg/crypto`, `pkg/mercury`)

**Philosophy:** nothing to protect if data disappears immediately after delivery.

#### Standalone `--enc` tier (v1.9.6)

Encrypt any export and auto-decrypt on any consumer command — no pipeline required.

**Producer:**
```bash
# Export encrypted — output renamed to .tdtp.enc automatically
tdtpcli --export financials --enc --output financials.tdtp.xml
# → writes financials.tdtp.enc (AES-256-GCM, burn-on-read key via xZMercury)
```

**Consumer (auto-detect by `.tdtp.enc` extension):**
```bash
tdtpcli --import      financials.tdtp.enc     # decrypt → parse → load to DB
tdtpcli --to-csv      financials.tdtp.enc     # decrypt → CSV
tdtpcli --to-xlsx     financials.tdtp.enc     # decrypt → Excel
tdtpcli --to-html     financials.tdtp.enc     # decrypt → HTML viewer
```

**Zero Trust properties:**
- Keys are never stored on disk — RAM only (Redis via xZMercury)
- **Burn-on-read**: key physically destroyed after first retrieval (Redis `GETDEL`)
- **One shot** — key lives from bind to retrieve, typically ~300 ms
- **HMAC-SHA256** — binding signature prevents key substitution in transit
- **UUID isolation** — each packet gets a unique key; one compromised key reveals nothing else

**xZMercury key lifecycle:**

```
Producer               xZMercury (Redis)            Consumer
────────               ─────────────────            ────────
GenerateUUID ────────► POST /api/keys/bind
                            ├─ AES-256 key in RAM
                            ├─ Bound to packet UUID
                            └─ HMAC-SHA256 signature
Encrypt(key, data) ◄── {key_b64, hmac}

                                                ExtractUUID(blob)
                                                POST /api/keys/retrieve
                            ├─ Credential check
                            └─ GETDEL → key destroyed ──► key_b64

                                                Decrypt(key, blob)
```

#### AES-256-GCM (`pkg/crypto`)
- Authenticated encryption (AEAD) — data cannot be tampered with undetected
- Unique nonce from `crypto/rand` per packet (replay attacks impossible)
- Binary format: `[2B version][1B algo][16B UUID][12B nonce][ciphertext + 16B GCM tag]`
- Packet UUID embedded in header — recipient retrieves key without decrypting body

#### Pipeline encryption (`pkg/mercury` + xZMercury)

For ETL pipelines, encryption is configured in YAML:

```yaml
output:
  tdtp:
    encryption: true

security:
  mercury_url: "http://mercury:3000"
  recipient_resource: "ETL_RESULTS"
  key_ttl_seconds: 86400
  mercury_timeout_ms: 5000
```

**Graceful degradation when xZMercury is unavailable:**

```
Mercury unavailable
  → business data discarded
  → error packet (MERCURY_UNAVAILABLE) written to tdtp_errors
  → pipeline completes normally (exit 0)
```

**Error codes in `tdtp_errors`:**

| Code | Cause |
|------|-------|
| `MERCURY_UNAVAILABLE` | timeout / connection refused |
| `MERCURY_ERROR` | HTTP 5xx from xZMercury |
| `HMAC_VERIFICATION_FAILED` | key signature mismatch (substitution attempt?) |
| `KEY_BIND_REJECTED` | quota exceeded or ACL denied |

---

### v1.4 Integrity Gate (Security)

All consumer commands apply a security gate after decompression.

**For v1.4 packets** (produced with `--integrity`):
- XXH3-128 row-level hashes verified against the packet header
- Optional full executor verification via xZMercury (`--mercury-url`)
- Policy: `FallbackDegrade` — integrity failure blocks the packet, not the pipeline

**For pre-v1.4 packets** (v1.0, v1.3.x):
- Gate is a no-op — backward compatibility preserved
- Old packets pass through all commands unchanged

**Commands with the gate applied:**
`--import`, `--import-broker`, `--listen`, `--to-csv`, `--to-xlsx`, `--to-html`

---

## CLI Utility (`tdtpcli`)

### Commands

**Database:**
```
--list                     List all tables (supports glob: --list=user*)
--list-views               List database views (U* updatable, R* read-only)
--export <table>           Export table/view to TDTP XML
--import <file>            Import TDTP XML into database
--inspect-table <table>    Inspect live DB table: native types, FKs, row count, sample
```

**File:**
```
--inspect <file>           Print YAML metadata summary of a TDTP file (no config needed)
--test <file>              Verify file integrity: checksum, row count, multi-part completeness
--diff <file-a> <file-b>   Compare two TDTP files
--merge <files>            Merge multiple TDTP files
--to-html <file>           Convert TDTP to HTML viewer
--to-csv <file>            Convert TDTP to CSV
```

**Object Storage (S3):**
```
--export <table> --output s3://bucket/key.xml   Export to S3 (multi-part automatic)
--import s3://bucket/key.xml                    Import from S3 (multi-part auto-discovered)
--inspect s3://bucket/key.xml                   Inspect packet from S3
--to-xlsx s3://bucket/in.xml --output s3://...  Convert S3 TDTP → S3 XLSX
--export-xlsx <table> --output s3://bucket/k    Export table → XLSX directly to S3
```

**XLSX:**
```
--to-xlsx <tdtp-file>      TDTP → XLSX
--from-xlsx <xlsx-file>    XLSX → TDTP
--export-xlsx <table>      Table → XLSX (directly, no intermediate XML)
--import-xlsx <xlsx-file>  XLSX → Database (directly)
```

**Broker:**
```
--export-broker <table>    Export to message broker (parallel compress + SendBatch)
--import-broker            Import from message broker (parallel decompress, atomic by default)
--import-broker --output   Save as TDTP files instead of importing to DB
--import-broker --raw      Save broker messages verbatim (no parse/decompress)
--import-broker --keep     Non-atomic mode: import each part immediately
--listen                   Streaming consumer daemon (Kafka only, production-ready)
```

**Cross-system Mapping (`--map`):**
```
--map <mapping.yaml>                       One-shot: read one packet, apply field remap, upsert into target DB
--map <yaml> --input <file>                Read from local TDTP file
--map <yaml> --input s3://bucket/key       Read from S3-compatible object storage
--map <yaml> --input broker://queue        One-shot from broker: one packet, ACK on success, exit
--map <yaml> --input broker://queue        Daemon mode: keep connection open, process messages in a loop;
    --listen                               ACK each packet only after successful upsert, NACK+requeue on
                                           error, graceful shutdown via SIGTERM/SIGINT
--map <yaml> --dry-run                     Validate mapping without writing to DB
--map <yaml> --mercury-url <url>           Decrypt .enc input via xZMercury before mapping
```

**ETL:**
```
--sync-incremental <table> Incremental table synchronization
--pipeline <file>          Run ETL pipeline from YAML config
```

### Options

**General:**
```
--config <file>            Configuration file (default: config.yaml)
--output <file>            Output file path
--table <name>             Target table name (overrides name from XML on import)
--strategy <name>          Import strategy: replace, ignore, fail, copy
--batch <size>             Batch size for bulk operations (default: 1000)
--readonly-fields          Include read-only fields (timestamp, computed, identity)
```

**Compression:**
```
--compress                 Enable compression for exported data
--compress-algo <algo>     Algorithm: zstd (default) or kanzi (4× denser than raw)
--compress-level <n>       Compression level: 1-19 (zstd) or 6-7 (kanzi), default: 3
--hash                     Add XXH3 checksum for integrity verification (requires --compress)
--packet-size <MB>         Max broker packet size in MB (default 0 = ~1.9MB; use 8 for kanzi)
--fast                     Skip NULL/NaN/Inf detection for maximum throughput
```

**Encryption:**
```
--enc                      Encrypt output with AES-256-GCM via xZMercury (burn-on-read keys)
                           Output file renamed: .tdtp.xml → .tdtp.enc
                           Consumer commands (--import, --to-csv, --to-xlsx, --to-html)
                           auto-detect .tdtp.enc and decrypt transparently.
--mercury-url <url>        xZMercury server URL (overrides config); enables full executor
                           verification for v1.4 integrity packets
```

**Field Name Sanitization (--import only):**
```
--clear                    Replace special chars in field names with safe SQL identifiers
                           Symbol map: % → _pct, $ → _usd, # → _xh, @ → _at, * → _star,
                                       & → _and, ? → _is, ~ → _not, + → _plus, = → _eq,
                                       ! → _bang, ^ → _hat, < → _lt, > → _gt,
                                       space . , - / \ ` : | ; → _
--translit                 Transliterate non-ASCII field names to ASCII (go-unidecode)
                           Cyrillic: "Name" → "Imia", "Date" → "Data_rozhdeniia"
                           European: "Österreich" → "Osterreich", "Ñoño" → "Nono"
                           Original names preserved as DB column comments (PG/MySQL)
```

**TDTQL Filters:**
```
--where <condition>        WHERE condition; bracket-quoted identifiers for names with
                           spaces or special chars: --where '[Termination Date] = "1753-01-01"'
                           Operators: = != < > >= <= IN NOT IN BETWEEN LIKE IS NULL IS NOT NULL
                           Single:    --where 'age > 18'
                           IN list:   --where 'status IN (active,pending)'
                           Multiple:  --where 'dept_id IN (10,11)' --where 'salary > 50000'
--order-by <fields>        ORDER BY (e.g. 'name ASC, age DESC')
--limit <n>                Row limit: +N = first N, -N = last N (like tail)
--offset <n>               Skip N rows
--fields <col1,col2,...>   Column projection: export/import only listed columns
                           Bracket-quoted names for fields with spaces: --fields "id,[Birth Date]"
                           On --export/--export-broker/--export-xlsx: SELECT col1,col2 FROM ...
                           On --import: whitelist — only these columns written to DB
                           On --sync-incremental: tracking field auto-included
```

**HTML Viewer:**
```
--open                     Open in browser after conversion
--row <range>              Row range (e.g. 100-500)
```

**XLSX:**
```
--sheet <name>             Excel sheet name (default: Sheet1)
```

**Incremental Sync:**
```
--tracking-field <field>   Field to track changes (default: updated_at)
--checkpoint-file <file>   Checkpoint file (default: checkpoint.yaml)
--batch-size <size>        Sync batch size (default: 1000)
```

**ETL:**
```
--unsafe                   Unsafe mode (all SQL operations, requires admin)
```

**Diff:**
```
--key-fields <fields>      Key fields for comparison (comma-separated)
--ignore-fields <fields>   Fields to ignore during comparison (comma-separated)
--case-sensitive           Case-sensitive comparison (default: false)
```

**Merge:**
```
--merge-strategy <name>    Strategy: union, intersection, left, right, append
                           (default: union)
--show-conflicts           Show detailed conflict information
```

**Data Processors:**
```
--mask <fields>            Mask sensitive fields (comma-separated)
--validate <file>          Field validation (YAML rules file)
--normalize <file>         Field normalization (YAML rules file)
```

**Configuration:**
```
--create-config-pg         Create PostgreSQL config template
--create-config-mssql      Create MS SQL config template
--create-config-sqlite     Create SQLite config template
--create-config-mysql      Create MySQL config template
```

**Misc:**
```
--version                  Show version
-h                         Brief help
--help                     Full help with examples
```

### Working with Views

`tdtpcli --list-views` shows all views with markers:
- `U*` = Updatable view (can be imported)
- `R*` = Read-only view (export only)

- `--export` supports all database views
- `--import` works only with updatable views

---

## Architecture

```
tdtp-framework/
├─ pkg/core/
│  ├─ packet/            TDTP packet parsing/generation + compression
│  ├─ schema/            Type validation, Converter, Builder
│  └─ tdtql/             Translator, Executor, SQL Generator
│
├─ pkg/adapters/
│  ├─ adapter.go         Universal interface
│  ├─ factory.go         Adapter factory
│  ├─ sqlite/            SQLite adapter (modernc.org/sqlite)
│  ├─ postgres/          PostgreSQL adapter (pgx/v5)
│  ├─ mssql/             MS SQL Server adapter (go-mssqldb)
│  ├─ mysql/             MySQL adapter (go-sql-driver/mysql)
│  └─ access/            MS Access adapter (ODBC + ADOX, Windows-only)
│
├─ pkg/processors/       Data processing and transformation
│  ├─ compression.go     zstd/kanzi compression/decompression
│  ├─ field_masker.go    PII masking (email, phone, card)
│  ├─ field_validator.go Field validation (regex, range, format)
│  ├─ field_normalizer.go Data normalization
│  ├─ chain.go           Processor chains
│  └─ factory.go         Processor factory
│
├─ pkg/sanitize/         Field name sanitization (--translit, --clear)
│  └─ sanitizer.go       Symbol map + go-unidecode transliteration
│
├─ pkg/security/         Security system
│  ├─ privileges.go      IsAdmin() for Unix/Windows
│  └─ validator.go       SQL validator (safe/unsafe modes)
│
├─ pkg/crypto/           AES-256-GCM TDTP packet encryption
│  └─ encryption.go      Encrypt/Decrypt/ExtractUUID with UUID-binding
│
├─ pkg/mercury/          xZMercury client (Zero Trust keys)
│  ├─ client.go          BindKey / RetrieveKey / VerifyHMAC (burn-on-read)
│  └─ types.go           KeyBinding, error codes (MERCURY_UNAVAILABLE etc.)
│
├─ pkg/etl/              ETL Pipeline processor
│  ├─ config.go          YAML configuration with validation
│  ├─ workspace.go       SQLite :memory: workspace management
│  ├─ loader.go          Parallel loading from sources
│  ├─ executor.go        SQL transformation execution
│  ├─ exporter.go        Export to TDTP/RabbitMQ/Kafka
│  └─ processor.go       Main ETL orchestrator
│
├─ pkg/resilience/       Circuit Breaker pattern
│  └─ circuit_breaker.go Protection against cascading failures
│
├─ pkg/audit/            Audit Logger
│  ├─ logger.go          Audit system (File, DB, Console)
│  └─ appenders.go       Log appenders
│
├─ pkg/retry/            Retry mechanism
│  └─ retry.go           Backoff strategies
│
├─ pkg/sync/             Incremental Sync
│  └─ state_manager.go   Incremental synchronization
│
├─ pkg/xlsx/             Excel integration
│  └─ converter.go       TDTP ↔ XLSX converter
│
├─ pkg/svg/              SVG integration
│  ├─ parser.go          SVG → tabular rows (namespace-aware)
│  └─ writer.go          Tabular rows → SVG (full round-trip)
│
├─ pkg/brokers/
│  ├─ broker.go          Broker interface
│  ├─ rabbitmq.go        RabbitMQ integration
│  ├─ kafka.go           Kafka integration
│  └─ msmq.go            MSMQ integration (Windows)
│
├─ pkg/storage/          Object storage abstraction
│  ├─ storage.go         ObjectStorage interface (Put/Get/List/Stat/Delete)
│  ├─ factory.go         Driver registry + ParseURI / IsRemote helpers
│  └─ s3/                S3 driver (aws-sdk-go-v2, UsePathStyle)
│                        Compatible: AWS S3, SeaweedFS, MinIO, Ceph RGW
│
├─ xzmercury/            Zero Trust key server (embedded)
│  └─ internal/          API, hashstore, quota, metrics
│
├─ cmd/tdtpcli/          CLI utility
│  ├─ main.go            Entry point
│  ├─ help.go            Help information
│  ├─ config.go          YAML configuration
│  ├─ processors.go      Processor integration
│  └─ commands/          Command handlers
│     ├─ security.go     v1.4 integrity gate (applyV14SecurityGate)
│     └─ encrypt.go      --enc tier (EncryptPacket / DecryptEncFile)
│
├─ docs/                 Documentation
│  ├─ SPECIFICATION.md   TDTP v1.0 specification
│  ├─ PACKET_MODULE.md   Packet documentation
│  ├─ SCHEMA_MODULE.md   Schema documentation
│  ├─ TDTQL_TRANSLATOR.md TDTQL documentation
│  ├─ SQLITE_ADAPTER.md  SQLite documentation
│  └─ ETL_PIPELINE_GUIDE.md ETL guide
│
├─ examples/             Production-ready examples
│  ├─ 01-basic-export/   PostgreSQL → TDTP XML export
│  ├─ 02-rabbitmq-mssql/ MSSQL → RabbitMQ (Circuit Breaker + Audit)
│  ├─ 03-incremental-sync/ PostgreSQL → MySQL incremental sync
│  ├─ 04-tdtp-xlsx/      Database ↔ Excel converter
│  ├─ 04-audit-masking/  Compliance: Audit logging + PII masking
│  ├─ 05-circuit-breaker/ API resilience patterns
│  └─ 06-etl-pipeline/   Complete ETL pipeline
│
└─ scripts/              Helper scripts
   ├─ create_sqlite_test_db.py
   ├─ create_postgres_test_db.py
   └─ README.md
```

---

## Quick Start

### Installation

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
```

### Build CLI

```bash
go build -o tdtpcli ./cmd/tdtpcli
```

### CLI Usage Examples

See the [User Guide](docs/USER_GUIDE.md#cli-usage-examples) for the full command reference.

```bash
# Discover → verify → load
tdtpcli --inspect delivery.xml
tdtpcli --test    delivery.xml
tdtpcli --import  delivery.xml

# Export with encryption (AES-256-GCM, burn-on-read)
tdtpcli --export financials --enc --output financials.tdtp.xml
tdtpcli --import financials.tdtp.enc   # auto-decrypted

# Export with compression + filter + checksum
tdtpcli --export orders \
  --where 'status = active' --compress --compress-algo kanzi --hash \
  --output orders.xml

# Export to Excel / S3
tdtpcli --export-xlsx orders --output orders.xlsx
tdtpcli --export users --output s3://my-bucket/exports/users.tdtp.xml
```

### Broker-native EDA (event-driven architecture)

Since v1.16.0 `--map --listen` turns any mapping YAML into a standalone daemon process.
One process per queue — no Python middleware or coordinator required.

**One-shot** (classic, coordinator-driven):
```bash
# coordinator subscribes to Redis pub/sub, spawns tdtpcli per notification
tdtpcli --map mappings/sync_flights.yaml --input broker://tdtp.sync.flights
```

**Daemon** (new, v1.16.0):
```bash
# each entity runs as its own process — stays connected until SIGTERM
tdtpcli --map mappings/sync_flights.yaml \
        --input broker://tdtp.sync.flights \
        --listen

# start all queues in parallel (systemd, Docker Compose, or background jobs)
tdtpcli --map mappings/sync_countries.yaml  --input broker://tdtp.sync.countries  --listen &
tdtpcli --map mappings/sync_tours.yaml      --input broker://tdtp.sync.tours      --listen &
tdtpcli --map mappings/sync_reservations.yaml --input broker://tdtp.sync.reservations --listen &
```

**How it works:**
- Opens one persistent broker connection per daemon instance
- `receive → decrypt → parse → decompress → field-remap → upsert → ACK` for every message
- On parse/execute error: NACK with requeue (message returns to queue for retry)
- Graceful shutdown: SIGTERM/SIGINT finishes the current message then exits
- Progress printed per message: `[map:listen] ✓  rows=42     total=420    18ms`

**Mapping YAML with inline broker config:**
```yaml
id: sync-flights-v1
input_source:
  broker:
    type:     rabbitmq
    host:     localhost
    port:     5672
    user:     guest
    password: guest
    queue:    tdtp.sync.flights
    durable:  true

target_connection:
  dsn: "host=central port=5432 dbname=tdtp user=tdtp password=secret sslmode=disable"

targets:
  - table: public.flights
    upsert_key: flight_id
    fields: [flight_id, route_id, aircraft_type, departure_time, arrival_time, status]
```

The source sends via `--export-broker`; each daemon consumes its own queue independently:
```bash
# source side (branch office or upstream system)
tdtpcli --export-broker flights --config branch.yaml
```

### Using in Code

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// Create schema
schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "ID", Type: "INTEGER", Key: true},
        {Name: "Name", Type: "TEXT", Length: 200},
        {Name: "Balance", Type: "DECIMAL"},
    },
}

// Generate packet
generator := packet.NewGenerator()
packets, err := generator.GenerateReference("Companies", schema, rows)

// Save
generator.WriteToFile(packets[0], "reference.xml")

// Parse
parser := packet.NewParser()
pkt, err := parser.ParseFile("reference.xml")
```

### Using Compression

```go
import (
    "github.com/ruslano69/tdtp-framework/pkg/core/packet"
    "github.com/ruslano69/tdtp-framework/pkg/processors"
)

generator := packet.NewGenerator()
generator.SetCompression(packet.CompressionOptions{
    Enabled:   true,
    Level:     3,      // 1 (fast) — 19 (best compression)
    MinSize:   1024,   // Minimum size for compression (bytes)
    Algorithm: "zstd",
})

packets, err := generator.GenerateReference("LargeTable", schema, rows)

// Direct usage
compressed, stats, err := processors.Compress([]byte("large data"), 3)
decompressed, err := processors.Decompress(compressed)
```

### Using Adapters

```go
import (
    "context"
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
)

ctx := context.Background()

adapter, err := adapters.New(ctx, adapters.Config{
    Type: "postgres",
    DSN:  "postgres://localhost/mydb",
})
defer adapter.Close(ctx)

// Export: DB → TDTP
packets, err := adapter.ExportTable(ctx, "users")

// Import: TDTP → DB
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
```

### Ready-to-Run Examples

```bash
# Database ↔ Excel converter
cd examples/04-tdtp-xlsx
go run main.go

# PostgreSQL → TDTP XML export
cd examples/01-basic-export
go run main.go

# RabbitMQ + MSSQL (Circuit Breaker, Audit, Retry)
cd examples/02-rabbitmq-mssql
go run main.go

# RabbitMQ + MS SQL + ETL Pipeline
cd examples/02b-rabbitmq-mssql-etl
go run main.go

# Incremental Sync (200x faster for large tables)
cd examples/03-incremental-sync
go run main.go

# Complete ETL pipeline
cd examples/06-etl-pipeline
go run main.go

# ETL with encryption (xZMercury + AES-256-GCM)
cd examples/08-pipeline-encrypted
go run main.go

# S3 pipeline chain (extract → split by region)
cd examples/09-s3-pipeline-chain
./run_chain.sh
```

Examples documentation: [`examples/README.md`](examples/README.md)

---

## Documentation

### Guides
- [User Guide](docs/USER_GUIDE.md) — complete CLI utility guide with all command examples
- [ETL Pipeline Guide](docs/ETL_PIPELINE.md) — ETL pipeline configuration and usage
- [Developer Guide](docs/DEVELOPER_GUIDE.md) — internals, extending the framework
- [Access Adapter](docs/ACCESS_ADAPTER.md) — MS Access specifics (ODBC + ADOX)

### Technical Specifications
- [TDTP Specification](docs/SPECIFICATION.md) — TDTP protocol specification (v1.0 – v1.4)

### Package READMEs
- [Circuit Breaker](pkg/resilience/README.md) — protection against cascading failures
- [Audit Logger](pkg/audit/README.md) — compliance and security
- [XLSX Converter](pkg/xlsx/README.md) — Database ↔ Excel

---

## Testing

```bash
# Run all tests
go test ./...

# With coverage
go test -cover ./...

# Verbose for specific package
go test -v ./pkg/core/packet/
```

---

## Roadmap

### v1.0 — v1.3 (completed)
- Packet, Schema, TDTQL modules
- SQLite, PostgreSQL, MS SQL adapters
- RabbitMQ, MSMQ, Kafka brokers
- CLI utility with TDTQL filters
- CircuitBreaker, AuditLogger, Retry mechanism
- IncrementalSync, Data Processors
- XLSX Converter (Database ↔ Excel)
- ETL Pipeline Processor with 4-level security
- MySQL adapter — integration-tested, 58/58 CLI tests pass
- Full documentation

### v1.3.1 (completed)
- **Compact format**: fixed fields written once per packet, omitted from each data row
  - `RowsToCompactData` / `ExpandCompactRows` in the packet core
  - `--compact` flag on export (works with `--fixed-fields` or `_`-prefix convention)
  - `--to-compact` CLI command for post-hoc conversion of existing TDTP files
  - Auto-detection of fixed fields: explicit list → `_` prefix → data analysis
  - Automatic expand on `--import` and all parser paths (transparent backwards compatibility)
- **SpecialValues**: protocol-level markers for values that cannot be expressed standardly
  - `[NULL]` — TEXT NULL vs empty string `""` (distinct semantics preserved across adapters)
  - `NaN`, `INF`, `-INF` — IEEE-754 special floats (PostgreSQL stores natively; others → NULL)
  - `0000-00-00` — NoDate sentinel for DATE fields (MySQL zero-date, historical databases)
  - Auto-detection on export from all adapters (PostgreSQL `NaN`/`Inf`, MSSQL zero-dates)
  - Python/pandas binding: markers applied before `astype()` to prevent `ValueError` crashes
  - XLSX adapter: full trap matrix for all 5 markers (see XLSX Converter section)

### v1.6.0 (completed)
- HTML Viewer (`--to-html`, `--open`, `--row`)
- Diff & Merge (`--diff`, `--merge`, `--merge-strategy`, `--show-conflicts`)
- Extended XLSX commands (`--from-xlsx`, `--export-xlsx`, `--import-xlsx`)
- Incremental sync via CLI (`--sync-incremental`)
- Data Processors in CLI (`--mask`, `--validate`, `--normalize`)
- Tail mode in limit (`--limit -N`)
- `--batch`, `--readonly-fields` options
- Zero Trust encryption: AES-256-GCM + xZMercury (burn-on-read keys, graceful degradation)

### v1.7.1 (completed)
- `--where` conditions for SQL filtering on export (repeatable flag, `IN (...)` support)
- `--fields` column projection: export only specified columns
- `--inspect` command: display TDTP file structure and metadata without full parse
- `--compact` format support: carry-forward encoding for repeated field values
- TDTP XML v1.3.1 spec: special values `[NULL]`, `[+INF]`, `[-INF]`, `[NaN]` with full cross-adapter support

### v1.8.0 (completed)
- **Object Storage (S3)** — `pkg/storage` with driver registry and `aws-sdk-go-v2`
  - `--export … --output s3://bucket/key` — upload multi-part TDTP to S3
  - `--import s3://bucket/key` — download + auto-discover all `_part_N_of_M` files
  - `--inspect s3://bucket/key` — inspect packet metadata from S3 (in-memory, no temp file)
  - `--to-xlsx / --export-xlsx … --output s3://` — XLSX output directly to S3 (temp file auto-deleted)
  - `--to-xlsx s3://… --output s3://…` — S3 TDTP → S3 XLSX (temp download + upload)
  - ETL pipeline source type `tdtp-s3`: load compressed multi-part TDTP sets from S3 into workspace
  - All existing flags transparent: `--compress`, `--hash`, `--where`, `--fields`, `--compact`
  - Compatible with SeaweedFS, MinIO, Ceph RGW, AWS S3 (path-style and virtual-hosted)
  - Build tag `nos3` to exclude driver and drop `aws-sdk-go-v2` dependency
- **`--test`** — file integrity verification (checksum, row count, multi-part, decompression)
- **`compress_algo`** YAML config field — set default algorithm in config file
- **CLI integration tests** — `tests/cli/test_sqlite.py` (31 tests), `tests/cli/test_postgres.py` (32 tests)

### v1.8.1 (completed)
- **Field Name Sanitizer** (`--translit`, `--clear`) — `pkg/sanitize`
  - `--clear`: symbol map replacement (`%` → `_pct`, `$` → `_usd`, `&` → `_and_`, etc.)
  - `--translit`: non-ASCII transliteration via go-unidecode (Cyrillic, European diacritics)
  - Combined mode: `--translit` runs first, then `--clear`
  - Applied **only on `--import`** — `--export` preserves original field names
  - Original names preserved as DB column comments (PostgreSQL, MySQL)
- **Bracket-quoted identifiers** in `--where` and `--fields`
  - `[Field Name]` syntax for names with spaces or special chars (MSSQL/Access style)
  - Brackets stripped, name properly quoted per dialect (PG/MySQL → `"..."`, MSSQL → `[...]`)
- **ETL sanitization** — per-source `sanitize: {translit: true, clear: true}` in YAML config

### v1.8.2 (completed)
- **2× import speedup** (1.55s → 0.77s, 100k rows × 7 fields, SQLite)
  - Streaming import: parts processed one at a time (constant memory, no GC pauses)
  - `GetRowValues` fast path: zero-alloc split for rows without escape chars (2.7–4.7×)
  - Parser/Converter singletons: eliminated ~2 allocs × 100k rows
  - `PrepareContext` for SQLite batch INSERT: 2.4× faster (prepared once, reused)
- **Help refactor**: ~100 `fmt.Println` → embedded text files via `//go:embed`
- **Pre-commit hook**: `gofmt`, `golint`, `go vet` on staged `.go` files

### v1.9.0 (completed)
- **Kafka production-ready** — removed `[BETA]` label
- **Parallel compress + serialize** on export: concurrent goroutines, per-goroutine generators
- **`SendBatch`**: all packets sent in a single network roundtrip
- **Parallel decompression** on import: packets 2…N decompressed concurrently
- **`--output` mode**: save received packets as `base_part_N_of_Total.tdtp.xml` files
- **`--raw` flag**: save broker messages verbatim (no parse/decompress/validate)
- **`--keep` flag**: non-atomic mode — import each part immediately as it arrives
- **Atomic multi-part import** (default): all parts in one transaction — all-or-nothing
- **`--import-broker` atomicity** mirrors `--import` (file) behaviour
- **Kafka config**: `brokers` list, `consumer_group`, `StartOffset: kafka.FirstOffset`
- **`BatchBytes`** raised to 100 MB, `BatchTimeout` lowered to 5ms
- **Performance** (50k rows, 5 packets, localhost): kanzi 6 → 3.6s export, 3.9s import, 1.8 MB traffic (4× reduction)

### v1.9.6 (completed)
- **`--enc` tier** — standalone AES-256-GCM encryption for any export
  - `--export --enc` → output renamed `.tdtp.enc` automatically
  - Auto-decrypt on `--import`, `--to-csv`, `--to-xlsx`, `--to-html` (detected by extension)
  - Burn-on-read key via xZMercury: key destroyed after first retrieve
  - S3 output for encrypted blobs: `--enc --output s3://bucket/key.tdtp.enc`
- **v1.4 integrity gate** — `applyV14SecurityGate` applied to all consumer commands
  - XXH3-128 row-level hash verification for v1.4 packets
  - Optional full executor verification via `--mercury-url`
  - Pre-v1.4 packets (v1.0, v1.3.x) pass through as no-op — backward compatible
- **MercuryURL propagation** — `--mercury-url` wired through all import/listen/broker commands
- **SVG namespace fix** — `xlink:href`, `inkscape:label` and other URI-namespaced attributes
  round-trip correctly through `pkg/svg` (last-colon split instead of first-colon)

### v1.11.0 (completed)
- **Full trust chain**: hardware anchor (CA/TPM) → online environment authorization (xZMercury) → offline CLI license → scenario orchestration with dual gate
- **xZMercury → CA on startup**: prod Mercury authorizes with CA before issuing keys; `CAGuard` blocks `/api/keys/*` on invalid session
- **`tdtp-certify`** — vendor-only CA management CLI: `keygen`, `issue-license`, `revoke-cert`, `list-licenses`, `list-active`
- **`pkg/license/`** — Ed25519-signed `tdtp.lic`, fully offline: `Community()` floor (SQLite only, 50k rows), `AllowsAdapter`/`AllowsFeature`/`RowLimit`
- **Orchestrator `TrustGate`**: `GateScenario` checks `scenario.permissions ⊆ (license ∩ Mercury)`; `--require-prod` refuses non-CA-authorized Mercury

### v1.12.0 (completed)
- **Air-gap offline cert** — issue `EnvCert` without live challenge-response for isolated networks
- **Seat policy** — one `env_id_pub` = one active license; re-enroll under same license is idempotent
- **Structured audit log** (`pkg/license/audit.go`) — `text` / `json` format, `TDTP_AUDIT_FORMAT` env var, syslog hook (build tag `syslog`)
- **Orchestrator per-job artifact** — SHA-256 + size recorded after successful job; `GET /jobs/{id}/artifact` download endpoint
- **LDAP auth in orchestrator** — HTTP Basic Auth → LDAP bind → `memberOf` → `RoleMap` → `Principal`; `--auth-type token|ldap`
- **Prometheus metrics** — `orchestrator_jobs_total`, `orchestrator_job_duration_seconds`, `orchestrator_jobs_active`, per-route HTTP metrics; `/healthz` extended for K8s readiness probe
- **Docker deployment stack** — `Dockerfile.worker` (~25 MB distroless), `Dockerfile.mercury`, `Dockerfile.ca`; `docker-compose.dev.yml` (4 services) + `docker-compose.prod.yml`; Grafana dashboard auto-provisioned

### v1.13.0 (completed)
- **`cmd/tdtp-xray` aligned with framework core** — delegates all DB/ETL operations to `pkg/adapters` and `pkg/etl`; removed ~600 lines of duplicated SQL and in-memory SQLite logic
- **`DeployToOrchestrator()`** — writes generated pipeline YAML directly to orchestrator `--scenarios` directory; picked up without restart
- **Schema passthrough** (`applySchemaPassthrough`) — restores `Type/Subtype/Length/SpecialValues` from input packet after SQLite `transform.sql`; closes silent type-drift bug where `DECIMAL(12,4)` → `REAL`, `BOOLEAN` → `INTEGER` in workspace output

### v1.14.0 (completed)
- **`--map --input s3://bucket/key`** — reads TDTP packet from S3-compatible object storage, applies decrypt → decompress → compact-expand, then maps fields and upserts into target DB
- **`InputSource`** in mapping YAML (`input_source.s3`) — S3 credentials and endpoint declared inline; `storage.IsRemote()` routes the download path
- Backward-compatible: `--input <local-file>` unchanged

### v1.15.0 (completed)
- **`--map --input broker://queue`** — reads TDTP packet from a message broker queue (RabbitMQ), ACKs on success; replaces the legacy three-step consumer flow (import-broker → staging table → merge proc) with a single CLI call
- **`input_source.broker`** in mapping YAML — broker connection parameters (`type`, `host`, `port`, `user`, `password`, `queue`, `durable`) declared inline; queue name in URI overrides YAML
- **yaml tags on `brokers.Config`** — all fields now deserialize from YAML; enables inline broker config in mapping files
- **Loop Guard** (`min_interval`) respected for broker input — rapid re-calls skip queue consume

### v1.16.0 (current)
- **`--map --input broker://queue --listen`** — daemon mode: keeps one persistent broker connection open, processes messages in a continuous loop; ACKs each packet only after a successful upsert, NACKs+requeues on error
- **Graceful shutdown** — SIGTERM/SIGINT finishes the current in-flight message, then exits cleanly
- **Zero coordinator dependency** — each entity queue runs as its own process; Python consumer/coordinator becomes optional for fully event-driven deployments
- **NACK with requeue** — parse/decompress/execute errors return the message to the queue for retry (no silent drops)
- **Loop guard skipped** in daemon mode — broker queue naturally throttles the rate; loop guard only applies to one-shot mode

### v2.0 (planned)
- Streaming export/import (TotalParts=0, "TCP for tables")
  - Code ready: `pkg/core/packet/streaming.go` — `StreamingGenerator` with channel-based API
  - Not connected to CLI yet (`--export-stream` / `--import-stream`)
- Parallel import workers
- Schema migration (ALTER TABLE — add/drop columns, type changes)

---

## Special Values — Cross-Adapter Data Integrity (v1.3.1)

Moving data between a strict relational database and a "shapeless" target like Excel or pandas
is like packing Swiss watch parts into a plastic bag. TDTP v1.3.1 solves this at the protocol
level with **SpecialValues markers** — strings embedded in the packet schema that describe values
that cannot be expressed standardly.

### Markers

| Marker | Element | Applies to | Semantics |
|--------|---------|------------|-----------|
| `[NULL]` | `<Null>` | TEXT | NULL — distinct from empty string `""` |
| `NaN` | `<NaN>` | REAL, DECIMAL | Not a Number (`0/0`, `sqrt(-1)`) |
| `INF` | `<Infinity>` | REAL, DECIMAL | Positive infinity |
| `-INF` | `<NegInfinity>` | REAL, DECIMAL | Negative infinity |
| `0000-00-00` | `<NoDate>` | DATE, TIMESTAMP | Absent date (not NULL — a distinct sentinel) |

Markers are declared in the packet schema `<SpecialValues>` element — any reader knows the
semantics without external configuration.

### Adapter Behaviour Matrix

| Situation | PostgreSQL | MS SQL | MySQL | SQLite | XLSX | pandas |
|-----------|-----------|--------|-------|--------|------|--------|
| `NaN` in REAL | native `'NaN'::numeric` | `NULL` | `NULL` | `NULL` | blank cell | `float('nan')` |
| `INF` in REAL | native `'infinity'::numeric` | `NULL` | `NULL` | `NULL` | blank cell | `float('inf')` |
| `[NULL]` in TEXT | `NULL` | `NULL` | `NULL` | `NULL` | blank cell | `None` |
| `0000-00-00` in DATE | `NULL` | `NULL` | `'0000-00-00'`* | text as-is | blank cell | `NaT` |
| BIGINT > 15 digits | stored correctly | stored correctly | stored correctly | stored correctly | **string cell** | no change |
| Date < 1900-01-01 | stored correctly | stored correctly | stored correctly | text as-is | **ISO text string** | no change |

\* MySQL strict mode (`NO_ZERO_DATE`) maps `0000-00-00` → `NULL`.

### Why blank cell and not `"NaN"` text in Excel?

A text string `"NaN"` in a numeric column breaks Excel's `=SUM()` formula — it returns `#VALUE!`.
A blank cell is the canonical Excel NULL: it is ignored by aggregate functions, just like SQL `NULL`.

### Why BIGINT → string in Excel?

Excel stores all numbers as IEEE-754 `float64`. Maximum precision: **15 significant digits**.
`1234567890123456789` (19 digits) becomes `1234567890123456800` silently — data corruption without
any error. Writing as a string cell preserves all digits exactly.

### 1900 Leap-Year Bug

Excel incorrectly treats 1900 as a leap year (inherited from Lotus 1-2-3 for compatibility).
Serial number 60 = Feb 29, 1900, which does not exist. All dates after Feb 28, 1900 are offset
by 1 from the real day count. TDTP compensates on import:

```
serial ≥ 61  →  date = Jan 1, 1900 + (serial − 2) days  ✓
serial = 60  →  Feb 28, 1900  (phantom day mapped to real last day of Feb)
serial ≤ 59  →  date = Jan 1, 1900 + (serial − 1) days  ✓
```

### Comparison with Other Frameworks

| Framework | NULL vs `""` | NaN/Inf | BIGINT Excel | Pre-1900 date | Formula injection | Markers in file |
|-----------|-------------|---------|-------------|---------------|------------------|----------------|
| **TDTP** | ✅ `[NULL]` | ✅ blank | ✅ string | ✅ ISO text | ✅ `SetCellStr` | ✅ in XML schema |
| Apache Spark | ✅ | ✅ in-memory | ✗ | ✗ | ✗ | ✗ |
| pandas | ⚠️ | ✅ in-memory | ✗ | ✗ | ✗ | ✗ |
| Airbyte | ⚠️ | ✗ | ✗ | ✗ | ✗ | ✗ |
| Talend | ✅ | ⚠️ configurable | ✗ | ✗ | ✗ | ✗ |
| dbt | ✅ SQL only | out of scope | out of scope | out of scope | out of scope | ✗ |

**Key difference**: other frameworks solve these issues per-pipeline, manually, in each project.
TDTP handles them at the adapter level, systematically — the ETL developer does not need to
think about IEEE-754 edge cases or Excel quirks.

Full adapter-specific details: [`docs/SPECIFICATION.md` — SpecialValues section](docs/SPECIFICATION.md).

---

## Contributing

The project is under active development. Welcome:
- Bug reports
- Feature suggestions
- Pull requests

---

## License

MIT

---

## Contacts

- **GitHub**: https://github.com/ruslano69/tdtp-framework
- **Issues**: https://github.com/ruslano69/tdtp-framework/issues
- **Email**: ruslano69@gmail.com

---
