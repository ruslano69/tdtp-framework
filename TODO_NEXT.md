# TODO NEXT

## Status: 1.x is frozen (2026-08-22)

**Patches and optimization only.** Anything that adds capability waits for 2.0.

| Allowed in 1.x | Not allowed in 1.x |
|---|---|
| Bug fixes | New adapters |
| Performance work with a measurement behind it | New flags, commands or subsystems |
| Tests, especially for paths never exercised | Changes to the packet wire format |
| Documentation and translation | Anything that changes the meaning of an existing flag |
| Security and dependency updates | |

Read that table before adding anything below. An item that does not fit the
left column belongs under [Behind the freeze](#behind-the-freeze--20), however
good it is.

---

## Current state — v1.25.1 (2026-08-22)

### Closed sprints

| Sprint | Version | What shipped |
|--------|---------|--------------|
| 1 | v1.11.0 | Full chain of trust: CA/TPM → xZMercury → tdtp.lic → Orchestrator TrustGate |
| 2 | v1.12.0 | Air-gap offline cert, seat policy, mock-clock renewal test, `issue-unsafe-cert` |
| 3 | v1.12.0 | Structured audit log (JSON/syslog), per-job artifact, LDAP auth, Prometheus, Docker stack |
| 4 | v1.13.0 | `--map --input file.tdtp.xml` (P8: mapping YAML, executor, enum remap, loop guard layers 2+4) |
|   | v1.13.0 | Schema passthrough (`applySchemaPassthrough`) — type-drift bug in the SQLite workspace (8 tests) |
|   | v1.13.0 | `cmd/tdtp-xray` aligned with the framework core (~600 lines removed) |
| 5 | v1.14.0 | `--map --input s3://bucket/key` — S3-sourced packets in the mapping flow |
| 6 | v1.15.0 | `--map --input broker://queue` — RabbitMQ-sourced packets, yaml tags on `brokers.Config` |
|   | v1.15.0 | consumer.py: `tdtp.sync.branch.customers` → `--map broker://` (staging + merge_proc no longer needed) |
| 7 | v1.15.0 | consumer.py: all 7 entities migrated to `--map broker://`; staging tables and merge procs removed |
|   | v1.15.0 | 7 new mapping YAMLs: `sync_flights`, `sync_reservations`, `sync_countries`, `sync_guides`, `sync_tours`, `sync_schedule`, `sync_branch_sales` |
| 8 | v1.16.0 | `--map --input broker://queue --listen` — daemon mode; NACK+requeue on error; graceful SIGTERM/SIGINT |
|   | v1.16.1 | RabbitMQ resilience: deliveryChan reset, QoS prefetch=1, heartbeat 10s, exponential reconnect backoff |
| 9 | v1.17.0 | P10 `--steps workflow.yaml` — DAG orchestration, parallel waves, `on_error: stop/skip/retry(N)` |

### Since v1.17.0 — no longer sprint-shaped, one theme per release

| Version | Theme |
|---------|-------|
| v1.18.0 | **TDTP v1.5 section-level encryption** — the redesign this file used to track as "next" |
| v1.18.1–1.18.3 | Go security advisories; audit SQL sink; libtdtp v1.5 fixes |
| v1.19.x | `--to-tdtp` (re-filter/re-version without a DB round-trip); fast-parser conformance |
| v1.20.x | `--sync-incremental --to-broker`; orchestrator `GET /jobs?limit=`; sub-second timestamps on export |
| v1.21.0 | `--quiet` |
| v1.22.x | XLSX written and read in-house, excelize dropped; byte-level `<sheetData>` scanner |
| v1.23.0 | `security.mercury_url` in the tdtpcli config |
| v1.24.x | Orchestrator job-log retention, `--drain`, WAL mode, parallel-safe audit |
| v1.25.0 | Datetime round-trip across SQLite/MySQL/PostgreSQL; cheaper escaped-row splitting; MSSQL datetime formatters |

**The v1.5 encryption redesign is done** — shipped in v1.18.0, 2026-07-22. Its
~290-line design writeup lived on in this file for a month after the fact, which
is the exact failure mode `CLAUDE.md` warns about. The spec reference is
[`docs/tdtp-protocol-schema.md`](docs/tdtp-protocol-schema.md) → "v1.5"; the
release notes are in [`CHANGELOG.md`](CHANGELOG.md) → 1.18.0. Nothing about it
belongs in a plan any more.

---

## Open in 1.x

### Optimization — measured, not yet done

Both came out of profiling the PostgreSQL export on 2026-08-22, and both sit
squarely inside the freeze: no new capability, no format change.

**`rows.Values()` on the PostgreSQL read path — measured, and smaller than it
looks.** pgx allocates a fresh `[]any` per row and boxes every value into it, and
the line carries 21% of CPU samples. But the achievable saving is bounded by the
wire: reading the same 100k×16 costs 103.9 ms as raw bytes, 125.8 ms scanning
into reusable typed destinations, and 150.3 ms through `Values()`. **The whole
prize is 24.5 ms** — about 6% of the export — plus some share of the GC relief
from 2.7M fewer allocations.

Against that, the loop in `readRowsWithSQL` has to be rebuilt around typed
destinations with a schema→destination mapping and a formatter per type, and a
schema/column mismatch stops being a soft conversion and becomes a hard scan
error. Worth doing only after the cheaper items above are gone.

**~~`DECIMAL` and `REAL` still round-trip through `ParseValue`~~ — done for
PostgreSQL.** 412 ms → 335 ms, 1.2M fewer allocations. Establishing that the
round-trip was a no-op turned out to be the whole job: it was **not** one, and
the reason was a real bug (see below). Left open for the other four adapters,
which still print floats with `'g'` and so cannot take the same shortcut until
they are fixed.

### The scientific-notation decimal bug — done, with one adapter unverified

Fixed in PostgreSQL and SQLite, where a `DECIMAL` column really does hand the
converter a `float64`. MySQL and MSSQL turned out **not** to be affected: their
drivers return `DECIMAL` as text, so it never reaches the float branch. The
formatting is `'f'` everywhere now anyway, so the hazard cannot come back
through a change of driver or type mapping.

An earlier version of this entry claimed all four adapters were affected. That
was wrong, and worth remembering why: it came from calling `DBValueToString`
directly with a `float64` for each `dbType`, which proves what the converter
does but not what the driver hands it. Reachability has to be checked against a
live driver, and the cheap way to check it is to run the new test against the
pre-fix code — the PostgreSQL and SQLite ones fail there, the MySQL and MSSQL
ones pass.

**Access is the one left unverified**, for want of a live Jet/ACE source. It
shares `genericValueToString` and so takes the fix, but nothing has confirmed
what its driver returns for a `DECIMAL`.

### The `version` attribute — done in 1.x

All three are done: numeric comparison (`pkg/core/packet/version.go` and the
mirror in `xzmercury/internal/api`), validation on read, and consistency between
what a packet declares and what it carries.

**~~Nothing validates the version on read~~ — done.** `validatePacket` now
refuses a value that is not a version (`abc`, `1..2`, `v1.4`), accepts a
well-formed but unknown one (`1.6`, `2.0`) as the compatibility rules require,
and warns without refusing when a packet's features are newer than its declared
version — the common case being a compressed packet declaring `1.0`, of which
years of archives exist.

**~~A declared version is not backed by its contents~~ — done.** A packet
declaring 1.4 or later without `xxh3` is now refused by `VerifyAndPrepare`. It
used to pass and print `✓ Local integrity: OK`, because the verification step
was skipped when there was nothing to verify and the caller read silence as
success — so relabelling a plain packet `1.5` produced a false assurance.

Refusing rather than flagging was the right call for a reason worth keeping:
with a Mercury registry configured the same packet **already** failed, as
`runMercuryCheck` turns an empty hash into `ErrHashNotRegistered`. The outcome
had been depending on whether a registry happened to be reachable. Now both
paths agree.

The 1.3.1 direction is deliberately not checked: the compact format is optional
at that version, so neither its presence nor its absence says anything.

All three steps under this heading are closed. What remains about the version
attribute is the 2.0 item — making the version the maximum of the features in
use — which is behind the freeze.

### Tests for paths that have never run

`pkg/adapters/mssql/integration_test.go` skipped for its entire life. Its first
live run (2026-08-22, SQL Server 2022) failed two tests and exposed a third
defect hiding behind them — none of the three in the adapter, all three in the
tests. Fixed. The lesson generalizes: **a suite that skips is not a suite that
passes.**

Worth doing the same wherever else a suite self-skips:

- `pkg/adapters/mysql` — has round-trip tests, needs a live MySQL to prove them
- `pkg/brokers` — Kafka tests and benchmarks skip without a broker
- `pkg/adapters/postgres` — now covered, but only against PostgreSQL 16

Standing the servers up is cheap. The value is not the green tick; it is that
these runs keep finding real defects.

The CLI suites under `tests/cli/` are uneven in the same way, and the gap is
which adapter a feature was written against rather than which feature matters:

| Suite | Checks | Missing against sqlite |
|---|---:|---|
| `test_sqlite.py` | 96 | — |
| `test_postgres.py` | 87 | `diff`, `merge`, S3, MSMQ |
| `test_mysql.py` | 58 | columnar, `--stream`, processors, dates |

MySQL is the one to take next: `--columnar`, `--stream` and the pre-export
processor chain are all unexercised there, and its `ReadAllRowsStream` is the
one that shares `base.StreamSQLRows` — so a divergence would show up in the
shared code, not in the adapter.

### Documentation — translation, Tier 2 onward

Tier 1 is **done**, against the 36 K of Cyrillic the old plan recorded across
those four files:

| File | Cyrillic left |
|------|--------------:|
| `docs/SPECIFICATION.md` | 0 |
| `docs/README.md` | 0 |
| `docs/USER_GUIDE.md` | 63 |
| `docs/ETL_PIPELINE.md` | 80 |

The Russian originals live in `docs/ru-archive/` — that directory is an archive,
not a debt, and must not be counted as outstanding work.

What remains, largest first, excluding the archive:

| File | Cyrillic |
|------|---------:|
| `pkg/python/libtdtp/README.md` | 4 758 |
| `cmd/tdtpserve/README.md` | 4 501 |
| `bindings/python/DEVELOPER_GUIDE.md` | 3 852 |
| `cmd/tdtp-xray/README.md` | 3 558 |
| `cmd/tdtpserve/AUTH_PLAN.md` | 3 348 |
| `examples/README.md` | 2 990 |
| `pkg/adapters/base/MIGRATION_EXAMPLE.md` | 2 014 |

Counted over the `U+0400–U+04FF` block only. A naive `[а-яА-Я]` character class
also matches `—` and `→` in some locales and roughly doubles every figure —
which is how the first pass of this table came out twice too large.

Then a long tail of per-example READMEs at 1–3 K each. These block integration
work rather than evaluation, which is why they are Tier 2 and not Tier 1.

Go comments (~153 K) are deliberately **not** on this list; the reasoning is
kept in `docs/ru-archive/` unchanged.

### The two partitioners disagree on NULL-bearing data

A streamed export fits about 0.3% more rows per part than the buffered one when
the table holds NULLs, and its parts run correspondingly over the size asked
for. On a NULL-free table the two agree exactly, boundary for boundary —
measured both ways on the same 100k-row table, with and without a nullable
column.

The cause is *when* the markers go on. The buffered path runs `DetectAndApply`
over the whole set before partitioning, so `estimateRowSize` measures `[NULL]`
— six characters. The streaming path applies markers per part, by which time
the row has already been counted at its raw one-byte size.

**No data is affected** and no check that compares content fails: both paths
emit the same rows in the same order. What is not true, and was quietly assumed,
is that one table exported both ways yields byte-identical files.

The obvious fix is not obviously right. Applying markers per row on arrival
would make the streaming estimate match, but `DetectAndApply` decides what to
declare in the *schema* from the whole set, so per-row is not the same
operation. Worth a measurement before a design: the error is 0.3%, and a part
that overshoots its budget by 0.3% matters only where the budget is a hard
external limit — which is the broker case, and the broker path is buffered.

Noticed while wiring `--packet-size` into the file export, not caused by it.
`tests/cli/test_postgres.py` T9.18 asserts the boundaries match and passes
because its fixture has no NULLs; the comment there says so, so that a NULL
added to the fixture reads as a fixture change and not a regression.

### `cmd/tdtp-xray` writes a lossy pipeline config

`app.go:1671` declares its own `TDTPOutputConfig` with three fields —
`destination`, `format`, `compression`. The real one, `pkg/etl/config.go:144`,
has thirteen. Saving a pipeline from the GUI therefore drops ten of them,
**`encryption` among them**, along with `compact`, `fixed_fields`,
`compress_algo`, `compress_level`, `s3` and `fast`. Silently: the YAML is
written, it parses, and the settings are simply not in it.

The reason it was copied rather than imported is worth knowing before choosing a
fix. Wails serializes the struct to the frontend as JSON, and the `pkg/etl`
version carries `yaml` tags only. So it is either json tags on
`pkg/etl.TDTPOutputConfig` and a type alias in xray — one declaration, the
duplication gone — or a field-by-field sync that will drift again on the next
field added.

Diagnosed, not fixed. The choice is a contract decision, and the GUI cannot be
exercised from here.

### Complexity outliers — funcfinder, 2026-08-25

Over the whole tree: 560 files, 4719 functions; 3688 of them scored, average
cognitive complexity 9.4, 109 VERY_HIGH and 40 CRITICAL. Only the part worth
acting on is below — **a high score is a question, not a defect**, and two of
these four resolve to "leave it alone".

| cx | lines | where |
|---:|------:|-------|
| 128 | 107 | `pkg/xlsx/ooxml_read.go:221` `parseSheetXML` |
| 64 | 89 / 98 | `pkg/core/packet/streaming.go` `GeneratePartsStream`, `…WithSender` |
| 64 | 105–134 | `InspectTable` — five of them: postgres, mssql, mysql, sqlite, access |
| 16 | **599** | `cmd/tdtpcli/main.go:29` `routeCommand` |

**`routeCommand` is the clear one, and its number is the low one.** cx=16 is not
complexity; 599 lines is length. It is the same shape as `newRouter`, which
reached 308 lines because every endpoint added since had landed in the same
body, and became seven `registerXxxRoutes` in 1.24.x. Splitting it on that
precedent fits the freeze: same commands, same dispatch order, same bodies.

**The two `GeneratePartsStream` functions are new code**, arrived with
`--stream`, and are BETA. Nesting depth 7 in a function that also owns the
finalize pass is worth reading before the flag loses its BETA label rather than
after.

**The five `InspectTable` are not five copies of one fact** and must not be
filed as duplication. Their queries differ in substance —
`information_schema` against `PRAGMA` against `sys.columns`. What repeats is the
frame: query, scan, assemble the report, the same non-fatal fallbacks. Folding
it means every adapter supplying a query plus a row→column mapper and sharing
the assembly, which is real work and larger than the table suggests. Not
scheduled.

**`parseSheetXML` carries the worst number in the tree and is probably
legitimate.** It is a nested state machine over a nested format, written on
purpose in 1.22 when excelize was dropped. Measure a replacement before
believing it would be simpler.

Regenerating: the binaries live in `H:\Ruslan\Code\Go\funcfinder\*.exe`, the
outputs in `.codemap/` and `docs/analysis/`, both gitignored.
`.funcfinder.config` still points `FUNCFINDER_BIN` at `/tmp/funcfinder/funcfinder`,
which no longer exists — so the commit hook it configures is silently a no-op,
and the path needs deciding rather than pointing at `/tmp` again.

### Housekeeping

- `CHANGELOG.md` carries **two** stranded `## [Unreleased]` sections, not the one
  this bullet used to name: `refactor/orchestrator-route-groups` (line 514,
  between 1.24.1 and 1.24.0) and `feature/sprint4-map` (line 1845). Both
  describe work that shipped — fold each under a version heading or drop it.
  The third, at line 5, is the current branch and belongs there.
- ~~The working tree accumulates untracked scratch~~ — **cleared.** `git status
  --untracked-files=all` reports zero untracked files. The funcfinder outputs
  that would otherwise land there (`.codemap/`, `docs/analysis/`) are
  gitignored.
- `TestCompressDataForTdtp` (`pkg/processors/compression_test.go:133`) asserts
  `stats.Time != 0` after compressing three short rows. On Windows the clock is
  coarser than the work, so the assertion flakes. It is testing the timer, not
  the compressor — assert on the output instead.
- `benchmarks/bench_duckdb` does not build with `CGO_ENABLED=0`, because
  go-duckdb needs cgo. Gate it behind a build tag or document the requirement.

---

## Behind the freeze — 2.0

Everything here is a capability change. **None of it goes into 1.x**, however
ready it looks. Kept because the analysis is worth having when 2.0 opens, not
because it is scheduled.

### Oracle adapter — not started

Raised while comparing the framework against Soft Review's integration services
(their stack is Oracle PL/SQL + J2EE). Oracle is the one mainstream DBMS
`pkg/adapters` does not cover, and in the banking and enterprise segment that
gap decides whether the framework is evaluated at all.

**No architectural obstacle.** `adapters.Adapter` (17 methods) and the `base`
helpers apply to Oracle unchanged; nothing in the shared layer needs
redesigning. The reference point is the MySQL adapter — written last, entirely
on the finished `base` layer, so it measures what a new adapter costs today:
**~1000 non-test lines** (adapter 249 / import 188 / inspect 213 / types 219 /
export 133).

**Estimate: 14–20 man-days, plan for 15–16.** The full breakdown — the two
high-risk items, the two round-trip traps, the config selectors with their
existing precedent, and the driver decision that has to be made before starting
— was written out in this file before the freeze. Recover it with
`git log -p TODO_NEXT.md` rather than re-deriving it.

### The `version` attribute as the maximum of the features in use

Every feature stamps the version except compression: compact format writes
`1.3.1`, integrity `1.4`, encryption `1.5`, and compression leaves `1.0` while
announcing itself on `<Data compression="…">`. Compression arrived in 1.2,
before the stamping convention existed, and was never brought into it.

The consequence is small but real: a compressed packet tells a strict 1.0-only
reader that it can be read, and that reader finds Base64 where rows should be
instead of refusing cleanly.

The rule to adopt is **version = max(features)**, not "compression means 1.2" —
compression is orthogonal to the ladder, so forcing 1.2 would *lower* a
compressed 1.4 packet and lose the fact that it carries integrity hashes.
Measured beforehand: a packet declaring `1.2` behaves today exactly as one
declaring `1.0`, on both the parse and the import path, so the change is inert
inside this implementation and carries risk only for external consumers that
match on the version string. `--to-tdtp` would also need `1.2` added to its
target list and a `--compress` option, and its integrity must be computed
**before** compression, as the export path already does.

### 2.1 Parallel daemon — `--map --listen --workers N`

- N independent goroutine workers, each with its own AMQP connection
- Each worker ACKs independently; no shared state between workers
- Graceful shutdown waits for the in-flight message in every worker
- RabbitMQ: N separate connections (multiple consumers on one channel is an anti-pattern)
- Kafka: consumer group, N partitions → N workers, the native model

### 2.2 Streaming CLI — the export half shipped, the import half did not

**The export half is done and this entry is what is left of the plan.**
`--stream` (BETA) is on `bench/sqlite-date-columns`, not yet merged: a
`ReadAllRowsStream` in each of SQLite, MSSQL, MySQL and PostgreSQL, driven from
`cmd/tdtpcli/commands/export_stream.go` through `GeneratePartsStream`. It went
in against a production case rather than a wish — a 24 M-row table wanted about
17 GB and could not be exported at all; streamed it holds 63 MB flat and writes
1408 parts in 213 s.

It reached 1.x on the freeze's own terms: no new capability, an existing export
that stops buffering. `--stream` is nonetheless BETA and stays that way until
each adapter has been exercised on a real table, because "the driver streams"
is a claim about the driver, not the API. The tell is a flat peak as the data
grows — MySQL holds 62 MB at both 524 288 and 4 194 304 rows, where the
buffered path goes 388 MB → 2 823 MB. A peak that grows linearly means the
driver materialized the result and the streaming is decorative.

What remains is genuinely new surface and stays behind the freeze:

- `--import-stream` reads from stdin or a broker row by row, upserting without accumulation
- together with the above it allows tables larger than RAM in both directions
- it needs 2.3 (schema negotiation) first

### 2.3 Schema migration — `ALTER TABLE`

Add and drop columns, change types, on schema drift between the packet and the
target table. Prerequisite for `--import-stream`, which needs schema negotiation.

### Grace period for `tdtp.lic`

Today expired = fatal, which hurts integrators mid-project. Proposal:
`--grace-period 30d` with read-only mode after expiry. Nothing is implemented —
no `GracePeriod` identifier exists anywhere in the tree.

---

## Keeping this file honest

`CLAUDE.md` asks that this file be checked for staleness **before** starting
work, and that a completed plan be deleted rather than left standing. The v1.5
encryption section outlived its release by a month and had grown to half the
file; the sprint table sat eight minor versions behind. Both are corrected
above.

The check is mechanical: read the plan, grep for the artefacts it proposes
writing, and if they are all there and it compiles, the plan is done.
