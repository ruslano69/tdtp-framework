# TDTP CLI — user guide

**tdtpcli** is the command-line tool for TDTP (Table Data Transfer Protocol).

**Version:** 1.25.1

---

## Contents

1. [Installation](#installation)
2. [Quick start](#quick-start)
3. [Configuration](#configuration)
4. [Commands](#commands)
   - [--list](#--list) · [--list-views](#--list-views) · [--inspect](#--inspect) · [--test](#--test)
   - [--export](#--export) · [--import](#--import) · [Sanitising field names](#sanitising-field-names---translit---clear)
   - [--export-xlsx](#--export-xlsx) · [--import-xlsx](#--import-xlsx) · [--to-xlsx](#--to-xlsx) · [--from-xlsx](#--from-xlsx)
   - [--to-csv](#--to-csv) · [--to-html](#--to-html) · [--to-compact](#--to-compact) · [--to-tdtp](#--to-tdtp)
   - [--steps](#--steps) · [--inspect-table](#--inspect-table) · [Integrity](#integrity---integrity---mercury-caller) · [Other flags](#other-flags)
   - [--export-broker](#--export-broker) · [--import-broker](#--import-broker) · [--listen](#--listen-beta)
   - [--sync-incremental](#--sync-incremental) · [--map](#--map)
   - [--diff](#--diff) · [--merge](#--merge)
   - [--pipeline](#--pipeline) · [--process-request](#--process-request)
5. [Workflow: inspect, test, import](#workflow-inspect-test-import)
6. [Compact format (v1.3.1)](#compact-format-v131)
7. [ETL pipeline](#etl-pipeline)
8. [AES-256-GCM encryption](#aes-256-gcm-encryption)
9. [Filtering with TDTQL](#filtering-with-tdtql)
10. [Message brokers](#message-brokers)
11. [Worked examples](#worked-examples)
12. [Troubleshooting](#troubleshooting)
13. [CLI usage examples](#cli-usage-examples)

---

## Installation

### Requirements

- **Go** 1.21 or later, to build from source
- **A database:** SQLite, PostgreSQL, MS SQL Server or MySQL
- **A message broker** (optional): RabbitMQ, MSMQ or Kafka

### Building from source

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
go build -o tdtpcli ./cmd/tdtpcli
```

### Checking the installation

```bash
./tdtpcli --help
```

---

## Quick start

### 1. Create a configuration

Pick your database:

**SQLite:**
```bash
./tdtpcli --create-config-sqlite
```

**PostgreSQL:**
```bash
./tdtpcli --create-config-pg
```

**MS SQL Server:**
```bash
./tdtpcli --create-config-mssql
```

**MySQL:**
```bash
./tdtpcli --create-config-mysql
```

This writes a `config.{dbtype}.yaml` template.

### 2. Edit it

Fill in your connection details:

**config.postgres.yaml:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: myuser
  password: mypassword
  database: mydb
  schema: public
  sslmode: disable
```

### 3. Check the connection

List the tables:

```bash
./tdtpcli -config config.postgres.yaml --list
```

### 4. Export

```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

### 5. Import

```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

---

## Configuration

### File structure

> The quickest way to a correct file is to let the tool write one:
> `tdtpcli --create-config-pg` and edit the result. What follows describes the
> same keys.

```yaml
# Database
database:
  type: postgres          # sqlite | postgres | mssql | mysql | access

  # The database name, or — for SQLite — the path to the file.
  # One key for both: there is no separate `path` or `dbname`.
  database: mydb

  host: localhost         # network databases only
  port: 5432              # 5432 PostgreSQL, 1433 MS SQL, 3306 MySQL
  user: username
  password: password

  schema: public          # PostgreSQL: schema (default public)
  sslmode: disable        # PostgreSQL: disable | require | verify-ca | verify-full
  windows_auth: false     # MS SQL: authenticate as the current Windows user

  # A raw connection string. Overrides every field above, and is required for
  # the Access adapter, which has no other way to be configured.
  dsn: ""

  # Charset for decoding strings from drivers that do not report one
  # (ODBC and other legacy drivers), e.g. "windows-1251".
  charset: ""

# Message broker (optional)
broker:
  type: rabbitmq          # rabbitmq | msmq | kafka
  host: localhost
  port: 5672              # 5672 plain, 5671 TLS
  user: guest
  password: guest
  queue: tdtp_queue       # queue or topic name
  vhost: /                # RabbitMQ virtual host
  use_tls: false          # true → amqps:// (port 5671)
  tls_skip_verify: false  # true → skip certificate verification (self-signed)
  exchange: ""            # RabbitMQ exchange (default: the empty exchange)
  routing_key: ""         # RabbitMQ routing key (defaults to the queue name)
  durable: true           # queue survives a broker restart
  auto_delete: false      # queue is not deleted when the last consumer leaves
  exclusive: false        # queue accepts more than one connection
  passive_declare: false  # true → do not create the queue, attach to the existing one
  queue_path: ""          # MSMQ: full queue path, e.g. ".\private$\tdtp_in"
  brokers: []             # Kafka: broker list, e.g. ["localhost:9092"]
  consumer_group: ""      # Kafka: consumer group ID

# Export defaults (optional)
export:
  compress: true
  compress_level: 3       # 1-19 zstd, 6-7 kanzi
  compress_algo: zstd     # zstd | kanzi

# Encryption (optional)
security:
  mercury_url: "http://mercury:3000"   # xZMercury; --mercury-url overrides it
```

### Example configurations

**SQLite:**
```yaml
database:
  type: sqlite
  database: ./database.db
```

**PostgreSQL with RabbitMQ:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: secure_password
  database: production_db
  schema: public
  sslmode: require

broker:
  type: rabbitmq
  host: rabbitmq.example.com
  port: 5672
  user: tdtp
  password: broker_password
  queue: tdtp_production_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
  passive_declare: false  # set true when the queue is owned by another system
```

**MS SQL Server:**
```yaml
database:
  type: mssql
  host: sql-server.example.com
  port: 1433
  user: sa
  password: MyStr0ngP@ssw0rd
  database: MyDatabase
```

For a named instance, Windows authentication, or any other MS SQL connection
option, use `dsn` — it overrides the individual fields:

```yaml
database:
  type: mssql
  dsn: "server=sql-server.example.com\SQLEXPRESS;database=MyDatabase;trusted_connection=yes"
```

**MySQL:**
```yaml
database:
  type: mysql
  host: localhost
  port: 3306
  user: root
  password: secret
  database: mydb
```

### S3 and object storage

`--export` and `--import` accept an `s3://` URI in place of a local path. The
storage configuration lives in a `storage:` section.

```yaml
storage:
  type: s3
  s3:
    bucket: my-tdtp-bucket
    region: us-east-1
    access_key: AKIAIOSFODNN7EXAMPLE
    secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    endpoint: ""       # leave empty for AWS; set it for MinIO and similar
```

**Usage:**
```bash
# Export to S3
./tdtpcli -config config.yaml --export users --output s3://my-bucket/exports/users.tdtp.xml

# Import from S3
./tdtpcli -config config.yaml --import s3://my-bucket/exports/users.tdtp.xml

# Inspect from S3, no --config needed
./tdtpcli --inspect s3://my-bucket/exports/users.tdtp.xml
```

---

## Commands

### --list

List the tables in the database.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --list
```

**Example:**
```bash
./tdtpcli -config config.postgres.yaml --list
```

**Output:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📋 Tables in database (4):
  1. users
  2. products
  3. orders
  4. activity_logs
```

---

### --list-views

List the views, with their status — updatable or read-only.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --list-views
```

**Example output:**
```
Views in database (2):
  dept_employees_report  [read-only]
  active_users_vw        [updatable]
```

---

### --inspect

Print a YAML summary of a TDTP file's metadata, with no database connection.
Accepts local paths and `s3://` URIs.

**Syntax:**
```bash
tdtpcli --inspect <file>
```

**Example:**
```bash
./tdtpcli --inspect report.tdtp.xml
```

**Output:**
```yaml
file: report.tdtp.xml
version: "1.3.1"
packet_type: reference
table: users
uuid: 550e8400-e29b-41d4-a716-446655440000
compressed: false
compact: false
schema:
  fields: 4
  names: [id, name, email, age]
data:
  rows: 10000
```

---

### --test

Check a TDTP file's integrity with no database connection: decompress it in
memory, verify the XXH3 checksum, compare the row count against the header.

> **How this differs from `--inspect`:**
> - `--inspect` answers **"what is in it?"** — field names, types, table name, UUID.
> - `--test` answers **"is it intact?"** — data integrity and checksums.
>
> The recommended order before importing: `--inspect` → `--test` → `--import`.

**Syntax:**
```bash
tdtpcli --test <file>
```

**What is checked:**
- the XML structure is well-formed
- the `<R>` row count matches `RecordsInPart` in the header
- with `--compress`: decompression succeeds (zstd and kanzi)
- with `--hash`: the XXH3 checksum of the data (`checksum OK` / `checksum MISMATCH`)
- for multi-part sets: every `_part_N_of_M` is present and all share the same `InReplyTo` UUID

**Examples:**
```bash
# Plain check
tdtpcli --test users.tdtp.xml
# ✓ 10 rows (uncompressed)   Total rows: 10

# Compressed, with a checksum
tdtpcli --test orders.tdtp.xml
# ✓ algo=zstd, 1000 rows, decompressed 12ms, checksum OK

# A file in S3, without downloading it locally
tdtpcli --test s3://my-bucket/exports/users.tdtp.xml --config config.yaml

# Check before importing — recommended
tdtpcli --test delivery.tdtp.xml && tdtpcli --import delivery.tdtp.xml
```

**Exit codes:**
- `0` — every check passed
- `1` — the file is damaged, parts are missing, or a checksum did not match

---

### --to-csv

Convert a TDTP file to CSV with no database connection. Handles compressed
files (zstd, kanzi), compact v1.3.1 and v1.4 integrity packets. TDTQL filters
are applied **in memory** before the CSV is written.

**Syntax:**
```bash
tdtpcli --to-csv <file> [--output <file>]
        [--delimiter <sep>] [-d <sep>]
        [--cp <encoding>] [--bom]
        [--where <condition>] [-w <condition>]
        [--order-by <fields>]
        [--limit <n>] [-n <n>]
        [--offset <n>]
        [--fields <col1,col2,...>]
```

**Options:**

| Flag | Description |
|------|-------------|
| `--to-csv <file>` | Input TDTP file |
| `--output <file>` | Output CSV (defaults to the same name with `.csv`) |
| `--delimiter <sep>` / `-d` | Separator: `,` (default), `;`, `\t` |
| `--cp <encoding>` | Output encoding: `utf8` (default), `1251`, `866` |
| `--bom` | Prepend a UTF-8 BOM — Excel needs it to detect the encoding |
| `--where` / `-w` | Row filter (TDTQL; repeatable, combined with AND) |
| `--order-by` | Sort order |
| `--limit` / `-n` | First N rows (`+N`) or last N rows (`-N`, tail mode) |
| `--offset` | Skip N rows |
| `--fields` | Only the named columns; names containing spaces go in square brackets |

**Examples:**
```bash
# Plain conversion
tdtpcli --to-csv users.tdtp.xml

# Naming the output
tdtpcli --to-csv users.tdtp.xml --output users_export.csv

# Semicolon separator plus a BOM, for Excel
tdtpcli --to-csv orders.tdtp.xml --delimiter ';' --bom --output orders.csv

# Tab separator, Windows-1251 — for older Excel and 1C
tdtpcli --to-csv report.tdtp.xml -d '\t' --cp 1251 --bom

# Selected columns only
tdtpcli --to-csv users.tdtp.xml --fields 'id,email,balance'

# Column names containing spaces
tdtpcli --to-csv staff.tdtp.xml --fields '[Last Name],[First Name],[Birth Date]'

# Filter, sort, limit
tdtpcli --to-csv orders.tdtp.xml \
  --where 'status = completed' \
  --order-by 'total DESC' \
  --limit 50

# Compressed input, decompressed automatically
tdtpcli --to-csv archive.tdtp.xml --where 'amount > 1000'

# v1.4 integrity packet — hashes verified before anything is written
tdtpcli --to-csv secure.tdtp.xml --fields 'id,name,amount'

# Last 100 rows (tail mode)
tdtpcli --to-csv events.tdtp.xml --limit -100 --order-by 'created_at ASC'

# Paging through a TDTP archive
tdtpcli --to-csv big_table.tdtp.xml --limit 100 --offset 500
```

> **No configuration needed:** `--to-csv` never touches a database. It works on
> any TDTP file, compressed, compact or carrying v1.4 integrity hashes.

---

### --to-html

Convert a TDTP file to HTML for viewing in a browser.

**Syntax:**
```bash
tdtpcli --to-html <input> [--output <file>] [--open] [--row <n1-n2>] [--limit N] [--where ...]
```

**Options:**

| Flag | Description |
|------|-------------|
| `--to-html <file>` | Input TDTP file |
| `--output <file>` | Output HTML file (a name is generated by default) |
| `--open` | Open it in a browser as soon as it is written |
| `--row <n1-n2>` | Row range to display, 1-indexed (for example `100-150`) |
| `--limit N` | Cap the number of rows |
| `--where` | Row filter (TDTQL, repeatable) |

**Examples:**
```bash
# Convert and open
./tdtpcli --to-html users.tdtp.xml --open

# Rows 500 to 600
./tdtpcli --to-html large_report.tdtp.xml --row 500-600 --open

# Filter before viewing
./tdtpcli --to-html orders.tdtp.xml --where "status = active" --open
```

---

### --process-request

Handle an incoming TDTP request file and produce a response. Database
configurations are looked up in the same directory as the request.

**Syntax:**
```bash
tdtpcli --process-request <request-file> [--output <response-file>] [-config <config.yaml>]
```

**Example:**
```bash
./tdtpcli --process-request ./requests/users_request.tdtp.xml \
          --output ./responses/users_response.tdtp.xml
```

---

### --listen `[BETA]`

Streaming consumer daemon for Kafka: subscribe to a topic and import data as it
arrives. Runs until SIGTERM.

> Kafka only. For RabbitMQ use `--import-broker`, and for a mapped import use
> [`--map --listen`](#--map).

**Syntax:**
```bash
tdtpcli -config <config.yaml> --listen [--strategy <strategy>]
```

**Example:**
```bash
./tdtpcli -config config.kafka.yaml --listen --strategy replace
```

**Stopping it:** `Ctrl+C` shuts down gracefully.

---

### --export

Export a table to a file or to stdout.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --export <table> [--output <file>]
         [--compact [--fixed-fields <fields>]]
```

**Options:**
- `<table>` — table or view name (required)
- `--output <file>` — output file (defaults to stdout)
- `--fields <cols>` — export only these columns, comma-separated (for example `id,email,status`)
- `--compress` — compress with zstd (level 3 by default)
- `--compress-level <1-19>` — 1 is fastest, 19 is smallest
- `--hash` — **deprecated, does nothing.** The XXH3 checksum is added automatically whenever `--compress` is used. The flag is still accepted so existing scripts keep working
- `--readonly-fields` — include read-only fields (timestamp, computed, identity)
- `--compact` — use the v1.3.1 compact format (carry-forward for fixed fields)
- `--fixed-fields <fields>` — comma-separated list of fixed fields, used with `--compact`; detected automatically from `_prefix` or from the data when omitted
- `--compact-tail` — append a tail row listing every fixed field explicitly, for stream validation and state handover

**Examples:**

To stdout:
```bash
./tdtpcli -config config.postgres.yaml --export users
```

To a file:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

The extension is added for you:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users
# writes: users.tdtp.xml
```

Compact export of a view with `_prefix` columns, detected automatically:
```bash
./tdtpcli -config config.yaml --export dept_employees_report \
  --compact --output report_compact.tdtp.xml
```

Compact export naming the fixed fields:
```bash
./tdtpcli -config config.yaml --export employees \
  --compact --fixed-fields dept_id --output emp_compact.tdtp.xml
```

---

### --import

Import data from a TDTP file.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --import <file> [--table <name>] [--strategy <strategy>]
```

**Options:**
- `<file>` — path to the TDTP file (required)
- `--table <name>` — target table (defaults to the one named in the packet)
- `--strategy <strategy>` — `replace` or `copy`
- `--fields <cols>` — import only these columns, comma-separated

**Example:**
```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

**Output:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📥 Importing from file: users.tdtp.xml
✅ Imported 100 rows into table 'users'
```

**Compact files (v1.3.1):**

A file carrying `compact="true"` has its carry-forward expanded
**automatically** — every row is restored to its full values before anything
reaches the database. No extra flag is needed.

```bash
# A compact file imports exactly like an ordinary one
./tdtpcli -config config.yaml --import dept_report_compact.tdtp.xml --table dept_emp_imported --strategy replace
```

**Import strategies:**

The default follows the packet type:
- **reference** → REPLACE (full replacement through a temporary table)
- **delta** → COPY (insert new rows)
- **response** → REPLACE

---

### Sanitising field names (`--translit`, `--clear`)

These flags apply **only to `--import`**. Export always preserves the original
names: they are the source of truth.

| Flag | Effect |
|------|--------|
| `--clear` | Replaces special characters in field names with safe tokens: `%` → `_pct_`, `$` → `_usd_`, `#` → `_xh_`, `@` → `_at_`, `&` → `_and_`, `?` → `_is_`, `~` → `_not_`, and space, `.`, `,`, `-` → `_`. Anything non-ASCII left over becomes `_`. |
| `--translit` | Transliterates non-ASCII characters to the nearest ASCII through go-unidecode: `Имя` → `Imia`, `Österreich` → `Osterreich`, `Ñoño` → `Nono`. |

They combine: `--translit` runs first, then `--clear`.

**The original names are kept as column comments:**
- PostgreSQL: `COMMENT ON COLUMN t.col IS 'original: Имя пользователя'`
- MySQL: `col TEXT COMMENT 'original: Имя пользователя'`

**Examples:**

```bash
# An MS Access export with fields "Order ID", "Total Cost $", "Discount %"
tdtpcli --import access_orders.tdtp.xml --clear --strategy replace
# Order ID     → Order_ID
# Total Cost $ → Total_Cost_usd_
# Discount %   → Discount_pct_

# An ERP export with Cyrillic field names
tdtpcli --import erp_export.tdtp.xml --translit --clear --strategy replace
# Имя пользователя → Imia_polzovatelia
# Дата рождения    → Data_rozhdeniia

# European diacritics, no special characters
tdtpcli --import eu_staff.tdtp.xml --translit --strategy replace
# Österreich → Osterreich

# Look at the schema before importing
tdtpcli --inspect access_orders.tdtp.xml
```

**In an ETL pipeline** sanitisation is configured per source in the YAML:

```yaml
sources:
  - name: legacy_erp
    type: tdtp
    dsn: erp_export.tdtp.xml
    sanitize:
      translit: true   # "Имя" → "Imia"
      clear: true      # "Total %" → "Total_pct_"
```

**Test files:** `tests/sanitize/` — `access_fields.tdtp.xml`,
`cyrillic_fields.tdtp.xml`, `exotic_mixed.tdtp.xml`, `safe_import.tdtp.xml`.

---

### --export-xlsx

Export a table straight to `.xlsx`, with no TDTP file in between. Useful when
the result is going to a person rather than to another system.

**Syntax:**
```bash
tdtpcli --export-xlsx <table> [--output <file>] [--sheet <name>]
        [--where <condition>] [--order-by <fields>] [--limit <n>] [--fields <col1,col2>]
```

**Options:**

| Flag | Description |
|------|-------------|
| `--export-xlsx <table>` | Table or view name |
| `--output <file>` | Output `.xlsx` (defaults to the table name) |
| `--sheet <name>` | Sheet name (default `Sheet1`) |
| `--where`, `--order-by`, `--limit`, `--fields` | As in `--export` |

**The headers carry the schema.** The first row of the sheet reads
`field_name (TYPE)`, with `*` appended to primary keys. That is not decoration:
it is what `--from-xlsx` and `--import-xlsx` read the types back from. Edit the
header row and you break the return trip.

**What is handled for you** — Excel traps, each of which otherwise corrupts data
silently:

| Case | Behaviour |
|------|-----------|
| `BIGINT` longer than 15 significant digits | Written as a text cell — otherwise Excel rounds it, since its numbers are float64 |
| `NaN`, `±Inf` | Empty cell, the canonical NULL in Excel |
| Dates before 1900 | Written as an ISO string — Excel's serial numbers cannot represent them |
| A string starting `=`, `+`, `-`, `@` | Written with `SetCellStr`, so Excel does not read it as a formula |
| The `[NULL]` marker in a text field | Empty cell |

**Examples:**
```bash
tdtpcli --export-xlsx orders --output orders.xlsx
tdtpcli --export-xlsx orders --sheet Orders --where "status = 'active'" --limit 1000
```

---

### --import-xlsx

Load an `.xlsx` straight into the database, with no TDTP file in between.

**Syntax:**
```bash
tdtpcli --import-xlsx <file> [--table <name>] [--sheet <name>] [--strategy <mode>]
```

**Options:**

| Flag | Description |
|------|-------------|
| `--import-xlsx <file>` | Input `.xlsx` |
| `--table <name>` | Target table (taken from the headers or the filename by default) |
| `--sheet <name>` | Sheet (the first one by default) |
| `--strategy <mode>` | `append`, `replace`, `upsert` — as in `--import` |

**What the file must look like:** headers in the form `field_name (TYPE)` with
keys marked `*` — that is, a file produced by `--export-xlsx` or `--to-xlsx`. An
arbitrary spreadsheet from the internet will not load: there is nowhere to get
the types from.

**What is handled for you:**

| Case | Behaviour |
|------|-----------|
| Error cells (`#N/A`, `#DIV/0!`, `#NUM!`, `#VALUE!`) | NULL — a formula that failed on the sender's machine must not become the literal string `"#N/A"` in your database |
| Dates | The raw serial number is read and converted through the 1900 epoch, **with a correction for Excel's leap-year bug** (serial 60 is 29 February 1900, a date that never existed) |
| Leading and trailing spaces | Trimmed |
| An empty cell | NULL for non-text types, an empty string for `TEXT` — two different things that Excel cannot distinguish |

**Examples:**
```bash
tdtpcli --import-xlsx orders.xlsx --strategy replace
tdtpcli --import-xlsx orders.xlsx --table orders_2026 --sheet Orders
```

---

### --to-xlsx

Convert an existing TDTP file to `.xlsx`, with no database connection.

**Syntax:**
```bash
tdtpcli --to-xlsx <tdtp-file> [--output <file>] [--sheet <name>]
```

Compressed packets (zstd, kanzi) are decompressed automatically. Headers and
trap handling are the same as [`--export-xlsx`](#--export-xlsx).

**Example:**
```bash
tdtpcli --to-xlsx orders.tdtp.xml --output orders.xlsx --sheet Orders
```

---

### --from-xlsx

Convert an `.xlsx` to a TDTP file, with no database connection. The reverse of
`--to-xlsx`.

**Syntax:**
```bash
tdtpcli --from-xlsx <xlsx-file> [--output <file>] [--sheet <name>]
```

Expects the same `field_name (TYPE)` headers and applies the same trap handling
as [`--import-xlsx`](#--import-xlsx). Without `--sheet`, the first sheet is used.

**Example:**
```bash
tdtpcli --from-xlsx orders.xlsx --output orders.tdtp.xml
```

---

### --export-broker

Export a table to a message broker queue.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --export-broker <table>
```

**Options:**
- `<table>` — table name (required)

**Example:**
```bash
./tdtpcli -config config.postgres.yaml --export-broker users
```

**Output:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

📤 Exporting table: users
✅ Successfully published 1 packets to queue 'tdtp_queue'
   Total rows: 100
```

Under `--quiet` this reduces to one line: `users  100 rows  82ms`.

---

### --import-broker

Import data from a message broker queue.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --import-broker
```

**How it behaves:**
- connects to the queue
- waits for a message (blocking)
- imports the data
- acknowledges it manually
- goes back to waiting

**Example:**
```bash
./tdtpcli -config config.postgres.yaml --import-broker
```

**Output:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

🎧 Listening for messages on queue 'tdtp_queue'...
   Press Ctrl+C to stop

📦 Received reference packet for table 'users' (100 rows)
   Type: REFERENCE - full sync via temp table
📋 Import to temporary table: users_tmp_20251116_204210
✅ Data loaded to temporary table
🔄 Replacing production table: users
✅ Production table replaced successfully
   ✓ Message acknowledged and removed from queue
✅ Imported 100 rows into table 'users' (total: 1 packets, 100 rows)

🎧 Waiting for next message...
```

**Stopping it:** `Ctrl+C`.

---

### --sync-incremental

Export only what has changed since the last run.

The watermark is kept in a checkpoint file. On every run `tdtpcli` takes the
rows whose `--tracking-field` is strictly greater than the stored value, and
after a successful export moves the watermark to the new maximum.

```bash
tdtpcli --sync-incremental orders --tracking-field updated_at --checkpoint-file orders.json
```

| Flag | Purpose |
|------|---------|
| `--tracking-field <field>` | The watermark field: `updated_at`, `id`, a version counter. Default `updated_at` |
| `--checkpoint-file <file>` | Where the watermark is stored. Default `checkpoint.yaml` |
| `--batch-size <n>` | Row ceiling for one run (`LIMIT`). Default 1000 |
| `--fields <a,b,c>` | Column projection; the watermark field is always included |
| `--to-broker` | Send to the broker from `--config` instead of writing files |

**What the watermark field must be.** It must only ever increase, and values
must never be reused: a monotonic timestamp, an auto-increment key, a version
counter. A field that can go down loses rows silently — they end up below the
watermark and the next run never sees them. Rows whose `updated_at` equals the
stored watermark are not exported: the comparison is strict, or the last row
would be re-sent on every run.

**The first run** has no watermark and therefore exports the whole table; with
`--batch-size` that takes several consecutive runs.

#### --to-broker

The same increment, into a queue rather than a file:

```bash
tdtpcli --sync-incremental orders --tracking-field updated_at \
        --checkpoint-file orders.json --to-broker --config rabbitmq.yaml
```

Sending goes through the same path as `--export-broker`, so all of its options
apply: `--compress`, `--compress-algo`, `--compress-level`, `--enc`, `--enc13`,
`--mercury-url`.

**The watermark moves only after a successful send.** If the broker is
unreachable or refuses, the checkpoint stays where it is and the reason is
written to `last_error` in the same file — the next run repeats the same rows.
Advancing the watermark before the send would lose data without a trace: the
rows would be below the mark with nothing left to go back to.

`--sync-incremental --to-broker` on the source and
`--map --input broker://<queue> --listen` on the receiver give continuous
replication with no external state scheduler.

---

### --map

Apply a cross-system field mapping: read a TDTP packet, remap fields and enums
according to a mapping YAML, and upsert the rows into the target database.

**Syntax:**
```bash
tdtpcli --map <mapping.yaml> --input <source> [--drain <duration>] [--listen] [--dry-run]
```

| Flag | Purpose |
|------|---------|
| `--map <file>` | The mapping YAML |
| `--input <src>` | A file path, an `s3://` URI, or `broker://<queue>` |
| `--dry-run` | Print the remapping plan and row counts, write nothing |
| `--listen` | Daemon: loop on the queue until SIGTERM |
| `--drain <duration>` | Bounded loop: consume until the queue has been idle this long, then exit |

**`--drain` makes the import a unit of work.** Import had two shapes and neither
suits a schedule: a plain `--input broker://` takes exactly one message per
invocation and cannot keep up with a burst, while `--listen` never ends and so
can never report a result. `--drain` consumes until the queue has been empty for
the given window, then exits with a total:

```bash
tdtpcli --map sync_flights.yaml --input broker:// --drain 5s
public.flights  10 rows  418ms
```

That is work an orchestrator can own: a job record, an approval, a quota, and a
failure that lands somewhere a person will see it. Inside it is the same
`--listen` loop with a deadline on the receive, so acknowledgement still happens
only after the upsert commits, and an interrupted run returns its message to the
queue.

The cost is latency below the tick: a row waits up to the schedule interval plus
the drain window instead of arriving as it is published. Where seconds matter,
`--listen` is still the right shape.

---

### --diff

Compare two TDTP files and show the differences.

**Syntax:**
```bash
tdtpcli --diff <file-a> <file-b> [options]
```

**Options:**
- `<file-a>` — the first file (required)
- `<file-b>` — the second file (required)
- `--key-fields <fields>` — key fields for matching rows, comma-separated
- `--ignore-fields <fields>` — fields to ignore, comma-separated
- `--case-sensitive` — respect case when comparing (off by default)

**Examples:**

```bash
./tdtpcli --diff users-old.xml users-new.xml
```

With an explicit key:
```bash
./tdtpcli --diff users-old.xml users-new.xml --key-fields user_id
```

Ignoring timestamps:
```bash
./tdtpcli --diff users-old.xml users-new.xml --ignore-fields created_at,updated_at
```

Case-sensitive:
```bash
./tdtpcli --diff users-old.xml users-new.xml --case-sensitive
```

**Output:**
```
=== Diff Statistics ===
Total in A: 100
Total in B: 105
Added:      5
Removed:    2
Modified:   3
Unchanged:  95

=== Added (5) ===
+ 101 | John Doe | john@example.com
+ 102 | Jane Smith | jane@example.com
...

=== Removed (2) ===
- 50 | Old User | old@example.com
...

=== Modified (3) ===
~ Key: 10
  [2] email: 'old@mail.com' → 'new@mail.com'
...
```

**Exit codes:**
- 0 — the files are identical, or the comparison completed
- 1 — an error occurred

---

### --merge

Combine several TDTP files into one.

**Syntax:**
```bash
tdtpcli --merge <file1,file2,file3,...> --output <result> [options]
```

**Options:**
- `<file1,file2,...>` — comma-separated list, at least two files
- `--output <file>` — output file (required)
- `--merge-strategy <strategy>` — default `union`
- `--key-fields <fields>` — key fields for deduplication, comma-separated
- `--show-conflicts` — print conflict detail
- `--compress` — compress the result with zstd
- `--compress-level <1-22>` — default 3

**Strategies:**

1. **union** (default) — all unique rows, deduplicated by key
2. **intersection** — only rows present in *every* file
3. **left** / **left-priority** — on conflict keep the value from the first file
4. **right** / **right-priority** — on conflict keep the value from the last file
5. **append** — concatenate everything, no deduplication

**Examples:**

Union with deduplication:
```bash
./tdtpcli --merge users-1.xml,users-2.xml,users-3.xml --output users-merged.xml
```

Intersection:
```bash
./tdtpcli --merge file1.xml,file2.xml --output common.xml --merge-strategy intersection
```

Left priority:
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy left --key-fields user_id
```

Right priority:
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy right --key-fields user_id
```

Append, no deduplication:
```bash
./tdtpcli --merge part1.xml,part2.xml,part3.xml --output all.xml --merge-strategy append
```

Compressed:
```bash
./tdtpcli --merge file1.xml,file2.xml --output merged.xml --compress --compress-level 9
```

Showing conflicts:
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --show-conflicts
```

**Output:**
```
=== Merge Statistics ===
Packets merged: 3
Total rows in:  300
Total rows out: 250
Duplicates:     50
Conflicts:      10

=== Conflicts ===
Key 42: used_new
Key 55: used_new
...
```

**Notes:**
- every file must belong to the same table
- the schemas — the field lists — must match
- deduplication needs key fields, either given or present in the schema

---

### --to-compact

Convert an existing TDTP v1.x file to the v1.3.1 compact format.

**Syntax:**
```bash
tdtpcli --to-compact <input-file> [--output <output-file>] --fixed-fields <fields> [-config <config.yaml>]
```

**Options:**
- `<input-file>` — the source file, v1.0 or v1.3.x (required)
- `--output <file>` — output file; without it the input is rewritten in place
- `--fixed-fields <fields>` — comma-separated fixed fields; detected automatically from `_prefix` or the data when omitted
- `-config <config.yaml>` — only needed if a database connection is involved

**How fixed fields are determined, in order:**
1. An explicit `--fixed-fields f1,f2`
2. `_prefix` — a field whose name begins with `_` becomes fixed, and the `_` is stripped (`_dept_id` → `dept_id`)
3. Data analysis — a field holding the same value on every row is marked fixed

**Examples:**

Naming the fixed fields:
```bash
./tdtpcli --to-compact employees_plain.tdtp.xml \
  --output employees_compact.tdtp.xml \
  --fixed-fields dept_id
```

Auto-detected from a view with `_prefix` columns:
```bash
./tdtpcli --to-compact dept_report.tdtp.xml --output dept_report_compact.tdtp.xml
```

In place:
```bash
./tdtpcli --to-compact report.tdtp.xml --fixed-fields dept_id,region
```

**Notes:**
- the packet version is set to `1.3.1` and `<Data>` gains `compact="true"`
- fixed fields in `<Schema>` gain `fixed="true"`
- rows are encoded carry-forward: within a group only the first row carries the fixed values, the rest leave them empty (`||`)
- compatible with `--compress`: compact and zstd combine

---

### --to-tdtp

Re-filter or re-version an existing TDTP file into a new one, with no database
round-trip.

**Syntax:**
```bash
tdtpcli --to-tdtp <input> [--output <file>] [--v1 | --v13 | --v14]
        [--where <condition>] [--order-by <fields>]
        [--limit <n>] [--offset <n>] [--fields <col1,col2>]
```

It takes the same TDTQL options as [`--to-csv`](#--to-csv), applied in memory,
and writes TDTP instead of CSV. Use it to cut a large export down to the rows a
particular consumer needs, or to hand a v1.4 packet to a reader that predates
v1.4.

**Version flags:**

| Flag | Result |
|------|--------|
| `--v14` | v1.4 with the xxh3 integrity hashes **recomputed** — the default when none is given |
| `--v13` | v1.3.1: expands and clears the Dictionary, leaves any v1.4 hashes untouched |
| `--v1` | Plain v1.0: strips the v1.4 hashes and the Dictionary, expanding its tokens first |

Recomputing rather than copying the v1.4 hashes matters: filtering changes the
rows, so a copied hash would describe data that is no longer there and every
consumer would reject the packet.

**Examples:**
```bash
# Narrow a big export down for one consumer
tdtpcli --to-tdtp all_orders.tdtp.xml --where "region = 'EU'" \
        --fields 'order_id,customer_id,total' --output eu_orders.tdtp.xml

# Downgrade for a reader that predates v1.4
tdtpcli --to-tdtp secure.tdtp.xml --v1 --output legacy.tdtp.xml

# Re-stamp fresh integrity hashes after filtering
tdtpcli --to-tdtp orders.tdtp.xml --limit 1000 --v14 --output sample.tdtp.xml
```

---

### --steps

Run a multi-step workflow from a YAML file. Each step is a `tdtpcli`
sub-process, and steps with no ordering constraint between them run in parallel.

**Syntax:**
```bash
tdtpcli --steps <workflow.yaml> [@name=value ...] [--quiet]
```

**The workflow file:**

```yaml
name: sync-out
description: "Push every changed row into its queue"

steps:
  - id: flights
    command: >-
      --config configs/config_airline.yaml
      --sync-incremental v_flights --tracking-field updated_at
      --to-broker --enc
    on_error: skip

  - id: reservations
    command: >-
      --config configs/config_airline.yaml
      --sync-incremental flight_reservations --tracking-field updated_at
      --to-broker --enc
    depends_on: [flights]
    on_error: retry(3)
```

| Field | Meaning |
|-------|---------|
| `id` | Step name, used by `depends_on` and printed in the log |
| `command` | Arguments passed to `tdtpcli` — the binary itself is implied |
| `depends_on` | Steps that must finish first; absent means "may start immediately" |
| `on_error` | `stop` (default), `skip`, or `retry(N)` |

**Ordering.** Steps are sorted topologically and executed in waves: everything
whose dependencies are satisfied starts at once. Independent steps therefore
finish in whatever order they finish in, and the log interleaves.

**With `--quiet`** the flag is passed down to every child, so a workflow of
eight steps produces eight result lines rather than eight banners. This is what
makes the output usable as an orchestrator job log — see
[ORCHESTRATOR_SCENARIOS.md](ORCHESTRATOR_SCENARIOS.md).

**Variables** work as they do for `--pipeline`: `@name=value` on the command
line is substituted for `{{name}}` in a step's command and in the description.

```bash
tdtpcli --steps workflows/sync_out.yaml @period=2026-07 --quiet
```

---

### --inspect-table

Print extended metadata about a **live database table**, as opposed to
[`--inspect`](#--inspect), which reads a file: native column types, foreign-key
relationships, row count and a sample row.

**Syntax:**
```bash
tdtpcli -config <config.yaml> --inspect-table <table>
```

Intended for finding your way around a schema you did not design — which table
holds what, what the real column types are, and what a row actually looks like —
before writing the query.

```bash
./tdtpcli -config config.mssql.yaml --inspect-table '[ZTR$Employee]'
```

> Exit code 255 from this command does not indicate failure, and it works on
> views as well as tables.

---

### Integrity: --integrity, --mercury-caller

Stamp an export with TDTP v1.4 integrity hashes — xxh3_128 over the Schema, the
Data and the packet as a whole. The full model is in
[SPECIFICATION.md](SPECIFICATION.md#integrity).

| Flag | Purpose |
|------|---------|
| `--integrity` | Compute the three hashes, set the attributes, set `version="1.4"` |
| `--mercury-url <url>` | Also register the fingerprint with xzMercury |
| `--mercury-caller <name>` | Caller identity sent as the `X-Caller` header (default `tdtpcli`) |

Hashes are computed **before** compression, so a receiver decompresses first and
verifies second.

```bash
# Local hashes only
tdtpcli --export users --compress --integrity --output users.tdtp.xml

# Registered with the fingerprint registry, under a service account name
tdtpcli --export orders --compress --integrity \
        --mercury-url http://mercury:3000 --mercury-caller svc-exporter \
        --output orders.tdtp.xml
```

Verification is automatic on the way back in — `--import`, `--to-csv`,
`--to-xlsx`, `--to-html` and `--test` all check the hashes, and a packet without
them is simply passed through.

---

### Other flags

| Flag | Purpose |
|------|---------|
| `--version` | Print the version and exit |
| `--license <file>` | Path to `tdtp.lic`. Without it: the `TDTP_LICENSE` environment variable, then `./tdtp.lic`, then community mode |
| `--quiet` | Reduce output to one result line per table: name, rows, elapsed. Also suppresses the licence banner. Intended for captured output — a workflow step, an orchestrator job, cron |
| `--keep` | Import each broker part as it arrives, accepting partial writes. The default is atomic: every part in one transaction, all or nothing. Use this for batches too large for a single transaction |
| `--expect-var <name=value>` | Require a PipelineContext variable to match before importing; repeatable. The import fails **before** any database write if it does not |
| `--fast` | Skip SpecialValues detection for maximum export speed — no NULL, NaN or Inf markers in the schema |
| `--fallback-row-limit <n>` | Row ceiling for the in-memory fallback when SQL pushdown fails (default 1000000, `0` for unlimited). It exists to stop a broken query turning into a full-table scan against a production database |
| `--validate <file>` | Apply field validation rules from a YAML file |
| `--normalize <file>` | Apply field normalisation rules from a YAML file |
| `--unsafe-cert <file>` | Path to an `unsafe-op.cert` capability certificate, required for privileged operations |

---

## Workflow: inspect, test, import

### Understanding the structure versus checking the integrity

Before importing a file from outside, two checks are worth running:

```
📋 --inspect   →  WHAT is in it?    (structure, fields, types, row count)
🔍 --test      →  Is it INTACT?     (decompression, checksum, completeness)
📥 --import    →  load it
```

| Command | Needs a database? | What it checks |
|---------|-------------------|----------------|
| `--inspect` | no | Field names and types, UUID, table name, row count, compression, compact format |
| `--test` | no | Data integrity: decompression, XXH3 checksum, row count, every part of a multi-part set |
| `--import` | yes | Loads the data — this changes your database |

### A typical run

```bash
# 1. Look at the file — understand its structure
tdtpcli --inspect delivery.tdtp.xml
# file: delivery.tdtp.xml
# table: orders
# schema:
#   fields: 8
#   names: [OrderID, CustomerID, Total, ...]
# data:
#   rows: 5000
# compressed: true  algo: zstd

# 2. Confirm it is intact
tdtpcli --test delivery.tdtp.xml
# ✓ algo=zstd, 5000 rows, decompressed 23ms, checksum OK

# 3. Only then import
tdtpcli --import delivery.tdtp.xml --config pg.yaml --strategy replace
```

### Parallel export and part ordering

Since v1.8.0 export processes packets **in parallel** for speed. That has one
consequence worth knowing:

> **The order of parts in a multi-part file is not guaranteed.**
> `_part_1_of_4`, `_part_2_of_4` and so on are numbered by the order in which
> parallel goroutines finish, not by the order of rows in the table.

This does **not** affect correctness on import: `--import` collects every part
by its `InReplyTo` UUID and reassembles the full set regardless of order. It
matters only when handling the files by hand.

`--test` always confirms that **every** part is present and that each carries
the right `InReplyTo`.

---

## Compact format (v1.3.1)

TDTP v1.3.1 adds a **compact format** for data whose values repeat across groups
of rows.

### How it works

Fields marked `fixed="true"` in the schema are written once per group of rows,
on the group's first row. The remaining rows in the group leave those positions
empty — this is **carry-forward**.

**A compact file:**
```xml
<DataPacket version="1.3.1" ...>
  <Schema>
    <Field name="dept_id"   type="INTEGER" fixed="true"/>
    <Field name="dept_name" type="TEXT"    fixed="true"/>
    <Field name="emp_id"    type="INTEGER"/>
    <Field name="full_name" type="TEXT"/>
  </Schema>
  <Data compact="true">
    <R>10|Sales|101|Ivan Petrov</R>        <!-- dept 10: header row -->
    <R>|||102|Anna Sidorova</R>            <!-- dept 10: carry-forward -->
    <R>|||103|Boris Kozlov</R>             <!-- dept 10: carry-forward -->
    <R>20|Engineering|201|Alice Volkov</R> <!-- dept 20: new group -->
    <R>|||202|Charlie Morozov</R>          <!-- dept 20: carry-forward -->
  </Data>
</DataPacket>
```

### The `_prefix` convention

When exporting a view whose group columns are named with a leading `_`,
`--compact` automatically:
- treats those fields as fixed
- strips the `_` from the name in the Schema (`_dept_id` → `dept_id`)

```sql
CREATE VIEW dept_employees_report AS
SELECT
    d.dept_id   AS _dept_id,    -- becomes fixed="true", named dept_id
    d.dept_name AS _dept_name,  -- becomes fixed="true", named dept_name
    e.emp_id,
    e.full_name
FROM employees e JOIN departments d ON e.dept_id = d.dept_id
ORDER BY d.dept_id, e.emp_id;
```

### Size saved

| Table | Plain | Compact | Saved |
|-------|-------|---------|-------|
| 15 rows, 3 groups × 5 employees, 3 fixed fields | 100% | ~60% | ~40% |
| 1000 rows, 10 groups × 100 employees, 5 fixed fields | 100% | ~30% | ~70% |

The benefit grows with the number of rows per group and the number of fixed
fields.

---

## ETL pipeline

### --pipeline

Run an ETL pipeline from a YAML configuration: load several sources, transform
them in an in-memory SQLite workspace, export the result.

Full documentation with examples: [ETL_PIPELINE.md](ETL_PIPELINE.md).

**Syntax:**
```bash
tdtpcli --pipeline <config.yaml> [--unsafe] [--enc] [--enc-dev]
```

**Options:**

| Flag | Description |
|------|-------------|
| `--pipeline <file>` | Path to the pipeline YAML |
| `--unsafe` | Allow any SQL operation (requires admin rights) |
| `--enc` | Override `output.tdtp.encryption: true` — encryption through xZMercury |
| `--enc-dev` | Dev mode: generate the key locally, no xZMercury (non-production builds only) |

**SQL safety modes:**

| Mode | SQL allowed | Rights |
|------|-------------|--------|
| Safe (default) | SELECT and WITH only | none |
| Unsafe (`--unsafe`) | everything | admin |

**Examples:**

```bash
./tdtpcli --pipeline pipeline.yaml
```

With encryption:
```bash
./tdtpcli --pipeline pipeline.yaml --enc
```

Dev-mode encryption, key generated locally:
```bash
./tdtpcli --pipeline pipeline.yaml --enc-dev
```

Unsafe mode:
```bash
sudo ./tdtpcli --pipeline pipeline.yaml --unsafe
```

**On success:**
```
Pipeline: employee-dept-report
   Salary report by department
   Version: 1.0
   Mode: SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 2
   Workspace: sqlite (:memory:)
   Output: tdtp [ENC: xZMercury]

Starting ETL pipeline execution...

ETL Pipeline completed successfully!
   Duration: 1.23s
   Sources loaded: 2
   Rows loaded: 14
   Rows exported: 4
   Package UUID: 550e8400-e29b-41d4-a716-446655440000
```

**When xZMercury degrades:**
```
WARNING: Encryption degraded: bind key: MERCURY_UNAVAILABLE: ...
   Error packet written to output. Pipeline completed with errors (exit 0).
```

---

## AES-256-GCM encryption

> This section describes the v1.3 whole-blob format. Protocol v1.5 adds
> section-level encryption, which `--enc` now produces by default; `--enc13`
> requests the legacy format described here. See
> [SPECIFICATION.md](SPECIFICATION.md).

The CLI encrypts its output through the **xZMercury UUID-binding flow**:

```
tdtpcli ──→ POST /api/keys/bind ──→ xZMercury
                                       │
                                ┌──────┘
                                │ {key_b64, hmac}
                                ▼
              Verify HMAC (MERCURY_SERVER_SECRET)
                                │
                                ▼
              AES-256-GCM encrypt(XML, key)
                                │
                                ▼
              Write .tdtp.enc (binary header + ciphertext)
```

### YAML configuration

```yaml
output:
  type: tdtp
  tdtp:
    destination: "out/report.tdtp.enc"
    encryption: true          # turns encryption on

security:
  mercury_url: "http://mercury:3000"
  key_ttl_seconds: 86400      # key TTL, 24 hours
  mercury_timeout_ms: 5000    # request timeout
```

### Environment

```bash
MERCURY_SERVER_SECRET=<secret>   # verifies the HMAC signature on the key
```

### Testing against the mock

```bash
# 1. Start the xZMercury mock
go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret

# 2. Set the secret
export MERCURY_SERVER_SECRET=dev-secret

# 3. Run the pipeline
./tdtpcli --pipeline examples/encryption-test/pipeline-enc.yaml
```

### Dev mode, without xZMercury

Development builds — `go build` without the `production` tag — accept `--enc-dev`:

```bash
./tdtpcli --pipeline pipeline.yaml --enc-dev
```

- the AES-256 key is generated locally
- xZMercury is not needed
- the HMAC is not verified
- the flag does not exist in a production build (`-tags production`)

### The encrypted file layout

```
[2 bytes: version] [1 byte: algorithm] [16 bytes: package UUID]
[12 bytes: AES-GCM nonce] [N bytes: ciphertext + tag]
```

### Graceful degradation

When xZMercury is unreachable:
- unencrypted data is **not written**
- an `error` packet goes to the destination (TDTP `Type=error`)
- the pipeline exits **0**
- the result log records `completed_with_errors` with a `package_uuid`

---

## Filtering with TDTQL

### Filter options

| Option | Description | Example |
|--------|-------------|---------|
| `--where` | A condition; **repeatable** — several flags combine with AND | `--where "age > 25"` |
| `--order-by` | Sort order | `--order-by "balance DESC"` |
| `--limit` | Row cap | `--limit 100` |
| `--offset` | Rows to skip | `--offset 50` |

### Field names with spaces and special characters

Fields from MSSQL and MS Access often contain spaces, `$`, `%`, `#` and more. In
TDTQL use **square brackets**, as in SSMS:

```bash
# The "Termination Date" field of ZTR$Employee (MSSQL)
# bash/zsh: a table name containing $ must be single-quoted, with brackets
tdtpcli --export '[ZTR$Employee]' --where '[Termination Date] = '"'"'1753-01-01'"'"'
# PowerShell: table in single quotes, --where in double quotes with the value in single
.\tdtpcli.exe --export '[ZTR$Employee]' --where "[Termination Date] = '1753-01-01'"

# A "Total Cost $" field from an Access export
tdtpcli --export orders --where '[Total Cost $] > 100'

# A field containing a question mark
tdtpcli --export leads --where '[Is Active?] = 1'
```

The brackets are removed during parsing and the name is quoted correctly for the
target database:
- MSSQL: `[Termination Date]`
- PostgreSQL and SQLite: `"Termination Date"`
- MySQL: `` `Termination Date` ``

This works with every operator: `=`, `>`, `IN`, `BETWEEN`, `IS NULL`, `LIKE`.

### WHERE operators

**Numeric comparisons:**
```bash
--where "age > 25"
--where "balance >= 1000.50"
--where "quantity < 10"
--where "price <= 99.99"
```

**Text:**
```bash
--where "username = 'admin'"
--where "status != 'deleted'"
```

**Boolean:**
```bash
--where "is_active = 1"
--where "is_verified = 0"
```

**NULL:**
```bash
--where "deleted_at IS NULL"
--where "email IS NOT NULL"
```

> **Important:** always use `IS NULL` and `IS NOT NULL`. The construction
> `field = NULL` is not valid SQL — it is always false.

**Lists (IN / NOT IN):**
```bash
--where "status IN (active,pending,review)"
--where "dept_id IN (10,11,12)"
--where "role NOT IN (guest,banned)"
```

Works with both numbers and strings. The parentheses are required.

**Ranges (BETWEEN):**
```bash
--where "age BETWEEN 18 AND 65"
--where "salary BETWEEN 50000 AND 150000"
```

**Patterns (LIKE):**
```bash
--where "email LIKE '%@gmail.com'"
--where "name LIKE 'Ivan%'"
```

**Several `--where` flags (AND):**

Each `--where` adds one condition, and all of them combine with AND:
```bash
./tdtpcli --export staff \
  --where "dept_id IN (10,11,12)" \
  --where "employment_type IN (1,2)" \
  --where "salary > 30000"
```

Equivalent to `WHERE dept_id IN (10,11,12) AND employment_type IN (1,2) AND salary > 30000`.

### Sorting

**One field:**
```bash
--order-by "created_at DESC"
--order-by "username ASC"
```

**Several:**
```bash
--order-by "balance DESC, age ASC"
--order-by "city ASC, created_at DESC"
```

### Paging

**First 100 rows:**
```bash
--limit 100
```

**Rows 51 to 100:**
```bash
--limit 50 --offset 50
```

### Combined queries

**Filter, sort, limit:**
```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "balance >= 5000" \
  --order-by "balance DESC" \
  --limit 20
```

**Paging with a filter:**
```bash
./tdtpcli -config config.postgres.yaml --export orders \
  --where "status = 'completed'" \
  --order-by "order_date DESC" \
  --limit 50 --offset 100
```

### Filtering on export to a broker

```bash
./tdtpcli -config config.postgres.yaml --export-broker users \
  --where "is_active = 1" \
  --limit 1000
```

### Filtering during CSV conversion

`--to-csv` supports **every TDTQL filter** that `--export` does. Filters are
applied in memory after the packet is read and decompressed — no database
involved.

**Compatible with:**
- ordinary TDTP files
- compressed files (zstd, kanzi) — decompressed before filtering
- compact v1.3.1
- v1.4 integrity packets — hashes verified before filtering

**Selected columns:**
```bash
# Simple names
tdtpcli --to-csv users.tdtp.xml --fields 'id,email,balance'

# Names containing spaces — square brackets
tdtpcli --to-csv staff.tdtp.xml --fields '[Last Name],[Birth Date],salary'
```

**Row filters:**
```bash
# Numeric
tdtpcli --to-csv orders.tdtp.xml --where 'total > 1000'

# Text
tdtpcli --to-csv users.tdtp.xml --where "status = 'active'"

# Several conditions, combined with AND
tdtpcli --to-csv orders.tdtp.xml \
  --where 'total > 500' \
  --where "status = 'completed'"
```

**Sorting and limiting:**
```bash
# Top ten by total
tdtpcli --to-csv orders.tdtp.xml \
  --order-by 'total DESC' \
  --limit 10

# Last hundred events (tail mode)
tdtpcli --to-csv events.tdtp.xml \
  --order-by 'created_at ASC' \
  --limit -100
```

**All together:**
```bash
tdtpcli --to-csv orders.tdtp.xml \
  --fields 'id,customer_id,total,status' \
  --where "status = 'completed'" \
  --where 'total >= 1000' \
  --order-by 'total DESC' \
  --limit 50 \
  --delimiter ';' --bom \
  --output top_orders.csv
```

---

## Message brokers

### RabbitMQ

**Local, no TLS:**
```yaml
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
```

**Managed with TLS — CloudAMQP, Amazon MQ and similar:**
```yaml
broker:
  type: rabbitmq
  host: seal.lmq.cloudamqp.com
  port: 5671              # TLS port
  user: myuser
  password: mypassword
  vhost: myuser           # on CloudAMQP the vhost is the username
  queue: myqueue
  use_tls: true
  tls_skip_verify: true   # for a self-signed or provider certificate
  passive_declare: true   # the queue already exists — leave its settings alone
```

**Queue settings:**
- `durable: true` — the queue survives a RabbitMQ restart
- `auto_delete: false` — the queue is not deleted automatically
- `exclusive: false` — more than one connection may use it
- `passive_declare: true` — **do not create** the queue, only attach to an existing one; this is what avoids `406 PRECONDITION_FAILED` when another system created the queue with different settings

> **When to set `passive_declare: true`?**
> When the queue belongs to another service — a Spring Boot application, a PHP
> consumer — and you neither know nor control its settings. `tdtpcli` then
> attaches without trying to redeclare it.

**A typical flow:**

1. **System A** exports:
```bash
./tdtpcli -config config.postgres.yaml --export-broker users --where "updated_at >= '2025-11-16'"
```

2. **System B** imports:
```bash
./tdtpcli -config config.sqlite.yaml --import-broker
```

### MSMQ (Windows)

**Configuration:**
```yaml
broker:
  type: msmq
  queue: .\\private$\\tdtp_queue
```

**Notes:**
- Windows only
- works with local and networked MSMQ queues
- transactional queues are supported

**Example:**
```bash
tdtpcli.exe -config config.mssql.yaml --export-broker users
```

---

## Worked examples

### 1. Synchronising a reference table from PostgreSQL to SQLite

**Step 1** — export from PostgreSQL:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

**Step 2** — import into SQLite:
```bash
./tdtpcli -config config.sqlite.yaml --import users.tdtp.xml
```

### 2. Exporting only active users

Active users with a balance over 1000:

```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "is_active = 1" \
  --where "balance > 1000" \
  --order-by "balance DESC" \
  --output active_users.tdtp.xml
```

`--where` is repeatable; the conditions combine with AND.

### 3. Replication through RabbitMQ

Continuous replication of orders from MS SQL to PostgreSQL.

**Terminal 1 (MS SQL, publisher)** — run from cron or a scheduled task:
```bash
./tdtpcli -config config.mssql.yaml --export-broker orders \
  --where "created_at >= '2025-11-16 12:00:00'"
```

**Terminal 2 (PostgreSQL, subscriber):**
```bash
./tdtpcli -config config.postgres.yaml --import-broker
```

### 4. Top 20 customers by balance

```bash
./tdtpcli -config config.postgres.yaml --export customers \
  --order-by "balance DESC" \
  --limit 20 \
  --output top_customers.tdtp.xml
```

### 5. Paging through a large table

A million rows, ten thousand at a time:

```bash
# First chunk (0–9999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 0 --output part_01.tdtp.xml

# Second chunk (10000–19999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 10000 --output part_02.tdtp.xml

# and so on
```

### 6. Exporting to stdout and piping

```bash
./tdtpcli -config config.postgres.yaml --export users | \
  grep "balance" | \
  wc -l
```

---

## Troubleshooting

### "Database connection failed"

**Symptom:**
```
❌ Error connecting to database: connection refused
```

**What to check:**
1. The database is running:
   ```bash
   # PostgreSQL
   sudo systemctl status postgresql

   # MS SQL in Docker
   docker ps | grep mssql
   ```
2. The connection settings in `config.yaml`
3. The firewall and the port:
   ```bash
   telnet localhost 5432
   ```

### "Table not found"

**Symptom:**
```
❌ Table 'users' does not exist
```

**What to check:**
1. The table list:
   ```bash
   ./tdtpcli -config config.yaml --list
   ```
2. On PostgreSQL, the schema:
   ```yaml
   database:
     schema: public  # or wherever the table actually is
   ```

### "Permission denied"

**Symptom:**
```
❌ Error: permission denied for table users
```

**What to check:**
1. The database user's rights
2. On PostgreSQL:
   ```sql
   GRANT SELECT, INSERT, UPDATE ON TABLE users TO tdtp_user;
   ```

### "Broker connection failed"

**Symptom:**
```
❌ Failed to connect to broker: dial tcp: connection refused
```

**What to check:**
1. RabbitMQ is running:
   ```bash
   sudo systemctl status rabbitmq-server
   ```
2. The connection settings:
   ```yaml
   broker:
     host: localhost  # correct host?
     port: 5672       # correct port?
   ```
3. The credentials — RabbitMQ's default `guest`/`guest` works only from localhost

### "Packet too large"

**Symptom:**
```
⚠️ Warning: Packet size exceeds recommended limit
```

**What to do:**
1. Filter to reduce the size:
   ```bash
   --limit 1000
   ```
2. Or raise `MaxMessageSize` in code:
   ```go
   generator.SetMaxMessageSize(5000000) // 5MB
   ```

### "Invalid TDTP format"

**Symptom:**
```
❌ Failed to parse TDTP file: invalid XML
```

**What to check:**
1. The file is well-formed XML:
   ```bash
   xmllint --noout users.tdtp.xml
   ```
2. The file is not truncated
3. The file was produced by `tdtpcli` rather than written by hand

---

## Further reading

- **[SPECIFICATION.md](SPECIFICATION.md)** — the full TDTP specification, including the compact format and special values
- **[ETL_PIPELINE.md](ETL_PIPELINE.md)** — pipeline configuration and worked scenarios
- **[DEVELOPER_GUIDE.md](DEVELOPER_GUIDE.md)** — architecture, core modules, TDTQL internals, writing an adapter
- **[ORCHESTRATOR_SCENARIOS.md](ORCHESTRATOR_SCENARIOS.md)** — running pipelines under the orchestration server
- **[DEPLOYMENT.md](DEPLOYMENT.md)** — deploying the whole system

---

## Feedback

- **GitHub Issues:** https://github.com/ruslano69/tdtp-framework/issues
- **Email:** ruslano69@gmail.com

---

## CLI usage examples

```bash
# List tables
tdtpcli --list --config pg.yaml

# Export table
tdtpcli --export users --output users.xml

# Inspect TDTP file metadata (no database connection needed)
tdtpcli --inspect users.xml

# Test file integrity before import (checksum, row count, multi-part)
tdtpcli --test users.xml

# Recommended pre-import workflow:
tdtpcli --inspect delivery.xml   # understand structure
tdtpcli --test    delivery.xml   # verify integrity
tdtpcli --import  delivery.xml   # load to DB

# Export only specific columns (column projection)
tdtpcli --export clients --fields id,email,status --output clients_slim.xml

# Export with bracket-quoted field names (spaces/special chars)
tdtpcli --export orders --where "[Termination Date] = '1753-01-01'"
tdtpcli --export orders --fields "id,[Birth Date],status"

# Export with column projection + filter + compression
tdtpcli --export orders --fields order_id,amount,status --where 'status = active' --compress

# Export with kanzi compression (4× denser than raw, ideal for large text)
tdtpcli --export operations_log --compress --compress-algo kanzi --compress-level 6 --output ops.xml

# Export with compression + XXH3 integrity checksum
tdtpcli --export orders --compress --hash --output orders.xml

# Archive-quality: kanzi max + checksum + large packet for broker
tdtpcli --export payroll --compress --compress-algo kanzi --compress-level 7 --hash --packet-size 8 --output archive.xml

# Encrypt export (AES-256-GCM, burn-on-read key via xZMercury)
tdtpcli --export financials --enc --output financials.tdtp.xml
# → writes financials.tdtp.enc

# Import encrypted file (auto-detected by .tdtp.enc extension)
tdtpcli --import financials.tdtp.enc

# Convert encrypted file to Excel or HTML
tdtpcli --to-xlsx financials.tdtp.enc --output financials.xlsx
tdtpcli --to-html financials.tdtp.enc --open

# Import only specific columns from a wide TDTP file
tdtpcli --import clients_full.xml --fields id,email,status --table clients_slim

# Import Access export with exotic field names (%, spaces, #, etc.)
tdtpcli --import access_export.xml --clear --strategy replace

# Import ERP export with Cyrillic field names — transliterate + clean
tdtpcli --import erp_orders.xml --translit --clear --strategy replace

# Export with filters and compression
tdtpcli --export orders --where 'status = active AND amount > 1000' --limit 100 --compress

# Filter by list of values (IN operator)
tdtpcli --export staff --where 'dept_id IN (10,11,12)'

# Multiple --where flags combined with AND
tdtpcli --export staff --where 'dept_id IN (10,11,12)' --where 'employment_type IN (1,2)'

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

# Incremental sync straight into a broker queue
tdtpcli --sync-incremental orders --tracking-field updated_at \
  --checkpoint-file orders.json --to-broker --config rabbitmq.yaml

# Drain a queue into its target table and report the total
tdtpcli --map mappings/sync_flights.yaml --input broker:// --drain 5s

# Export with PII masking
tdtpcli --export customers --mask email,phone

# ETL pipeline (safe mode)
tdtpcli --pipeline pipeline.yaml

# ETL pipeline (unsafe mode, requires admin)
sudo tdtpcli --pipeline pipeline.yaml --unsafe

# Quiet mode: one line per table — name, rows, elapsed
tdtpcli --quiet --export-broker users --config rabbitmq.yaml

# Create configuration file
tdtpcli --create-config-pg > config.yaml
tdtpcli --create-config-mysql > mysql.yaml

# --- Broker Operations ---

# Export to Kafka with kanzi compression (parallel compress + SendBatch)
tdtpcli --export-broker users --compress --compress-algo kanzi --compress-level 6 --config kafka.yaml

# Import from RabbitMQ / Kafka to database (atomic by default)
tdtpcli --import-broker --config rabbitmq.yaml
tdtpcli --import-broker --config kafka.yaml --strategy replace

# Import from broker — save as TDTP files instead of importing to DB
tdtpcli --import-broker --output users.xml --config kafka.yaml

# Import from broker — save raw bytes (no parse/decompress)
tdtpcli --import-broker --raw --output users.bin --config kafka.yaml

# Streaming consumer daemon — Kafka only (production-ready)
tdtpcli --listen --config kafka.yaml --strategy replace

# --- Object Storage (S3 / SeaweedFS / MinIO) ---

# Export table to S3 (multi-part automatic)
tdtpcli --export users --output s3://my-bucket/exports/users.tdtp.xml

# Export with compression + filter → S3
tdtpcli --export orders \
  --where "status = active" --where "amount > 1000" \
  --compress --compress-level 3 \
  --output s3://my-bucket/exports/orders_active.tdtp.xml

# Export with checksum integrity
tdtpcli --export users --compress --hash \
  --output s3://my-bucket/exports/users.tdtp.xml

# Import from S3 (all _part_N_of_M files discovered automatically)
tdtpcli --import s3://my-bucket/exports/users.tdtp.xml --strategy replace

# Import only selected columns from S3
tdtpcli --import s3://my-bucket/exports/users.tdtp.xml \
  --fields "id,email,balance" --table users_slim

# Inspect a packet in S3 without downloading full data
tdtpcli --inspect s3://my-bucket/exports/users.tdtp_part_1_of_6.xml

# Test a file in S3 before import
tdtpcli --test s3://my-bucket/exports/users.tdtp.xml

# Export table directly to XLSX in S3
tdtpcli --export-xlsx users --output s3://my-bucket/reports/users.xlsx

# Convert local TDTP to XLSX and upload to S3
tdtpcli --to-xlsx users.tdtp.xml --output s3://my-bucket/reports/users.xlsx

# S3 → S3: convert compressed TDTP in S3 to XLSX in S3
tdtpcli --to-xlsx s3://my-bucket/exports/users.tdtp_part_1_of_6.xml \
  --output s3://my-bucket/reports/users_part1.xlsx
```
