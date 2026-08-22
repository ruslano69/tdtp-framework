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

## Current state — v1.25.0 (2026-08-22)

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

### Housekeeping

- `CHANGELOG.md` carries a stray `## [Unreleased] — refactor/orchestrator-route-groups`
  section stranded between 1.24.1 and 1.24.0. It describes work that shipped —
  fold it under a version heading or drop it.
- The working tree accumulates untracked scratch: `PR_DESCRIPTION.md`,
  `docs/SESSION_SUMMARY.md`, `docs/analysis/`, four `cmd/bench_*` directories,
  a pile of `examples/travel-agency/*.yaml` and `*.py`. Decide per item — commit,
  `.gitignore`, or delete. Leaving it makes `git status` useless as a signal.
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

### 2.2 Streaming CLI — `--export-stream` / `--import-stream`

**The code already exists and is not wired up.**
`pkg/core/packet/streaming.go` holds `StreamingGenerator` with a channel-based
API, 7 methods, and no CLI caller. Cheapest of the three by a wide margin — but
still new surface, so it waits.

- `--export-stream` writes rows as they are read from the DB, without buffering the full set
- `--import-stream` reads from stdin or a broker row by row, upserting without accumulation
- Together they allow tables larger than RAM

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
