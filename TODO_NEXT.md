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

**`rows.Values()` on the PostgreSQL read path — 1.75 s out of 9.27 s of
samples.** pgx allocates a fresh `[]any` per row and boxes every value into it.
Scanning into pre-allocated typed destinations would take most of that back.
It is now the largest single item in a 100k×16 export, the string round-trip
having been removed. Not a two-line edit — the loop in `readRowsWithSQL` has to
be rebuilt around typed destinations — so it wants its own before/after
measurement.

**`DECIMAL` and `REAL` still round-trip through `ParseValue`.**
`UniversalTypeConverter.ConvertValueToTDTP` has a fast path for
`TEXT`/`INTEGER`/`BOOLEAN`, and the read loops have one for `time.Time`; numeric
types have neither, so every such cell is formatted, parsed and formatted again.
On the 16-column benchmark table that is 3 columns out of 16. Whether the
round-trip is genuinely a no-op for them has to be **established first**, the
way the `time.Time` case was — see
`pkg/adapters/postgres/export_timefastpath_test.go` for the shape of that proof.
Do not skip the pass on the assumption that it is idempotent.

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
