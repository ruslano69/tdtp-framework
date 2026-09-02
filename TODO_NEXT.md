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

**Recorded exception: `--columnar` and `--stream` landed after this table.**
Both are new flags — exactly what the right-hand column forbids — and both
merged into `bench/sqlite-date-columns` on 2026-08-24/25, two to three days
after this freeze was declared. They are staying: both are already measured,
covered by tests, and written up in `CHANGELOG.md` as part of the 1.26.0
release, and pulling them back out at this point would cost more than the
freeze buys. Noted here so the discrepancy is a decision, not a thing that
slipped through. The freeze otherwise holds — read it as "no more of these
after 1.26.0," not as license to keep adding capability.

---

## Current state — v1.26.0 (2026-08-31)

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
| v1.25.1 | Protocol-version comparison and validation hardening (three integrity-gate bypasses) |
| v1.26.0 | `--columnar`/`--stream`, `pkg/transform` step ordering, the `--limit`/`--offset`/`--fields`/`--packet-size` silent-ignore fixes, the unread-flag checker, workspace driver bypass and box-reuse, PostgreSQL typed-scan read path |

**The v1.5 encryption redesign is done** — shipped in v1.18.0, 2026-07-22. Its
~290-line design writeup lived on in this file for a month after the fact, which
is the exact failure mode `CLAUDE.md` warns about. The spec reference is
[`docs/tdtp-protocol-schema.md`](docs/tdtp-protocol-schema.md) → "v1.5"; the
release notes are in [`CHANGELOG.md`](CHANGELOG.md) → 1.18.0. Nothing about it
belongs in a plan any more.

---

## Open in 1.x

### Optimization — measured, not yet done

Came out of profiling the PostgreSQL export on 2026-08-22, and sits squarely
inside the freeze: no new capability, no format change.

**~~`rows.Values()` on the PostgreSQL read path~~ — done.** `readAllRowsTyped`
(`pkg/adapters/postgres/read_typed.go`) scans into reused, per-column typed
`pgtype.*` destinations instead of letting `rows.Values()` allocate a fresh
`[]any` per row and box every decoded value into it. Each destination is then
converted back to the exact `any` shape `Values()` already produced for that
cell (`time.Time` or `pgtype.InfinityModifier` for dates, `pgtype.Numeric` by
value, …), so `pgCellToTDTP` runs completely unmodified downstream.

Measured end to end on a live PostgreSQL 16, 524 288 rows × 9 columns (three
text, `id`, `NUMERIC`, `BOOLEAN`, three date/time — the shape that made
`Values()` 76% of read allocations in the first place), three repeats:

| | before | after |
|---|---|---|
| time | 1258 ms | **1044 ms** (−17%) |
| memory | 469 MB | **325 MB** (−31%) |
| allocations | 16.26M | **11.54M** (−29%) |

The earlier estimate on this line ("24.5 ms, about 6% of the export") measured
only the decode step in isolation and turned out to understate the real
number — end to end the win is close to 3× that.

Scoped to `ReadAllRows` only, the one place the adapter builds its own
`SELECT * FROM table` against the same table `GetTableSchema` just described,
so column order and type are guaranteed to match by position — the same
precedent already set for SQLite's `CAST(...AS TEXT)` path. `ReadRowsWithSQL`
(TDTQL, `--where`, views) takes a caller-built query where that guarantee does
not hold, and keeps the old `rows.Values()` path unchanged.

Deliberately excludes `REAL`/`FLOAT`/`DOUBLE` (TDTP doesn't distinguish float4
from float8, and widening float4 into float64 changes the formatted digits) and
any `TEXT` field with a non-empty `Subtype` (uuid/json/jsonb/inet/cidr/macaddr/
xml/array — `pgtype.Text` isn't guaranteed to hand back what `pgValueToString`
expects for those). `pgTypedScanSupported` gates on this before the query runs;
a schema outside the list falls straight through to the old path, unchanged.

**~~`DECIMAL` and `REAL` still round-trip through `ParseValue`~~ — done for
PostgreSQL.** 412 ms → 335 ms, 1.2M fewer allocations. Establishing that the
round-trip was a no-op turned out to be the whole job: it was **not** one, and
the reason was a real bug (see below). Left open for the other four adapters,
which still print floats with `'g'` and so cannot take the same shortcut until
they are fixed.

### ~~`output.tdtp.*` has no part-size control~~ — done

`output.tdtp.packet_size_mb` now reaches `Generator.SetMaxMessageSize` the
same way `--packet-size` does on the CLI — same formula, in fact:
`packetSizeBudget` moved from `cmd/tdtpcli/commands` to
`packet.PacketSizeBudget` so both paths share one implementation instead of
the pipeline growing a second copy. `packet_kb` (Kafka-only) is untouched and
still has no `×2` — the two were never meant to align.

`TestExporter_TDTP_PacketSizeMB` exports a ~1.2 MB dataset at the default
budget and at `packet_size_mb: 1`, and requires the second to produce more
parts. Mutation-tested: removing the wiring in `exportToTDTP` fails it.

### `output.rabbitmq.vhost` — done

Found rereading `docs/ETL_PIPELINE.md` end to end against `pkg/etl/config.go`.
`RabbitMQOutputConfig` had no `VHost` field at all, so a pipeline naming any
vhost but `/` silently connected to `/` instead. Fixed (`rabbitMQBrokerConfig`,
tested) — a one-line wiring fix for an already-declared field, unlike the
`error_handling` design question found in the same pass (below, moved behind
the freeze — it is not this kind of fix).

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

### ~~The two partitioners disagree on NULL-bearing data~~ — done

Both halves of the entry that stood here were wrong, and worth recording as a
lesson rather than quietly replacing.

**The size was the dataset, not the defect.** It said "about 0.3%", which was
one table with a single nullable column in ten. The undercount scales with NULL
density: on eight all-empty date columns it reached **43%** — 2.78 MB where
`--packet-size 2` asked for 2.

Note what the budget actually governs, because it is easy to overstate: the
**uncompressed** payload. With compression on, what leaves the machine is
whatever the codec achieves — three to five times smaller, data-dependent — so
sizing to a hard external limit through this flag was never possible for a
compressed packet anyway. The overshoot bites on uncompressed exports, and the
plain fact underneath is simply that the estimator measured something other than
what gets written.

**And the fix was smaller than the entry claimed.** It said the obvious remedy
was not obviously right, because moving `DetectAndApply` would be a redesign.
Nothing had to move: `estimateRowSize` measured the raw row while the markers go
on later, so it was enough to measure a value as it will be written — NUL counts
as the six characters of `[NULL]`. Other markers shrink a value or leave it
alone, so measuring them raw over-estimates, and that is the safe direction.

The buffered path is untouched by construction: it applies markers before
measuring, so it was already counting `[NULL]`. Verified against a binary built
from the previous commit — broker 3 packets either way, buffered file export 9
parts either way, streamed 6 → 9.

### ~~`cmd/tdtp-xray` writes a lossy pipeline config~~ — done

The local `TDTPOutputConfig` is a type alias to `pkg/etl.TDTPOutputConfig` now,
so there is one declaration instead of two, and settings the GUI has no control
for survive a load-and-save round trip untouched.

**No encryption switch was added to the GUI, deliberately.** The bug was never
"xray lacks a checkbox" — it was "xray destroyed what it did not understand".
Those are different problems, and only the second one was ours to fix.

**Do not add `./cmd/tdtp-xray` to `go.work`** — asked and answered
(2026-08-27). It would put the module back under `go build ./...` and drag the
Wails dependency tree into the workspace, and the value is not there any more:

Practice has moved the tool's role. Agents turn out to write pipeline YAML
better than the visual builder does, so what remains for xray is **visual
inspection**, not authoring. That reframes what matters about it: a viewer must
never corrupt what it opens, which is exactly the property just fixed, and it
does not need to understand every field to be useful. Coverage of a builder it
is no longer being used as would be the wrong thing to buy.

The module still does not build standalone without `go mod tidy`. Left as is
for the same reason.

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
- `benchmarks/bench_duckdb` needs cgo, which on this machine means the mingw64
  under the user's home rather than the one on PATH — with the right compiler it
  builds in under a minute. The old wording here said it "does not build", which
  came from reading `cgo.exe: exit status 2` as "no cgo" instead of "wrong gcc".
  Still worth a build tag or a line in the README so the requirement is stated
  rather than discovered.
- DuckDB as the pipeline workspace was tried and measured — three times slower
  on load, because every `database/sql` call crosses into cgo and its fast path
  is the Appender. Written up in `CLAUDE.md` so it is not re-derived; the code
  was reverted rather than kept, since an engine seam with one implementation
  buys nothing.

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

### 2.4 Pipeline survivability belongs to the orchestrator, which has no tools for it yet

`ErrorHandlingConfig` (`pkg/etl/config.go`) parses, defaults and validates
`on_transform_error`, `on_output_error`, `retry_attempts` and
`retry_delay_seconds`, but nothing in the pipeline runner reads them — only
`on_source_error` actually branches on anything. Found rereading
`docs/ETL_PIPELINE.md` against the code; `docs/ETL_PIPELINE.md` now says so
plainly instead of implying the four fields work, but the fields themselves
are unchanged.

Not a 1.x bug fix, because there is no single obvious behavior to restore —
the design space is genuinely open, and the number of ways a pipeline can
fail (source, transform, output) multiplied by the number of ways to respond
to each is large enough that a fourth quiet default would just be a
differently-wrong guess:

- **fall back to a secondary output** — already exists as `output.fallback`
  for the primary/fallback channel pair, but nothing routes an
  `on_output_error: fallback` (or similar) into triggering it explicitly;
  right now the only path in is the circuit breaker, which switches on
  *any* primary failure, not on a policy choice per error type
- **raise an alert through a broker** — a side channel separate from both
  the primary output and `result_log`, for "the pipeline is unhealthy" as
  opposed to "this run's result"
- **fail with a distinguishable exit code / error packet**, the one thing
  that already happens today, unconditionally
- **retry the failing step after a delay**, which is not idempotent for
  free: a `transform` failure may have already populated the result table
  in the workspace, an `output` failure may have already written some of a
  multi-part TDTP file or partially drained a spool — retrying blindly
  risks compounding the failure rather than recovering from it
- **retry the whole pipeline** (as opposed to just the failing step) after
  a timeout, which is a different knob than `retry_attempts` on a single
  step and does not currently exist as a concept anywhere in the config

And the part that makes this a design problem rather than four independent
flags: **any of the above can itself fail** — the fallback output can be
unreachable too, the broker carrying the alert can be down, the retried
step can fail again. A real design has to say what happens then (a bounded
retry count so this cannot loop forever is the obvious first constraint,
already half-present as `retry_attempts`, but the same question applies to
every strategy above, not only "retry") rather than leaving each fallback's
own failure mode implicit.

**Guiding scope, stated by the person who'll approve the design:** a pipeline
run is not a long-lived daemon fighting to stay alive — it's the opposite of
a service that never unloads and never sleeps, always fighting a network that
may recover, disk that may free up, a broker that may come back once someone
restarts it. A pipeline run is short-lived and disposable. Its job is: try
one fix or emit a warning, and if that doesn't clear it, **fail loud and
fail fast** — a clear error packet, a clear non-zero exit, a clear
`result_log` entry. Whatever decides to retry the whole thing later — a
schedule in `cmd/orchestrator`, cron, systemd, k8s — is a separate layer with
its own state and its own judgment about backoff, and does not need the
pipeline itself to reimplement that judgment. **The one hard requirement is
never staying silent** — every failure mode has to surface somewhere a
supervisor can see it, not get swallowed.

This narrows 2.4 considerably from the four-strategies list above: self-
retry and self-fallback inside a single pipeline run are *not* in scope —
that's the pipeline's own job description settled, not just a preference.
Retry-with-backoff, fallback-on-repeated-failure and alerting belong to the
supervisor. `docs/ETL_PIPELINE.md`'s `error_handling` section stays exactly
what it honestly is today: `on_source_error` (real), and four fields that
are accepted, validated, and never acted on — not a placeholder waiting to
be filled in, a closed decision.

**What's actually missing is on the other side.** Checked
`cmd/orchestrator`: it has none of this either. A schedule
(`ScheduleRecord`) is a bare cron expression plus `last_status` — a failed
run just waits for the next regular tick, with no backoff, no earlier
retry, and no distinction between "the network blipped" and "the config is
wrong and every future tick will fail the same way." The 2.0 work is
building that layer, not extending the pipeline:

- per-schedule retry policy (attempts, backoff) distinct from the cron
  interval itself — a schedule that fires every 15 minutes should not have
  to wait 15 minutes to retry a failure that clears in 30 seconds
- reading the pipeline's own signal for *why* it failed, once the pipeline
  side gives it one to read (an error code on the error packet is the
  natural carrier — already exists, just not classified into
  transient/permanent yet)
- the alert side channel, if it's wanted, lives here too — the orchestrator
  already has job records and a result-log style output; a queue alert is
  the same shape of thing already built once for `result_log`
- its own bounded-retry guard, for the same reason every strategy in the
  list above needs one: a retry policy that can retry forever on a
  permanent failure is not a safety net, it's a second silent failure mode

**A starting point narrower than all of the above, and worth building
first: inventory before scheduling, not at 3 AM when the cron fires.** A
lone `tdtpcli --pipeline` run has no view of anything beyond its own YAML —
it cannot know a sibling scenario's database is also down. The orchestrator
is the one thing that loads the whole scenarios directory at once and is
positioned to know, up front, whether every source and output every
scenario declares is actually reachable.

It doesn't do this today. `LoadScenariosDir`/`LoadScenario`
(`cmd/orchestrator/scenario.go`) parse *only* the `orchestrator:` YAML
block — name, params, permissions, runner. The `sources:`/`output:` sections
underneath, the ones that actually name a DSN or a broker host, are never
even unmarshaled at load time, let alone connected to. A scenario with a
typo'd DSN, an unreachable RabbitMQ host, or a `transform.sql` that doesn't
parse loads into the orchestrator without a complaint and stays silent
until its schedule fires for the first time.

The shape of the fix: on load (and on a refresh trigger — the scenarios
directory is not read-only), parse each scenario's full pipeline config via
`pkg/etl.LoadConfig`, and open/ping every source and output connection it
declares — not run the pipeline, just confirm what it depends on answers.
A scenario that fails this gets marked, surfaced (this is also a "never
stay silent" case), and left out of the schedule rather than being allowed
to fail predictably on its first real tick. Cheap relative to what it
prevents: this is exactly the class of failure — "the config was wrong
before anyone ran it" — that costs the most when it's discovered by an
unattended 3 AM run instead of at deploy time.

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
