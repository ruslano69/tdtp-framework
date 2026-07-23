# Changelog

All notable changes to tdtp-framework are documented in this file.

## [1.18.3] — 2026-07-22

### Added — audit.database: SQL sink for the audit logger

`AuditConfig` gained an optional `database` block (`type`, `dsn`, `table`,
`batch_size`, `auto_create_table`) so `tdtpcli` can write audit entries to
their own SQL database (sqlite/mysql/mssql/postgres) alongside the existing
file/console appenders — previously `pkg/audit.DatabaseAppender` was reachable
only by importing the library directly, with zero CLI config surface and
zero e2e coverage (`tests/cli/` had no audit tests at all). Deliberately a
separate connection from the pipeline's own `database:` config: reusing the
same connection/credentials would let the process being audited also rewrite
its own audit trail.

### Fixed — pkg/audit: two real bugs found while wiring this up

- **`generateID()`** ([entry.go](pkg/audit/entry.go)) built IDs from
  `time.Now().UnixNano()` alone. In a tight loop with no I/O between calls
  (exactly what a batched `DatabaseAppender.Append` does), the OS clock
  resolution can be coarser than the loop itself — observed producing
  duplicate IDs on Windows, which then broke every subsequent insert in the
  batch on the `id` PRIMARY KEY. Fixed with an atomic sequence counter,
  independent of clock resolution.
- **`flushBatch()`** ([database_appender.go](pkg/audit/database_appender.go))
  rolled back and returned an error on a failed batch but never cleared
  `batchQueue` — the same poisoned entries stayed queued and re-failed every
  later flush, permanently wedging the appender after a single collision
  (reproduced: 0 of 12 entries committed, not just the one bad batch). Now
  clears the queue unconditionally before returning, bounding a failure to
  the batch that hit it.
- **`newAuditDatabaseAppender`** ([production.go](cmd/tdtpcli/production.go)) —
  found only by testing concurrent `tdtpcli` invocations against the same
  `audit.database` SQLite file (a question raised in review, not something
  the original design considered): SQLite allows one writer at a time, and
  without a `busy_timeout` a second process hit an immediate
  `SQLITE_BUSY "database is locked"` instead of waiting — 3 of 8 concurrent
  runs failed outright. `PRAGMA busy_timeout` + `journal_mode = WAL` fixes
  it, but only in that order: switching journal mode itself takes the write
  lock, so applying WAL first left one more race window (still ~1-in-8
  bursts failing) before `busy_timeout` was active to cover it. Verified
  reliable across 5 bursts of 8 concurrent processes (40/40 entries
  committed, 0 errors) after fixing the order.

`tests/cli/test_audit_database.py` (new) covers all three: config wiring
(A1), the failure path (A2), and concurrent-writer regression (A3).

## [1.18.2] — 2026-07-22

### Fixed — libtdtp silently returned garbage for v1.5 encrypted packets

`Tdtp.read()`/`parse_bytes()`/`read_multipart()` (the Python facade's whole
reason for existing — no xZMercury client, pure parse/compress only) had
zero awareness that TDTP v1.5 encryption exists. An encrypted packet parses
as valid XML (`Header` always stays plain), so `J_ReadFile`/`J_ParseBytes`/
`J_ReadMultipart` would parse it "successfully" and hand back
`{"schema": {"fields": []}, "data": [["<entire base64 ciphertext blob>"]]}`
— no error, no field, just one opaque row masquerading as data. Combined
with `--compress`, the same read path instead failed with a confusing
"decompress error" (it tried to zstd-decompress still-encrypted bytes) —
loud, but for the wrong reason.

- New `packet.IsEncrypted(pkt)` ([pkg/core/packet/encryption.go](pkg/core/packet/encryption.go))
  is the single canonical detector, used by both the CLI's
  `IsEncryptedPacket` (now a thin wrapper, no logic duplicated) and libtdtp.
- `J_ReadFile`, `J_ParseBytes`, and the shared `readPacketToJPacket` behind
  `J_ReadMultipart` now check it immediately after parsing and return a
  clear `error_code: "ENCRYPTED_PACKET"` explaining that decrypting a v1.5
  packet requires the full `tdtpcli` binary plus a reachable xZMercury
  server for burn-on-read key retrieval — libtdtp itself has no key-exchange
  capability by design and never will.
- Same gap, same fix, in the parallel C-struct ABI used by
  `TDTPClientDirect`/`PacketHandle`: `D_ReadFile`/`D_ParseBytes`
  (`exports_d.go`) now call `dSetError` + return 1 instead of returning
  `field_count=0`/`row_count=1` (the ciphertext blob as a single opaque
  "row") with a success code. No separate error-code channel exists on the
  D_ struct ABI, so this surfaces as the same plain-text "encrypted
  packet: ..." message, raised as `TDTPParseError` like every other D_ read
  failure — consistent with how that API already reports errors.
- Python bindings gained `TDTPEncryptedPacketError` (subclass of
  `TDTPParseError`, so existing broad `except TDTPParseError` handlers still
  catch it) mapped from the new J_ error code.
- Verified against the actual compiled library (not just Go unit tests):
  built `libtdtp.so`/`libtdtp.dll`, called `J_ReadFile`/`J_ParseBytes`/
  `J_ReadMultipart`/`D_ReadFile`/`D_ParseBytes` through ctypes exactly as
  the Python facade does, confirmed all five now reject a genuine
  v1.5-encrypted fixture instead of returning garbage.

## [1.18.1] — 2026-07-22

### Security — golang.org/x/text infinite loop (GO-2026-5970)

Upgraded `golang.org/x/text` `v0.30.0 → v0.39.0`: crafted input can drive an
internal `norm` routine into an infinite loop, reachable through
`pkg/xlsx/converter.go` (excelize) and `pkg/adapters/postgres/adapter.go`
(pgxpool). Caught by CI's `govulncheck`; `go mod tidy` cascaded consistent
upgrades to `golang.org/x/crypto`, `x/mod`, `x/net`, `x/sync`, `x/sys`,
`x/telemetry`, `x/tools` as transitive consequences — verified no breaking
API changes (`go build`/`go vet`/full unit test suite unaffected).

### Security — crypto/tls Encrypted Client Hello privacy leak (GO-2026-5856)

`go.work` was missing a `toolchain` directive, so in workspace mode its own
`go 1.25.0` line silently governed toolchain selection for every build —
the `toolchain go1.26.5` already declared in `go.mod` was never honored,
and every `go build`/`go vet`/`govulncheck` run kept using the
locally-installed `go1.26.4` regardless. Added a matching
`toolchain go1.26.5` line to `go.work`; verified via `go version` on a
freshly built binary that builds now actually use go1.26.5 (fixed version)
instead of go1.26.4.

Verified locally with `govulncheck` scoped to non-cgo packages
(`./cmd/tdtpcli/... ./pkg/... ./xzmercury/...`, working around this
environment's broken cgo toolchain): 0 reachable vulnerabilities, was 2.

## [1.18.0] — 2026-07-22

### Added — TDTP v1.5: section-level encryption

Encryption redesigned to mirror how compression (v1.2+) already works:
instead of wrapping the whole packet in an opaque binary envelope (v1.3),
`<QueryContext>`, `<Schema>`, and `<Data>` are each individually replaced
with `encryption="aes-256-gcm"` ciphertext; `<Header>` always stays plain
XML so routing, dedup, and multi-part reassembly need no key. Full design
and wire-format diagrams: `docs/tdtp-protocol-schema.md` → "v1.5".

- **Wire format** (`pkg/core/packet`): `EncryptSections`/`DecryptSections`
  (new `encryption.go`) encrypt/decrypt each section with one AES-256 key
  and a unique nonce per section. New `Encryption`/`Encrypted` fields on
  `Schema`/`QueryContext`; `Data` reuses its existing opaque-row shape
  (`Rows []Row`) unchanged. Fixed, non-configurable order: hash (v1.4) →
  compress → encrypt on write; decrypt → decompress → verify on read.
- **Key binding**: bound to the packet's own `Header.MessageID` (not a
  freshly generated UUID as v1.3 does) — the consumer must be able to read
  the UUID straight from the plain Header before decrypting anything.
  Multi-part packets need no special handling: each part already carries
  its own distinct `MessageID` (`{base}-P{n}`).
- **CLI**: `--enc` now produces v1.5 by default (was v1.3 whole-blob); new
  `--enc13` flag requests the legacy format explicitly for consumers not
  yet updated. Both share the same xZMercury `BindKey`/`RetrieveKey` flow —
  verified zero xZMercury server-side changes needed (`keystore.Bind`
  treats the key as opaque bytes regardless of client-side usage shape).
  Wired into `--export`, `--export-broker`, and `--pipeline`
  (`output.tdtp.encryption`).
- **`--export-broker`/`--import-broker`** (`cmd/tdtpcli/commands/broker.go`):
  gained encryption support entirely — previously zero (confirmed by grep
  before starting). `--import-broker`, `--map`, and `--import` all gained
  dual-format decrypt dispatch (legacy blob vs. v1.5 attribute-based,
  auto-detected from the bytes) via shared helpers
  (`parseAndDecryptBrokerMessage`, `decryptLegacyBlobIfNeeded`,
  `decryptV15PacketIfNeeded`).
- **Mandatory v1.4 integrity ahead of v1.5 encryption** (new
  `pkg/pipeline/produce.go`, `ComputeAndRegisterIntegrity`): found live,
  not planned — `VerifyAndPrepare`'s consumer-side pre-flight runs for any
  packet with `Version >= "1.4"` (v1.5 packets included) and blocks on an
  empty `XXH3` with `HASH_NOT_REGISTERED`. Every v1.5 encryption call site
  now stamps + registers the integrity hash first, so no v1.5 packet is
  ever missing what the existing v1.4 gate already required — zero
  regression to the pre-v1.5 security posture, no changes needed to the
  gate itself.

### Fixed — two real bugs found via live end-to-end testing (not unit tests)

- **`pkg/core/packet.EncryptSections` silent plaintext leak**: an earlier
  version left `MaterializeRows()` to the caller; a caller that skipped
  compression (or went through `pkg/etl`'s exporter, which only
  materializes as a compression side effect) left `rawRows` populated, so
  `writePacketTo`'s fast path silently wrote the *original plaintext rows*
  into `<Data>` right next to a truthful `encryption="aes-256-gcm"`
  attribute — a packet that looks encrypted but isn't. Caught by
  `pkg/etl/exporter_v15_test.go` before it shipped; guarantee now lives
  inside `EncryptSections` itself, not duplicated per call site.
- **`integrityProc` schema-hash/`@MRC` ordering** (`cmd/tdtpcli/commands/export.go`):
  the Mercury base-URL `@MRC` Dictionary entry was embedded *after*
  `ComputeIntegrity` stamped `Schema.XXH3` — the hash covered a Schema
  that was about to change, so `VerifyIntegrity` on import always failed
  with a schema hash mismatch. 100% reproducible whenever `--integrity`/
  v1.5 encryption and `--mercury-url` were combined — not v1.5-specific,
  but v1.5 made this combination unconditional, surfacing it. Fixed by
  reordering: embed `@MRC` before computing integrity. Regression test:
  `export_integrity_test.go`.

Both confirmed via live round-trips against a real `xzmercury --dev`
instance, real RabbitMQ (Docker), and real SQLite — not mocks alone.

## [1.17.2] — 2026-07-02

> **⚠️ Rebuild `libtdtp.dll`/`.so` before upgrading.** This release changes the
> binary layout of `D_Packet` (see "Changed" below). Any non-Python consumer
> that links against `libtdtp.h`/`D_Packet` directly (C, C#, PureBasic, etc.)
> must recompile against the new header — old binaries will silently read
> garbage from the new struct layout instead of failing loudly.

### Fixed — libtdtp: JSON casing drift, BOOLEAN serialization bug, v1.4 gate bypass

- **JSON field casing**: `Field`, `SpecialValues`, `MarkerValue`, `Schema`,
  `Dictionary`, `DictEntry` now carry explicit `json:` tags (snake_case),
  matching the already-documented convention instead of Go's default
  PascalCase. `pandas_ext.py`'s defensive PascalCase/camelCase fallbacks are
  kept for DLLs built before this change.
- **BOOLEAN serialization**: `pandas_ext.py` wrote `"true"`/`"false"` for
  `bool`/`numpy.bool_` values — the framework's own validator
  (`schema/converter.go`'s `parseBoolean`) only accepts `"0"`/`"1"` and
  rejected every boolean column written through the pandas bridge. Now
  writes `"1"`/`"0"`.
- **`J_Stamp` never set `Version = "1.4"`**: a packet stamped with real XXH3
  integrity hashes was read back as pre-1.4 by any consumer of
  `pipeline.VerifyAndPrepare` (including the new `J_VerifyMercury` below),
  silently skipping the entire v1.4 security gate. Mirrors the version bump
  `cmd/tdtpcli`'s `integrityProc` already does on export.
- **Error code classification**: `J_WriteColumnar`'s validation errors
  (`"invalid input: ..."`) fell through to `INTERNAL_ERROR` instead of
  `INVALID_INPUT` — added the missing prefix to `errorCodeFor`.

### Added — libtdtp: J_VerifyMercury, four previously-unreachable C ABI exports

- **`J_VerifyMercury(path, mercuryUrl)`**: the C ABI's first network-verifying
  v1.4 integrity check. `J_Verify` is deliberately local-only (no network);
  any non-Go consumer of `libtdtp.dll` had no way to confirm a packet was
  registered by an authenticated producer via xzMercury. Wraps
  `pkg/pipeline.VerifyAndPrepare` with `FallbackDegrade`, matching
  `tdtpcli --test --mercury-url`. Verified against a live `xzmercury --dev`
  instance (registered+reachable, empty URL, not-registered, Mercury-down,
  pre-1.4 packet). Python bindings: `TDTPClientJSON.J_verify_mercury`,
  `Tdtp.verify_mercury`.
- **`D_ParseBytes` / `J_ParseBytes` / `J_InspectBytes` / `J_WriteColumnar`**:
  all four were exported by the Go DLL (present in `libtdtp.h`) but had zero
  `ctypes` binding in `_loader.py` — unreachable from Python despite
  compiling fine. Wired with `argtypes`/`restype` and wrapper methods on
  `TDTPClientDirect`, `TDTPClientJSON`, and `Tdtp`.

### Changed — libtdtp: `D_Packet` row storage is now a flat buffer (C ABI breaking change)

`D_Row` previously stored each row as its own `C.malloc`'d array of
individually `C.CString`'d cells — reading it back from Python meant one
`ctypes`/FFI crossing per cell, which made `D_*` slower than `J_*` on
read/filter/mask at 100k rows despite skipping JSON entirely (the opposite
of what the Direct API is documented to deliver). Replaced with a single
flat data buffer plus one offsets array (`row_count*col_count+1` int32
entries) — the same shape `D_ColumnUTF8` already used for one column,
generalized to the whole grid. `D_Packet.rows`/`D_Row` are gone; new fields
are `row_data`/`row_offsets`/`col_count`.

Benchmarked on a 100k-row export: `read`/`filter`/`filter_only`/`mask_only`
flip from "`J_*` faster" to "`D_*` faster" (0.63–0.87×), matching the
`DEVELOPER_GUIDE.md` performance claim for the first time it was actually
measured. Verified byte-identical output vs `J_*` across 5 repeat read/free
cycles (no corruption, no leaks under repeated use).

Note: this flat-buffer layout specifically targets Python's `ctypes`
per-call overhead. Benchmarked separately against a native C consumer
(gcc/MinGW, no `ctypes` in the loop) on the same 100k-row file: the *old*
per-row layout was faster there (8ms vs 11ms) — native calling-convention
code doesn't pay the FFI tax this change eliminates. Native/PureBasic
consumers should keep using the row-major JSON boundary (`J_ReadFile`) or
the `D_Column*` accessors rather than assuming this change benefits them.

### Added — PureBasic example (`bindings/purebasic/`)

Empirically verified example calling `J_ReadFile`/`J_FreeString` via
`OpenLibrary`/`GetFunction` (dynamic loading). Documents two crash-causing
gotchas found by running the code, not by reading PureBasic's docs:
`ParseJSON(#PB_Any, ...)` returns the real handle as the function's return
value (not written back into the input variable), and `CloseLibrary()` on
`libtdtp.dll` crashes during `DLL_PROCESS_DETACH` because Go's `c-shared`
runtime doesn't support being unloaded. Static linking
(`-buildmode=c-archive`) was evaluated and rejected: it links cleanly via
gcc/MinGW and PureBasic's own linker accepts the archive format, but the
resulting PureBasic executable hangs on startup (Go runtime init appears
incompatible with PureBasic's own runtime bootstrap on Windows).

### Fixed — cmd/tdtp-xray build

`ExpandCompactRows` moved to a parser method and `DecompressData`'s
decompressor callback gained an `algo` parameter upstream; `cmd/tdtp-xray`
wasn't updated and failed to build.

### Fixed — `J_WriteFile`/`J_Stamp` trusted a caller-supplied `records_in_part`

`jPacketToDataPacket` (used by `J_WriteFile`, `J_Stamp`, and indirectly
`J_Diff`/`J_Merge`) copied `records_in_part` from the input JSON header
as-is instead of deriving it from the actual payload. Any caller hand-
assembling a multi-part packet (anything not going through the sanctioned
`J_ExportAll`/`--export` partitioner, which already computed this correctly)
could write an internally inconsistent file with no error at write time —
the mismatch only surfaces later, at read time, in a parser validation error
possibly seen by a completely different consumer
(`RecordsInPart mismatch: header declares N rows, <Data> contains M`).
`jPacketToDataPacket` now always sets `RecordsInPart = len(Data)`, ignoring
whatever the caller passed. Verified: a deliberately wrong
`records_in_part: 999` in the input JSON is now silently corrected to the
real row count in the written file.

`D_WriteFile` was checked and found already correct — `DataPacket.SetRows()`
sets `RecordsInPart` as a side effect, so the Direct API never had this bug.

This surfaced via `TestJReadMultipart::test_two_part_assembly`: the test
sliced `sample_data_j` into two parts but only patched
`part_number`/`total_parts` in each part's header, leaving `records_in_part`
at the original 8-row value — a real instance of the exact caller mistake
described above. Fixed the test fixture too, by setting
`records_in_part = len(data)` on each part after slicing.

### Security — SQL injection in jackc/pgx/v5 (GO-2026-5004)

Upgraded `github.com/jackc/pgx/v5` `v5.7.4 → v5.9.2`: placeholder confusion
with dollar-quoted string literals, reachable through
`postgres.Adapter.ExecuteRawQuery → pgxpool.Pool.Query`. Caught by CI's
`govulncheck`; verified locally that the fixed version resolves it (0
reachable vulnerabilities, was 1).

### Changed — minimum Go version bumped to 1.25

`pgx v5.9.2` itself requires `go 1.25.0` — checked every pgx release between
the vulnerable `5.7.4` and the fixed `5.9.2`: the module's own Go-version
requirement jumps from `1.24.0` (v5.8.0) to `1.25.0` starting at `v5.9.0`,
and the CVE fix lands at `v5.9.2`. No version exists with both the fix and
Go 1.24 compatibility, so Go 1.24 support is dropped. `go.mod`/`go.work` →
`go 1.25.0`; CI matrix/pins (`ci.yml`, `release.yml`) updated to match.

### Merged

Merged `feature/kanzi-compression-packet-size` (libtdtp/Python-bindings work
above) into `feature/sprint4-map` — clean merge, no conflicts, verified with
`go build`/`go test` (including `pkg/core/mapping`, `cmd/tdtpcli`) and the
full Python test suite (233/234 passing; the one failure predates this merge
and is unrelated — `TestJReadMultipart::test_two_part_assembly`).

---

## [1.17.1] — 2026-06-15

### Fixed — loop guard conflict in parallel `--steps` execution

`pkg/core/mapping/loopguard.go`: the "already running" check now scopes to
`(SourceSystem, TargetSystem, MappingID)` instead of just `(SourceSystem,
TargetSystem)`. Previously, parallel steps targeting the same database (e.g.
guides + schedule + tours all writing to `postgres-branch`) would block each
other even though they are completely independent mappings. Each mapping now
only blocks itself from running twice concurrently.

Also added graceful recovery from a corrupted log file (partial write from a
prior crash): the log is silently reset instead of failing with
`"unexpected end of JSON input"`.

---

## [1.17.0] — 2026-06-15

### Added — `--steps <workflow.yaml>` (P10: multi-step orchestration)

New top-level command for running a sequence of tdtpcli sub-commands defined in a
YAML file. Replaces shell scripts for multi-stage ETL pipelines.

**Key features:**

- **DAG execution** — `depends_on` builds a dependency graph; steps without ordering
  constraints between them run in parallel within each wave (Kahn's topological sort).
- **Error policies per step** via `on_error`:
  - `stop` (default) — abort the workflow, exit 1.
  - `skip` — mark the step as skipped, continue; direct and transitive dependents are
    also skipped automatically.
  - `retry(N)` — retry up to N times with exponential back-off (2 s → 4 s → 8 s → 30 s).
    After exhausting retries the step is treated as `stop`.
- **Sub-process isolation** — each step runs `tdtpcli <command>` as a subprocess with
  its own environment; stdout/stderr stream to the parent terminal in real time.
- **Cycle detection** — circular `depends_on` chains produce a clear error at startup.

**Workflow YAML format:**

```yaml
name: nightly-sync
description: Export ZTR-Live → validate → map to EDM
steps:
  - id: export
    command: "--pipeline pipelines/export_staff.yaml"

  - id: validate
    command: "--test out/staff.tdtp.xml"
    depends_on: [export]
    on_error: skip

  - id: map_staff
    command: "--map mappings/sync_staff.yaml --input out/staff.tdtp.xml"
    depends_on: [export]
    on_error: retry(3)

  - id: map_salary
    command: "--map mappings/sync_salary.yaml --input out/salary.tdtp.xml"
    depends_on: [export]
    on_error: retry(3)

  - id: notify
    command: "--pipeline pipelines/notify_done.yaml"
    depends_on: [map_staff, map_salary]
```

**New files:** `pkg/workflow/config.go`, `pkg/workflow/runner.go`,
`cmd/tdtpcli/commands/steps.go`.

---

## [1.16.1] — 2026-06-15

### Fixed — RabbitMQ daemon resilience (`pkg/brokers/rabbitmq`)

Three bugs identified by comparing against the production QueueBridge (3 months of
reconnect-loop debugging). All affect `--map --listen` daemon mode.

- **Infinite error loop after connection drop** (`pkg/brokers/rabbitmq.go`):
  `deliveryChan` was never reset to `nil` on `Close()` or `Connect()`.
  `startConsuming()` checked `if r.deliveryChan != nil { return nil }` — so after a
  drop it kept reading from the closed AMQP delivery channel, returning the same error
  every 2 s forever. Fixed: `Connect()` and `Close()` now both set `r.deliveryChan = nil`
  before touching the connection, ensuring `startConsuming()` re-registers the consumer
  on the next `Receive()` call.

- **No exponential backoff on reconnect** (`cmd/tdtpcli/commands/map.go`):
  The receive-error retry was a flat `sleep 2s; continue` that never actually
  reconnected the broker — just retried `Receive()` on the dead connection. Replaced
  with `reconnectBroker()`: calls `br.Close()` + `br.Connect()` with 2s→4s→8s→…→30s
  exponential back-off. One log line on disconnect, one on restore. No per-second spam.

- **QoS prefetch not set** (`pkg/brokers/rabbitmq.go`):
  Without `Qos(1, 0, false)`, RabbitMQ pushes all queued messages to the consumer at
  once (no backpressure). For 1.9 MB TDTP packets and slow upserts, this could exhaust
  memory. Fixed: `startConsuming()` now calls `ch.Qos(1, 0, false)` before
  `ch.Consume()` — broker delivers one message at a time, waits for ACK.

- **Slow dead-connection detection** (`pkg/brokers/rabbitmq.go`):
  `amqp.Dial()` inherited broker heartbeat (~60s default). After a network partition,
  `Receive()` blocked silently for up to 60s. Replaced with `amqp.DialConfig` +
  `Heartbeat: 10*time.Second`, matching production bridge behaviour.

- **`pkg/core/version/version.go`**: `1.16.0` → `1.16.1`.

---

## [1.16.0] — 2026-06-15

### Added — `--map --listen` daemon mode (Sprint 8)

Turns any mapping YAML into a standalone daemon process: `tdtpcli --map <yaml>
--input broker://queue --listen` keeps one persistent broker connection open and
processes messages in a continuous loop — no Python coordinator required.

- **`cmd/tdtpcli/commands/map.go`** — `Listen bool` field added to `MapOptions`;
  `RunMap()` branches to `runMapListen()` when `--listen` is set and `--input` is a
  `broker://` URI. Loop Guard is intentionally skipped in daemon mode — the broker
  queue regulates the rate.

- **`runMapListen()`** (new, ~80 lines) — daemon loop:
  - Connects once; connection stays open for the daemon lifetime.
  - `for { Receive → decrypt → parse → decompress → expand → mapping.Execute → ACK }`
  - On parse/decompress/execute error: **NACK with requeue** (`nackIfAble()`) so the
    message returns to the queue for retry; no silent drops.
  - On receive error (transient): 2 s back-off, then retry.
  - Signal handling: `SIGTERM`/`SIGINT` → `cancel()` → current message finishes, then
    exit. Pattern mirrors `cmd/tdtpcli/commands/listen.go:106–117`.
  - Per-message progress: `[map:listen] ✓  rows=42     total=420    18ms`.
  - Final summary on exit: `[map:listen] stopped. total rows upserted: N`.

- **`nackIfAble(br)`** (new helper) — type-asserts the broker to `NackLast(requeue bool)`;
  no-op for brokers that don't implement NACK (e.g. Kafka — uses offset commit instead).

- **`cmd/tdtpcli/main.go`** — `Listen: *flags.Listen` wired into `MapOptions`.

- **`cmd/tdtpcli/flags.go`** — `--listen` help text updated to document both the new
  `--map --input broker://` daemon mode and the legacy Kafka streaming consumer.

- **`pkg/core/version/version.go`**: `1.15.0` → `1.16.0`.

---

## [1.15.0] — 2026-06-15

### Added — `--map --input broker://` (Sprint 6)

Extended `tdtpcli --map` with broker-sourced packets: `--input broker://<queue>` reads a
TDTP packet directly from a message broker queue, ACKs on success, remaps fields per the
mapping YAML, and upserts into the target database — no staging table, no merge procedure.

- **`pkg/brokers/broker.go`** — added `yaml:` struct tags to all fields of `Config`
  (`type`, `host`, `port`, `user`, `password`, `queue`, `vhost`, `durable`, `exchange`,
  `routing_key`, `passive_declare`, `consumer_group`, `topic`, etc.) so broker connection
  parameters can be declared inline in a mapping YAML under `input_source.broker`.

- **`pkg/core/mapping/types.go`** — extended `InputSource` with `Broker *brokers.Config`
  (tagged `yaml:"broker,omitempty"`). `type: broker` in `input_source:` is now a
  first-class input kind alongside `type: s3`.

- **`cmd/tdtpcli/commands/map.go`** — broker branch in `loadPacket()`:
  - `isBrokerURI(path string) bool` — detects `broker://` prefix.
  - `acker` interface (local) — type-asserts the broker driver to call `AckLast()`.
  - On receive: connects to broker, calls `Receive(ctx)` with 30 s timeout, then `AckLast()`.
    Queue name in the URI (`broker://q`) overrides the queue declared in the YAML.
  - On error: skips ACK so the message returns to the queue automatically.
  - `RunMap()` now extracts both `s3cfg` and `brokercfg` from `cfg.InputSource` and passes
    both to `loadPacket()`.

- **`mappings/sync_branch_customers.yaml`** — new mapping with `input_source.broker`
  (RabbitMQ, queue `tdtp.sync.branch.customers`) → upsert into
  `public.branch_customers_inbox` (Central PostgreSQL, upsert key `customer_uuid`).

- **`examples/travel-agency/consumer.py`** — `tdtp.sync.branch.customers` handler migrated
  from the three-step legacy flow (`--import-broker` → staging table → `CALL merge_*()`)
  to a single `--map broker://` call via new `run_map_broker()` helper.
  Remaining 7 entity handlers retain the legacy flow for comparison (Sprint 7 target).

- **Loop Guard** (`min_interval: 5s`): rapid successive `--map broker://` calls within the
  guard interval return immediately without consuming from the queue.

- **`pkg/core/version/version.go`**: `1.14.0` → `1.15.0`.

### Changed — consumer.py full migration (Sprint 7, no binary change)

All 7 remaining entity handlers in `examples/travel-agency/consumer.py` migrated from
the legacy three-step flow (`--import-broker` → staging table → `CALL merge_*()`) to the
single `--map broker://` call pattern introduced in Sprint 6. No Go code changed.

- **`QUEUE_HANDLERS`** dict — all 8 entries now use `map_yaml` key pointing to a mapping
  YAML under `mappings/`; legacy `dsn_key` / `merge_proc` keys removed.
- **`handle_notify()`** — simplified: single `run_map_broker()` call for every entity,
  no if/else branch for legacy vs. new handlers.
- **`run_import_broker()` / `run_merge()`** helper functions — removed (~40 lines).
- **`psycopg2` / `re` imports** — `psycopg2` removed (no direct DB calls); `re` retained
  for `run_map_broker()` row-count extraction.
- **New mapping YAMLs** — `sync_flights.yaml`, `sync_reservations.yaml`,
  `sync_countries.yaml`, `sync_guides.yaml`, `sync_tours.yaml`, `sync_schedule.yaml`,
  `sync_branch_sales.yaml` added under `mappings/`.

---

## [1.14.0] — 2026-06-13

### Added — `--map --input s3://` (Sprint 5)

Extended `tdtpcli --map` to source the input packet from S3-compatible object storage:
`--input s3://bucket/key` downloads the object, then applies the shared decrypt →
decompress → compact-expand pipeline before field mapping and upsert.

- **`pkg/core/mapping/types.go`** — new `InputSource` struct with `S3 *storage.S3Config`
  (tagged `yaml:"s3,omitempty"`); parsed from the `input_source:` key in the mapping YAML.
  `type: s3` selects the S3 download path.

- **`cmd/tdtpcli/commands/map.go`** — S3 branch in `loadPacket()`:
  `storage.IsRemote(path)` detects `s3://` URIs; `storage.Download()` fetches the object;
  the resulting bytes feed through the existing decrypt/decompress/expand layers.
  `RunMap()` extracts `s3cfg` from `cfg.InputSource.S3` and passes it to `loadPacket()`.

- **`mappings/sync_branch_customers.yaml`** — initial version of the example mapping
  (used as the Sprint 5 S3 round-trip target in the travel-agency demo).

- Backward-compatible: `--input <local-file>` path unchanged.

- **`pkg/core/version/version.go`**: `1.13.0` → `1.14.0`.

---

## [Unreleased] — feature/sprint4-map

### Added — `--map` cross-system field mapping (Sprint 4, P8)

New `tdtpcli --map <mapping.yaml> --input <file.tdtp.xml>` command: reads a TDTP
packet, remaps fields and enum values per a mapping config, and upserts the rows
into a target database via `pkg/adapters`. Enables on-demand record sync between
systems (e.g. ZTR-Live → EDM) without a hand-written importer.

- **`pkg/core/mapping`** — new package:
  - `types.go` / `parser.go` — `MappingConfig` (id, loop_guard, target_connection,
    targets[].fields with `from`/`to`/`enum`) parsed and validated from YAML.
  - `executor.go` — `Execute()` builds a per-target packet (field remap + enum
    remap), resolves schema-qualified table names (`edm.edm_employees` →
    schema `edm` + table), and upserts via `adapter.ImportPacket(StrategyReplace)`.
  - `loopguard.go` — Loop Guard Layers 2+4: `correlation_id` + `min_interval`
    cooldown recorded in `~/.tdtp/mapping_log.json`; blocks rapid re-runs of the
    same `source_system → target_system` pair to prevent recursive sync loops.
- **`cmd/tdtpcli`** — `--map`, `--input`, `--dry-run` flags wired into routing;
  `--map` is a no-DB-config command (target DSN comes from the mapping file).
- **Compressed input** — `--map` auto-decompresses zstd/kanzi packets via the
  shared `decompressPacketData` helper before reading rows, so a compressed
  export (`output.tdtp.compression: true`) maps transparently. Round-trip test
  in `map_test.go`. (zstd lvl 3 on the 1478-employee export: 218 KB → 52 KB.)
- **Encrypted input** — `--map --mercury-url <url>` decrypts xZMercury
  AES-256-GCM packets (burn-on-read key retrieval) before parsing. `loadPacket`
  now reads the file as bytes and resolves the encryption → compression →
  compact layers in order. Encryption is detected by content (`IsEncryptedBlob`,
  the binary header) as well as the `.enc` extension, since a pipeline may write
  the encrypted blob to a `.tdtp.xml` destination. New `commands.IsEncryptedBlob`
  helper; false-positive guard test in `map_test.go`. Burn-on-read means an
  encrypted packet decrypts exactly once — a replay is blocked with `KEY_BURNED`.
- Test assets: `docker/sprint4/` (PostgreSQL + Redis + `edm.edm_employees` DDL),
  `pipelines/export-single-employee.yaml`, `mappings/edm_mapping.yaml`,
  `scripts/emulate_button.py` (UI-button emulator).
- **Field types preserved** — `buildTargetPacket` carries over each source field's
  `Type`/`Subtype`/`Length`/`SpecialValues` instead of stringifying everything, so
  the target adapter applies the full conversion contract (dates, NoDate, NULL
  markers). Enum-remapped fields drop to `TEXT` since their value becomes free text.

### Fixed — NoDate marker not decoded to NULL on PostgreSQL import

The PostgreSQL adapter's `convertValue` honored the `SpecialValues.Null` marker
but not `SpecialValues.NoDate` ("0000-00-00", the canonical Navision/MSSQL
"no date" produced by `no_date_sentinels`). The raw marker reached a DATE/TIMESTAMP
column and failed with `date/time field value out of range` (SQLSTATE 22008).
`convertValue` now decodes NoDate → SQL NULL, matching `base/import_helper.go`.
Affected every PostgreSQL import of Navision date sentinels (`--import` and
`--map`), not just the new command.

### Fixed — Cyrillic double-encoding on PostgreSQL import

`parseRow` in `pkg/adapters/postgres/export.go` accumulated row values via
`current += string(ch)` where `ch` is a byte. For any byte ≥ 0x80 this re-encodes
the byte as the UTF-8 of rune U+00XX, double-encoding every multi-byte UTF-8
character (Cyrillic "С" d0a1 → c390c2a1). Now accumulates into a `[]byte` and
converts once, preserving UTF-8 verbatim. Affected all non-ASCII PostgreSQL
imports (`--import`, `--map`). Regression test added in `parserow_test.go`.

## [1.13.0] — 2026-06-12

### Refactoring — cmd/tdtp-xray: align with framework core

`cmd/tdtp-xray` now delegates all DB and ETL operations to `pkg/adapters` and
`pkg/etl` instead of duplicating ~600 lines of custom SQL and in-memory SQLite
logic. Changes are transparent to the Wails frontend — public method signatures
unchanged.

- **Phase 1 — ConnectionService** (`services/connection_service.go`):
  replace `sql.Open` + custom `INFORMATION_SCHEMA` queries with
  `adapters.New()` + `adapter.GetTableNames()` / `adapter.GetViewNames()`.
  Removed: `mapDriverName`, `getTablesQuery`, `getViewsQuery` (~170 lines).

- **Phase 2 — MetadataService** (`services/metadata_service.go`):
  replace per-dialect `INFORMATION_SCHEMA` column/PK queries with
  `adapter.GetTableSchema()` → `packet.Schema` conversion.
  Retained: `InferTDTPSchema()` (uses `pkg/core/packet` directly).
  Removed: `getColumns`, `getColumnsQuery`, `getPrimaryKeys`, `getPrimaryKeysQuery` (~287 lines).

- **Phase 3 — in-memory preview** (`app.go`):
  replace manual `sql.Open("sqlite", ":memory:")` + hand-rolled CREATE/INSERT
  with `etl.NewWorkspace()` + `ws.CreateTable()` + `ws.LoadData()` + `ws.ExecuteSQL()`.
  DB sources loaded via `adapter.ExportTableWithQuery(&packet.Query{Limit: 1000})`;
  TDTP sources via `packet.NewParser().ParseFile()`.
  Removed: `loadSourceToMemory`, `loadDBSourceToMemory`, `loadTDTPSourceToMemory`,
  `mapTDTPToSQLiteType`, `createAndFillTable`, `convertValue` (~150 lines).

- **Phase 4 — PreviewService** (`services/preview_service.go`):
  replace `EstimateRowCount` `sql.Open` + `COUNT(*)` with
  `adapter.InspectTable().Stats.TotalRows`.
  SQL drivers registered transitively via adapter `init()` — no direct imports needed.

- **Phase 5 — go.mod cleanup** (`cmd/tdtp-xray/go.mod`):
  removed direct driver dependencies
  (`go-mssqldb`, `go-sql-driver/mysql`, `lib/pq`, `modernc.org/sqlite`) —
  now resolved transitively through `pkg/adapters/*`.
  Direct requires reduced to: `tdtp-framework`, `wails/v2`, `yaml.v3`.

### Added — cmd/tdtp-xray: Deploy to Orchestrator

- **`DeployToOrchestrator() ConfigFileResult`** (`app.go`):
  writes the generated pipeline YAML directly to the orchestrator `--scenarios`
  directory; orchestrator picks it up automatically without restart.
  Filename derived from pipeline name (`My Pipeline` → `My-Pipeline.yaml`);
  directory created if absent.

- **`Settings.OrchestratorScenariosPath`** (`app.go`):
  new settings field exposed to the Wails frontend for configuring the
  orchestrator scenarios directory path.

## [1.12.0] — 2026-06-03

Operational maturity: air-gap enrollment, seat policy, structured audit log,
per-job artifacts, orchestrator LDAP auth, Prometheus metrics, and Docker deployment stack.

### Added — Sprint 2: CA operational maturity

- **Air-gap offline cert** (`xzmercury/cmd/tdtp-certify issue-offline-cert`):
  issue `EnvCert` without a live challenge-response for isolated networks.
  Cert is signed by the CA key offline; `Offline: true` in payload.
  Mercury accepts it at `POST /api/env/authorize/offline` — no nonce, no CA network.
  Online endpoint (`/authorize`) explicitly rejects offline certs (400).
- **Seat policy** (`xzmercury/internal/ca/db.go`):
  one `env_id_pub` = one active license. Re-enroll with a different license → HTTP 409.
  `GetActiveCertByEnvID` + cross-license guard in enroll handler.
  Re-enroll under the same license is idempotent (200 + new cert).
- **Mock clock + AutoRenew test** (`xzmercury/internal/caClient`):
  `clock interface { Now() time.Time }`, `MockClock.Advance(d)`, `SetClock(clk)`.
  AutoRenew polling every 100ms against `c.clk.Now()` — testable without real time.
  `TestAutoRenew_MockClock`: cert with TTL, clock advance, callback verification.
- **`issue-unsafe-cert`** (`xzmercury/cmd/tdtp-certify`):
  issue `CapabilityCert` for `--unsafe` pipeline operations.
  Flags: `--to`, `--op`, `--tables`, `--db`, `--host`, `--ttl`, `--key`, `--out`.
  Roundtrip test: sign → `LoadCert` → `VerifyWith`.

### Added — Sprint 3: observability and SIEM

- **Structured audit log** (`pkg/license/audit.go`):
  `AuditEntry{Timestamp, Nonce, Operation, IssuedTo, Host, TdtpcliVersion}`.
  `NewAuditLog(path, format)` — `format: "text" | "json"` (one JSON object per line).
  `TDTP_AUDIT_FORMAT` env var (default `"text"`).
  `DefaultAuditLog()` reads `TDTP_AUDIT_LOG` and `TDTP_AUDIT_FORMAT`.
  `HasNonce` parses both formats + text fallback on JSON parse error.
- **Syslog audit hook** (`pkg/license/audit_syslog.go`, build tag `syslog`):
  `SyslogAuditLog` writes JSON entries to syslog. `HasNonce` → always false
  (replay protection requires a file-backed log alongside).
- **Orchestrator per-job artifact** (`cmd/orchestrator`):
  after successful job: `extractOutputPath` → SHA-256 + size → `db.UpdateJobArtifact`.
  `GET /jobs/{id}/artifact` — download with `Content-Disposition: attachment`.
  DB: three new columns (`artifact_path`, `artifact_sha256`, `artifact_size`) +
  idempotent migration; timezone migration fixed for new DBs.
- **LDAP auth in orchestrator** (`cmd/orchestrator/auth.go`):
  `LDAPAuthenticator`: HTTP Basic Auth → LDAP bind → `memberOf` → `RoleMap` → `Principal`.
  `roleForGroups` selects the highest-ranked role.
  Flags: `--auth-type token|ldap`, `--ldap-url`, `--ldap-bind-dn`, `--ldap-bind-pass`, `--ldap-base-dn`.
  In LDAP mode `POST /tokens` returns 501.

### Added — Monitoring

- **Prometheus metrics** (`cmd/orchestrator/metrics.go`, `GET /metrics`):
  `orchestrator_jobs_total{scenario, status}` — completed job counter.
  `orchestrator_job_duration_seconds{scenario, status}` — histogram (buckets 1–600s).
  `orchestrator_jobs_active` — active queue gauge; seeded from DB on startup.
  `orchestrator_schedule_last_status{id, scenario}` — `1`=ok `0`=failed `-1`=never.
  `orchestrator_http_requests_total / _duration_seconds` — per-route (chi pattern labels).
  `prometheusMiddleware` embedded in router.
- **Extended `/healthz`**: `{status, active_jobs, license_tier, mercury}`.
  Ready for K8s readiness probe.

### Added — Docker deployment stack

- **`deployments/docker/Dockerfile.worker`**: orchestrator + tdtpcli, `CGO_ENABLED=0`,
  `gcr.io/distroless/static-debian12:nonroot`, ~25 MB. Build tag `production`.
- **`deployments/docker/Dockerfile.mercury`**: xzmercury from separate `go.mod`.
- **`deployments/docker/Dockerfile.ca`**: tdtp-ca + tdtp-certify. CA key — mount only,
  never baked into the image.
- **`docker-compose.dev.yml`**: 4 services with one command — mercury (--dev),
  worker (--no-auth), Prometheus (:9090), Grafana (:3001, admin/tdtp-dev).
  Grafana dashboard **TDTP Orchestrator** — auto-provisioned via provisioning/.
- **`docker-compose.prod.yml`**: CA + Redis×2 (mercury RAM-only + pipeline with persistence) +
  xzmercury (CA enrollment on startup) + worker (--require-prod). `.env.example`.
- **`.dockerignore`**: excludes tests, binaries, keys, logs from build context.

### Added — Documentation

- **`docs/DEPLOYMENT.md`**: service map with ports and dependencies, step-by-step
  local dev → production, air-gap offline cert, startup order, audit log formats,
  smoke test sequence.
- **`cmd/orchestrator/README.md`**: updated API table (artifact), flags table
  (LDAP), sections "Job artifacts" and "LDAP auth".

### Fixed

- **Timezone migration**: `ALTER TABLE schedules ADD COLUMN timezone` added to
  idempotent migration block — fixes `TestScheduleRecordTimezone` on new DBs.
- **Offline cert rejected at online endpoint**: explicit guard at the top of `Step1`
  (`if req.Cert.Offline → 400`) — fixes test `TestOfflineCertOnlineEndpointRejected`.
- **`hashstore.SetArgs` NX**: replaced deprecated `SetNX` with `SetArgs{Mode:"NX"}`;
  detection via `errors.Is(err, redis.Nil)` instead of checking result string.
- **`tdtp-certify loadPrivKey`**: support for both PEM types —
  `"PRIVATE KEY"` (PKCS8) and `"ED25519 PRIVATE KEY"` (raw); `fmt.Sscanf` for errcheck.

### Tests

~25 new tests:
`ca/offline_cert_test.go` (4) · `ca/integration_test.go` +2 (seat policy) ·
`infra/ca_bootstrap_test.go` +1 (MockClock AutoRenew) ·
`tdtp-certify/unsafe_cert_test.go` (4) ·
`pkg/license/audit_test.go` +3 (JSON format, roundtrip, env var) ·
`orchestrator/executor_test.go` +1 (artifact) ·
`orchestrator/auth_test.go` +5 (LDAP middleware, roleMap, basicAuth).

## [1.11.0] — 2026-06-02

Full trust chain closed: hardware anchor (CA/TPM) → online environment authorization
(xZMercury) → offline CLI license (tdtp.lic) → scenario orchestration with dual gate.
All enforced in the binary.

### Added — CA wiring (Priority 1)

- **xZMercury → CA on startup** (`xzmercury/internal/infra/ca_bootstrap.go`):
  prod Mercury authorizes with CA before issuing keys. `BootstrapCA`:
  envkey.Load → enroll/authorize → AutoRenew (12h before expiry) on server-ctx.
- **`infra.CASession`** + **`api.caGuardMiddleware`**: invalid CA session → 503
  on `/api/keys/*`. `CAGuard` interface decouples api from infra.
- **`CAConfig`** in config (`url`, `license_key`, `env_key_dir`, `cert_path`),
  `TDTPCA_LICENSE_KEY` env fallback; dev skips CA entirely.
- `ca.NewRouter` extracted — shared between binary and tests.

### Added — CA admin tool (Priority 2)

- **`tdtp-certify`** (`xzmercury/cmd/tdtp-certify/`): vendor-only CA management CLI.
  `keygen` · `issue-license` · `revoke-cert` · `revoke-license` ·
  `list-licenses` · `list-active` · `list-certs`.
  `issue-license` generates `TDTP-XXXXX-…`, shown once; CA stores only SHA-256.
  `list-active` counts environments by `last_seen` within a window — real activity.
- DB methods: `ListLicenses`, `ListActiveCerts(since)`, `ListAllCerts`.

### Added — Offline license (Priority 3)

- **`pkg/license/`**: Ed25519-signed tdtp.lic, fully offline (embedded vendor key).
  `License`/`Limits`/`Tier`, `Load`/`Verify`/`VerifyWith`/`Sign`/`New`,
  `Community()` floor (sqlite only, 50k rows). Accessors: `AllowsAdapter`/
  `AllowsFeature`/`RowLimit`/`PipelineLimit`.
- **`cmd/tdtpcli`**: `--license` flag; resolution: flag → `TDTP_LICENSE` →
  `./tdtp.lic` → community. Tampered/expired = fatal. Adapter gate, `--enc`, `--unsafe`.
- **`cmd/tdtp-license`**: vendor signer (`keygen`/`issue`/`verify`), separate from CA key.

### Added — Orchestrator trust integration (Priority 4)

- **Mercury `/status`** → `{mode, dev, ca_authorized, permissions}`.
  `CAGuard.Permissions()` added; `infra.CASession` implements it.
- **`cmd/orchestrator/preflight.go` — `TrustGate`**:
  OFFLINE verification of own tdtp.lic + ONLINE preflight Mercury.
  `--require-prod` refuses against dev/non-CA-authorized Mercury.
  `GateScenario`: scenario.permissions ⊆ (license ∩ Mercury). `CheckPipelineLimit`.
- Gate in run-handler (403 permission, 429 limit) and in cron scheduler tasks.

### Architecture

Two independent trust branches, both enforced in the binary:
- **ONLINE** (CA → Mercury): runtime environment authorization. TPM challenge,
  24h rolling cert, seat-count, /hello DDoS gate, mode-in-HMAC.
- **OFFLINE** (tdtp.lic): CLI capabilities without network. Air-gapped tdtpcli respects the license.

### Binaries

`tdtpcli` (27M, license-gate) · `xzmercury` (23M, CA-guard + /status) ·
`tdtp-ca` (11M) · `tdtp-certify` (8.9M) · `tdtp-license` (3.4M) · `orchestrator` (14M).

### Tests

~34 new: `ca/integration_test.go` (5), `infra/ca_bootstrap_test.go` (3),
`ca/db_listing_test.go` (3), `cmd/tdtp-certify/main_test.go` (4),
`pkg/license/license_test.go` (9), `orchestrator/preflight_test.go` (8),
`api/status_test.go` (3). All CA tests run the real 2s /hello gate.

## [1.10.0] — 2026-06-01

Three independent tracks: `--limit -N` fix on MSSQL, full xZMercury security stack
(burn marker + mode-in-HMAC + error receipt), and new scenario execution infrastructure
(CA server `tdtp-ca` + `orchestrator`).

### Fixed

- **Bug #3 — `--limit -N` on SQL Server** (`pkg/adapters/base/sql_adapter.go`):

  Three fix iterations:
  1. `LIMIT N` → `SELECT TOP N` (SQL syntax no longer fails).
  2. `SELECT TOP N` → subquery `SELECT * FROM (SELECT TOP N … ORDER BY col DESC) AS _tail ORDER BY col ASC`
     (semantics of "last N rows" instead of first).
  3. `--fields` + `--limit -N` → `Invalid column name` on a column outside the projection fixed:
     - `firstProjectedColumn(sql)` — ORDER BY taken from the SELECT list, not from the schema.
     - `firstWritableColumn(schema)` — skips `ReadOnly` fields (`timestamp`/rowversion)
       for `SELECT *` so ORDER BY doesn't reference a trimmed column.
  - Same fix for `OFFSET/FETCH` fallback ORDER BY.
  - 5 new tests: `NoOrderBy`, `WithFields`, `WithOrderBy`, `SelectStar`, regression.

### Added — xZMercury security stack

- **Key consumption audit** (`xzmercury/internal/request/`):
  `Request.ConsumedBy` + `ConsumedAt` record who and when burned the key.
  `retrieveRequest.Caller` → Mercury audit trail. Three distinguishable states:
  named burn / anonymous burn / TTL expiry.

- **Mode in HMAC** (`xzmercury/internal/keystore/`):
  `HMAC = HMAC-SHA256(uuid + ":" + mode, secret)` — `mode` ("dev"/"prod") is part of the signature,
  not a self-reported label. Dev-binding is cryptographically ≠ prod-binding.

- **Mandatory `serverSecret`** (`pkg/processors/encryption.go`):
  Empty `MERCURY_SERVER_SECRET` → `ErrHMACVerificationFailed` (previously — silent bypass).
  Dev opt-out: explicit sentinel `"dev-mode"`.

- **Burn marker** (`xzmercury/internal/keystore/store.go`):
  Lua script: atomic `GETDEL` + `SET mercury:burned:{uuid} {mode, burned_at}` (TTL=24h).
  Three distinguishable states on retrieve:
  - `GETDEL → value` → legitimate burn by this call.
  - `nil + burned marker` → `ErrKeyBurnedByOther` (*KeyBurnedError with mode + BurnedAt).
  - `nil + no marker` → `ErrKeyExpired` (TTL or UUID never existed).
  HTTP: 410 Gone (burned) / 404 Not Found (expired).

- **`ServerMode` in error packet** (`pkg/core/packet/types.go`, `cmd/tdtpcli/commands/`):
  `AlarmDetails.ServerMode` = "dev" | "prod" from burn marker.
  `mode=dev` → dev-failover on Redis cluster failure (not an alarm).
  `mode=prod` → key burned by unknown party in prod (investigate).
  `*_error.tdtp.xml` is written alongside `.tdtp.enc` on any decrypt failure.

- **CA server `tdtp-ca`** (`xzmercury/cmd/tdtp-ca/`, `xzmercury/internal/ca/`):
  Separate binary (11 MB, SQLite backend).
  - `POST /api/env/enroll` + `/confirm` — two-step enrollment with challenge-response.
  - `POST /api/env/authorize` + `/confirm` — re-auth; implicit cert renewal (not_after += 24h).
  - `GET /hello` — DDoS gate: 2s delay, single-use token (TTL=30s).
    `X-Hello-Token` required for step-1 enroll/authorize. Max 3 parallel /hello per IP.
  - `DELETE /api/env/certs/{id}` / `/licenses/{hash}` — revocation.
  - DB schema: `licenses` (hash·permissions·seat_limit·status·paid_until) +
    `certs` (cert_id·license_hash·env_id_pub·not_after·status·last_seen·signature).
  - **Cert TTL = 24h** (not = paid_until). Authorize = implicit renewal → rolling 24h window.
    CA sees `last_seen` daily → accurate active environment count.
  - Seat-count enforcement: `COUNT(active certs) >= seat_limit` → 409.
  - Idempotent enrollment: same (license_hash, env_id_pub) → same cert.

- **envkey** (`xzmercury/internal/envkey/`):
  Ed25519 env-keypair (TPM-stub: 0600 file, same interface as real TPM).

- **caClient** (`xzmercury/internal/caClient/`):
  xzmercury → CA HTTP client. `Enroll()` + `Authorize()` with challenge-response.
  `AutoRenew()` goroutine: fires at `CertTTL - RenewalThreshold` (12h before expiry),
  retries hourly when CA is unavailable.

### Added — Orchestrator

- **`orchestrator`** (`cmd/orchestrator/`):
  Thin HTTP wrapper around `tdtpcli --pipeline`. Nothing in tdtpcli was changed.

  - **Scenario files** = existing pipeline YAML + optional `orchestrator:` block
    (tdtpcli ignores unknown yaml keys — backward compatible).
    `text/template` substitutes `{{.period}}` before execution.
  - **Schedule storage: SQLite** (not in YAML):
    YAML files = seed only (idempotent upsert on startup).
    DB = source of truth: `enabled`, `last_run_at`, `last_status`, `next_run_at`.
    Runtime management via API without restart.
  - **DB schema**: `schedules` + `jobs` (schedule_id=NULL for manual runs).
  - **Magic params**: `{{current_month}}`, `{{current_date}}`, `{{yesterday}}`.
  - **API**: `GET/POST /scenarios`, `POST /scenarios/{name}/run`,
    `GET /jobs`, `GET /jobs/{id}`,
    `GET/POST /schedules`, `PATCH /schedules/{id}/enable|disable`, `DELETE /schedules/{id}`.

### Security

- `TDTPCLI_CALLER` env var → `Caller` in Mercury audit trail.
- `ErrKeyBurnedByOther` / `ErrKeyExpired` instead of generic 404.
- `pkg/etl/loader.go` passes `source.Name` as caller during pipeline decrypt.

---

## [1.9.7] — 2026-05-30

Python library modernization: facade API, CLI-parity in-process, and Arrow bridge (read + write).

### Added

- **Arrow columnar bridge** (`exports_d_arrow.go`, `exports_j_columnar.go`, `arrow_ext.py`):
  read and write `pyarrow.Table ↔ TDTP` via typed C-buffers and
  vectorized column processing. Write path (`J_WriteColumnar`) transposes
  column-major to row-major inside Go — **×2.1** vs the old `itertuples` on 10k rows.
  API: `Tdtp.read_arrow` / `to_arrow` / `from_arrow` / `write_arrow`.

- **`Tdtp` facade** (`facade.py`): plain-verb API without `J_` prefixes and manual
  memory management (`read`/`write`/`filter`/`sort`/`merge`/`stamp`/`verify`/…).

- **CLI parity in-process**: `J_Inspect`, `J_Test`, `J_Sort`, `J_Merge`,
  `J_ReadMultipart`, `J_Stamp`, `J_Verify`; `J_ExportAll` extended with compact + compress
  + checksum in a single call.

- **Packaging & versioning**: single version source (`pkg/core/version`), `py.typed`
  (PEP 561), stable error codes in JSON-envelope, extras `tdtp[arrow]` / `tdtp[pandas]`.
  Lockstep `.so` ↔ package: `build-lib` runs `sync-version` (build-time), import
  checks `J_GetVersion` against package metadata and warns on mismatch (runtime).

- **C# wrapper (`libcs/`) — parity with new exports**: `TdtpWrapper.cs`
  adds P/Invoke declarations and public methods `Inspect`, `Test`, `Verify`,
  `Stamp`, `ReadMultipart`, `Sort`, `Merge`, `WriteColumnar`. Version is still
  read at runtime via `GetVersion()` — no hardcoded constants, always in lockstep
  with core. `BUILD.md` extended with C++ examples.

### Fixed

- `J_Test` was not exported — cgo excludes `*_test.go` (renamed to `exports_j_integrity.go`).
- `J_Inspect` lost the compact flag — `ParseBytes` instead of auto-expanding `ParseFile`.
- `J_Sort` ignored direction — `normalizeDirection` now returns `"ASC"`/`"DESC"`.

### Tests

- `test_arrow.py` (24, read+write roundtrips), `test_facade.py` (13), `test_examples.py`
  (smoke of three agent recipes), extended `test_api_j.py`, write-benchmarks in `test_bench.py`.

## [1.9.6] — 2026-05-30

### Added

- **`--to-csv`** (`cmd/tdtpcli/commands/csv.go`): TDTP → CSV converter with security gate.

  TDTP remains the transport with full guarantees; CSV is a last-mile adapter for
  legacy systems (1C, SAP, bulk DB load). Delimiter, encoding, and integrity check
  are configured independently.

  ```bash
  tdtpcli --to-csv report.tdtp.xml -d=';' --cp=1251          # legacy Windows
  tdtpcli --to-csv report.tdtp.xml --bom                      # Excel UTF-8
  tdtpcli --to-csv report.tdtp.xml -d=';' -w 'Balance > 0' -l=100
  ```

  - **Security gate**: `v1.0` — pass-through; `v1.4` — `VerifyAndPrepare` before write.
  - **Delimiter** `-d=';'`: works in PowerShell and bash; RFC 4180 auto-quoting.
  - **Encodings** `--cp`: `utf8`, `1251`, `866`.
  - **`--bom`**: UTF-8 BOM for Excel.
  - TDTQL filters (`--where`, `--order-by`, `--limit`, `--fields`) work as for all commands.
  - **43 integration tests** (`tests/cli/test_csv.py`).

- **TDTQL alias `-l`** (`cmd/tdtpcli/flags.go`): `-l=10` as shorthand for `--limit=10`
  (reads as "lines", analogous to `tail -n`). Alias `-w` for `--where` retained.

- **`--enc` tier — standalone AES-256-GCM encryption** (`cmd/tdtpcli/commands/encrypt.go`):

  Encryption via xZMercury is now available for all standalone commands — not only
  for `--pipeline`. Each part gets its own UUID; the key is bound in xZMercury
  and deleted after the first read by the consumer (burn-on-read).

  ```bash
  # Producer
  tdtpcli --export payroll --enc --mercury-url http://mercury:3000 --output payroll.tdtp.xml
  # → payroll.tdtp.enc  (binary AES-256-GCM blob)

  # Consumer — import to DB
  tdtpcli --import payroll.tdtp.enc --mercury-url http://mercury:3000

  # Consumer — convert
  tdtpcli --to-csv  payroll.tdtp.enc --mercury-url http://mercury:3000
  tdtpcli --to-xlsx payroll.tdtp.enc --mercury-url http://mercury:3000
  tdtpcli --to-html payroll.tdtp.enc --mercury-url http://mercury:3000
  ```

  - **Auto-detect**: `.tdtp.enc` / `.enc` extension auto-detected in all consumer
    commands (`--import`, `--to-csv`, `--to-xlsx`, `--to-html`). No extra flag required.
  - **`encOutputKey`**: output named automatically — `.tdtp.xml` / `.xml` / `.tdtp`
    → `.tdtp.enc`; already correct extensions are not changed.
  - **Burn-on-read**: `POST /api/keys/retrieve` removes the key from Mercury; a second
    `--import` of the same file fails with "key not found".
  - **`MERCURY_SERVER_SECRET`**: if the env var is set — HMAC verification of the
    Mercury response is performed. Empty value → skipped (dev / internal-only).
  - **stdout guard**: `--enc` with stdout (`--output -`) returns an error — binary blob
    cannot be piped to a text stream.
  - **S3 support**: encrypted blob uploaded to S3 with metadata
    `package_uuid`, `protocol: TDTP-ENC 1.0`.
  - Tests: 10 unit tests (`commands/enc_tier_test.go`) — extension detection,
    round-trip, burn-on-read, all three converters with `.tdtp.enc` input.

- **v1.4 security gate — all import paths** (`cmd/tdtpcli/commands/`):

  Single helper `applyV14SecurityGate` (`commands/security.go`) now applied to
  all commands that write data to the DB:

  | Command | File | Behavior on failure |
  |---------|------|---------------------|
  | `--import` | `import.go` | error, file not imported |
  | `--listen` (Kafka) | `listen.go` | `NackLast(false)` — packet not returned to queue |
  | `--import-broker` | `broker.go` | error before first write |
  | `--import-broker --keep` | `broker.go` | error inside `importOne` |

  Previously the gate existed only in `--to-csv`; `--to-xlsx` and `--to-html` were
  unprotected. Now all converters and all import paths are guarded by the same code.

  - `MercuryURL string` added to `ImportOptions`, `ListenConfig`,
    `ImportBrokerOptions`; wired through `main.go`.
  - Policy: `FallbackDegrade` — Mercury unavailable → local xxh3; hash
    not registered or tampered → `BLOCK`.
  - Tests: 20 unit tests (`commands/v14_security_test.go`) — CSV, XLSX, HTML
    × v1.0 pass-through / v1.4 valid / tampered / Mercury OK / not-registered / tampered.

- **xZMercury dev configs** (`xzmercury/configs/`):

  ```
  xzmercury.dev.yaml      — dev server: port 3000, key_ttl 15m, rate_limit 0
  ldap-users.dev.json     — mock users with full DN groups
  pipeline-acl.dev.yaml   — ACL for test pipelines
  ```

  LDAP mock requires exact string matching — groups specified as full DNs
  (`cn=tdtp-pipeline-users,ou=groups,dc=corp,dc=local`).

### Fixed

- **`--from-xlsx` empty MessageID** (`pkg/xlsx/converter.go`): XLSX converter created
  the packet manually (`DataPacket{}`), bypassing UUID generation. Fixed by switching to
  `packet.NewDataPacket()`.

- **`--limit` on MSSQL compat level 80/90/100** (`pkg/adapters/base/sql_adapter.go`):
  `OFFSET/FETCH NEXT` requires SQL Server 2012+. For `--limit` without `--offset` a
  `SELECT TOP N` is now generated (works with SQL Server 2000+). `OFFSET/FETCH` retained
  only for pagination with `--offset`.

- **`exportToTDTP` and `exportToKafkaSpool` lost rows** (`pkg/etl/exporter.go`):
  calling `packet.ParseRows(dataPacket.Data.Rows, ...)` returned 0 rows when data was
  stored in `rawRows` (fast-path). Fixed by using `dataPacket.GetRows()`.

- **Windows backslash in YAML** (`tests/integration/xzmercury_pipeline_test.go`):
  paths like `C:\Users\...` were parsed as Unicode escapes. Fixed via `filepath.ToSlash()`.

- **`TestEndToEndExportImport`** (`tests/integration/broker_test.go`):
  `createTestTable` was a stub — implemented via `database/sql`.

### Performance

- **In-memory filter (`pkg/core/tdtql/`)** — two hot-path fixes:

  | What | Before | After |
  |------|--------|-------|
  | LIKE regexp (10k rows) | 51 ms · 440k allocs | **5.4 ms · 20k allocs** (9.5×) |
  | Field lookup in schema | O(fields) per row | O(1) map, once per call |

  `comparator.go`: regexp cache via `sync.Map`. `filter.go`: `map[string]int` and
  `map[string]FieldDef` built once in `ApplyFilters`, `FieldDef` not allocated per row.

- **Compact encode/decode (`pkg/core/packet/compact.go`)**:

  | Benchmark (10k rows) | Before | After |
  |----------------------|--------|-------|
  | Encode `RowsToCompactData` | 2 594 ms · 2 087 KB | **1 274 ms · 807 KB** (2×) |
  | Decode `ExpandCompactRows` | 7 163 ms · 5 755 KB | **5 755 ms · 4 476 KB** (1.24×) |

  Per-row `[]string` replaced with `[]byte buf` with `buf[:0]` — buffer retains capacity
  between rows. `strings.Builder.Reset()` not used (it resets `buf = nil`).

### Refactoring

- **`packetOverheadSize = 5000`** (`pkg/core/packet/generator.go`, `streaming.go`):
  magic constant extracted from three locations into `packetOverheadSize`.

### Tests

- `tests/cli/test_csv.py` — 43 integration tests for `--to-csv`.
- `pkg/core/tdtql/filter_bench_test.go` — LIKE and field lookup benchmarks old/new.
- `pkg/core/packet/compact_bench_test.go` — compact encode/decode benchmarks old/new.

## [1.9.5] — 2026-05-25

### Added

- **`--to-csv`** (`cmd/tdtpcli/commands/csv.go`): TDTP → CSV converter with security gate.

- **TDTQL shorthands `-n` and `-w`** (`cmd/tdtpcli/flags.go`):

  ```bash
  -n=10         # alias for --limit=10
  -w 'X > 1'   # alias for --where 'X > 1' (repeatable, AND-chain)
  ```

- **TDTP v1.4 integrity + xzMercury hash notary** (`cmd/tdtpcli/`, `pkg/pipeline/`,
  `xzmercury/`): `--integrity` flag stamps the packet with three xxh3_128 hashes
  (Schema / Data / Packet, UUID salt). `--mercury-url` registers the fingerprint
  in xzMercury (`SET NX`). Consumer verifies via `VerifyAndPrepare`.

  ```bash
  tdtpcli --export payroll --integrity --mercury-url http://mercury:3000 --compress
  ```

  See: [`docs/xZMercury-TDTP-TZ-v1.2.md`](docs/xZMercury-TDTP-TZ-v1.2.md),
  [`xzmercury/README.md`](xzmercury/README.md).

## [1.9.4] — 2026-05-20

### Added
- **TDTP v1.4 Dictionary** (`pkg/core/packet/`): `<Dictionary>` section in schema —
  tokens `@name` → full strings (URI namespace, domain types).
  `ExpandDictionary` / `ContractDictionary`. Backward compatibility preserved.
- **`tdtp-svg`** (`cmd/tdtp-svg/`, `pkg/svg/`): SVG ↔ TDTP converter.
  Each element → table row, tree via `(id, parent_id, order_idx)`.
  Schema: 24 columns — 6 structural + `attrs_json` + 17 wide attributes.
  Parser is streaming — O(depth), not O(file size).
  Benchmark: 4171 elements, 580 KB SVG → 87 KB TDTP (kanzi, 8.9×).
- **`--fallback-row-limit N`** (`pkg/adapters/base/export_helper.go`):
  limits `ReadAllRows` on SQL pushdown fallback. Default 0 (unlimited).

### Fixed
- **MSSQL full table scan on names with `$` / spaces** (`pkg/adapters/base/sql_adapter.go`):
  `AdaptSQL` corrupted ANSI-quoted name (`"ZTR$Timesheet Line"` → `"[dbo].[ZTR$Timesheet Line]"`),
  MSSQL rejected it, code fell back to `ReadAllRows`. Observed 17 GB RAM usage.
  ANSI form is now replaced first. Regression test added.
- **MSSQL datetime `Z` suffix** (`pkg/adapters/base/sql_adapter.go`):
  `'2024-08-12T00:00:00Z'` → `'2024-08-12T00:00:00'`.
- **SQL pushdown silent fallback** (`pkg/adapters/base/export_helper.go`):
  silent fallback replaced with `log.Printf WARNING`.

## [1.9.3] — 2026-05-08

### Added

- **PipelineContext in TDTP packet header + `--expect-var`** (`pkg/core/packet/types.go`, `pkg/etl/`, `cmd/tdtpcli/`):

  On `--pipeline` export, every generated packet now contains a `<PipelineContext>` block —
  pipeline-source metadata embedded directly in the XML:

  ```xml
  <PipelineContext>
    <Pipeline name="daily-sync" version="2.1"/>
    <Variables>
      <Var name="dept" value="sales"/>
      <Var name="region" value="EMEA"/>
    </Variables>
  </PipelineContext>
  ```

  `Variables` contains **only variables actually used in the config**
  (via `@name` in SQL or `{{name}}` in YAML fields). Variables passed on CLI but
  not used in the config are excluded.

  Coverage: `exportToTDTP` (each part), `exportToRabbitMQ`, `exportToKafka` (legacy),
  `exportToKafkaSpool` (each part).

  **New flag `--expect-var name=value`** (`cmd/tdtpcli/`):

  On import (`--import`, `--import-broker`) — verify source variables
  **before** any DB operations — fail-fast with no side effects:

  ```bash
  tdtpcli --config cfg.yaml --import-broker --expect-var region=EMEA --expect-var dept=sales
  ```

  If a variable is missing or has a different value — import is aborted with a clear message:

  ```
  --expect-var check failed (pipeline: daily-sync):
    @dept: expected "hr", got "sales"
    @region: expected "APAC", not present in packet
  ```

  Flag is repeatable; check order does not matter.

  **`--inspect` now shows PipelineContext** (`cmd/tdtpcli/commands/inspect.go`):

  ```yaml
  pipeline: daily-sync v2.1
  pipeline_vars:
    dept: sales
    region: EMEA
  ```

  Lines are output only when `<PipelineContext>` is present in the packet — backward
  compatible with packets created before v1.4.

  **New functions and methods:**
  - `packet.PipelineContext`, `packet.PipelineInfo`, `packet.PipelineVar` — data types
  - `etl.UsedVariables(config, vars)` — returns only used variables
  - `Exporter.WithPipelineContext(ctx)` — sets context on exporter
  - `Processor.SetPipelineContext(ctx)` — sets context on processor
  - `commands.CheckPipelineVars(pkt, expectVars)` — pre-import check

- **Pipeline Variables — parametric pipelines via CLI** (`pkg/etl/variables.go`):

  Variables are passed directly on the command line without an extra flag:

  ```bash
  ./tdtpcli.exe --pipeline dept_staff.yaml @dept=97-256 @date_from=2025-01-01 @date_to=2025-12-31
  ```

  Substitution syntax:

  | Context               | Pattern     | Example                                |
  |-----------------------|-------------|----------------------------------------|
  | SQL — string literal  | `'@name'`   | `WHERE dept = '@dept'`                 |
  | SQL — numeric/bare    | `@name`     | `WHERE year = @year`                   |
  | YAML fields           | `{{name}}`  | `destination: "out/{{dept}}.tdtp.xml"` |

  Quotes around the value are stripped automatically (`@dept="97-256"` → `97-256`).
  For string literals, single quotes inside values are escaped (`'` → `''`).

  Substitution applies to: `sources[].query`, `sources[].dsn`, `transform.sql`,
  `description`, `output.tdtp.destination`, `output.xlsx.destination` (including `fallback` chain).

  **Validation:**
  - Variable declared in config but not passed on CLI → error with names.
  - Passed on CLI but not used in config → warning (not an error).

  Substitution runs **before** the SQL validator — injections via variable values
  are blocked by the existing SQL validator (`SELECT/WITH only` in safe mode).

  Output on startup shows active variables:
  ```
  Pipeline: dept-staff-with-hiredate
     Department 97-256 list for 2025-01-01 – 2025-12-31
     Variables: @date_from=2025-01-01, @date_to=2025-12-31, @dept=97-256
  ```

- **`pkg/etl/variables_test.go`** — 18 unit tests:
  `ParsePipelineVars`, `substituteSQL` (string literal, quote escaping, bare-numeric,
  mixed, unknown variable), `substituteYAML`, `ApplyVariables` (full substitution,
  error on missing variable, warning on extra variable, noop for empty config).

### Fixed

- **NULL marker in TIMESTAMP columns on PostgreSQL / MSSQL import**
  (`pkg/adapters/postgres/import.go`, `pkg/adapters/mssql/import.go`):

  TDTP encodes NULL field values as the string `[NULL]` in the packet body.
  `convertValue` (PostgreSQL) and `stringToValue` (MSSQL) did not check this marker
  before passing the value to `schema.Converter.ParseValue()`. As a result, `[NULL]`
  reached the driver as a string and caused a DB-level error:

  ```
  ERROR: invalid input syntax for type timestamp: "[NULL]" (SQLSTATE 22007)
  ```

  Fix: `field.SpecialValues.Null.Marker` check added **before** calling `ParseValue` —
  same as already implemented in `base/import_helper.go` (used by the MySQL adapter).

  Discovered in `examples/travel-agency` during sync of `branch_sales_inbox_staging`
  where `cancellation_date TIMESTAMP NULL` contained real NULL values.

- **Regression test T4.9** (`tests/cli/test_postgres.py`):
  export table with two nullable TIMESTAMP columns (5 rows, 2 NULLs each),
  import, verify that NULL values are preserved exactly.

- **`setup_staging_central.sql`** (`examples/travel-agency`):
  `cancellation_date` in `branch_sales_inbox_staging` changed from `TEXT` to `TIMESTAMP NULL`;
  corresponding cast `NULLIF(NULLIF(x,''),'[NULL]')::TIMESTAMP` in `merge_branch_sales_inbox`
  simplified to direct value pass-through.

---

## [1.9.2] — 2026-04-21

### Added

- **MySQL adapter — 58/58 CLI integration tests pass** (`tests/cli/test_mysql.py`):
  - T1 Basic Export: export all rows, `--fields` projection, `--list`
  - T2 TDTQL Filters: WHERE, compound AND (multiple `--where`), IN, ORDER BY, LIMIT/OFFSET,
    negative LIMIT (tail mode), bracket-quoted field names with spaces and `$`
  - T3 Compression: zstd level 3/19, kanzi level 6, `--hash` checksum, corruption detection,
    compress from config
  - T4 MySQL→MySQL Roundtrip: plain/compressed import, replace/ignore strategies, `--fields`
    projection, bracket-quoted tables (`[ERP$Entry]`, `[complex_fields]`), bracket-quoted WHERE
  - T5 File Integrity: `--test`, `--test` with checksum, `--inspect`
  - T6 Edge Cases: empty result set, nonexistent table error, import missing file error
  - T7 Compact Format (v1.3.1): `--compact --fixed-fields`, compress+hash roundtrip,
    `--to-compact` conversion, compact MySQL→MySQL roundtrip
  - T8 MySQL→SQLite Roundtrip: plain/compressed cross-DB import, strategies, `--fields`,
    bracket-quoted `[ERP$Entry]`
  - T9 Diff: identical/added/removed/modified, `--ignore-fields`, `--key-fields`, error cases
  - T10 Merge: union (non-overlapping/overlapping), intersection, append, left/right priority
    with `--show-conflicts`, 3-file union, error cases

- **`tests/cli/test_mysql.py`** rewritten: inline `setup_db()` via `docker exec`
  (no external scripts, no `pymysql` dependency), aligned with `test_sqlite.py` structure.
  Test environment: MySQL 8.4 in Docker (`docker compose up -d mysql`).

---

## [1.9.1] — 2026-04-07

### Fixed

- **PostgreSQL TIME type** (`pkg/adapters/postgres/types.go`, `pkg/adapters/base/type_converter.go`):
  - PostgreSQL `time without time zone` column now exports correctly as `08:00:00` instead of
    failing with "invalid timestamp format, expected RFC3339".
  - Root cause: `time` type was mapped to `TIMESTAMP` with subtype `"time"`, but converter
    didn't handle the subtype during validation.
  - Fix: Added `Subtype` field to `schema.FieldDef`, updated all `FieldDef` creation sites
    to copy subtype from `packet.Field`, and modified `parseTimestamp` to check for
    `subtype == "time"` and delegate to new `parseTime` function.
  - Added `pgtype.Time` handler in `DBValueToString` for PostgreSQL driver.

- **Test data reproducibility** (`scripts/create_postgres_test_db.py`):
  - Added `random.seed(42)` for deterministic test data generation.
  - Updated expected values in `tests/cli/test_postgres.py`: `ACTIVE_USERS=73`, `USERS_BALANCE_GT_5000=53`.

### Added

- **35/35 PostgreSQL CLI integration tests pass**:
  - All tests in `tests/cli/test_postgres.py` now pass with deterministic data.
  - Coverage: basic export, TDTQL filters, compression (zstd/kanzi/hash), export/import roundtrip,
    file integrity, edge cases, compact format.

---

## [1.9.0] — 2026-04-06

### Message Broker — Production Release

Kafka broker graduates from `[BETA]` to production-ready.
Full pipeline (DB → Kafka → DB, DB → Kafka → files) benchmarked at **50 000 rows in ~7s**
over localhost with 5 packets; traffic reduced 4× with kanzi vs uncompressed.

#### Export (`--export-broker`)

- **Parallel compress + serialize**: all packets processed in concurrent goroutines
  (`sync.WaitGroup`); each goroutine owns its own `packet.NewGenerator()` instance.
  kanzi: 6.7s → 5.1s (1.3×) on 100k rows.
- **`SendBatch`** (`pkg/brokers/kafka.go`): all serialized packets sent in a single
  `WriteMessages` call — one network roundtrip instead of N sequential sends.
  kafka-go `BatchTimeout` lowered from default 1s to 5ms (eliminates per-packet 1s wait).
  kafka-go `BatchBytes` raised to 100 MB (was 1 MB — caused "Message Size Too Large" on kanzi packets).

#### Import (`--import-broker`)

- **Parallel decompression**: all raw packets buffered first (receive is inherently serial),
  then packets 2…N decompressed in parallel goroutines; results assembled in order.
  ACK: single `CommitLast()` after all processing — for Kafka this commits the highest
  offset, implicitly covering all previous offsets.
- **`--output` mode**: instead of importing to DB, saves decompressed packets as
  `base_part_N_of_Total.tdtp.xml` files compatible with `--import` multi-part convention.
- **`--raw` flag** (`--import-broker --raw --output`): saves queue messages verbatim
  without any parse, decompress, or validation. Peeks the first message header to read
  `TotalParts` for correct `_part_N_of_Total` naming. No DB connection required.

#### Broker Configuration (Kafka)

- `brokers` (list) and `consumer_group` YAML fields added to `BrokerConfig`.
- `StartOffset: kafka.FirstOffset` (was `LastOffset`) — fixes race where reader
  positioned after messages sent during consumer group rebalance.

#### Performance (50k rows, 5 packets, localhost)

| Mode | Export | Import→files | Traffic |
|------|--------|-------------|---------|
| No compression | 3.4s | 3.8s | 7.2 MB |
| zstd level 3 | 3.5s | 3.9s | 2.4 MB (3×) |
| kanzi level 6 | 3.6s | 3.9s | 1.8 MB (4×) |

Import time dominated by receive + XML re-serialize; decompression parallelism eliminates
its contribution entirely at 5 packets.

### Changed

- `--listen` flag: removed `[BETA]` label. Streaming consumer is production-ready for Kafka.
- **`--import-broker` atomicity** (`cmd/tdtpcli/commands/broker.go`): multi-part imports now
  use a single `ImportPackets` transaction by default — all-or-nothing, mirrors `--import`
  (file) behaviour. Previously each part was committed with a separate `ImportPacket` call,
  leaving the table partially updated on failure.
- **`--keep` flag** (`--import-broker --keep`): opt-in streaming mode — each packet is
  received, decompressed, and committed immediately without buffering the full batch in
  memory. On failure, successfully committed parts remain in the table for analysis.
  Implemented in `importBrokerKeep()` as a separate code path (no full-batch buffer).
- Help (`help_full.txt`, `help_short.txt`): broker section expanded with `--raw`,
  `--output` multi-part naming, `--keep` semantics, parallel processing notes, kanzi
  traffic comparison.

### Fixed

- **`--fields` bracket-quoting** (`cmd/tdtpcli/main.go`): `splitCommaSeparated` now parses
  `[Field Name]` syntax for field names containing spaces or commas, matching the
  bracket-quoting already supported in `--where` (TDTQL lexer).
  - `--fields "id,[Birth Date],status"` → `["id", "Birth Date", "status"]`
  - `--fields "[First, Last],[Birth Date]"` → `["First, Last", "Birth Date"]`
  - Same parser used for `--key-fields`, `--ignore-fields`, `--fixed-fields`.
- **SELECT projection quoting** (`pkg/core/tdtql/sql_generator.go`): field names in
  `query.Fields` were joined bare into `SELECT f1, f2 FROM ...` — a name like `Birth Date`
  produced invalid SQL. Now each field passes through `quoteFieldName()` (same function
  already used for WHERE and ORDER BY), producing `SELECT "Birth Date", id FROM ...`.
  MSSQL/MySQL dialect adapters convert ANSI double-quotes downstream as before.

---

## [1.8.2] — 2026-04-05

### Performance

#### Import pipeline — 2× speedup (1.55s → 0.77s, 100k rows × 7 fields, SQLite)

- **Streaming import** (`cmd/tdtpcli/commands/import.go`): parts processed one at a time —
  read → parse → insert → release. Previously all parts were buffered in memory
  simultaneously before any inserts began. Memory usage is now constant regardless
  of part count; GC pauses during insertion eliminated.

- **`GetRowValues` fast path** (`pkg/core/packet/parser.go`): rows without escape
  characters (`\|`, `\\`, `\n`) — the vast majority of real data — are split via
  index scan returning subslices of the original string with zero per-field
  allocations. Benchmark: `simple_10_fields` 409 ns/11 allocs → 150 ns/1 alloc (2.7×);
  `many_fields_100` 5034 ns/105 allocs → 1079 ns/1 alloc (4.7×).

- **Parser/Converter singletons** (`pkg/adapters/base/import_helper.go`,
  `pkg/adapters/postgres/import.go`, `pkg/adapters/mssql/import.go`):
  `packet.NewParser()` and `schema.NewConverter()` were allocated on every single row
  in all adapters. Both structs are stateless (`{}`); replaced with package-level
  singletons. Eliminates ~2 allocs × 100k rows per import.

- **`PrepareContext` for SQLite batch INSERT** (`pkg/adapters/sqlite/import.go`):
  the 994-parameter INSERT query was re-parsed by SQLite on every batch call
  (~700 calls for 100k rows). Now prepared once; reused for all full batches.
  Args slice reused across batches. Raw benchmark: 1043 ms → 433 ms (2.4×).

#### Misc

- **`help.go` refactor**: ~100 `fmt.Println` calls replaced with two embedded text
  files (`help_short.txt`, `help_full.txt`) via `//go:embed`. Version injected via
  `strings.ReplaceAll("{VERSION}", version)` at runtime.

### Infrastructure

- **Pre-commit hook** (`.git/hooks/pre-commit`): runs `gofmt`, `golint`, `go vet`
  on staged `.go` files before every commit. `gofmt` and `go vet` are blocking;
  `golint` is advisory.

---

## [1.8.1-beta] — 2026-04-02

### Added

#### Field Name Sanitizer (`--translit`, `--clear`)
- `pkg/sanitize` — new package with `ApplyToSchema()` single entry point
  - `--clear`: symbol map replacement (`%` → `_pct_`, `$` → `_usd_`, `&` → `_and_`, `@` → `_at_`, `#` → `_xh_`, `?` → `_is_`, `~` → `_not_`, spaces/dots/dashes → `_`; consecutive `__` collapsed)
  - `--translit`: non-ASCII transliteration via `github.com/mozillazg/go-unidecode v0.2.0` (Cyrillic, European diacritics)
  - Combined mode: `--translit` runs first, then `--clear`
  - Applied **only on `--import`** — `--export` always preserves original field names (source of truth)
- `cmd/tdtpcli/flags.go`: `--translit` and `--clear` CLI flags
- `cmd/tdtpcli/commands/import.go`: `SanitizeClear` / `SanitizeTranslit` options, applied after field whitelist
- `pkg/etl/config.go`: `SanitizeFieldsConfig` struct; `sanitize.translit/clear` keys in ETL source YAML
- `pkg/etl/processor.go`: per-source sanitization in `populateWorkspace`
- `pkg/core/packet/types.go`: `OriginalName string` runtime field on `packet.Field` (never serialized)
- DB column comments preserving original names:
  - PostgreSQL: `COMMENT ON COLUMN t.col IS 'original: ...'`
  - MySQL: inline `COMMENT 'original: ...'` in column definition
- Test XMLs: `tests/sanitize/` — `access_fields.tdtp.xml`, `cyrillic_fields.tdtp.xml`, `exotic_mixed.tdtp.xml`, `safe_import.tdtp.xml`
- `pkg/sanitize/fieldname_test.go` — 7 unit tests covering all sanitizer modes

#### TDTQL: Bracket-Quoted Identifiers
- `pkg/core/tdtql/lexer.go`: support for `[Field Name]` syntax (MSSQL/Access style)
  - `[` token now reads to `]` and emits `TokenIdent` with the inner name (brackets stripped)
  - Fixes: `--where "[Termination Date] = '1753-01-01'"` — was "parse error: expected field name, got 1"
- `pkg/core/tdtql/sql_generator.go`: `quoteFieldName()` helper
  - Names with non-safe chars → ANSI `"field name"` in generated SQL
  - Applied in `generateFilterCondition`, `generateOrderByClause`, `generateReversedOrderByClause`
- `pkg/adapters/base/sql_adapter.go`: `MSSQLAdapter.AdaptSQL` now converts ANSI-quoted `"field"` → `[field]`
  - `StandardSQLAdapter` MySQL mode: existing `ReplaceAll("\"", "`")` handles ANSI → backtick conversion

### Fixed
- `pkg/brokers/kafka_stub.go`: removed unused `config Config` field; added doc comments to all exported methods (revive lint)
- `pkg/processors/compression_test.go`: removed trailing blank line (gofmt)
- `.git/hooks/pre-commit`: `golangci-lint run --tags` → `--build-tags` (golangci-lint v2 rename)

### Documentation
- `docs/USER_GUIDE.md`: added `--test` command section, `--translit`/`--clear` section, bracket-quoted WHERE section, parallel export note, pre-import workflow `--inspect → --test → --import`
- `AGENTS.md`: added `--test` workflow, `--import --translit/--clear` skills, bracket-quoted `--where` examples
- `cmd/tdtpcli/help.go`: bracket-quoted `--where` examples, `--test`/`--inspect` pre-import workflow in EXAMPLES section

### Tests
- `tests/cli/test_sqlite.py`: added `complex_fields` table (column names with spaces and special chars); T2.8 and T2.9 tests for bracket-quoted `--where` on this table

---

## [1.8.0-beta] — 2026-03-31

### Added

#### Object Storage (S3)
- `pkg/storage` — ObjectStorage interface, factory, and S3 driver (`aws-sdk-go-v2`)
- `--output s3://bucket/key` on export — upload multi-part TDTP directly to S3
- `--import s3://bucket/key` — download + auto-discover all `_part_N_of_M` siblings from S3
- `--inspect s3://bucket/key` — inspect packet metadata from S3 in-memory (no temp file)
- `--to-xlsx / --export-xlsx --output s3://` — XLSX output directly to S3
- ETL pipeline source type `tdtp-s3`: load compressed multi-part TDTP sets from S3 into workspace
- Compatible with SeaweedFS, MinIO, Ceph RGW, AWS S3 (path-style and virtual-hosted)
- Build tag `nos3` to exclude driver and drop `aws-sdk-go-v2` dependency

#### File Integrity (`--test`)
- `--test <file>` — dry-run integrity check of TDTP files (no database required)
  - Multi-part file discovery: auto-resolves `_part_N_of_M` siblings from any part path
  - Missing part detection: reports which parts are absent before validating
  - Batch consistency: all parts must share the same `InReplyTo` UUID and `TableName`
  - Row count validation: actual `<R>` count vs `RecordsInPart` header field
  - XXH3 checksum validation for files exported with `--hash`
  - Decompression integrity: dry decompress in memory for zstd and kanzi files
  - Duplicate `MessageID` detection across parts

#### Compression
- `compress_algo` YAML config field in `ExportConfig` — set default algorithm in config file
  - Flag `--compress-algo` takes precedence over config file value
  - Example: `compress_algo: kanzi` in config enables kanzi without CLI flags

#### CLI Integration Tests
- `tests/cli/test_sqlite.py` — 31 integration tests for SQLite source
  - T1: Basic Export (3 tests) — row counts, field projection, `--list`
  - T2: TDTQL Filters (7 tests) — WHERE, AND, IN, ORDER BY, LIMIT, OFFSET, tail mode
  - T3: Compression (6 tests) — zstd levels, kanzi, hash, corrupt file detection
  - T4: Export/Import Roundtrip (5 tests) — data identity, strategies (replace/ignore), field subset
  - T5: File Integrity (3 tests) — `--test` on plain/compressed/checksum files, `--inspect`
  - T6: Edge Cases (3 tests) — empty result, nonexistent table/file error handling
  - T7: Compact Format (4 tests) — protocol v1.3.1, compact+compress pipeline, `--to-compact`, roundtrip
- `tests/cli/test_postgres.py` — 32 integration tests for PostgreSQL source
  - Same T1–T7 structure; T4 roundtrip imports into same PG database
  - Preflight check: `pg_isready` + row count verification + auto-setup via `create_postgres_test_db.py`
  - Dynamic WHERE assertions: expected counts queried live via psql subprocess
  - Run a single group: `python3 tests/cli/test_postgres.py T3`

#### Kanzi Compression (from v1.7.x)
- `--compress-algo kanzi` — kanzi-go compression alongside existing zstd
- Compression levels 6–7 for kanzi (6× ratio vs raw, vs 3× for zstd level 3)
- `pkg/python/libtdtp` — multi-algorithm support in Python bindings compress/decompress paths
- Build tag `nokafka` for offline builds without Kafka dependency

#### S3 + Pipeline Features
- `examples/09-s3-pipeline-chain` — extract → split by region pipeline example
- ETL `output.type: tdtp` with S3 output
- Smart Failover in ETL — fallback delivery channel with circuit breaker
- `--fast` flag to skip SpecialValues detection on export

### Changed
- `CreateSampleConfig` includes `CompressAlgo: "zstd"` in default template
- `--test` is an early-exit command: no database connection required
- `commandWasSpecified()` updated to include `--test`

### Performance (from v1.7.x)
- Parallel packet processing for file/S3 export
- Skip `GetRowCount` in TDTQL export when no LIMIT is set
- Single-pass XML escaping with schema-aware escape mask
- Manual `bufio` writer replacing `xml.MarshalIndent` in data section
- `strconv` replacing `fmt.Sprintf` in hot data conversion path
- DATE/DATETIME scanned as string in SQLite (skip `modernc.parseTime`)
- PostgreSQL full-export benchmark infrastructure (`cmd/bench_raw`)

---

## [1.7.1-beta] — 2025 Q4

### Added
- `--compact` — TDTP v1.3.1 compact format on export (fixed fields written once per group)
- `--to-compact <file>` — convert existing TDTP v1.x file to compact v1.3.1 in-place
- `--compact-tail` — tail + carry attributes for streaming support
- `--fields <col1,col2>` — column projection on export and import
- `--inspect <file>` — YAML metadata summary of a TDTP file or S3 object
- `--listen` — streaming consumer daemon (v1.7.1-beta)
- `--where` flag repeatable — multiple conditions combined with AND
- `--where` supports `IN (...)` operator
- `--limit` with negative value — tail mode (last N rows)
- `--list` accepts optional glob pattern for table name filtering
- `--validate` and `--normalize` YAML-based processors
- `FieldValidator` with `on_error` strategy: fail / filter / warn
- SpecialValues v1.3.1: `[NULL]`, `NaN`, `INF`, `-INF`, `0000-00-00` markers
  - Auto-detected on export; correctly restored to NULL/native on import
  - Excel data-integrity traps handled automatically (BIGINT, dates pre-1900, formula strings)
- RabbitMQ: flexible queue config, TLS skip-verify, passive declare
- MSMQ broker support (`queue_path` config field)
- xZMercury AES-256-GCM encryption layer for pipeline output
- `tdtpserve` — standalone HTTP encrypted TDTP data viewer
- Python bindings: `J_ExportAll`, `read_pandas` / `write_pandas`, zstd+XXH3 support
- C# .NET 3.5 P/Invoke wrapper for `libtdtp.dll`
- Redis result publisher for pipeline state reporting

### Fixed
- `RecordsInPart=0` in `ExecuteRawQuery` and `workspace.ExecuteSQL`
- rawRows regression: `ImportPacket` importing nothing after fast-path optimization
- Compact format auto-expansion at parser boundary (broker, ETL importer, diff/merge, HTML, XLSX)
- `--fields` projection applied to `<Schema>` and `<R>` in MSSQL export
- `StrategyReplace` = full table swap (TRUNCATE + INSERT), not UPSERT
- `StrategyCopy` = full replace; other strategies = UPSERT accumulate
- Batch-aware broker import — match by batchID, nack foreign packets
- Compression: `SetRows(GetRows())` clearing `rawRows` fixed
- DATE type detection and rowversion filtering in MSSQL adapter
- Scientific notation handling in DECIMAL parser

---

## [1.7.0] — 2025 Q4

### Added
- kanzi-go compression (`--compress-algo kanzi`, levels 6–7)
- `--fields <col1,col2>` — column projection on export
- MSMQ broker support
- `--packet-size` flag — control maximum packet size in bytes

---

## [1.6.0] — 2025 Q3

### Added
- `--where` TDTQL filter with SQL-to-TDTQL translation
- `pkg/cliquery` — WHERE/fields parsing with unit tests
- PostgreSQL `--fields` projection in `ExportTableWithQuery`
- `pkg/etl` — ETL pipeline with workspace, smart failover, processor chain (mask → normalize → compact → compress → encrypt → hash)
- MS Access adapter (ODBC, 32-bit, Windows-1251, ADOX schema via VBScript)
- kanzi-go compression (direct dependency)
- `--packet-size` flag
- `--hash` flag — XXH3 checksum embedded in packet header
- Pagination: `ExportTableWithQuery` with Limit/Offset/MoreDataAvailable
- TDTP HTML viewer (`--to-html`)
- TDTP XLSX export/import (`--to-xlsx`, `--export-xlsx`)
- Zero Trust encryption layer (xZMercury)

---

## Version History Summary

| Version | Highlights |
|---------|-----------|
| 1.12.0 | Air-gap offline cert, seat policy, structured audit log (JSON/text/syslog), per-job artifacts, orchestrator LDAP auth, Prometheus metrics, Docker stack (dev+prod), Grafana dashboard |
| 1.11.0 | Full trust chain: CA/TPM → xZMercury → tdtp.lic → orchestrator dual gate; `tdtp-certify` admin CLI; offline license engine; `--require-prod` enforcement |
| 1.10.0 | `--limit -N` MSSQL fix (3 iterations), burn marker (410/404), mode-in-HMAC, mandatory serverSecret, CA server `tdtp-ca`, orchestrator with SQLite schedule DB |
| 1.9.7 | Arrow columnar bridge (×2.1 write), `Tdtp` facade API, CLI parity in-process, C# wrapper parity, lockstep `.so` versioning |
| 1.9.6 | `--to-csv`, `-l` alias, MSSQL `SELECT TOP N` fix, xlsx MessageID fix, rawRows data-loss fix, LIKE filter 9.5×, compact 2×, `--enc` tier, v1.4 security gate on all import paths |
| 1.9.5 | `-n`/`-w` shorthands, v1.4 integrity + xzMercury hash notary (`--integrity`, `--mercury-url`) |
| 1.9.4 | TDTP v1.4 Dictionary, `tdtp-svg` (SVG↔TDTP, 8.9×), MSSQL 17 GB RAM fix, `--fallback-row-limit` |
| 1.9.3 | PipelineContext in packet header, `--expect-var`, pipeline variables `@name=value` |
| 1.9.2 | MySQL adapter — 58/58 CLI tests pass (T1–T10), rewritten test harness |
| 1.9.1 | PostgreSQL TIME type fix, deterministic test data (seed=42), 35/35 tests pass |
| 1.9.0 | Kafka production-ready, parallel compress/decompress, `SendBatch`, `--raw`, `--output` multi-part save, `--keep` streaming mode |
| 1.8.2 | 2× import speedup, streaming import, `PrepareContext` singleton, embedded help files |
| 1.8.1-beta | `--translit`/`--clear` sanitization, bracket-quoted identifiers in TDTQL, ETL sanitize per-source |
| 1.8.0-beta | S3 object storage, `--test` integrity check, `compress_algo` config, SQLite+PostgreSQL CLI test suites |
| 1.7.1-beta | Compact v1.3.1, `--compact`/`--to-compact`, `--inspect`, `--listen`, SpecialValues, xZMercury pipeline encryption |
| 1.7.0 | kanzi compression, `--fields` projection, MSMQ, `--packet-size` |
| 1.6.0 | TDTQL `--where`, ETL pipeline, Access adapter, `--hash` XXH3, XLSX/HTML viewer |
| 1.3.1 | TDTP protocol v1.3.1 — compact format specification |
| 1.0–1.3 | Core protocol, XML serialization, SQLite/PostgreSQL/MSSQL adapters |
