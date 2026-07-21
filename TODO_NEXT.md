# TODO NEXT — Sprint план

## Поточний стан (v1.17.0, 2026-06-15)

### Закриті спринти

| Sprint | Версія | Що закрили |
|--------|--------|------------|
| 1 | v1.11.0 | Повний ланцюг довіри: CA/TPM → xZMercury → tdtp.lic → Orchestrator TrustGate |
| 2 | v1.12.0 | Air-gap offline cert, seat policy, mock-clock renewal test, `issue-unsafe-cert` |
| 3 | v1.12.0 | Structured audit log (JSON/syslog), per-job artifact, LDAP auth, Prometheus, Docker stack |
| 4 | v1.13.0 | `--map --input file.tdtp.xml` (P8: mapping YAML, executor, enum remap, loop guard layers 2+4) |
|   | v1.13.0 | Schema passthrough (`applySchemaPassthrough`) — type-drift bug у SQLite workspace (8 тестів) |
|   | v1.13.0 | `cmd/tdtp-xray` aligned with framework core (~600 рядків видалено) |
| 5 | v1.14.0 | `--map --input s3://bucket/key` — S3-sourced packets в mapping flow |
| 6 | v1.15.0 | `--map --input broker://queue` — RabbitMQ-sourced packets, yaml tags на brokers.Config |
|   | v1.15.0 | consumer.py: `tdtp.sync.branch.customers` → `--map broker://` (staging + merge_proc більше не потрібні) |
| 7 | v1.15.0 | consumer.py: всі 7 entity мігровані на `--map broker://`; staging tables і merge procs видалені |
|   | v1.15.0 | 7 нових mapping YAML: `sync_flights`, `sync_reservations`, `sync_countries`, `sync_guides`, `sync_tours`, `sync_schedule`, `sync_branch_sales` |
| 8 | v1.16.0 | `--map --input broker://queue --listen` — daemon mode; NACK+requeue on error; graceful SIGTERM/SIGINT |
|   | v1.16.1 | RabbitMQ resilience: deliveryChan reset, QoS prefetch=1, heartbeat 10s, exponential reconnect backoff |
| 9 | v1.17.0 | P10 `--steps workflow.yaml` — DAG orchestration, parallel waves, on_error: stop/skip/retry(N) |

---

## Encryption format redesign — XML-preserving sections (target: v1.5)

Origin: designing `examples/travel-agency/TRAVEL-AGENCY_NEXT.md` (orchestrator-
governed, encrypted showcase) surfaced a real spec inconsistency between how
compression and encryption protect packet content.

Full protocol-level flow diagrams (producer/consumer sequence, dual-format
detection, fallback policy, attack/defense table) — same depth as the
existing v1.4 writeup — now live in
[`docs/tdtp-protocol-schema.md`](docs/tdtp-protocol-schema.md) → "v1.5
(planned)". This section stays the implementation-task-tracking summary;
that doc is the spec reference.

### The inconsistency

`pkg/core/packet/generator.go`'s compression path never breaks XML validity:
`<Header>`/`<Schema>` stay plain, only `<Data>`'s rows collapse into one
opaque value with a `Compression="zstd"` attribute marking it. A parser can
always read Header/Schema without decompressing anything.

Encryption (introduced v1.3, `cmd/tdtpcli/commands/encrypt.go`) does the
opposite: serializes the *whole packet*, wraps it in a proprietary binary
envelope (`[2B ver][1B algo][16B uuid][12B nonce][ciphertext]`) that isn't
XML at all — no `<DataPacket>`, nothing readable without the key, including
routing metadata that downstream infrastructure needs.

Concretely this breaks `--export-broker`/`--import-broker`
(`cmd/tdtpcli/commands/broker.go`): confirmed by direct grep, that command
pair has **zero** encryption support and **zero** `result_log` support —
both exist only on the `--pipeline` path. The broker's import side always
does `parser.ParseBytesWithDecompression(xmlData, ...)`, which can't handle
a non-XML blob, and nobody wired a "try decrypt first" branch there the way
`import.go` has (`data, decErr = DecryptEncBlob(...)`).

### Agreed design (selective section encryption, mirrors compression)

```xml
<DataPacket protocol="TDTP" version="1.5">
  <Header>...</Header>                                              <!-- stays plain: routing/dedup/part-reassembly need no key -->
  <QueryContext encryption="aes-256-gcm">BASE64_CIPHERTEXT</QueryContext>  <!-- was: filter conditions, business logic -->
  <Schema encryption="aes-256-gcm">BASE64_CIPHERTEXT</Schema>              <!-- was: field names/types -->
  <Data compression="zstd" encryption="aes-256-gcm">
    <R>BASE64_CIPHERTEXT</R>                                         <!-- same opaque-row shape compression already uses -->
  </Data>
</DataPacket>
```

`Header` stays in the clear deliberately — transport needs *something* to
route/reassemble on without a key (same as a queue name or `result_name`
already isn't secret). `QueryContext` and `Schema` are the two places that
leak real information without ever touching row data: which filter
conditions were interesting (business logic — e.g. `balance >= 1000`) and
which fields/types exist (structure). Both get the same treatment as
`Data`, not left exposed as an oversight.

### This is NOT touching dead code — check before implementing

`--map --input broker://queue` (`cmd/tdtpcli/commands/map.go`, sprints 6-8,
v1.15-1.16, the mechanism `examples/travel-agency/consumer.py` actually
uses today) **already decrypts the current whole-blob format**:
`IsEncryptedBlob(data)` → `DecryptEncBlob(ctx, data, opts.MercuryURL)`
(`map.go:163-164`, also `map.go:338`). This is a real, working, in-repo
consumer of the v1.3 format — not an orphaned feature nobody adopted.
Full blast radius of the detection primitives (`encrypt.go:40,48`):

- `cmd/tdtpcli/commands/import.go:127` (`--import`)
- `cmd/tdtpcli/commands/map.go:163,338` (`--map`, both one-shot and `--listen` daemon paths)

Changing the wire format must not silently break either. **Plan: additive,
not a replacement** — `IsEncryptedBlob`/`DecryptEncBlob` gain a second
detection branch (old binary-header blob vs. new `encryption="..."`
XML attribute) and dispatch accordingly; old packets already in flight or
archived keep decrypting exactly as they do today.

### Versioning question — resolved: bump to v1.5, don't revise v1.4 in place

Considered folding this into v1.4 instead of minting v1.5 (rationale
floated: v1.4 shipped only 2026-05-26, ~2 months ago, and this whole
framework has no confirmed external production consumers yet outside this
repo's own examples — so is a version bump even meaningful pre-adoption?).

**Rejected that, for a concrete reason found above, not just "many releases
already, don't want more churn":** `--map`'s decrypt path is a real,
already-working consumer of the *current* format. Revising "1.4" in place
would mean the string `"1.4"` stops meaning one fixed wire shape — exactly
the kind of silent redefinition that breaks the "read the version, know the
shape" contract the version field exists for, even with zero external
users. Since backward-compatible dual-format detection is required anyway
(see above), there's no actual cost saved by not incrementing the version
— may as well let the version string carry the truth.

- [ ] `pkg/core/packet/generator.go:120`, `utils.go:23` — new packets get
      `Version = "1.5"` once the new encryption path lands.
- [ ] `pkg/core/packet/types.go`, `dictionary.go`, `integrity.go`,
      `parser.go` — audit each `v1.4` comment/reference found via
      `grep -rn "v1\.4" pkg/core/packet/` for anything that needs a v1.5
      counterpart (mostly doc comments today; no version-string switch/case
      gating found in the parser — it detects features by attribute
      presence, not exact version match, which is why dual-format decrypt
      detection above is the right shape of fix).
- [ ] `docs/SPECIFICATION.md` "Версионирование" section: new `v1.5` entry
      following the existing changelog format (see v1.4/v1.3.1/v1.3
      entries for the expected shape — date, bullet list of additions,
      explicit "Backward compatibility" line).
- [ ] `cmd/tdtpcli/commands/broker.go` — add `result_log` publish (mirror
      `pipeline.go:144-153`) and the new v1.5 encryption path to
      `--export-broker`; `--import-broker` gains the same dual-format
      decrypt dispatch as `--map`/`--import`.

### Corrected understanding of travel-agency's current state (for TRAVEL-AGENCY_NEXT.md)

`TRAVEL-AGENCY_NEXT.md` was drafted assuming `consumer.py` still used
`--import-broker` → staging tables → SQL merge. **That's stale** — per this
file's own Sprint 6-7 entries, `consumer.py` was already migrated to
`--map --input broker://queue` (no staging, no merge procs) before this
plan was written. Only `coordinator.py` (export side, `--export-broker`)
still needs the Phase 1.5 work; the import side needs only the dual-format
decrypt update once the wire format changes, not new plumbing. Fix the
plan doc to match before starting Phase 1.5 implementation.

---

## Open Items (v1.x)

### 1. Grace period для tdtp.lic

Зараз: expired = fatal. Для integrators під час активного проекту — проблема.
Варіант: `--grace-period 30d` flag, read-only mode після expiry. Не критично зараз.

---

## v2.0 Roadmap — Масштабування, паралелізм, стрімінг

Великий перехід: від single-threaded ETL до паралельної та потокової обробки.
Всі пункти залежать один від одного — вводяться разом як breaking architecture change.

### 2.1 Parallel daemon (`--map --listen --workers N`)

- `--workers N` — N незалежних goroutine-воркерів, кожен зі своїм AMQP connection
- ACK кожного воркера незалежно (жодного shared state між воркерами)
- graceful shutdown: дочекатись завершення поточного повідомлення в кожному воркері
- RabbitMQ: N окремих connections (multiple consumers на одному channel — anti-pattern)
- Kafka: consumer group, N partitions → N workers (нативна модель)

### 2.2 Streaming CLI (`--export-stream` / `--import-stream`)

Код вже готовий: `pkg/core/packet/streaming.go` — `StreamingGenerator` з channel-based API.
Не підключено до CLI.

- `--export-stream` → пише рядки в output по мірі читання з DB (без буферизації всього набору)
- `--import-stream` → читає з stdin/broker рядок за рядком, upsert без accumulation
- Дозволяє обробляти таблиці розміром більше RAM

### 2.3 Schema migration (`ALTER TABLE`)

Add/drop columns, type changes при schema drift між версіями пакета і target таблицею.
Потрібно як основа для `--import-stream` (streaming import потребує schema negotiation).
