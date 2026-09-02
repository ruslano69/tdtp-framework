# ETL Pipeline — guide and scenarios

Reference for `tdtpcli --pipeline`.

> **Encryption in this document is the v1.3 whole-blob format** (`.tdtp.enc`).
> Protocol v1.5 adds section-level encryption, where the `Header` stays plain so
> a broker can still route the message while `Schema` and `Data` are ciphertext.
> See [SPECIFICATION.md](./SPECIFICATION.md). Both formats work; the examples
> below have not yet been rewritten for v1.5.

---

## Contents

1. [Overview](#overview)
2. [Configuration reference](#configuration-reference)
3. [Pipeline variables](#pipeline-variables)
4. [Scenario 1: two TDTP sources, joined](#scenario-1-two-tdtp-sources-joined)
5. [Scenario 2: PostgreSQL to TDTP](#scenario-2-postgresql-to-tdtp)
6. [Scenario 3: encrypted output through xZMercury](#scenario-3-encrypted-output-through-xzmercury)
7. [Scenario 4: Redis orchestration](#scenario-4-redis-orchestration)
8. [Scenario 5: graceful degradation](#scenario-5-graceful-degradation)
9. [Pipeline CLI flags](#pipeline-cli-flags)
10. [Exit codes](#exit-codes)

---

## Overview

A pipeline runs in three phases:

```
Extract                Transform              Load
────────               ────────────           ────────
TDTP File ──┐                                 TDTP File
PostgreSQL ─┼─→ SQLite Workspace ─→ SQL ─┬─→ RabbitMQ
MSSQL ──────┘    (:memory:)         JOIN  └─→ Kafka
MySQL ──────┘                              └─→ XLSX
SQLite ─────┘
```

Every source is loaded as a table into an in-memory SQLite workspace. One SQL
transformation joins them and produces the result. The export writes that result
wherever the output section points.

---

## Configuration reference

### Full YAML schema

```yaml
name: "pipeline-name"       # required
version: "1.0"              # optional
description: "..."          # optional

# ─── SOURCES ──────────────────────────────────────────────────────────────────
sources:
  - name: table_alias       # table name in the SQLite workspace (required)
    type: sqlite            # sqlite | postgres | mssql | mysql | tdtp | tdtp-enc | tdtp-s3
    dsn: "path/to/db.db"    # DSN, or path to a TDTP file (or S3 key for tdtp-s3)
    query: |                # SQL query (not used by type: tdtp/tdtp-enc/tdtp-s3)
      SELECT id, name FROM users
    timeout: 30             # seconds (0 = no timeout)
    multi_part: false       # type: tdtp/tdtp-s3 — load every part of the set
    fast: false              # skip SpecialValues detection for this source's own export path
    no_date_sentinels: []    # e.g. ["1900-01-01", "1753-01-01"] — DB-specific "no date" values
    sanitize:                # field-name sanitizer, applied on this source's own import paths
      translit: false        # transliterate non-ASCII names
      clear: false           # replace unsafe characters (spaces, %, $, ...)
    mercury_url: ""          # type: tdtp-enc only — xZMercury URL for burn-on-read decrypt
    mercury_timeout_ms: 5000 # type: tdtp-enc only
    s3:                      # type: tdtp-s3 only — S3-compatible storage config
      bucket: "my-bucket"
      endpoint: "http://localhost:9000"
      access_key: "..."
      secret_key: "..."

# ─── WORKSPACE ────────────────────────────────────────────────────────────────
workspace:
  type: sqlite
  mode: ":memory:"          # ":memory:" or a file path ("workspace.db")

# ─── TRANSFORM ────────────────────────────────────────────────────────────────
transform:
  result_table: "result"    # name of the result table (optional)
  timeout: 60               # seconds
  sql: |
    SELECT ...
    FROM table_alias_1 t1
    JOIN table_alias_2 t2 ON t1.id = t2.fk_id
    WHERE ...

# ─── OUTPUT ───────────────────────────────────────────────────────────────────
output:
  type: tdtp                # tdtp | rabbitmq | kafka | xlsx

  tdtp:
    destination: "out/result.xml"
    format: "xml"           # xml (the only supported value)
    compression: false      # zstd — same setting as `compress` below; SetDefaults folds compress into it
    compress_algo: "zstd"   # zstd (default) | kanzi
    compress_level: 3       # zstd 1-19, kanzi 6-7
    encryption: false       # AES-256-GCM through xZMercury (v1.5 section-level by default)
    encryption_v13: false   # true = legacy v1.3 whole-blob format instead of v1.5
    compact: false          # v1.3.1 compact format (carry-forward fixed fields)
    compact_tail: false     # v1.3.1: write an explicit tail row
    fixed_fields: []        # v1.3.1: explicit fixed-field names; empty = auto-detect from `_` prefix
    columnar: false         # Data layout="columns" — see --columnar on the CLI
    packet_size_mb: 0       # part size in MB of real XML; 0 = library default (~1.9 MB)
    fast: false             # skip SpecialValues detection for this output; overrides performance.fast
    s3:                     # S3-compatible destination instead of a local path
      bucket: "my-bucket"
      endpoint: "http://localhost:9000"
      access_key: "..."
      secret_key: "..."

  rabbitmq:                 # when type: rabbitmq
    host: localhost
    port: 5672
    user: guest
    password: guest
    queue: etl_results
    vhost: "/"

  kafka:                    # when type: kafka
    brokers: "localhost:9092"
    topic: etl_results
    packet_kb: 750          # packet size before compression, KB (see "Message size")
    batch_send: 10          # packets per produce request
    spool_dir: ""           # "" = os.TempDir()/tdtp-kafka-spool; a path makes the spool persistent
    mem_limit_mb: 0         # 0 = disk-spool (resumable); >0 = in-memory path with backpressure, no resumability
    compress_algo: "zstd"   # zstd (default) | none
    compress_level: 3       # zstd level

  xlsx:                     # when type: xlsx
    destination: "out/result.xlsx"
    sheet: "Sheet1"

# ─── SECURITY (for encryption: true) ──────────────────────────────────────────
security:
  mercury_url: "http://mercury:3000"  # xZMercury URL
  key_ttl_seconds: 86400              # key TTL in Redis (default 86400)
  mercury_timeout_ms: 5000            # request timeout (default 5000)
  recipient_resource: ""              # recipient queue/resource name, for the audit trail
  server_secret: ""                   # HMAC key for xZMercury responses; falls back to $MERCURY_SERVER_SECRET

# ─── RESULT LOG ───────────────────────────────────────────────────────────────
result_log:
  type: redis               # redis (empty = disabled)
  address: "127.0.0.1:6379"
  name: "PIPELINE_V001"     # Redis key/channel
  password: ""              # optional
  db: 0                     # Redis database index
  ttl: 3600                 # seconds

# ─── PERFORMANCE ──────────────────────────────────────────────────────────────
performance:
  batch_size: 10000
  parallel_sources: true    # load sources concurrently
  max_memory_mb: 2048
  fast: false                # global --fast: skip SpecialValues detection everywhere; ~5x faster GenerateReference

# ─── ERROR HANDLING ───────────────────────────────────────────────────────────
error_handling:
  on_source_error: "fail"        # fail | continue — the only one of these five actually enforced (see below)
  on_transform_error: "fail"     # accepted, validated, not yet acted on
  on_output_error: "fail"        # accepted, validated, not yet acted on
  retry_attempts: 3              # accepted, not yet acted on — nothing retries
  retry_delay_seconds: 5         # accepted, not yet acted on
```

**Only `on_source_error` changes what actually happens.** `continue` skips a
failed source and carries on with the rest; `fail` (default) stops the
pipeline on the first source error — both are real, both are exercised by
tests. `on_transform_error`, `on_output_error`, `retry_attempts` and
`retry_delay_seconds` are parsed, defaulted, and validated for their allowed
values, but nothing in the pipeline runner reads them at the point a
transform or an output actually fails — such a failure always stops the
pipeline immediately, regardless of what these four say. Filed as an open
item in `TODO_NEXT.md`; do not rely on them today. `performance.timeout` is
not a real field at all — earlier revisions of this document described one
that was never implemented.

**Note the two spellings of the same field:** `output.tdtp.compression` and
`output.tdtp.compress` set the same thing (the loader folds `compress` into
`compression`); either works, but reading `compress` back after `SetDefaults`
runs will show `false` even when compression is on — read `compression`.

### Kafka: message size

Kafka rejects a produce request against `message.max.bytes`, and what it
measures is **the whole compressed record batch**, not the individual record.
Two consequences follow, and both have been hit in practice.

**The batch counts as one.** Ten packets of 270 KB are 2.7 MB in a single
request, and a broker with the default 1 MB limit rejects all ten — though any
one of them would have passed alone. The setting to reduce is `batch_send`, not
`packet_kb`.

**Compression does not always save you.** The client compresses with Snappy, and
ordinary tabular data shrinks about threefold, which is why this went unnoticed
for years. But the ratio belongs to the data: UUIDs, base64, ciphertext and
already-compressed fields do not shrink at all, and the same export is rejected
on them. Test against your data, not against the demo set.

**Recommended broker setting is 4 MB per message:**

```properties
message.max.bytes=4194304
replica.fetch.max.bytes=4194304   # must not be lower than message.max.bytes
```

Four megabytes cover a whole TDTP part (roughly 1.9 MB of uncompressed XML) even
when compression does nothing, and leave room for a batch of several small ones.

`replica.fetch.max.bytes` must rise with `message.max.bytes` and never sit below
it. Otherwise the leader accepts a message the replicas cannot fetch and the
partition goes under-replicated — invisible at replication factor 1, and a quiet
loss of fault tolerance on a real cluster.

Where broker settings cannot be changed — managed services such as Confluent
Cloud, Event Hubs and MSK keep the 1 MB default — reduce `packet_kb` and
`batch_send`. `tdtpcli` warns before exceeding the limit, and on rejection
prints the size of every packet in the batch.

### Source types

| type | DSN format | query |
|------|-----------|-------|
| `sqlite` | `path/to/db.sqlite` | SQL SELECT |
| `postgres` | `postgres://user:pass@host:5432/db?sslmode=disable` | SQL SELECT |
| `mssql` | `server=host;user id=sa;password=X;database=DB` | SQL SELECT |
| `mysql` | `user:pass@tcp(host:3306)/db?parseTime=true` | SQL SELECT |
| `tdtp` | `path/to/file.tdtp.xml` | not used |
| `tdtp-enc` | `path/to/file.tdtp.enc` | not used — needs `mercury_url` |
| `tdtp-s3` | `s3://bucket/key`, or just `key` with `s3.bucket` set | not used |

---

## Pipeline variables

Variables make a pipeline parametric: one YAML file, many runs with different
filters — department, period, object code and so on.

### Passing them

```bash
# One variable
./tdtpcli.exe --pipeline report.yaml @dept=97-256

# Quotes around the value are stripped automatically — equivalent
./tdtpcli.exe --pipeline report.yaml @dept="97-256"

# Several
./tdtpcli.exe --pipeline report.yaml @dept=97-256 @date_from=2025-01-01 @date_to=2025-12-31
```

### Using them in YAML

| Context | Pattern | Example |
|---------|---------|---------|
| SQL — string literal | `'@name'` | `WHERE dept = '@dept'` |
| SQL — numeric or bare | `@name` | `WHERE year = @year` |
| YAML fields | `{{name}}` | `destination: "out/dept_{{dept}}.tdtp.xml"` |

The same variable may be used in both contexts at once.

### Fields that accept substitution

- `sources[].query` — source SQL (`'@name'` and `@name`)
- `sources[].dsn` — connection string (`{{name}}`)
- `transform.sql` — transformation SQL (`'@name'` and `@name`)
- `description` — pipeline description (`{{name}}`)
- `output.tdtp.destination` — output path (`{{name}}`)
- `output.xlsx.destination` — XLSX path (`{{name}}`)
- `output.fallback.tdtp.destination` — fallback chain (`{{name}}`)

### Validation

- Declared in the config (`'@dept'` or `{{dept}}`) but **not passed** on the CLI → **error**, the pipeline does not start.
- Passed on the CLI but **not used** in the config → **warning**, the pipeline continues.

### Safety

Substitution happens **before** the SQL validator, so injection attempts through
a variable value are blocked by the existing validator (`SELECT`/`WITH` only in
safe mode). Inside string literals (`'@name'`) single quotes in the value are
escaped automatically (`'` → `''`).

### Example: a departmental report over a period

```yaml
name: "dept-staff-hiredate"
description: "Department {{dept}} roster, {{date_from}} - {{date_to}}"
version: "1.0"

sources:
  - name: data
    type: mssql
    dsn: "server=sql-srv1;database=ZTR;trusted_connection=yes"
    query: |
      SELECT
          s.[ТабНомер],
          s.[ФИО],
          s.[ПолнаяДолжність],
          CONVERT(varchar(10), eh.[Employment Date], 104) AS ДатаПриема
      FROM dbo.vw_ActiveStaff s
      JOIN [ZTR$Employment History] eh
          ON eh.[Employee No_] = s.[КодСотрудника]
         AND eh.[Termination Date] = '1753-01-01'
      WHERE s.[КодПодразділення] = '@dept'
        AND eh.[Employment Date] >= '@date_from'
        AND eh.[Employment Date] <= '@date_to'
        AND s.[ТипЗанятості] = 1
      ORDER BY s.[ФИО]

workspace:
  type: sqlite
  mode: memory

transform:
  sql: SELECT * FROM data

output:
  type: tdtp
  tdtp:
    destination: "pipelines/out/dept_{{dept}}_{{date_from}}_{{date_to}}.tdtp.xml"
    compress: true
    compress_level: 3
```

> The Cyrillic identifiers above are deliberate: they are the real column names
> in the system this example came from, and renaming them would leave an example
> that does not run. Non-ASCII identifiers work wherever the underlying database
> supports them.

```bash
./tdtpcli.exe --pipeline pipelines/dept_staff_hiredate.yaml \
  @dept=97-256 @date_from=2025-01-01 @date_to=2025-12-31
```

Output on start:

```
Pipeline: dept-staff-hiredate
   Department 97-256 roster, 2025-01-01 - 2025-12-31
   Version: 1.0
   Mode: 🔒 SAFE (READ-ONLY: SELECT/WITH only)
   Variables: @date_from=2025-01-01, @date_to=2025-12-31, @dept=97-256
   Sources: 1
   ...
```

The result lands in `pipelines/out/dept_97-256_2025-01-01_2025-12-31.tdtp.xml`.

---

## Scenario 1: two TDTP sources, joined

**Goal:** join two TDTP files (employees and departments), compute salary
statistics per department, write a new TDTP file.

**Inputs:**
- `employees.tdtp.xml` — 10 records (employee_id, full_name, department_id, salary, …)
- `departments.tdtp.xml` — 4 records (department_id, department_name, …)

**`pipeline-basic.yaml`:**

```yaml
name: "employee-dept-report"
version: "1.0"
description: "Salary report by department"

sources:
  - name: employees
    type: tdtp
    dsn: "examples/encryption-test/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "examples/encryption-test/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_salary_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id)    AS headcount,
      ROUND(AVG(e.salary), 2) AS avg_salary,
      SUM(e.salary)           AS total_salary,
      MIN(e.salary)           AS min_salary,
      MAX(e.salary)           AS max_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name
    ORDER BY total_salary DESC

output:
  type: tdtp
  tdtp:
    destination: "out/dept_salary_report.xml"

error_handling:
  on_source_error: "fail"
```

**Run:**
```bash
mkdir -p out
./tdtpcli --pipeline pipeline-basic.yaml
```

**Result** (`out/dept_salary_report.xml`):
```xml
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>dept_salary_report</TableName>
    ...
  </Header>
  <Schema>
    <Field name="department_name" type="TEXT"></Field>
    <Field name="headcount"       type="INTEGER"></Field>
    <Field name="avg_salary"      type="REAL"></Field>
    <Field name="total_salary"    type="REAL"></Field>
    <Field name="min_salary"      type="REAL"></Field>
    <Field name="max_salary"      type="REAL"></Field>
  </Schema>
  <Data>
    <R>Engineering|5|98000.00|490000|70000|120000</R>
    <R>Product|2|101000.00|202000|92000|110000</R>
    <R>Finance|1|88000.00|88000|88000|88000</R>
    <R>Human Resources|1|75000.00|75000|75000|75000</R>
  </Data>
</DataPacket>
```

---

## Scenario 2: PostgreSQL to TDTP

**Goal:** export orders from PostgreSQL, keep the active ones, write a TDTP file.

**`pipeline-pg-orders.yaml`:**

```yaml
name: "active-orders-export"
version: "1.0"

sources:
  - name: orders
    type: postgres
    dsn: "postgres://user:password@localhost:5432/shop_db?sslmode=disable"
    query: |
      SELECT
        order_id,
        customer_id,
        order_date,
        total_amount,
        status
      FROM orders
      WHERE status IN ('pending', 'processing')
        AND order_date >= NOW() - INTERVAL '30 days'
      ORDER BY order_date DESC

  - name: customers
    type: postgres
    dsn: "postgres://user:password@localhost:5432/shop_db?sslmode=disable"
    query: |
      SELECT customer_id, name, email, city
      FROM customers
      WHERE active = true

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "active_orders_with_customers"
  sql: |
    SELECT
      o.order_id,
      o.order_date,
      o.total_amount,
      o.status,
      c.name     AS customer_name,
      c.email    AS customer_email,
      c.city
    FROM orders o
    JOIN customers c ON o.customer_id = c.customer_id
    ORDER BY o.total_amount DESC
    LIMIT 500

output:
  type: tdtp
  tdtp:
    destination: "out/active_orders.xml"
    compression: true     # zstd, for large files

performance:
  parallel_sources: true  # load both tables at once
  timeout: 120
```

**Run:**
```bash
./tdtpcli --pipeline pipeline-pg-orders.yaml
```

**Sources from different database engines** need nothing special: give each one
its own `type` and `dsn`. The workspace joins them regardless of where they came
from.

---

## Scenario 3: encrypted output through xZMercury

**Goal:** export confidential data (salaries), encrypt it with AES-256-GCM
through xZMercury, write `.tdtp.enc`.

### Step 1: start the xZMercury mock

```bash
go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret
# or with Docker:
# docker run -p 3000:3000 -e MERCURY_SERVER_SECRET=dev-secret xzmercury-mock
```

### Step 2: set the shared secret

```bash
export MERCURY_SERVER_SECRET=dev-secret
```

### Step 3: write `pipeline-enc.yaml`

```yaml
name: "employee-dept-report-encrypted"
version: "1.0"
description: "Salary report — AES-256-GCM through xZMercury"

sources:
  - name: employees
    type: tdtp
    dsn: "examples/encryption-test/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "examples/encryption-test/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_salary_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id) AS headcount,
      SUM(e.salary)        AS total_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name

output:
  type: tdtp
  tdtp:
    destination: "out/dept_salary_report.tdtp.enc"
    encryption: true          # AES-256-GCM through xZMercury

security:
  mercury_url: "http://localhost:3000"
  key_ttl_seconds: 86400
  mercury_timeout_ms: 5000

result_log:
  type: redis
  address: "127.0.0.1:6379"
  name: "EMP_DEPT_RPT_V001"
  ttl: 3600
```

### Step 4: run it

```bash
./tdtpcli --pipeline pipeline-enc.yaml
```

**Output:**
```
Pipeline: employee-dept-report-encrypted
   Version: 1.0
   Mode: SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 2
   Workspace: sqlite (:memory:)
   Output: tdtp [ENC: xZMercury]

Starting ETL pipeline execution...

ETL Pipeline completed successfully!
   Duration: 0.54s
   Sources loaded: 2
   Rows loaded: 14
   Rows exported: 4
   Package UUID: 550e8400-e29b-41d4-a716-446655440000
```

### Alternative: `--enc-dev`, without xZMercury

Development builds accept `--enc-dev`, which generates the key locally:

```bash
./tdtpcli --pipeline pipeline-enc.yaml --enc-dev
```

Useful for development and CI with no xZMercury deployed. The flag does not
exist in a production build (`-tags production`).

### `--enc`: turning encryption on from the CLI

Encryption can be enabled without editing the YAML — in CI/CD, for instance:

```bash
./tdtpcli --pipeline pipeline-basic.yaml --enc
# equivalent to output.tdtp.encryption: true
# still requires the security.mercury_url section in the YAML
```

---

## Scenario 4: Redis orchestration

**Goal:** several pipelines run in parallel and an orchestrator follows their
status through Redis.

**`pipeline-with-resultlog.yaml`:**

```yaml
name: "daily-summary-v2"
version: "2.1"

sources:
  - name: sales
    type: postgres
    dsn: "postgres://user:pass@db:5432/warehouse"
    query: "SELECT * FROM daily_sales WHERE sale_date = CURRENT_DATE"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  sql: |
    SELECT
      region,
      SUM(amount) AS total_sales,
      COUNT(*)    AS orders_count
    FROM sales
    GROUP BY region

output:
  type: tdtp
  tdtp:
    destination: "out/daily_summary.xml"

result_log:
  type: redis
  address: "redis:6379"
  name: "DAILY_SUMMARY_V2"   # Redis key
  password: "redispass"
  db: 0
  ttl: 7200                  # 2 hours
```

**Run:**
```bash
./tdtpcli --pipeline pipeline-with-resultlog.yaml
```

**What lands in Redis when it finishes:**

```json
{
  "pipeline":      "daily-summary-v2",
  "status":        "success",
  "started_at":    "2026-02-26T10:00:00Z",
  "finished_at":   "2026-02-26T10:00:05Z",
  "duration":      "5.12s",
  "sources_loaded": 1,
  "rows_loaded":   1500,
  "rows_exported": 8,
  "package_uuid":  ""
}
```

**Statuses:**
- `success` — finished with no errors
- `failed` — finished with an error
- `completed_with_errors` — finished, but xZMercury was unreachable and an error packet was written

**Reading the status from Python:**

```python
import redis, json

r = redis.Redis(host='redis', port=6379, password='redispass')
result = json.loads(r.get('pipeline:DAILY_SUMMARY_V2'))
print(result['status'])        # "success"
print(result['package_uuid'])  # UUID for decryption, when encryption: true
```

**If Redis is down**, the pipeline logs a warning and exits 0. A missing result
log is not treated as a failure.

---

## Scenario 5: graceful degradation

**Goal:** understand what happens when xZMercury is unreachable.

**Condition:** `encryption: true`, xZMercury down.

**Behaviour:**

1. Data is loaded and the SQL transformation runs — all normal
2. `POST /api/keys/bind` gets connection refused, or times out
3. Unencrypted data is **not written**
4. An `error` packet goes to the destination (`Type=error`, table `tdtp_errors`)
5. The result log records `completed_with_errors` with a `package_uuid`
6. The pipeline exits **0**

**Output:**
```
Pipeline: employee-dept-report-encrypted
   ...
Starting ETL pipeline execution...

WARNING: Encryption degraded: bind key: MERCURY_UNAVAILABLE: connect: connection refused
   Error packet written to output. Pipeline completed with errors (exit 0).
```

**What the destination contains** (`out/dept_salary_report.tdtp.enc`):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>error</Type>
    <TableName>tdtp_errors</TableName>
    <MessageID>ERR-2026-a1b2c3d4-P1</MessageID>
    ...
  </Header>
  <Schema>
    <Field name="package_uuid"  type="TEXT" length="36" key="true"></Field>
    <Field name="pipeline"      type="TEXT" length="255"></Field>
    <Field name="error_code"    type="TEXT" length="64"></Field>
    <Field name="error_message" type="TEXT" length="1000"></Field>
    <Field name="created_at"    type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>550e8400-...|employee-dept-report-encrypted|MERCURY_UNAVAILABLE|connect: connection refused|2026-02-26T10:00:00Z</R>
  </Data>
</DataPacket>
```

A **downstream consumer** receives an ordinary TDTP packet, and can:
- insert the row into a `tdtp_errors` table
- tell the orchestrator a retry is needed
- carry on — an `error` packet has the same Schema-plus-Data structure as any other

**Error codes:**

| Code | Situation |
|------|-----------|
| `MERCURY_UNAVAILABLE` | connection refused, timeout |
| `MERCURY_ERROR` | HTTP 5xx from xZMercury |
| `HMAC_VERIFICATION_FAILED` | signature mismatch — wrong `MERCURY_SERVER_SECRET` |
| `KEY_BIND_REJECTED` | HTTP 403 (not permitted) or 429 (rate limited) |

---

## Pipeline CLI flags

```
--pipeline <file>     Path to the YAML configuration
--unsafe              Allow any SQL (requires admin; use sudo)
--enc                 Override: set output.tdtp.encryption=true (v1.5 section-level)
--enc13               Override: same, but the legacy v1.3 whole-blob format
--enc-dev             Dev mode: local key (non-production builds only)
--mask <fields>       Mask sensitive fields, comma-separated (email,phone,card)
--validate <file>     Validate fields against a YAML rules file
--normalize <file>    Normalize fields against a YAML rules file
```

**Precedence:**
- `--enc` and `--enc-dev` **override** `output.tdtp.encryption` in the YAML
- `encryption: true` in the YAML behaves exactly like `--enc`
- `--enc` without `security.mercury_url` in the YAML is a configuration error

**Production build:**
```bash
# Excludes --enc-dev, DevClient, and every other dev-only path
go build -tags production -o tdtpcli ./cmd/tdtpcli/
```

---

## Exit codes

| Code | Situation |
|------|-----------|
| 0 | Pipeline succeeded |
| 0 | `completed_with_errors` — Mercury unreachable, error packet written |
| 1 | Configuration, SQL validation, source or export failure |
| 1 | Unsafe mode without admin rights |
