# TDTP Framework

**Table Data Transfer Protocol** — a self-describing packet format and a full ecosystem
of tools for moving tabular data between databases, message brokers, files and object
storage, with built-in integrity verification, encryption, and offline licensing.

> Looking for what changed recently? See [CHANGELOG.md](CHANGELOG.md). This README
> describes what the framework *is*, not its release history.

---

## Why TDTP?

### For AI agents and data engineers

**Explore any database in minutes — no documentation needed:**

```bash
# 1. What tables exist?
tdtpcli --list --config my-db.yaml

# 2. What is the table structure? (types, keys, FKs)
tdtpcli --inspect-table orders

# 3. What does the data look like? (latest record)
tdtpcli --export orders --limit -1
```

**Result:** AI understands the structure of ANY database (Navision 2003, MSSQL,
PostgreSQL, Access...) and can build ETL automatically — the schema, sample data
and query context travel together in one self-contained file.

### For enterprise integrations

**Full cycle: discovery → ETL → orchestration → synchronization → protection**

| Layer | What it does | Entry point |
|-------|-------------|-------------|
| Discovery | Understand any DB structure | `tdtpcli --list`, `--inspect-table` |
| Transfer | Extract → Transform → Load | `tdtpcli --pipeline etl.yaml` |
| Orchestration | Schedule, monitor, run pipelines as a service | `orchestrator` |
| Sync | Event-driven distributed sync | `--export-broker` / `--map --listen` |
| Protection | Zero Trust encryption + integrity notary | `--enc` / `--integrity` + xZMercury |
| Governance | Offline, Ed25519-signed capability licensing | `tdtp.lic` + `pkg/license` |

---

## The TDTP Ecosystem

TDTP grew from a CLI parser into a set of cooperating binaries and libraries. Everything
below reads and writes the same packet format, so any two components can exchange data
without adapters.

| Component | What it is | Location |
|---|---|---|
| **`tdtpcli`** | The core CLI: multi-DB export/import, TDTQL SQL translator, ETL pipeline runner, compression, encryption, S3, brokers. Everything else builds on this. | `cmd/tdtpcli/` |
| **`orchestrator`** | HTTP service that runs `tdtpcli --pipeline` scenarios on a schedule (cron) or on demand, with job history, artifacts, LDAP auth and Prometheus metrics. | `cmd/orchestrator/` |
| **`tdtp-xray`** | Desktop GUI (Wails/Go+Vue) for browsing databases, previewing/decoding `.tdtp.xml` packets, and building ETL pipelines visually. | `cmd/tdtp-xray/` |
| **`tdtp-svg`** | Converts SVG documents to/from TDTP packets for vector-graphics data pipelines. | `cmd/tdtp-svg/` |
| **`tdtpserve`** | Lightweight standalone HTTP server exposing DB adapters over the network. | `cmd/tdtpserve/` |
| **`tdtp-license`** | Vendor tool: issues and verifies Ed25519-signed `tdtp.lic` capability licenses (tiers, adapters, features, row limits). | `cmd/tdtp-license/` |
| **xZMercury** | Separate Go module: Zero-Knowledge key store (burn-on-read AES keys) + integrity notary (XXH3 hash registry) + a full CA trust chain (`tdtp-ca`, `tdtp-certify`, `tdtp-redis`). | `xzmercury/` (own `go.mod`) |
| **Python SDK** | `pip`-installable client with a C ABI (`libtdtp.dll`/`.so`), pandas/Arrow bridges, JSON (`J_*`) and direct-struct (`D_*`) APIs. | `bindings/python/` |
| **PureBasic binding** | Verified example calling `libtdtp` from native PureBasic via dynamic loading. | `bindings/purebasic/` |
| **Core packages** | Packet/Schema/TDTQL engine, adapters, brokers, storage, crypto, resilience, ETL — everything importable as a Go library. | `pkg/` |

---

## Quick Start

### 1. Install and try

```bash
# Download binary for your OS, or build from source (see below)

# Create config for your database
tdtpcli --create-config-pg > config.yaml
# or --create-config-mssql / --create-config-mysql / --create-config-sqlite

tdtpcli --list --config config.yaml
```

### 2. What is inside a TDTP file

Every TDTP.xml is a **self-contained packet** — schema, query context and data all in
one file, no external documentation required:

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

### 3. Compression at a glance

Measured on a 100k-row SQLite export (synthetic `Users` table):

| Mode | Time | Size | Ratio |
|------|------|------|-------|
| No compression | 673 ms | 9.9 MB | — |
| zstd level 3 (default) | 751 ms | 2.9 MB | 3.4× |
| zstd level 19 | 2363 ms | 2.4 MB | 4.1× |
| kanzi level 6 | 1279 ms | 1.5 MB | 6.6× |
| kanzi level 7 | 1449 ms | 1.4 MB | 7.1× |

`zstd level 3` is the framework default — near-free, 3× reduction, ideal for real-time
streams. `kanzi level 6` is roughly **2× denser than zstd3**, and on real-world text-heavy
data (HR records, free-text descriptions) kanzi's BWT stage can reach 10-12× — use it for
archives and backups where the extra CPU time doesn't matter.

### 4. Real-world example

**Travel Agency** (`examples/travel-agency/`) — 3-node event-driven sync:
- Central → Branch: countries, tours, guides
- Branch → Central: clients, sales
- Airline → Central: flights, bookings

More in [examples/](examples/README.md).

---

## Core Modules (`pkg/core`)

#### Packet (`pkg/core/packet`)
- XML parser/generator for all message types (Reference, Delta, Response, Request)
- Automatic multi-part splitting (pagination up to ~3.8 MB per part)
- zstd and kanzi compression, transparent decompression on parse
- XXH3-128 checksums (`--hash`), three-tier v1.4 integrity (Schema/Data/Packet hashes)
- Compact format (v1.3.1): fixed fields written once per packet header instead of per row
- `--test`: standalone file-integrity verification (checksum, row count, multi-part
  consistency, dry-run decompression) — no DB connection needed

#### Schema (`pkg/core/schema`)
- Validation for all TDTP data types, universal converter used by every adapter
- Builder API for programmatic schema construction

#### TDTQL (`pkg/core/tdtql`)
- SQL ⇄ TDTQL translator (WHERE, ORDER BY, LIMIT, OFFSET, IN, BETWEEN, LIKE, IS NULL)
- In-memory executor and a SQL-generator that pushes filters down to the adapter

---

## Database Adapters (`pkg/adapters`)

Two-tier architecture: a universal `Adapter` interface plus one implementation per
database, all registered through a common factory.

| Adapter | Driver | Notable |
|---|---|---|
| SQLite | `modernc.org/sqlite` | 10K+ rows/sec, used as the ETL in-memory workspace |
| PostgreSQL | `pgx/v5` | `COPY` bulk import, UUID/JSONB/INET/ARRAY/NUMERIC, `ON CONFLICT` strategies |
| MS SQL Server | `go-mssqldb` | `IDENTITY_INSERT`, NVARCHAR/UNIQUEIDENTIFIER/DATETIME2, SQL 2012+ |
| MySQL | `go-sql-driver/mysql` | Full type mapping, bracket-quoted NAV/BC/ERP-style identifiers |
| MS Access (Windows) | ODBC + ADOX | Exact Access catalog types via COM, `.mdb`/`.accdb` |

All adapters share: `ExportTable`/`ExportTableWithQuery`, `ImportPacket` with
transaction support, and import strategies `REPLACE`/`IGNORE`/`FAIL`/`COPY`.

---

## Message Brokers (`pkg/brokers`)

| Broker | Status | Highlights |
|---|---|---|
| Kafka | Production | `SendBatch`, parallel compress/decompress, atomic multi-part import, replay via offset management |
| RabbitMQ | Production | Manual ACK, durable queues, daemon mode (`--map --listen`) |
| MSMQ (Windows) | Production | Transactional queues |

---

## Resilience & Production Features

- **CircuitBreaker** (`pkg/resilience`) — Closed/Half-Open/Open states, configurable
  recovery timeout, concurrent call limiting, groups
- **AuditLogger** (`pkg/audit`) — File/DB/Console appenders, Minimal/Standard/Full
  levels for GDPR/HIPAA/SOX, async or sync
- **Retry** (`pkg/retry`) — Constant/Linear/Exponential backoff with jitter, DLQ support
- **IncrementalSync** (`pkg/sync`) — checkpoint-based sync (timestamp/sequence/version
  tracking), ~200× faster than full re-export for large tables
- **Data Processors** (`pkg/processors`) — field masking (PII), validation,
  normalization, chainable

---

## Conversions & Integrations

- **XLSX** (`pkg/xlsx`) — TDTP ⇄ Excel with a full data-integrity trap matrix (BIGINT
  precision, NaN/Inf, pre-1900 dates, formula injection, error cells) — see
  [Special Values](#special-values--cross-adapter-data-integrity) below
- **CSV** (`pkg/csv`) — TDTP → CSV, encrypted input auto-decrypted, v1.4 gate applied
- **HTML Viewer** (`pkg/html`) — quick browser preview (`--to-html`, `--row`, `--open`)
- **SVG** (`pkg/svg`, `tdtp-svg`) — namespace-aware SVG ⇄ TDTP round-trip
- **Diff & Merge** (`pkg/diff`, `pkg/merge`) — compare/merge TDTP files with configurable
  keys, ignored fields, and five merge strategies
- **Object Storage** (`pkg/storage`) — S3-compatible (AWS S3, SeaweedFS, MinIO, Ceph);
  every CLI command that takes a file path also accepts `s3://bucket/key`

---

## ETL Pipeline (`pkg/etl`)

Multi-source SQL transformation with a 4-level safety model:

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
    SELECT u.username, COUNT(o.order_id) total_orders, SUM(o.amount) total_spent
    FROM users u LEFT JOIN orders o ON u.user_id = o.user_id
    GROUP BY u.username ORDER BY total_spent DESC

output:
  type: TDTP
  tdtp:
    destination: "report.xml"
    compress: true
```

Sources load in parallel into a SQLite `:memory:` workspace; the SQL runs there; the
result exports to TDTP XML, RabbitMQ or Kafka. Safe mode (default) allows only
SELECT/WITH and needs no admin rights; `--unsafe` unlocks all SQL but requires
administrator privileges and an explicit flag.

Full reference: [`docs/ETL_PIPELINE.md`](docs/ETL_PIPELINE.md).

---

## Orchestrator (`cmd/orchestrator`)

An HTTP service that turns ETL pipeline YAML files into schedulable, monitorable jobs —
`tdtpcli --pipeline` without a cron daemon or a script wrapper.

```
orchestrator --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
```

| Endpoint | Purpose |
|---|---|
| `GET /scenarios`, `GET /scenarios/{name}` | list / inspect pipeline scenarios |
| `POST /scenarios/{name}/run` | run on demand → `{job_id}` |
| `GET /jobs`, `GET /jobs/{id}`, `GET /jobs/{id}/artifact` | job history, status, output download |
| `GET /schedules`, `POST /schedules` | cron-based recurring runs |
| `GET /healthz` | liveness/readiness, includes xZMercury connectivity status |
| `GET /metrics` | Prometheus exposition (see below) |

**Auth**: HTTP Basic → static token or LDAP bind → `memberOf` → role map → principal
(`--auth-type token\|ldap`).

**Metrics** (`orchestrator_*`, scraped via `GET /metrics`): `jobs_total{scenario,status}`,
`job_duration_seconds` (histogram), `jobs_active` (gauge), `schedule_last_status{id,scenario}`
(1=done/0=failed/-1=never), plus per-route HTTP request counters/latency. The dev Docker
stack (`deployments/docker/docker-compose.dev.yml`) wires up Prometheus (`:9090`) and a
pre-provisioned Grafana dashboard (`:3001`).

**Trust gate**: every scenario run is checked against `license ∩ Mercury-authorization`
before execution (`--require-prod` refuses non-CA-authorized Mercury instances).

Full reference: [`docs/ORCHESTRATOR_SCENARIOS.md`](docs/ORCHESTRATOR_SCENARIOS.md),
[`docs/DEPLOYMENT.md`](docs/DEPLOYMENT.md).

---

## tdtp-xray (`cmd/tdtp-xray`)

A desktop GUI (Wails: Go backend + Vue frontend) for working with databases and TDTP
packets without the command line: browse tables and live schemas, preview and decode
`.tdtp.xml` files (including compressed/multi-part), and assemble ETL pipelines visually,
with `DeployToOrchestrator()` writing the generated YAML straight into the orchestrator's
`--scenarios` directory (picked up without a restart). Shares `pkg/adapters` and `pkg/etl`
with the CLI — no duplicated database logic.

---

## Zero Trust: Encryption, Integrity & Licensing

**Philosophy:** nothing to protect if data disappears immediately after delivery, and
nothing to trust if it can't prove where it came from.

### Standalone encryption (`--enc`)

Encrypt any export, auto-decrypt on any consumer command, no pipeline required:

```bash
tdtpcli --export financials --enc --output financials.tdtp.xml   # → writes .tdtp.enc
tdtpcli --import financials.tdtp.enc                              # decrypt → parse → load
tdtpcli --to-csv / --to-xlsx / --to-html financials.tdtp.enc      # decrypt → convert
```

- AES-256-GCM (`pkg/crypto`), unique nonce per packet, packet UUID embedded in the
  header so the recipient can fetch its key before decrypting the body
- Keys never touch disk — held in Redis RAM only, **burn-on-read** (`GETDEL`, destroyed
  after first retrieval), HMAC-SHA256 binding signature prevents key substitution

### v1.4 Integrity Gate

For packets produced with `--integrity`: XXH3-128 row-level hashes verified against the
header on every consumer command (`--import`, `--import-broker`, `--listen`, `--to-csv`,
`--to-xlsx`, `--to-html`), plus optional network-verified producer authentication via
`--mercury-url`. Pre-v1.4 packets pass through as a no-op — fully backward compatible.
Failure policy is `FallbackDegrade`: the packet is blocked, the pipeline keeps running.

### xZMercury (`xzmercury/`, separate Go module)

Plays two roles for the same Redis instance: **key store** (burn-on-read AES delivery,
namespace `mercury:key:*`) and **hash notary** (`GET`/`SET NX` integrity registry,
namespace `mercury:hash:*`). Graceful degradation when unreachable — the pipeline writes
an error packet to `tdtp_errors` and completes with exit 0 rather than crashing.

Also home to the offline trust chain: `tdtp-ca` (certificate authority), `tdtp-certify`
(vendor CA management — `keygen`, `issue-license`, `revoke-cert`), and `tdtp-redis`.
Details: [`xzmercury/README.md`](xzmercury/README.md).

### Offline licensing (`pkg/license`, `tdtp-license`)

`tdtpcli` and `orchestrator` gate adapters/features/row-limits against an Ed25519-signed
`tdtp.lic` file — no network call required to enforce it. `Community()` is the
unrestricted floor (SQLite only, capped rows) when no license is present.
`cmd/tdtp-license` is the vendor-side tool that issues and verifies these files.

---

## CLI Utility (`tdtpcli`)

### Commands

**Database**
```
--list                     List all tables (supports glob: --list=user*)
--list-views               List database views (U* updatable, R* read-only)
--export <table>           Export table/view to TDTP XML
--import <file>            Import TDTP XML into database
--inspect-table <table>    Inspect live DB table: native types, FKs, row count, sample
```

**File**
```
--inspect <file>           Print YAML metadata summary of a TDTP file (no config needed)
--test <file>              Verify file integrity: checksum, row count, multi-part completeness
--diff <file-a> <file-b>   Compare two TDTP files
--merge <files>            Merge multiple TDTP files
--to-html <file>           Convert TDTP to HTML viewer
--to-csv <file>            Convert TDTP to CSV
```

**Object Storage (S3)**
```
--export <table> --output s3://bucket/key.xml   Export to S3 (multi-part automatic)
--import s3://bucket/key.xml                    Import from S3 (multi-part auto-discovered)
--inspect s3://bucket/key.xml                   Inspect packet from S3
--to-xlsx s3://bucket/in.xml --output s3://...  Convert S3 TDTP → S3 XLSX
--export-xlsx <table> --output s3://bucket/k    Export table → XLSX directly to S3
```

**XLSX**
```
--to-xlsx <tdtp-file>      TDTP → XLSX
--from-xlsx <xlsx-file>    XLSX → TDTP
--export-xlsx <table>      Table → XLSX (directly, no intermediate XML)
--import-xlsx <xlsx-file>  XLSX → Database (directly)
```

**Broker**
```
--export-broker <table>    Export to message broker (parallel compress + SendBatch)
--import-broker            Import from message broker (parallel decompress, atomic by default)
--import-broker --output   Save as TDTP files instead of importing to DB
--import-broker --raw      Save broker messages verbatim (no parse/decompress)
--import-broker --keep     Non-atomic mode: import each part immediately
--listen                   Streaming consumer daemon (Kafka only, production-ready)
```

**Cross-system Mapping (`--map`)**
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

**ETL**
```
--sync-incremental <table> Incremental table synchronization
--pipeline <file>          Run ETL pipeline from YAML config
```

### Options

**General**
```
--config <file>            Configuration file (default: config.yaml)
--output <file>            Output file path
--table <name>             Target table name (overrides name from XML on import)
--strategy <name>          Import strategy: replace, ignore, fail, copy
--batch <size>             Batch size for bulk operations (default: 1000)
--readonly-fields          Include read-only fields (timestamp, computed, identity)
```

**Compression**
```
--compress                 Enable compression for exported data
--compress-algo <algo>     Algorithm: zstd (default) or kanzi (denser, slower)
--compress-level <n>       Compression level: 1-19 (zstd) or 6-7 (kanzi), default: 3
--hash                     Add XXH3 checksum for integrity verification (requires --compress)
--packet-size <MB>         Max broker packet size in MB (default 0 = ~1.9MB; use 8 for kanzi)
--fast                     Skip NULL/NaN/Inf detection for maximum throughput
```

**Encryption**
```
--enc                      Encrypt output with AES-256-GCM via xZMercury (burn-on-read keys)
                           Output file renamed: .tdtp.xml → .tdtp.enc
                           Consumer commands (--import, --to-csv, --to-xlsx, --to-html)
                           auto-detect .tdtp.enc and decrypt transparently.
--mercury-url <url>        xZMercury server URL (overrides config); enables full executor
                           verification for v1.4 integrity packets
```

**Field Name Sanitization (--import only)**
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

**TDTQL Filters**
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

**HTML Viewer**
```
--open                     Open in browser after conversion
--row <range>              Row range (e.g. 100-500)
```

**XLSX**
```
--sheet <name>             Excel sheet name (default: Sheet1)
```

**Incremental Sync**
```
--tracking-field <field>   Field to track changes (default: updated_at)
--checkpoint-file <file>   Checkpoint file (default: checkpoint.yaml)
--batch-size <size>        Sync batch size (default: 1000)
```

**ETL**
```
--unsafe                   Unsafe mode (all SQL operations, requires admin)
```

**Diff**
```
--key-fields <fields>      Key fields for comparison (comma-separated)
--ignore-fields <fields>   Fields to ignore during comparison (comma-separated)
--case-sensitive           Case-sensitive comparison (default: false)
```

**Merge**
```
--merge-strategy <name>    Strategy: union, intersection, left, right, append
                           (default: union)
--show-conflicts           Show detailed conflict information
```

**Data Processors**
```
--mask <fields>            Mask sensitive fields (comma-separated)
--validate <file>          Field validation (YAML rules file)
--normalize <file>         Field normalization (YAML rules file)
```

**Configuration**
```
--create-config-pg         Create PostgreSQL config template
--create-config-mssql      Create MS SQL config template
--create-config-sqlite     Create SQLite config template
--create-config-mysql      Create MySQL config template
```

**Misc**
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

## Broker-native EDA (event-driven architecture)

`--map --listen` turns any mapping YAML into a standalone daemon process — one process
per queue, no Python middleware or coordinator required.

**One-shot** (classic, coordinator-driven):
```bash
tdtpcli --map mappings/sync_flights.yaml --input broker://tdtp.sync.flights
```

**Daemon**:
```bash
tdtpcli --map mappings/sync_flights.yaml --input broker://tdtp.sync.flights --listen

# start all queues in parallel (systemd, Docker Compose, or background jobs)
tdtpcli --map mappings/sync_countries.yaml    --input broker://tdtp.sync.countries    --listen &
tdtpcli --map mappings/sync_tours.yaml        --input broker://tdtp.sync.tours        --listen &
tdtpcli --map mappings/sync_reservations.yaml --input broker://tdtp.sync.reservations --listen &
```

**How it works:** one persistent broker connection per daemon instance;
`receive → decrypt → parse → decompress → field-remap → upsert → ACK` per message;
NACK+requeue on parse/execute error; graceful shutdown on SIGTERM/SIGINT.

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

The source side sends via `--export-broker`; each daemon consumes its own queue
independently.

---

## Architecture

```
tdtp-framework/
├─ pkg/core/
│  ├─ packet/            TDTP packet parsing/generation + compression
│  ├─ schema/             Type validation, Converter, Builder
│  └─ tdtql/              Translator, Executor, SQL Generator
│
├─ pkg/adapters/          Universal interface + factory + one package per DB
│  ├─ sqlite/  postgres/  mssql/  mysql/  access/
│
├─ pkg/processors/        zstd/kanzi compression, PII masking, validation, normalization
├─ pkg/sanitize/          Field name sanitization (--translit, --clear)
├─ pkg/security/          IsAdmin() + SQL validator (safe/unsafe modes)
├─ pkg/crypto/            AES-256-GCM packet encryption
├─ pkg/mercury/           xZMercury client (BindKey / RetrieveKey / VerifyHMAC)
├─ pkg/pipeline/          v1.4 integrity verification (VerifyAndPrepare, fallback policies)
├─ pkg/license/           Ed25519 offline licensing (tiers, adapters, features, row limits)
├─ pkg/etl/               ETL pipeline: config, workspace, loader, executor, exporter
├─ pkg/workflow/          Multi-step scenario config + runner (used by orchestrator)
├─ pkg/cliquery/          Programmatic query builder over tdtpcli
├─ pkg/resultlog/         Redis-backed job/result logging
├─ pkg/resilience/        Circuit Breaker
├─ pkg/audit/             Audit Logger (File/DB/Console appenders)
├─ pkg/retry/             Backoff strategies + DLQ
├─ pkg/sync/              Incremental Sync (StateManager)
├─ pkg/xlsx/  pkg/csv/  pkg/html/  pkg/svg/    Format converters
├─ pkg/diff/  pkg/merge/                       Compare / merge TDTP files
├─ pkg/brokers/           RabbitMQ, Kafka, MSMQ
├─ pkg/storage/           Object storage abstraction + S3 driver
│
├─ xzmercury/             Separate Go module: key store, hash notary, CA trust chain
│  └─ cmd/                xzmercury  tdtp-ca  tdtp-certify  tdtp-redis
│
├─ cmd/
│  ├─ tdtpcli/            Core CLI
│  ├─ orchestrator/       Scenario scheduler + HTTP API + Prometheus metrics
│  ├─ tdtp-xray/          Desktop GUI (Wails)
│  ├─ tdtp-svg/           SVG ⇄ TDTP converter CLI
│  ├─ tdtpserve/          Standalone adapter HTTP server
│  ├─ tdtp-license/       Vendor license issuance/verification tool
│  └─ xzmercury-mock/     Minimal mock Mercury server for tests
│
├─ bindings/
│  ├─ python/             pip package: C ABI (libtdtp), pandas/Arrow bridges
│  └─ purebasic/          Verified PureBasic + libtdtp integration example
│
├─ docs/                  Guides and protocol specification
├─ examples/               Production-ready runnable examples
└─ scripts/                Test DB generators (Python)
```

---

## Using in Code

```go
import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

schema := packet.Schema{
    Fields: []packet.Field{
        {Name: "ID", Type: "INTEGER", Key: true},
        {Name: "Name", Type: "TEXT", Length: 200},
        {Name: "Balance", Type: "DECIMAL"},
    },
}

generator := packet.NewGenerator()
packets, err := generator.GenerateReference("Companies", schema, rows)
generator.WriteToFile(packets[0], "reference.xml")

parser := packet.NewParser()
pkt, err := parser.ParseFile("reference.xml")
```

```go
import (
    "context"
    "github.com/ruslano69/tdtp-framework/pkg/adapters"
    _ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
)

ctx := context.Background()
adapter, err := adapters.New(ctx, adapters.Config{Type: "postgres", DSN: "postgres://localhost/mydb"})
defer adapter.Close(ctx)

packets, err := adapter.ExportTable(ctx, "users")           // DB → TDTP
err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace) // TDTP → DB
```

### Ready-to-Run Examples

```bash
cd examples/01-basic-export        && go run main.go   # PostgreSQL → TDTP XML
cd examples/02-rabbitmq-mssql      && go run main.go   # MSSQL → RabbitMQ (Circuit Breaker + Audit)
cd examples/03-incremental-sync    && go run main.go   # PostgreSQL → MySQL incremental sync
cd examples/04-tdtp-xlsx           && go run main.go   # Database ↔ Excel converter
cd examples/05-circuit-breaker     && go run main.go   # API resilience patterns
cd examples/06-etl-pipeline        && go run main.go   # Complete ETL pipeline
cd examples/08-pipeline-encrypted  && go run main.go   # xZMercury + AES-256-GCM
cd examples/09-s3-pipeline-chain   && ./run_chain.sh   # S3 pipeline chain
```

Examples documentation: [`examples/README.md`](examples/README.md).

---

## Building from Source

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
go build -o tdtpcli ./cmd/tdtpcli
```

`-tags nokafka` excludes `kafka-go` (offline/no-broker builds); `-tags nosqlite` excludes
`modernc.org/sqlite`. Minimum Go version: 1.25 (see `go.mod`).

---

## Documentation

- [User Guide](docs/USER_GUIDE.md) — full CLI reference with examples
- [ETL Pipeline Guide](docs/ETL_PIPELINE.md) — pipeline YAML reference
- [Developer Guide](docs/DEVELOPER_GUIDE.md) — internals, extending the framework
- [Deployment](docs/DEPLOYMENT.md) — service map, dev/prod stacks, LDAP/TLS, air-gap certs
- [Orchestrator Scenarios](docs/ORCHESTRATOR_SCENARIOS.md) — scenario YAML reference
- [S3 as Sync Broker](docs/S3_AS_SYNC_BROKER.md)
- [Access Adapter](docs/ACCESS_ADAPTER.md) — MS Access specifics (ODBC + ADOX)
- [TDTP Specification](docs/SPECIFICATION.md) — protocol spec (v1.0 – v1.4)
- [xZMercury README](xzmercury/README.md) — key store + integrity notary + CA chain
- Package-level docs: [`pkg/resilience`](pkg/resilience/README.md),
  [`pkg/audit`](pkg/audit/README.md), [`pkg/xlsx`](pkg/xlsx/README.md)

---

## Testing

```bash
go test ./...
go test -cover ./...
go test -v ./pkg/core/packet/
```

---

## Special Values — Cross-Adapter Data Integrity (v1.3.1)

Moving data between a strict relational database and a "shapeless" target like Excel or
pandas is like packing Swiss watch parts into a plastic bag. TDTP solves this at the
protocol level with **SpecialValues markers** — strings embedded in the packet schema
that describe values that cannot be expressed standardly.

### Markers

| Marker | Element | Applies to | Semantics |
|--------|---------|------------|-----------|
| `[NULL]` | `<Null>` | TEXT | NULL — distinct from empty string `""` |
| `NaN` | `<NaN>` | REAL, DECIMAL | Not a Number (`0/0`, `sqrt(-1)`) |
| `INF` | `<Infinity>` | REAL, DECIMAL | Positive infinity |
| `-INF` | `<NegInfinity>` | REAL, DECIMAL | Negative infinity |
| `0000-00-00` | `<NoDate>` | DATE, TIMESTAMP | Absent date (not NULL — a distinct sentinel) |

Markers are declared in the packet schema `<SpecialValues>` element — any reader knows
the semantics without external configuration.

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

**Why blank cell and not `"NaN"` text in Excel?** A text string `"NaN"` in a numeric
column breaks `=SUM()` (`#VALUE!`). A blank cell is the canonical Excel NULL — ignored
by aggregates, same as SQL `NULL`.

**Why BIGINT → string in Excel?** Excel stores all numbers as IEEE-754 `float64` (15
significant digits max). `1234567890123456789` silently becomes `1234567890123456800` —
data corruption without an error. A string cell preserves every digit.

**1900 leap-year bug:** Excel inherits a Lotus 1-2-3 compatibility bug that treats 1900
as a leap year; serial 60 = a Feb 29, 1900 that never existed. TDTP compensates on
import (`serial ≥ 61 → Jan 1 1900 + (serial−2) days`, `serial = 60 → Feb 28 1900`).

### Comparison with Other Frameworks

| Framework | NULL vs `""` | NaN/Inf | BIGINT Excel | Pre-1900 date | Formula injection | Markers in file |
|-----------|-------------|---------|-------------|---------------|------------------|----------------|
| **TDTP** | ✅ `[NULL]` | ✅ blank | ✅ string | ✅ ISO text | ✅ `SetCellStr` | ✅ in XML schema |
| Apache Spark | ✅ | ✅ in-memory | ✗ | ✗ | ✗ | ✗ |
| pandas | ⚠️ | ✅ in-memory | ✗ | ✗ | ✗ | ✗ |
| Airbyte | ⚠️ | ✗ | ✗ | ✗ | ✗ | ✗ |
| Talend | ✅ | ⚠️ configurable | ✗ | ✗ | ✗ | ✗ |
| dbt | ✅ SQL only | out of scope | out of scope | out of scope | out of scope | ✗ |

**Key difference:** other frameworks solve these issues per-pipeline, manually, in each
project. TDTP handles them at the adapter level, systematically.

Full adapter-specific details: [`docs/SPECIFICATION.md`](docs/SPECIFICATION.md).

---

## Roadmap

Planned, not yet shipped:
- Streaming export/import (`TotalParts=0`, "TCP for tables") — core is ready
  (`pkg/core/packet/streaming.go`, channel-based `StreamingGenerator`), not yet wired to
  the CLI (`--export-stream` / `--import-stream`)
- Parallel import workers
- Schema migration (ALTER TABLE — add/drop columns, type changes)

For everything already shipped, see [CHANGELOG.md](CHANGELOG.md).

---

## Contributing

The project is under active development. Welcome: bug reports, feature suggestions,
pull requests.

## License

MIT

## Contacts

- **GitHub**: https://github.com/ruslano69/tdtp-framework
- **Issues**: https://github.com/ruslano69/tdtp-framework/issues
- **Email**: ruslano69@gmail.com
