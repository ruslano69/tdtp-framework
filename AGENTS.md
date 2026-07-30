# AGENTS.md — tdtpcli for agent-driven data work

## The short version

`tdtpcli` is the only tool you need for data work. One binary is both the eyes
(exploration) and the hands (transformation). **It physically cannot damage your
data** — every operation is read-only by default.

---

## An agent's workflow

```
1. --list              → what is in the database?
2. --inspect <file>    → what is its structure?   (understand — fields, types, UUID)
3. --test    <file>    → is the file intact?      (integrity — checksum, row count)
4. --export --limit 5  → what does the data look like?
5. --pipeline etl.yaml → transform it
6. --diff a.xml b.xml  → what changed?
```

> **`--inspect` versus `--test`:**
> - `--inspect` is about **understanding**: what this file is, which fields, how many rows, whether it is compressed.
> - `--test` is about **integrity**: the data is undamaged, the checksum is fine, a multi-part set is complete.
> - Neither needs a database connection. Both work with `s3://`.

---

## Commands

### Exploring (read-only, safe)

```bash
# What is in the database
tdtpcli --list
tdtpcli --list=order*          # glob filter
tdtpcli --list=%log%           # SQL-style filter

# Views (U* = updatable, R* = read-only)
tdtpcli --list-views
# U* orders_view    → can be imported into
# R* orders_summary → export only

# A table's structure (types, keys, subtypes)
tdtpcli --inspect orders
tdtpcli --inspect orders.tdtp.xml   # or a TDTP file

# Look at the data
tdtpcli --export orders --limit 10                          # first 10
tdtpcli --export orders --limit -1                          # last 1 (tail mode)
tdtpcli --export orders --limit -10                         # last 10
tdtpcli --export orders --order-by "id ASC" --limit -1      # last by id
tdtpcli --export orders --offset 100 --limit 50             # paging

# Filtering
tdtpcli --export orders --where 'status = active'
tdtpcli --export orders --where 'amount > 1000' --limit 5

# Only the columns you need
tdtpcli --export orders --fields id,status,total_amount

# Include read-only fields (timestamp, computed, identity)
tdtpcli --export orders --readonly-fields

# Mask PII before it leaves
tdtpcli --export customers --mask email,phone --output safe.tdtp.xml

# Compression, for large tables
tdtpcli --export logs --compress --output logs.tdtp.xml          # zstd level 3
tdtpcli --export logs --compress --compress-level 19 --output logs.tdtp.xml  # archival

# Write to a file
tdtpcli --export orders --limit 100 --output sample.tdtp.xml
```

### Creating a config from scratch

```bash
tdtpcli --create-config-pg     > pg.yaml
tdtpcli --create-config-sqlite > sqlite.yaml
tdtpcli --create-config-mysql  > mysql.yaml
tdtpcli --create-config-mssql  > mssql.yaml
```

### `--to-csv` — conversion without a database, plus filtering

`--to-csv` needs no database config: it works on any TDTP file, compressed ones
and v1.4 ones included. Every TDTQL filter is applied **in memory** after
decompression.

```bash
# Plain conversion
tdtpcli --to-csv users.tdtp.xml

# Separator, encoding and a BOM, for Excel or 1C
tdtpcli --to-csv report.tdtp.xml --delimiter ';' --bom --output report.csv
tdtpcli --to-csv report.tdtp.xml -d '\t' --cp 1251 --bom    # Windows-1251 for older systems

# Only the columns you need — no need to rebuild the table
tdtpcli --to-csv users.tdtp.xml --fields 'id,email,balance'
tdtpcli --to-csv staff.tdtp.xml --fields '[Last Name],[First Name],[Birth Date]'

# Row filters — like --export --where, but from a file
tdtpcli --to-csv orders.tdtp.xml --where 'total > 1000'
tdtpcli --to-csv users.tdtp.xml --where 'status = active' --where 'balance > 0'

# Sorting and limits
tdtpcli --to-csv orders.tdtp.xml --order-by 'total DESC' --limit 10
tdtpcli --to-csv events.tdtp.xml --order-by 'created_at ASC' --limit -100  # last 100

# Paging
tdtpcli --to-csv big_table.tdtp.xml --limit 100 --offset 500

# Everything at once: projection, filter, sort, CSV settings
tdtpcli --to-csv orders.tdtp.xml \
  --fields 'id,customer_id,total,status' \
  --where 'status = completed' \
  --where 'total >= 1000' \
  --order-by 'total DESC' \
  --limit 50 \
  --delimiter ';' --bom \
  --output top_orders.csv
```

> Compressed files (zstd, kanzi), compact v1.3.1 and v1.4 integrity packets are
> all handled automatically. No database needed.

### Comparing and merging

```bash
tdtpcli --diff before.tdtp.xml after.tdtp.xml
tdtpcli --diff a.xml b.xml --key-fields order_id --ignore-fields updated_at

# Merge strategies: union (default) | intersection | left | right | append
tdtpcli --merge file1.xml,file2.xml,file3.xml --output merged.xml
tdtpcli --merge old.xml,new.xml --merge-strategy right --show-conflicts
```

### ETL pipeline (transformation)

```bash
tdtpcli --pipeline etl.yaml          # safe mode: SELECT only
tdtpcli --pipeline etl.yaml --unsafe # full SQL — only when you actually need it
```

### Checking a file before importing it

```bash
# STEP 1: understand the structure (fields, types, table name)
tdtpcli --inspect delivery.tdtp.xml

# STEP 2: check integrity (decompression, checksum, row count)
tdtpcli --test delivery.tdtp.xml
# ✓ algo=zstd, 5000 rows, decompressed 23ms, checksum OK

# STEP 3: only now, import
tdtpcli --import delivery.tdtp.xml --config pg.yaml
```

### Importing the result

```bash
# Strategies: replace | ignore | fail | copy
tdtpcli --import result.tdtp.xml --strategy replace
tdtpcli --import result.tdtp.xml --strategy ignore   # leave existing rows alone
tdtpcli --import result.tdtp.xml --table new_table_name

# Import only the columns you want (a whitelist)
tdtpcli --import wide.tdtp.xml --fields id,email,status --table slim_table

# Import with field-name sanitising, for exotic names out of MSSQL, Access or an ERP
tdtpcli --import access_export.tdtp.xml --clear --strategy replace
# "Order ID" → Order_ID  |  "Total Cost $" → Total_Cost_usd_  |  "Discount %" → Discount_pct_

tdtpcli --import erp_1c.tdtp.xml --translit --clear --strategy replace
# "Имя пользователя" → Imia_polzovatelia  |  "Дата рождения" → Data_rozhdeniia

# Transliteration only — no special characters, just Cyrillic or diacritics
tdtpcli --import eu_staff.tdtp.xml --translit --strategy replace
# "Österreich" → Osterreich

# --clear and --translit do NOT apply to --export: export is the source of truth
```

### Quoting tables and fields — the enterprise case

Real ERP, 1C, NAV and Access databases contain names like `ZTR$Employee`,
`Last Name`, `Дата рождения`, `Total Cost $`. Here is the whole cheat sheet.

#### Quoting rules

| Situation | Syntax | Example |
|---|---|---|
| A table name with a special character (`$`, a space) | `[TableName]` | `[ZTR$Employee]` |
| A field with a space in `--where` | `[Field Name]` or `"Field Name"` | `[Last Name]` |
| A field with a space in `--fields` | `[Field Name]` | `[Last Name],[Birth Date]` |
| A string value in `--where` | single quotes `'...'` | `'Иванов'` |
| A string value containing `%` (LIKE) | `"%pattern%"` | `"%ЧЕРКАС%"` |

> Double quotes `"..."` in `--where` mean an **identifier** (ANSI SQL).
> Single quotes `'...'` in `--where` mean a **string literal**.

#### A full enterprise query

```bash
# bash / zsh / Linux / macOS
tdtpcli --config mssql.yaml \
  --export '[ZTR$Employee]' \
  --where '[Last Name] LIKE "%ЧЕРКАСОВ%" AND [Termination Date] = "1753-01-01"' \
  --fields 'No_,FullName,[Last Name],[Birth Date],[Termination Date]' \
  --compress --compress-algo kanzi --compress-level 6 --hash \
  --output exports/cherkasov_active.tdtp.xml
```

```powershell
# PowerShell — table in single quotes (to stop $ expanding), the LIKE value in single quotes inside double
.\tdtpcli.exe --config mssql.yaml `
  --export '[ZTR$Employee]' `
  --where "[Last Name] LIKE '%ЧЕРКАСОВ%' AND [Termination Date] = '1753-01-01'" `
  --fields 'No_,FullName,[Last Name],[Birth Date],[Termination Date]' `
  --compress --compress-algo kanzi --compress-level 6 --hash `
  --output exports\cherkasov_active.tdtp.xml
```

> **The PowerShell 5.x double-quote bug:** when arguments are passed to a native
> `.exe`, Windows parses the command line and `"[Last Name] LIKE "%С%""` gets cut
> into three pieces — the inner `"` characters are lost. Hence the rule:
> - `--export` and `--fields` take **single quotes** (which stops `$` expanding)
> - values in `--where` take **single quotes inside double**: `"[Field] LIKE '%pattern%'"`
> - a `--where` with no `$` and no `%` in the value works either way

#### PowerShell — the quoting rule for native .exe

```powershell
# WRONG — double quotes outside: $Employee expands, the table becomes "[ZTR]"
.\tdtpcli.exe --export "[ZTR$Employee]"

# WRONG — single quotes outside with double quotes inside for the LIKE value:
#         Windows CommandLineToArgvW cuts the string apart and the quotes are lost
.\tdtpcli.exe --where '[Last Name] LIKE "%ЧЕРКАСОВ%"'
# the program receives: [Last Name] LIKE %ЧЕРКАСОВ%  → parse error

# RIGHT — the table and --fields in single quotes ($ does not expand)
.\tdtpcli.exe --export '[ZTR$Employee]'
.\tdtpcli.exe --fields 'No_,[Last Name],FullName'

# RIGHT — a --where with a string value: double quotes outside, the value in single
.\tdtpcli.exe --where "[Last Name] LIKE '%ЧЕРКАСОВ%'"
.\tdtpcli.exe --where "[Termination Date] = '1753-01-01'"
.\tdtpcli.exe --where "[Age] > 30 AND [Last Name] LIKE '%ов%'"
```

#### Quick examples

```bash
# A field with a dollar sign in its name
tdtpcli --export orders --where '[Total Cost $] > 100'

# LIKE against a Cyrillic field (bash/zsh: double quotes inside single is fine)
tdtpcli --export '[ZTR$Employee]' --where '[Last Name] LIKE "%Черкас%"'
# LIKE against a Cyrillic field (PowerShell: single inside double)
.\tdtpcli.exe --export '[ZTR$Employee]' --where "[Last Name] LIKE '%Черкас%'"

# Several compound field names in a projection
tdtpcli --export employees --fields '[Last Name],[First Name],[Birth Date],id'

# inspect-table with a compound name
tdtpcli --inspect-table '[ZTR$Employee]' --config mssql.yaml

# 1753-01-01 is the "zero" date in MSSQL (Dynamics NAV/BC)
# bash/zsh: single quotes outside, single quotes for the string value inside (escaped)
tdtpcli --export '[ZTR$Employee]' --where '[Termination Date] = '"'"'1753-01-01'"'"'
# PowerShell: --export in single quotes (to stop $ expanding), --where in double, value in single
.\tdtpcli.exe --export '[ZTR$Employee]' --where "[Termination Date] = '1753-01-01'"
```

### Incremental synchronisation

```bash
# Sends only the rows that are new or changed, per the tracking field
tdtpcli --sync-incremental orders --tracking-field updated_at
tdtpcli --sync-incremental orders --tracking-field updated_at --checkpoint-file orders.checkpoint.yaml
```

---

## TDTP XML — what is inside

Every file carries everything you need:

```xml
<QueryContext>
  <OriginalQuery>          → what was asked for (fields, ORDER BY, Limit)
  <ExecutionResults>
    <TotalRecordsInTable>  → how many rows the table holds
    <RecordsReturned>      → how many came back
    <MoreDataAvailable>    → whether there is more
</QueryContext>
<Schema>                   → types, keys, subtypes (uuid, jsonb, …)
<Data>
  <R>val1|val2|val3</R>   → rows, separated by a pipe
```

An agent needs no separate `DESCRIBE table` — the schema is already in the file.

---

## Negative `--limit` (tail mode)

```bash
--limit  5   # first 5 rows
--limit -5   # last 5 rows (like tail -n 5)
--limit -1   # the last row
```

Without `--order-by` the order is undefined. For a reliable "last", always give
`--order-by`.

---

## Pipeline template

```yaml
name: "pipeline-name"
sources:
  - name: src
    type: postgres          # postgres | sqlite | mysql | mssql
    dsn: "postgres://user:pass@host:5432/db?sslmode=disable"
    query: "SELECT ..."     # or table: orders

workspace:
  type: sqlite
  mode: ":memory:"          # transformed in memory; the source is never touched

transform:
  result_table: "result"
  sql: |
    SELECT ... FROM src WHERE ...

output:
  type: tdtp
  tdtp:
    format: xml
    destination: "/tmp/result.tdtp.xml"
    # or S3:
    # destination: "s3://bucket/path/result.tdtp.xml"
```

---

## Database config

```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: tdtp_dev_pass_2025
  database: tdtp_test
  sslmode: disable
```

```bash
tdtpcli --export orders --config config.yaml
```

---

## Safety

| Mode | SQL | Risk |
|------|-----|------|
| `--export`, `--list`, `--inspect`, `--test` | none | zero |
| `--pipeline` (default) | SELECT/WITH only | zero |
| `--pipeline --unsafe` | any SQL | only when asked for explicitly |
| `--import` | INSERT/UPDATE | only on an explicit import |

An agent **cannot accidentally** run an UPDATE, DELETE or DROP.

## Skills worth having

### `--test` — packet integrity

```bash
# Always run this before importing a compressed or externally supplied file
tdtpcli --test <file>

# Works with S3
tdtpcli --test s3://bucket/key.tdtp.xml --config cfg.yaml

# In a script: import only if --test passed
tdtpcli --test delivery.tdtp.xml && tdtpcli --import delivery.tdtp.xml --config pg.yaml
```

### `--import` with field sanitising (`--translit`, `--clear`)

For files out of MSSQL, Access, 1C or an ERP where field names contain Cyrillic,
spaces, `%` or `$`:

```bash
# Access or legacy MSSQL: spaces and special characters
tdtpcli --import legacy.tdtp.xml --clear

# 1C or a Russian ERP: Cyrillic plus special characters
tdtpcli --import 1c_export.tdtp.xml --translit --clear

# European data: diacritics only (ö, ñ, ü, …)
tdtpcli --import eu_data.tdtp.xml --translit
```

The original names are preserved as column comments (PostgreSQL and MySQL).
**`--export` never sanitises** — it is the source of truth.
