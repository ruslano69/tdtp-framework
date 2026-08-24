# Claude Code Notes — tdtp-framework

## Keeping these notes honest (IMPORTANT)

**Before starting work, check `TODO_NEXT.md` and the notes in this file for
staleness.**

If a task a note describes is **already implemented**, delete the note or
replace it with a current one — don't wait to be told. The tell-tale sign of a
completed task: the code the note proposes writing already exists in the
repository.

How to check `TODO_NEXT.md`:
1. Read the heading and the plan.
2. Grep for the plan's key artefacts — packages, files, functions, dependencies.
3. If they are all there and it compiles, the plan is done → delete or replace it.

Example: the S3 plan (`pkg/storage`, AWS SDK) shipped in full, and `TODO_NEXT.md`
went on pointing at it as the "next" task for another month. Don't repeat that —
clean up on time.

## Go module downloads (IMPORTANT)

If `go build` cannot fetch dependencies (blocked proxy, missing zip, and so on),
use this **immediately**:

```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build ...
```

Or set it for the session:
```bash
export GOPROXY=https://goproxy.io
export GONOSUMDB='*'
```

`proxy.golang.org` redirects to `storage.googleapis.com`, which is blocked
(`no_proxy=*.googleapis.com`). `goproxy.io` serves packages directly with no
redirect — it **always works**.

---

## Test databases

### The Python scripts already exist, in `/scripts/`

- `scripts/create_postgres_test_db.py` — PostgreSQL (users, orders, products, activity_logs; 100/200/50 rows)
- `scripts/create_test_db.py` — SQLite
- `scripts/generate_test_db.py` — SQLite benchmark
- `scripts/create_benchmark_db.py` — SQLite benchmark, large. `--out FILE`,
  `--rows N`, `--no-dates`. By default it now emits `BirthDate DATE`,
  `LastLoginAt DATETIME` (10% NULL) and `UpdatedAt TIMESTAMP` alongside the
  original `RegisteredAt TEXT`; `--no-dates` reproduces the old seven-column
  set. The date columns are the point: the SQLite adapter reads the type from
  the column declaration (`pkg/adapters/sqlite/types.go`), so a `TEXT` column
  never touches the date path, and the old set exercised none of it
- `tests/compact_v131/setup_db.py` — SQLite for the compact-format tests

**Do not write new ones — use these.**

The existing PostgreSQL credentials are `tdtp_user` / `tdtp_dev_pass_2025`, from
`create_postgres_test_db.py`.

Config for tests:
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

### Starting PostgreSQL
```bash
pg_ctlcluster 16 main start
pg_isready
```

### Connection strings for the integration tests

Every adapter's integration tests default to a local server and **skip** when
they cannot reach one, so a machine without databases still gets a green
`go test ./...`. On CI point them somewhere else with:

| Variable | Package | Default |
|---|---|---|
| `POSTGRES_TEST_DSN` | `pkg/adapters/postgres` | `postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test` |
| `MYSQL_TEST_DSN` | `pkg/adapters/mysql` | `tdtp_user:tdtp_dev_pass_2025@tcp(127.0.0.1:3306)/tdtp_test?parseTime=true` |
| `MSSQL_TEST_DSN_DEV` | `pkg/adapters/mssql` | `server=localhost,1433;...;database=DevDB` |
| `MSSQL_TEST_DSN_PROD` | `pkg/adapters/mssql` | `server=localhost,1434;...;database=ProdSimDB` |

SQLite needs nothing — its tests use `t.TempDir()`.

**Keep `parseTime=true` in the MySQL DSN.** Without it the driver hands back
`[]byte` instead of `time.Time` for DATE/DATETIME/TIMESTAMP, and the tests
would be exercising a code path the CLI never takes.

---

## Compression (zstd and kanzi)

Re-measured 2026-08-24 on tdtpcli 1.25.1 (Go 1.26.5), Intel Core i7-7700
@ 3.60 GHz (4C/8T), Windows 10 Pro, idle machine. Export of 100k rows to file,
wall time of the whole CLI process, best of three runs.

**Two data sets, and the difference between them matters.** `benchmark_100k.db`
has seven columns and no real date types; `benchmark_100k_dates.db` adds
`BirthDate DATE`, `LastLoginAt DATETIME` (10% NULL) and `UpdatedAt TIMESTAMP`.
Both come from `scripts/create_benchmark_db.py` (`--out`, `--rows`,
`--no-dates`).

| Mode | 7 cols | 10 cols, with dates | Size 7 / 10 | Ratio 7 / 10 |
|------|--------|---------------------|-------------|--------------|
| No compression | 531 ms | 708 ms | 9.84 / 15.40 MB | — |
| zstd level 3 | 575 ms | 817 ms | 3.03 / 5.79 MB | 3.2× / 2.7× |
| zstd level 19 | 712 ms | 1063 ms | 2.50 / 5.03 MB | 3.9× / 3.1× |
| kanzi level 6 | 674 ms | 1005 ms | 1.54 / 3.12 MB | 6.4× / 4.9× |
| kanzi level 7 | 761 ms | 1162 ms | 1.47 / 3.01 MB | 6.7× / 5.1× |
| zstd 3 + `--integrity` | 574 ms | 819 ms | 3.03 / 5.80 MB | 3.2× / 2.7× |

**What to pick:**
- `zstd level 3` — the default for real-time streams: nearly free, 3× saving
- `kanzi level 6` — the optimum for archives and backups: **twice as dense as zstd3**
- `kanzi level 7` — maximum density, +90 ms over level 6, only worth it on a slow link
- `--integrity` is free — it lands inside the measurement noise of `zstd 3`

**The old numbers said kanzi 6 wins on speed. That is no longer the argument.**
Against the previous table (673 / 751 / 2363 / 1279 / 1449 ms) everything got
faster, but not evenly: `zstd 19` fell 3.3× and `kanzi 6` only 1.9×, so the gap
between them shrank from 1.8× to about 5%. Compression stopped dominating the
export — pick `kanzi 6` for its density, not its speed.

**Dates cost bytes, not time.** With dates the export takes 29–55% longer, and
none of that is date conversion: the set is 56% larger (+56 B per row, exactly
the length of three ISO-8601 fields plus separators) and splits into 9 parts
instead of 6. Per byte the date path is slightly *faster* — 18.2 → 22.1 MB/s
uncompressed. What genuinely suffers is density: ISO-8601 stamps barely repeat,
so kanzi 6 drops from 6.4× to 4.9×. **When sizing an archive of date-heavy
data, the seven-column ratios overstate the win by about 1.3×.**

On real data with heterogeneous text (HR orders, narrative descriptions) kanzi
reaches 10–12× against the original — BWT gets to do its work properly. On short
synthetic strings it manages 6–7×, which is still **30–50% denser than zstd**.

`compress: true` and `compress_level: 3` are the defaults in the config template
(`CreateSampleConfig`). Leave them. For archival work:
`--compress-algo kanzi --compress-level 6 --hash`.

---

## Build tags

- `nokafka` — excludes kafka-go and its dependencies, for offline builds or builds without Kafka
- `nosqlite` — excludes modernc.org/sqlite, for builds without SQLite

A quick build without Kafka:
```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build -tags nokafka -o H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe ./cmd/tdtpcli/
```

---

## Dev branch

Feature branches: `claude/wonderful-fermi-4qXUT`

---

## Packet sizes: what is capped and what is not (IMPORTANT)

**Only the schema is capped.** The Data section has no limit at all — however
many rows fit the budget get written, and the rest goes into the next part.

| Value | What it governs | Where |
|---|---|---|
| `DefaultMaxMessageSize` = 3 800 000 | the budget for one part → **about 1.9 MB of real XML** | `generator.go` |
| `packetOverheadSize` = 5000 | the floor on the envelope reserve | `generator.go` |
| `MaxSchemaBytes` = 200 KB | Schema **on write** — refused above it | `generator.go` |
| `MaxSchemaBytesRead` = 1 MB | Schema **on read** — refused above it; between 200 KB and 1 MB it is read with a `WARNING` | `parser.go` |
| `maxBufferedParse` = 64 MB | the threshold that picks a parse path, **not** a size limit | `parser.go` |

**The budget is counted in units twice the size of a UTF-8 byte.**
`estimateRowSize` does `len(value) * 2`, and `len()` in Go counts bytes, not
characters. So the "budget → real XML = ÷2" relationship holds for any alphabet;
the comment about UTF-16 in the code is misleading. The 2× reserve is there for
the envelope, not for a re-encoding.

**The write and read thresholds differ deliberately.** The write limit is a rule
about the format and applies only to new packets. The same limit on read would
reject data that already exists: nothing written earlier was bounded at all. The
upper read threshold is protection against a pathological input, not a rule
about the format.

**The packet size is the user's to choose and has no upper bound:**
`--packet-size` on a broker export (`broker.go`, which multiplies by 2) and
`packet_kb` in a pipeline YAML (`etl/exporter.go`, which does **not**).

### `packet_kb` — do NOT "align" it with `--packet-size` (IMPORTANT)

It looks inconsistent: `--packet-size N` yields about N MB of real XML, while
`packet_kb K` yields only about K/2 KB. The urge is to "fix" it by adding the
same factor of 2.

**Do not.** Multiplying would double the packet size, and `packet_kb` is wanted
precisely where the size is hard-limited from outside — Windows exporting to
MSMQ. Doubling would make the broker refuse the packet, so the "fix" would break
the exchange. The current behaviour delivers half of what was asked, which is to
say it errs on the safe side, and that is the only side it can afford to err on
here.

Touch it only if a real refusal happens, and then work out that specific
broker's limit rather than aligning the two formulas with each other.

---

## The parser: a hybrid parse (IMPORTANT)

The writer took reflection off the hot path long ago (`xmlwriter.go`): Header and
Schema go through `xml.Marshal` (they are about 200 bytes), while the thousands
of `<R>` elements are written by hand. The parser now mirrors that
(`parser_fast.go`).

`tryFastParse` cuts out the Data body and assembles a "skeleton" — the same
packet with an empty Data section, about 500 bytes regardless of row count. The
skeleton goes to `xml.Unmarshal`, which parses the root attributes, Header,
Query, QueryContext, Schema, the Data attributes and AlarmDetails. Only the rows
are scanned by hand.

**The fallback is mandatory and not decorative.** Any unexpected shape →
`ok=false` → ordinary `xml.Unmarshal`, including raising an error on malformed
XML. The fallback triggers on: CDATA, comments, a raw `<` inside a row, `<R/>`,
`<Data/>`, an unrecognised entity, a second root `<Data>`, or a skeleton that
would not assemble.

`ParseBytes`: 34.5 ms → 3.57 ms on 10k×10 (**9.7×**), 686 MB/s. `Parse` and
`ParseFile` read the input whole up to `maxBufferedParse` and take the same path;
above the threshold, the previous streaming `xml.Decoder` is used (there is no
need to rewind the reader — what was already read is spliced back in through
`io.MultiReader`).

**Do not measure the schema's size with `xml.Marshal` inside the parser.**
Marshalling a ten-field schema costs about 13.7 µs against about 51 µs to parse a
small packet end to end — the check would have eaten a quarter of the parse.
`schemaSpanSize` is used instead: the `<Schema>...</Schema>` span is located with
two `bytes.Index` calls straight in the source bytes, and yields the same number
the writer measures (a test pins this).

**An asymmetry, fixed by a test:** `Parse` and `ParseFile` expand the compact
format; `ParseBytes` deliberately does not.

---

## `GetRows` — the shared node of both paths, and its parse is parallel (IMPORTANT)

**A compressed and an uncompressed packet converge at one point.**
`DecompressData` brings `Data.Rows` to the same form (pipe-joined strings) as an
ordinary parse, so from there both go through `GetRows` and cost the same there.
That was confirmed by direct measurement rather than reasoning: 2.6–2.8 ms on
10k×10 either way, with the same 10001 allocations.

**Do not decompose the cost by subtracting benchmarks from one another.** That is
how the wrong picture of "split costs 3.7 ms compressed against 2.2 uncompressed"
was produced — there is no difference, it was noise. Measure the stage you care
about with its own benchmark.

Which is also why this is the place to parallelise: one change fixes both paths.

```
GetRows: 2.7 ms → 1.5 ms (1.8×), about 720 MB/s on 10k×10
```

- the `parallelGetRowsThreshold` is 512 rows; below it the work runs sequentially, because goroutine overhead costs more than the work (10 rows: about 2 µs, 11 allocations)
- results are written **by index** into a pre-allocated slice, so the ordering is structural and does not depend on the scheduler
- `Parser` holds no state (`struct{}`) — one instance serves every goroutine
- the `rawRows` branch (the fast path after `GenerateReference`) is untouched

Tests: agreement with the sequential reference on both sides of the threshold,
order stability across repeats, the escaped slow path `GetRowValues`, and rows of
uneven length. Run them with `-race`.

### Where the time goes on the compressed path (10k×10, 1.09 MB payload)

| Stage | Time | Note |
|---|---|---|
| XML parse | 52 µs | the whole Data is one `<R>` holding a blob — nothing to parse |
| zstd decompression | about 2.8 ms | already parallel (`WithDecoderConcurrency(4)`) |
| `GetRows` | 1.5 ms | was 2.7 before the parallelisation |

**The compressed path is slower end to end than the uncompressed one** (about
6.5 ms against about 3.7 before the change): you pay for compression with
decompression on every read. `compress: true` is justified by storage and link
capacity, not by read speed.

---

## Splitting into parts, and the dictionary (IMPORTANT)

`Schema` is copied into **every** part, so that a file export is
self-contained — and since v1.4 the Dictionary lives inside Schema. Two
non-obvious rules in `rowBudget` follow, both pinned by tests:

1. **The envelope reserve is capped at half the budget.** Subtracting a large
   envelope honestly makes things worse: every new part adds another copy of
   Schema, so splitting moves the parts further from the budget rather than
   closer. Measured: a 400-entry dictionary against a 200 KB budget went from
   1 part to **4000** — one row each, every one carrying 431 KB of dictionary.
2. **If Schema does not fit the budget, do not split at all.** The decision is
   made on `serializedSchemaSize` (without the floor), not on
   `measureEnvelopeSize` (with it): otherwise the conservative floor of 5000
   forbids splitting whenever the budget is small and the schema is tiny.

For a schema with no dictionary, `measureEnvelopeSize` hits that same floor of
5000, so part boundaries are **unchanged**
(`TestPartitionRows_UnchangedWithoutDictionary` reproduces the old formula and
requires them to match).

**The framework does not build dictionaries automatically.** There is only
`ValidateDictionary`, `ExpandDictionary` and `ContractDictionary`. In practice a
dictionary gets filled in exactly two places: one service entry, `@MRC`, holding
the Mercury address (`export.go`), and a static five-entry SVG dictionary. A
large dictionary can only arrive from an external producer through the library
API. If automatic construction is ever added, it must fit inside
`MaxSchemaBytes`.

---

## Reading someone else's packet: what the signature protects and what it does not (IMPORTANT)

**The Mercury check runs AFTER decompression, and could not run anywhere else.**
`import.go` says so outright: `// Security gate (after decompression)`. The
reason is what gets hashed: "Hash covers plain-text rows BEFORE compression"
(`help_full.txt`, `pipeline/produce.go`). To compare the hash you must first have
the decompressed rows.

From which follows the part that is easy to miss: **anything that breaks the
parse fires before the signature gets to say anything at all.**

| Threat | Does Mercury help? |
|---|---|
| Content substitution | yes — the hash will not match |
| Replay of an old packet | yes — registration is per UUID and part |
| A different packet than expected | yes |
| Decompression bomb | **no** — it detonates before the gate |
| Bloated Schema | **no** — parsed before the gate |
| Malformed XML | **no** — parsed before the gate |

On top of that, `--mercury-url` is optional, and `applyV14SecurityGate` is a
no-op for pre-v1.4 packets. In a configuration without Mercury this layer does
not exist at all.

**So: the read limits do not duplicate the signature, they close the window
before it.** Do not remove them "because packets are signed" — inside that
window there is no signature yet.

### The decompression bomb — the only measured, exploitable problem

The compression ratio has no upper bound, so a tiny packet can expand into
anything. Measured on **ordinary repetitive data**, not on a crafted input:

```
25 KB in the packet → 200 MB decompressed (8184×)
1 MB                → about 8 GB
```

Closed by `MaxDecompressedBytes = 256 MB` (`processors/compression.go`): zstd
through `WithDecoderMaxMemory`, kanzi through `io.LimitReader` plus a length
check.

**No other limit reaches this** — worth holding on to, because there are several
of them and it is easy to assume one already covers it: `maxBufferedParse` bounds
the input (which is tiny here), and `MaxSchemaBytesRead` bounds the Schema
section (and the bomb is in Data).

Rejection on the kanzi path is **not verified at full scale** — compressing
256 MB through kanzi takes tens of seconds. The test there only covers
round-trip integrity.

---

## SQLite dates: do not "skip parseTime" by scanning into a string (IMPORTANT)

`modernc.org/sqlite` decides whether to parse a cell into `time.Time` from the
column's **declared type** — `DATE`, `DATETIME`, `TIMESTAMP`. Those are exactly
the types worth special-casing, which is what makes the trap so easy to walk
into.

There used to be a fast path in `ScanSQLRows` that bound such columns to
`*string`, on the stated theory that it skipped `modernc.parseTime` and saved
about 450 ms per 100k rows.

**The cost it named is real — the remedy was not.** Measured below: the parse
runs to about 1.65 µs and 11 allocations per date cell, which on 100k rows with
three date columns is roughly 250 ms of a 350 ms read. But binding the scan to
`*string` does not avoid any of it. The driver decides whether to parse from the
column's *declared type*, not from what you scan into, so it parses anyway, and
`database/sql`'s `convertAssign` then formats the `time.Time` back into the
string with `RFC3339Nano`. The path bought an extra format and an allocation per
cell, and cost three things:

1. **The export died on the first NULL date** — `converting NULL to string is
   unsupported`. A whole table refused to export because one cell was empty.
2. `normalizeSQLiteDateTime` became dead code. It looked for the space
   separator in `"YYYY-MM-DD HH:MM:SS"`, and its input always arrived as
   RFC3339 already. Its unit test passed the whole time — it called the
   function directly and never checked that anything reached it.
3. The driver's own zone rode into a packet whose Schema declares UTC.

### Where the read time actually goes

`pkg/adapters/sqlite/driver_cost_bench_test.go`, 50k rows, six columns, three
of them dates (Xeon 2.80GHz):

| Query | Time | Allocations per row |
|---|---|---|
| `SELECT id` | 19 ms | 1 |
| `id, name, amount` | 50 ms | 5 |
| all six, dates via `CAST(... AS TEXT)` | 103 ms | 11 |
| all six, dates as the driver returns them | **351 ms** | **45** |

So the three date columns cost about 250 ms more as `time.Time` than as text —
about **1.65 µs and 11 allocations per date cell**. For scale, the formatter on
our side of the boundary costs 120 ns and one allocation. The driver's parse is
an order of magnitude past anything the conversion layer does.

**Going around `database/sql` does not help.** The same query driven through
`driver.Rows` directly measures the same 351 ms with the same 45 allocations —
`convertAssign` hits a plain assignment when the scan target is `*any`, and
`database/sql` adds nothing else worth naming per row. `driver.Rows` also does
not expose wire bytes: `go-mssqldb`'s `Rows.Next` copies out of an
already-decoded `[]interface{}`, and modernc does the same. Do not go looking
for raw TDS or SQLite bytes at that layer — they are gone before it.

**The lever that does work is the SQL, not the API**, and `ReadAllRows` now
pulls it. `selectExprForField` wraps every date column in
`CAST(col AS TEXT) AS col`, which changes the declared type the driver sees so
the parse never runs: 351 ms → 103 ms on the read.

End to end on the CLI, interleaved A/B over five pairs, 100k rows with three
date columns: **0.97 s → 0.83 s median, and every single pair faster** — unlike
the earlier scanner change, this one is outside the ±7% this VM carries. All
100 000 rows come out byte-identical; the only difference between the two
packets is the per-export `MessageID`.

Storage classes check out, and one of them improves: a TEXT-stored date comes
back byte-for-byte as stored, an INTEGER one as the same digits `strconv`
produced before, and a REAL one as its exact stored text (`2460909.11`) instead
of the old `2.46090911e+06`. `TestDateStorageClasses` pins all four cases.

`datetimeFormats` gained `"2006-01-02 15:04:05Z07:00"` for this: SQLite text
storage can carry its own offset, and that spelling previously failed every
layout and fell through to the packet raw. It parses now, and
`parseTimestamp` normalizes it to UTC exactly as the driver's value was.

**Only `ReadAllRows` does this** — it is the one place we build the SELECT
ourselves. `ReadRowsWithSQL` (TDTQL, `--where`, views) takes a caller-supplied
query whose projection cannot be rewritten safely, so it keeps the old path.

### And then the conversion side, which CAST made the new bottleneck

Raw SQLite text does not match the first layout in `datetimeFormats`, so
`ParseValue` burns three failed `time.Parse` calls — each allocating an error —
before the space-separated layout hits. Measured on the shapes SQLite actually
stores: **1630 ns and 14 allocations per cell.**

`fastSQLiteDateTime` (`scanner.go`) does it with one string splice: **111 ns,
1 allocation — 14.7×.** This is the job the old `normalizeSQLiteDateTime` was
written for, finally on a live code path, because CAST is what puts the space
separator back in front of it.

**It declines rather than guess.** `ok=false` sends the value down the ordinary
path, and that is the only reason the bytes still match. It declines on: an
explicit offset (needs converting to UTC, not keeping), trailing zeros in the
fraction (`RFC3339Nano` trims them), a fraction longer than nine digits,
anything `time.Parse` would reject — including a day past the length of its
month, checked with the leap-year rule — and, the easiest one to miss, **a
shape that does not match the declared type**: a bare date in a TIMESTAMP
column expands to midnight on the slow path, and a full timestamp in a DATE
column is not parsed at all. `TestFastSQLiteDateTime_AgreesWithSlowPath` and a
250k-case random test hold both halves together.

### What the two changes did together

100k rows, three date columns, interleaved A/B against a binary built from the
preceding commit:

| Step | Export | Против исходного |
|---|---|---|
| before both | 0.97 s | — |
| `CAST(... AS TEXT)` | 0.83 s | 1.17× |
| `+ fastSQLiteDateTime` | **0.46 s** | **2.1×** |

All 100 000 rows byte-identical at every step; the packets differ only in the
per-export `MessageID`.

Everything now scans into `any`. If the value comes back as a `time.Time`,
`DBValueToString` already produced the canonical string and the
`ParseValue → FormatValue` round trip in `ConvertValueToTDTP` is skipped.

**This is not a speedup — do not quote one.** An interleaved A/B of ten pairs
(old binary and new, alternating, both orders) on 100k rows with three date
columns puts both at ~0.97–0.98 s median, inside a 0.93–1.04 s run-to-run
spread. An earlier note here claimed 1.05 s → 0.93 s; that came from comparing
runs taken minutes apart rather than interleaved, and the "before" figure was
simply the first, cold-cache run. Skipping the round trip does save work, but
`formatTimeForField` adds a `NormalizeType` call per date cell and the two
cancel. **These changes are correctness fixes; treat the cost as unchanged.**

The measurement lesson generalizes: on this VM a single wall-clock run carries
±7%, so any A/B smaller than that has to be interleaved before it means
anything.

### The round trip, both directions

`DATE` is a date. `formatTimeForField` writes `2026-08-21`, not
`2026-08-21T00:00:00Z`, and `TypedValueToSQL` writes it back as `2026-08-21`
rather than `2026-08-21 00:00:00`. Before, export produced midnight and import
kept it, so a DATE column drifted into a timestamp on every hop.

`TypedValueToSQL` keeps sub-second precision **for SQLite only**, via the
`"2006-01-02 15:04:05.999999999"` layout — it trims trailing zeros and drops
the dot entirely when there is no fraction, so whole seconds are written with
the same bytes as before. Export was taught to carry `.fff` long ago; import
was still cutting it off.

**MySQL and MSSQL deliberately still get whole seconds.** MySQL `DATETIME`
without explicit precision is `DATETIME(0)` and **rounds** the fraction
(`14:38:11.527 → 14:38:12`) — it shifts the value instead of truncating it.
Changing that needs the column's actual precision checked against a live
database first.

Measured on a 100k-row round trip: zero semantic loss. The only textual
difference is `RFC3339Nano` trimming trailing zeros (`.110 → .11`), which is
`FormatTimestamp`'s documented behaviour and not new.

Regression tests: `pkg/adapters/sqlite/datetime_roundtrip_test.go` (needs the
live driver — none of this is visible to a unit test) and the
`TypedValueToSQL`/`formatTimeForField` cases in
`pkg/adapters/base/timestamp_precision_test.go`.

---

## PostgreSQL dates: what pgx actually hands over (IMPORTANT)

Scanned into `any` (which is what the adapters do), pgx v5 returns:

| column | Go type it comes back as |
|---|---|
| `date`, `timestamp`, `timestamptz` | `time.Time` |
| the same columns holding `infinity` | **`pgtype.InfinityModifier`** |
| `time` | `pgtype.Time` |
| NULL | `nil` |

So the `case pgtype.Date:` / `case pgtype.Timestamp:` / `case pgtype.Timestamptz:`
branches in `pgValueToString` **never fire** — the same shape of dead code the
SQLite date fast path had. They are harmless (the `time.Time` branch does the
right thing), but do not reason about behaviour from them.

Two real defects came out of that list:

### `time` columns broke the round trip outright

`pgtype.Time` was formatted with `"%02d:%02d:%02d"`, which threw away the
microseconds PostgreSQL stores. Worse, the field arrives as `TIMESTAMP` with
`Subtype: "time"`, and `FormatValue` ignored the subtype — so `parseTime`'s
zero-year `time.Time` was printed through `FormatTimestamp` as
`0000-01-01T14:38:11Z`. PostgreSQL will not accept that back into a `time`
column: `invalid input syntax for type time`. **A table with a TIME column
could not round-trip at all.**

`TypedValue` now carries `Subtype`, and `FormatValue`/`TypedValueToSQL` render
it through `schema.FormatTimeOfDay` — `15:04:05.999999999`, so whole seconds
still format to the same bytes as the old `%02d:%02d:%02d`.

### `infinity` never became a marker

Falling through to `fmt.Sprintf("%v", v)` produced **lowercase** `infinity`, and
`rawInfinityForms` only lists the capitalized spellings. `DetectAndApply`
therefore left `SpecialValues.Infinity` unset, `ConvertValueToTDTP` logged a
parse failure for every affected cell, and the packet carried a raw PostgreSQL
literal.

Postgres→Postgres appeared to work — `infinity` happens to be valid input on
the way back in. **Postgres→SQLite did not**: with no marker declared, the
importer tried to parse `infinity` as a date and failed. The marker path
(`INF` → `"infinity"` for pg, NULL elsewhere) exists precisely for this and was
simply never reached.

Also note `postgres.convertValue` is the adapter's **own** copy of the marker
decoding — it is not `base.ConvertRowToSQLValues` — and it handled only `Null`
and `NoDate`. Anything added to the base version has to be added there too.

### What round-trips cleanly now

Verified against a live PostgreSQL 16 on `DATE`, `TIMESTAMP`, `TIMESTAMPTZ` and
`TIME`: microseconds, NULL, `infinity`/`-infinity`, year 0001, pre-1900 dates,
`+03:00` offsets (normalized to UTC, as the schema declares) — **zero
differences**, and the second pass is a fixed point. A packet with `infinity`
imported into SQLite lands as NULL, which is the designed behaviour for a
database that cannot store it.

Regression tests: `pkg/adapters/postgres/datetime_roundtrip_test.go` (needs a
live database; skips without one, override the DSN with `POSTGRES_TEST_DSN`).
Start it with `pg_ctlcluster 16 main start`.

---

## MySQL dates: precision is not optional (IMPORTANT)

### Starting MySQL here

Docker Hub is blocked by the egress policy (`production.cloudfront.docker.com`
answers 403 to CONNECT), so `docker pull mysql` does not work. Install the
Ubuntu package instead — it is real MySQL 8.0, not MariaDB:

```bash
apt-get update -q && DEBIAN_FRONTEND=noninteractive apt-get install -y -q mysql-server
(mysqld_safe > /tmp/mysqld.log 2>&1 &) && sleep 12 && mysqladmin status
mysql -e "CREATE USER IF NOT EXISTS 'tdtp_user'@'%' IDENTIFIED BY 'tdtp_dev_pass_2025';
          CREATE DATABASE IF NOT EXISTS tdtp_test;
          GRANT ALL PRIVILEGES ON *.* TO 'tdtp_user'@'%' WITH GRANT OPTION;"
```

To exercise the zero-date path, drop `NO_ZERO_DATE`/`NO_ZERO_IN_DATE` from
`sql_mode` — the default in 8.x forbids `0000-00-00`.

### `DATETIME` without a precision ROUNDS (measured)

MySQL 8.0.46, `DATETIME` (i.e. `DATETIME(0)`):

```
'2026-08-21 14:38:11.527'  →  2026-08-21 14:38:12
'2026-08-21 23:59:59.999'  →  2026-08-22 00:00:00   ← another day
```

It rounds, it does not truncate. So the import **cannot** simply hand MySQL a
fractional value the way it does for SQLite: on a whole-second column that
shifts the data, sometimes across a date boundary.

The import therefore writes **exactly as many fractional digits as the column
declares**. `Precision` comes from `information_schema.columns.datetime_precision`
— note that `data_type` for `datetime(6)` is just `"datetime"`, with no
parameters, so `BuildFieldFromColumn` cannot see it from the type name alone.
Go's `Format` truncates the fraction, so nothing rounds on either side.
`Precision` 0 produces the same bytes as before this change.

`CreateTable` writes the precision back out (`DATETIME(6)`, `TIMESTAMP(6)`,
`TIME(6)`), otherwise a freshly created target table would silently be
`DATETIME(0)` and lose the microseconds on the very first import.

**MSSQL still gets whole seconds**, deliberately: `datetime` rounds to 1/300 s
while `datetime2` keeps 100 ns, and there was no live MSSQL here to measure it
on. Do the same exercise before changing that branch.

### MySQL `TIME` is a duration, not a time of day

Range `-838:59:59 .. 838:59:59`. It is not a clock reading, and neither
`time.Time` nor PostgreSQL `time` can hold the ends of that range.

It used to be rejected outright — `unsupported MySQL type: TIME` straight out of
`GetTableSchema`, so a table with a TIME column could not even be *described*,
let alone exported. It now maps to **`TEXT` with `Subtype: "time"`**: the value
travels verbatim, `Subtype` remembers what it was, and `MapTDTPTypeToMySQL`
turns it back into `TIME(n)`. `-12:30:15.250000` and `838:59:59` round-trip
exactly.

Do not "improve" this into `TIMESTAMP` + subtype `"time"` the way PostgreSQL
`time` is handled — that path goes through `parseTime`, which cannot represent
either a negative value or an hour past 23.

### The driver: `parseTime=true` matters, and TIME is exempt

The DSN the CLI builds (`cmd/tdtpcli/config.go`) carries `parseTime=true`, so
`DATE`/`DATETIME`/`TIMESTAMP` arrive as `time.Time`. **`TIME` does not** — it
arrives as `[]byte` regardless, which is why it lands in the `[]byte` branch of
`genericValueToString` and passes through as a plain string.

A MySQL zero date (`0000-00-00`) arrives as `time.Time{}`, which is
`0001-01-01`, so `v.IsZero()` catches it and it becomes the `NoDate` marker.

### What round-trips, and the one thing that does not

Verified on live MySQL 8.0.46 over `DATE`, `DATETIME`, `DATETIME(6)`,
`TIMESTAMP`, `TIMESTAMP(6)`, `TIME` and `TIME(6)`: microseconds, NULL,
pre-1900, leap day, negative and over-24h TIME — **zero differences**.

The exception is by design: `0000-00-00` comes back as **NULL**. The `NoDate`
marker maps to NULL for every database (`import_helper.go`), because most of
them cannot store a zero date. MySQL can, so the distinction between "no date"
and NULL is lost on the way back in. Changing that would mean a MySQL-specific
branch on the import side — worth doing only if someone actually depends on it.

Regression tests: `pkg/adapters/mysql/datetime_roundtrip_test.go` (needs a live
server; skips without one, override the DSN with `MYSQL_TEST_DSN`). One of them,
`TestMySQLRoundsSubSecond`, exists purely to keep the rounding rationale honest
— if a future server truncates instead, it fails and tells you to revisit.

---

## SeaweedFS S3 (local testing)

### The binary
```
/tmp/weed   (version 3.80, linux amd64)
```

`/tmp` in the container is cleared periodically. To restore it:

```bash
curl -sSL -o /tmp/weed.tar.gz \
  https://github.com/seaweedfs/seaweedfs/releases/download/3.80/linux_amd64_large_disk.tar.gz
tar xzf /tmp/weed.tar.gz -C /tmp/ && chmod +x /tmp/weed
mkdir -p /tmp/seaweedfs-data
```

**The GitHub API is blocked; direct release links are not.** `api.github.com`
returns 403 for other people's repositories ("access to this repository is not
enabled"), while `github.com/.../releases/download/...` downloads fine. So the
version has to be named explicitly — "latest" cannot be resolved through the API.

The `tdtp-test` bucket does not exist after a clean start. Create it:
```bash
curl -s -X PUT http://127.0.0.1:8333/tdtp-test
```

### IMPORTANT: `-ip` does not work in `weed server` — start the components separately

`weed server -ip=127.0.0.1` **ignores the flag** and uses 192.0.2.2 (the external
IP) anyway. The sandbox's Envoy proxy blocks gRPC between components over an
external IP. **The fix** is to start each component separately with an explicit
`-ip=127.0.0.1`:

```bash
# 1. Master (port 9333)
/tmp/weed master -ip=127.0.0.1 -defaultReplication=000 -volumeSizeLimitMB=100 -port=9333 &
sleep 18  # wait for the leader election (about 15s)

# 2. Volume server (port 8080)
/tmp/weed volume -ip=127.0.0.1 -dir=/tmp/seaweedfs-data -mserver=127.0.0.1:9333 -port=8080 &
sleep 2

# 3. Filer (port 8888) — creates filerldb2/ in the CWD; it is in .gitignore
/tmp/weed filer -ip=127.0.0.1 -master=127.0.0.1:9333 -port=8888 &
sleep 3

# 4. S3 gateway (port 8333) — does not support -ip, so use -ip.bind
/tmp/weed s3 -ip.bind=127.0.0.1 -filer=127.0.0.1:8888 -port=8333 &
sleep 2

# Check
curl -s http://127.0.0.1:9333/cluster/status   # {"IsLeader":true,...}
curl -s http://127.0.0.1:8333/                 # <ListAllMyBucketsResult>...
```

### The existing bucket
```
tdtp-test   — already has a volume, and is writable
```
`tdtp-new-bucket` was created but has no volume assigned, so writes fail with a
500. **Use `tdtp-test`** for every S3 test.

### Credentials
In dev mode weed accepts any keys:
```
access_key: any
secret_key: any
```

### IMPORTANT: `curl -u` does NOT test S3 authorisation

`curl -u "tdtp_access:tdtp_secret" http://127.0.0.1:8333/` always returns
`AccessDenied` — because curl sends **HTTP Basic Auth** and S3 requires **AWS
Signature V4**.

**Test access only through boto3 or tdtpcli:**
```python
import boto3, botocore.config
s3 = boto3.client('s3', endpoint_url='http://127.0.0.1:8333',
    aws_access_key_id='tdtp_access', aws_secret_access_key='tdtp_secret',
    config=botocore.config.Config(signature_version='s3v4'), region_name='us-east-1')
print([b['Name'] for b in s3.list_buckets()['Buckets']])
```

### S3 for travel-agency (`H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed`)

The binary, data and configs live in
`H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed\`:
```
weed.exe          — SeaweedFS 30GB 4.17 (Windows)
s3.json           — credentials: tdtp_access / tdtp_secret
data/             — volume data and filerldb2/
config.yaml       — tdtpcli config for MSSQL plus S3 (endpoint 8333, bucket tdtp-exports)
```

Starting it — components separately, from the `weed/` directory:
```powershell
cd H:\Ruslan\Code\Go\TDTP\tdtp-framework\weed

.\weed.exe master -ip=127.0.0.1 -defaultReplication=000 -volumeSizeLimitMB=30000 -port=9333
# wait about 18s for the leader election

.\weed.exe volume -ip=127.0.0.1 -dir=./data -mserver=127.0.0.1:9333 -port=8080
.\weed.exe filer  -ip=127.0.0.1 -master=127.0.0.1:9333 -port=8888
.\weed.exe s3     -ip.bind=127.0.0.1 -filer=127.0.0.1:8888 -port=8333 -config=./s3.json
```

**Buckets:** `travel-agency`, `tdtp-test`, `tdtp-exports`

### Config for tests
```yaml
storage:
  type: s3
  s3:
    endpoint: "http://127.0.0.1:8333"
    region: "us-east-1"
    bucket: "tdtp-test"
    access_key: "any"
    secret_key: "any"
    path_style: true
    disable_ssl: true
```

### Checking `--test` against S3
```bash
H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe --config /tmp/test_s3_cfg.yaml \
  --export users --output "s3://tdtp-test/ci/users.tdtp.xml" --compress --hash

H:\Ruslan\Code\Go\TDTP\tdtp-main-clean\tdtpcli.exe --config /tmp/test_s3_cfg.yaml \
  --test "s3://tdtp-test/ci/users.tdtp.xml"
# ✓ algo=zstd, 10 rows, decompressed 0s, checksum OK
```

### Log files
```
/tmp/seaweed.log        — the previous session (data from 17 March 2026)
/tmp/seaweedfs-data/    — volume data (8.dat, 8.idx, 8.vif)
filerldb2/              — the LevelDB filer (in .gitignore)
```
