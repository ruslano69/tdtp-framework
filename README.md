# TDTP Framework

**Table Data Transfer Protocol** — a framework for universal tabular data exchange via message brokers.

---

## Project Goals

- **Universality** — works with any tables and databases
- **Transparency** — self-documenting XML messages
- **Reliability** — stateless pattern, validation, pagination
- **Security** — Zero Trust encryption, TLS, authentication, audit trail
- **Simplicity** — clean API, intuitive structure

---

## What's Implemented (v1.6.0)

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
- Export/Import with MySQL type mapping
- Support for MySQL-specific types

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

#### Kafka
- High-throughput message streaming
- Producer/Consumer with manual commit
- Configurable partitioning and consumer groups
- Stats and offset management (replay capability)
- Tested with PostgreSQL adapter

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

---

### HTML Viewer (`pkg/html`)

- TDTP → HTML conversion for quick browser-based data preview
- Row range support (`--row 100-500`)
- Tail-mode preview (`--limit -50` — last 50 rows)
- Combined range and limit
- Open in browser with a single command (`--open`)

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

Documentation: [`docs/ETL_PIPELINE_GUIDE.md`](docs/ETL_PIPELINE_GUIDE.md)

---

### Encryption & Zero Trust (`pkg/crypto`, `pkg/mercury`)

**Philosophy:** nothing to protect if data disappears immediately after delivery.

#### AES-256-GCM (`pkg/crypto`)
- Authenticated encryption (AEAD) — data cannot be tampered with undetected
- Unique nonce from `crypto/rand` per packet (replay attacks impossible)
- Binary format: `[2B version][1B algo][16B UUID][12B nonce][ciphertext + 16B GCM tag]`
- Packet UUID embedded in header — recipient retrieves key without decrypting body

#### Key lifecycle (`pkg/mercury` + xZMercury)

```
Sender                xZMercury (Redis)            Recipient
──────                ─────────────────            ─────────
GenerateUUID ───────► POST /api/keys/bind
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

**Zero Trust properties:**
- Keys are never stored on disk — RAM only (Redis)
- **Burn-on-read**: key is physically destroyed after first retrieval (Redis `GETDEL`)
- **One shot** — key lives from inhale (bind) to exhale (retrieve), typically ~300 ms
- **HMAC-SHA256** — binding signature prevents key substitution in transit
- **UUID isolation** — each packet gets a unique key; one compromised key reveals nothing else

**Graceful degradation when xZMercury is unavailable:**

If the key service is unreachable — data does not leak unencrypted. Instead of panic:

```
Mercury unavailable
  → business data discarded
  → error packet (MERCURY_UNAVAILABLE) written to tdtp_errors
  → pipeline completes normally (exit 0)
```

The error sits on the "organized junk yard" — queryable with a plain SELECT.

**Configuration:**

```yaml
output:
  tdtp:
    encryption: true

security:
  mercury_url: "http://mercury:3000"
  recipient_resource: "ETL_RESULTS"
  key_ttl_seconds: 86400       # Key TTL in Redis (failsafe expiry)
  mercury_timeout_ms: 5000     # Timeout — triggers degradation, not hang
```

**Error codes in `tdtp_errors`:**

| Code | Cause |
|------|-------|
| `MERCURY_UNAVAILABLE` | timeout / connection refused |
| `MERCURY_ERROR` | HTTP 5xx from xZMercury |
| `HMAC_VERIFICATION_FAILED` | key signature mismatch (substitution attempt?) |
| `KEY_BIND_REJECTED` | quota exceeded or ACL denied |

---

## CLI Utility (`tdtpcli`)

### Commands

**Database:**
```
--list                     List all tables
--list-views               List database views (U* updatable, R* read-only)
--export <table>           Export table/view to TDTP XML
--import <file>            Import TDTP XML into database
```

**File:**
```
--diff <file-a> <file-b>   Compare two TDTP files
--merge <files>            Merge multiple TDTP files
--to-html <file>           Convert TDTP to HTML viewer
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
--export-broker <table>    Export to message broker
--import-broker            Import from message broker
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
--compress                 Enable zstd compression for exported data
--compress-level <n>       Compression level: 1 (faster) — 19 (better), default: 3
```

**TDTQL Filters:**
```
--where <condition>        WHERE condition (e.g. 'age > 18 AND status = active')
--order-by <fields>        ORDER BY (e.g. 'name ASC, age DESC')
--limit <n>                Row limit: +N = first N, -N = last N (like tail)
--offset <n>               Skip N rows
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
│  └─ mysql/             MySQL adapter (go-sql-driver/mysql)
│
├─ pkg/processors/       Data processing and transformation
│  ├─ compression.go     zstd compression/decompression (klauspost/compress)
│  ├─ field_masker.go    PII masking (email, phone, card)
│  ├─ field_validator.go Field validation (regex, range, format)
│  ├─ field_normalizer.go Data normalization
│  ├─ chain.go           Processor chains
│  └─ factory.go         Processor factory
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
├─ pkg/brokers/
│  ├─ broker.go          Broker interface
│  ├─ rabbitmq.go        RabbitMQ integration
│  ├─ kafka.go           Kafka integration
│  └─ msmq.go            MSMQ integration (Windows)
│
├─ cmd/tdtpcli/          CLI utility
│  ├─ main.go            Entry point
│  ├─ help.go            Help information
│  ├─ config.go          YAML configuration
│  ├─ processors.go      Processor integration
│  └─ commands/          Command handlers
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

```bash
# List tables
tdtpcli --list --config pg.yaml

# Export table
tdtpcli --export users --output users.xml

# Export with filters and compression
tdtpcli --export orders --where 'status = active AND amount > 1000' --limit 100 --compress

# Export last 50 rows (tail mode)
tdtpcli --export logs --order-by 'created_at DESC' --limit -50

# View data in browser
tdtpcli --to-html customers.xml --open

# View rows 100-500
tdtpcli --to-html data.xml --row 100-500 --open

# View last 20 rows from range
tdtpcli --to-html data.xml --row 100-500 --limit -20 --open

# Export directly to Excel
tdtpcli --export-xlsx orders --output orders.xlsx

# Convert TDTP to Excel with sheet name
tdtpcli --to-xlsx orders.xml --output orders.xlsx --sheet Orders

# Convert Excel to TDTP
tdtpcli --from-xlsx orders.xlsx --output orders.xml

# Import Excel to database
tdtpcli --import-xlsx orders.xlsx --strategy replace

# Compare two TDTP files
tdtpcli --diff users-old.xml users-new.xml

# Compare with key fields and ignore fields
tdtpcli --diff old.xml new.xml --key-fields user_id --ignore-fields updated_at

# Merge multiple files (union strategy)
tdtpcli --merge file1.xml,file2.xml,file3.xml --output merged.xml

# Merge with conflict resolution
tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy right --show-conflicts

# Incremental synchronization
tdtpcli --sync-incremental orders --tracking-field updated_at --checkpoint-file orders.yaml

# Export with PII masking
tdtpcli --export customers --mask email,phone

# ETL pipeline (safe mode)
tdtpcli --pipeline pipeline.yaml

# ETL pipeline (unsafe mode, requires admin)
sudo tdtpcli --pipeline pipeline.yaml --unsafe

# Create configuration file
tdtpcli --create-config-pg > config.yaml
tdtpcli --create-config-mysql > mysql.yaml
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

# RabbitMQ + MSSQL (Circuit Breaker, Audit, Retry)
cd examples/02-rabbitmq-mssql
go run main.go

# Incremental Sync (200x faster for large tables)
cd examples/03-incremental-sync
go run main.go

# Complete ETL pipeline
cd examples/06-etl-pipeline
go run main.go
```

Examples documentation: [`examples/README.md`](examples/README.md)

---

## Documentation

### Guides
- [Installation Guide](docs/INSTALLATION.md) — installation, configuration, quick start
- [User Guide](docs/USER_GUIDE.md) — complete CLI utility guide
- [ETL Pipeline Guide](docs/ETL_PIPELINE_GUIDE.md) — ETL pipeline guide
- [Documentation Index](docs/INDEX.md) — full documentation catalog

### Technical Specifications
- [TDTP Specification](docs/SPECIFICATION.md) — TDTP v1.0 protocol specification
- [Packet Module](docs/PACKET_MODULE.md) — packet parsing and generation
- [Schema Module](docs/SCHEMA_MODULE.md) — type and schema validation
- [TDTQL Translator](docs/TDTQL_TRANSLATOR.md) — query language
- [SQLite Adapter](docs/SQLITE_ADAPTER.md) — SQLite integration

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
- MySQL adapter
- Full documentation

### v1.6.0 (current)
- HTML Viewer (`--to-html`, `--open`, `--row`)
- Diff & Merge (`--diff`, `--merge`, `--merge-strategy`, `--show-conflicts`)
- Extended XLSX commands (`--from-xlsx`, `--export-xlsx`, `--import-xlsx`)
- Incremental sync via CLI (`--sync-incremental`)
- Data Processors in CLI (`--mask`, `--validate`, `--normalize`)
- Tail mode in limit (`--limit -N`)
- `--batch`, `--readonly-fields` options
- Zero Trust encryption: AES-256-GCM + xZMercury (burn-on-read keys, graceful degradation)

### v2.0 (planned)
- Streaming export/import (TotalParts=0, "TCP for tables")
- Parallel import workers
- Python bindings (ctypes wrapper)
- Docker image (multi-stage build)
- Monitoring & metrics (Prometheus exporter)
- Schema migration (ALTER TABLE)

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

*Version: v1.6.0 | Last updated: 23.02.2026*
