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

- [x] `pkg/core/packet/encryption.go` (new) — `EncryptSections`/
      `DecryptSections`: fixed hash→compress→encrypt order, one key per
      packet with a unique nonce per section. New packets get
      `Version = "1.5"` set by `EncryptSections` itself.
      **Critical bug found and fixed during implementation**: the function
      MUST call `pkt.MaterializeRows()` itself, unconditionally, as its
      first step — an earlier version left this to callers, and a caller
      that skips compression (or calls through `pkg/etl`'s exporter, which
      only materializes as a side effect of `compressDataPacket`) left
      `rawRows` populated, so `writePacketTo`'s fast path silently wrote
      the ORIGINAL PLAINTEXT rows into `<Data>` right next to a truthful
      `encryption="aes-256-gcm"` attribute — a packet that looks encrypted
      but leaks every row in the clear. Caught by
      `pkg/etl/exporter_v15_test.go`'s `TestExporter_ExportToTDTP_V15Default`
      before it shipped. Guarantee now lives at the lowest common layer, not
      duplicated per call site.
- [x] `pkg/core/packet/types.go` `Schema` struct, `query.go` `QueryContext`
      struct — added `Encrypted string \`xml:",chardata"\`` +
      `Encryption string \`xml:"encryption,attr,omitempty"\`` to both.
      `Data` needed no struct change (`Rows []Row` chardata already fit) —
      only a new `encryption` attribute in the hand-written `xmlwriter.go`
      writer.
- [x] `pkg/core/packet/parser.go`'s `validatePacket` — found and fixed a
      second real bug while wire-round-trip-testing: two existing checks
      ("schema required when data present", `RecordsInPart` count match)
      unconditionally assumed plaintext `Schema.Fields`/`Data.Rows` and
      would reject every valid v1.5 encrypted packet. Both now skip when
      `Schema.Encryption`/`Data.Encryption` is set.
- [x] Encode order (fixed, not configurable): `ComputeIntegrity` →
      compress `Data` → encrypt `QueryContext`/`Schema`/`Data` content.
      Decode order: decrypt → decompress → verify xxh3. Enforced by
      `EncryptSections` running after both steps in every call site
      (`cmd/tdtpcli/commands/export.go`'s `writePacket`,
      `pkg/etl/exporter.go`'s `exportToTDTP`).
- [x] `cmd/tdtpcli/commands/encrypt.go` — `EncryptPacketV15`/
      `DecryptPacketV15`/`IsEncryptedPacket` (CLI-level, `--export`/
      `--import` path). `bindAndVerifyKey` factored out for reuse.
      Verified against a live `xzmercury.exe --dev` instance (real HTTP,
      real HMAC, real burn-on-read), not only a mock — see live smoke test
      run during this session (not committed, was scratch verification).
- [x] `pkg/processors/encryption.go` — `FileEncryptor.bindAndDecodeKey`
      factored out of `Encrypt` (shared with `--enc13`) +
      `EncryptSectionsV15` new method, so `pkg/etl`'s Exporter gets the
      same `MercuryBinder`-override support (dev-mode / tests) v1.3 already
      had, not a parallel implementation.
- [x] `pkg/etl/exporter.go` — `exportToTDTP`'s per-part loop branches on
      `TDTPOutputConfig.EncryptionV13` (new field): `true` → unchanged
      legacy call; `false`/unset → new `exportEncryptedV15`, keyed by
      `part.Header.MessageID` (see Multi-part note above — no special
      per-message coordination needed, confirmed by test).
- [x] `cmd/tdtpcli/flags.go` — `--enc` (name unchanged) now defaults to
      v1.5; new `--enc13` flag maps to the legacy `EncryptPacket` call
      unchanged. Both gated under the same `GateFeature("enc")` license
      check. Wired through `ExportOptions.EncryptLegacy` /
      `PipelineOptions.EncryptLegacy` / `TDTPOutputConfig.EncryptionV13`.
      `--enc-dev` unaffected (orthogonal: key source, not wire format).
      Full rationale in
      [`docs/tdtp-protocol-schema.md`](docs/tdtp-protocol-schema.md) →
      "v1.5" → "CLI flag naming".
- [x] `cmd/tdtpcli/commands/export.go`'s `writePacket` — all four
      destination×format combinations wired (file/S3 × v1.5/legacy); v1.5
      output can go to stdout (still valid XML), legacy `--enc13` still
      cannot (binary blob).
- [x] Multi-part: corrected after checking `GenerateReference` directly —
      each part already gets its own distinct `Header.MessageID`
      (`{base}-P{n}`, not shared), so `BindKey` just happens once per part
      at the same per-part call site `--enc13` already uses
      (`pkg/etl/exporter.go`'s `exportToTDTP` loop) — no special multi-part
      handling needed. See `docs/tdtp-protocol-schema.md` → "v1.5" →
      "Multi-part packets" (also flags a pre-existing `--enc13` race this
      uncovered, out of scope for v1.5 itself).
- [x] Audited every `v1.4` reference in `pkg/core/packet/` (`grep -rn
      "v1\.4" pkg/core/packet/`). Two needed action:
      `dictionary.go`'s `Downgrade` — checked, no change needed: it only
      ever runs after decryption (VerifyAndPrepare's FallbackDowngrade,
      consumer-side, post-`DecryptPacketV15`), so it always sees plaintext
      Schema/Data regardless of v1.5. `utils.go`'s `NeedsRowCountCheck` —
      doc comment expanded: this predicate is the *same* one gating
      `pipeline.VerifyAndPrepare`'s Mercury pre-flight (inverted), which is
      exactly the assumption that caused the mandatory-integrity bug above
      — now documented explicitly so the next person touching it doesn't
      rediscover it live. Everything else (`generator.go`, `integrity.go`,
      `types.go`'s struct comments) was already accurate as v1.4-specific
      text; no v1.5 counterpart needed since v1.5's own struct fields
      already carry their own doc comments.
- [x] `docs/SPECIFICATION.md` "Версионирование" section — new `v1.5` entry
      added following the existing changelog format, plus updated
      "Текущая версия"/header version line and "Последнее обновление"
      date. Cross-references `docs/tdtp-protocol-schema.md` → "v1.5" for
      full diagrams/design rationale rather than duplicating them here.
- [x] `cmd/tdtpcli/commands/broker.go` — `--export-broker` gained
      `--enc`(v1.5)/`--enc13`(legacy) support (previously zero encryption
      support at all, confirmed by grep before starting). `--import-broker`
      gained the same dual-format decrypt dispatch as `--map`/`--import`,
      factored into a shared `parseAndDecryptBrokerMessage` helper (used by
      both the atomic and `--keep` streaming import paths, which
      previously duplicated the same parse logic). Verified against real
      infrastructure, not only mocks: full v1.5 encrypt→send→receive→decrypt
      round-trip through a live RabbitMQ (Docker) + live `xzmercury --dev`
      instance, including the decrypt-then-decompress ordering with
      compression also enabled (scratch verification, not committed —
      same pattern as the earlier live xZMercury-only smoke test).
      **Still open, separate from v1.5 and not done here:** `result_log`
      publish (mirror `pipeline.go:144-153`) — orchestrator pub/sub
      integration for the broker path, unrelated to the encryption format.
- [x] `cmd/tdtpcli/commands/map.go` — `loadPacket` (file/S3/broker input)
      and `runMapListen` (daemon mode) both gained v1.5 detection, via two
      new shared helpers factored out of `broker.go`'s
      `parseAndDecryptBrokerMessage`: `decryptLegacyBlobIfNeeded` (raw
      bytes, pre-parse) and `decryptV15PacketIfNeeded` (parsed packet,
      post-parse) — same two atomic steps, reused by both files instead of
      three separate copies of the same detection logic.
- [x] `cmd/tdtpcli/commands/import.go:127` (`--import`) — added the same
      `IsEncryptedPacket` → `DecryptPacketV15` dispatch after `p.ParseBytes`,
      with the same error-packet-on-failure handling the legacy blob path
      already had (`WriteErrorPacket` + `KeyBurnedError` mode extraction).
      A v1.5 file now works with a bare `--import --mercury-url`, same as
      `--enc13` output does today.
- [x] **Bug found and fixed via live end-to-end testing, not part of the
      original plan:** `integrityProc.ProcessPacket` (`export.go`) computed
      `Schema.XXH3` (`ComputeIntegrity`) *before* embedding the `@MRC`
      Dictionary entry (Mercury base URL, added whenever
      `--integrity`/v1.5 + `--mercury-url` are combined) — the stamped
      hash covered a Schema that was about to change, so every consumer's
      `VerifyIntegrity` on import failed with a schema hash mismatch.
      100% reproducible, not a corner case — affects **any** use of
      `--integrity --mercury-url` together, not just v1.5; nothing
      exercised that combination end-to-end before (no prior test
      existed). Fixed by reordering: embed `@MRC` first, then
      `ComputeIntegrity`. Regression test:
      `cmd/tdtpcli/commands/export_integrity_test.go`. Confirmed via a
      full live `--export --enc` → `--import` round trip against real
      xZMercury + real SQLite (scratch verification).

### v1.4 integrity made mandatory for v1.5, not optional (deliberate scope addition)

Found live, not planned up front: `pipeline.VerifyAndPrepare`'s consumer-side
pre-flight runs for *any* packet with `Version >= "1.4"` and treats an empty
`pkt.XXH3` as a hard block (`ErrHashNotRegistered`), not "integrity wasn't
requested." A v1.5-encrypted packet that skipped `ComputeIntegrity` would
therefore be unimportable the instant a consumer sets `--mercury-url` —
which v1.5 decryption itself always requires. Reproduced live before fixing.

Decision (explicit user call, not a default assumption): rather than
loosening `runMercuryCheck`'s existing block-on-empty-XXH3 behavior (which
would be a real security-relevant change to v1.4's own gate, shared by
`--map`/`--import`/`--import-broker`), make `ComputeIntegrity` +
`RegisterHash` **mandatory** ahead of every v1.5 encryption call, so no
v1.5 packet is ever missing what the existing gate already requires — zero
regression to the pre-v1.5 security posture, and the gate itself needed no
changes.

New shared helper: `pkg/pipeline/produce.go`'s
`ComputeAndRegisterIntegrity` (+ `HashRegistrar` interface, mirroring
`processors.MercuryBinder`'s dev/test-substitution shape for `BindKey`).
Wired into all three v1.5 encryption call sites:
- `cmd/tdtpcli/commands/export.go`'s `ExportTable` — `integrityProc` now
  added to the chain whenever `opts.Encrypt && !opts.EncryptLegacy`, not
  only when `--integrity` was passed explicitly.
- `cmd/tdtpcli/commands/broker.go`'s `ExportToBroker` — per-packet call
  before compression, same condition.
- `pkg/etl/exporter.go`'s `exportToTDTP` — same, per part; this path had
  **zero** v1.4 integrity wiring at all before (`--pipeline` output could
  never stamp/register a hash, encrypted or not).

Tests: `pkg/pipeline/produce_test.go` (including
`TestV15EncryptionEnablesVerifyAndPrepare`, which fails on the *old*
unregistered-hash behavior and passes once `ComputeAndRegisterIntegrity`
runs — proves the fix, not just the happy path), plus assertions folded
into the existing `pkg/etl` and `cmd/tdtpcli/commands` v1.5 test suites.
Confirmed end-to-end against real xZMercury + real SQLite.

### xZMercury pairing — verified: zero server-side changes needed

Checked directly against source, not assumed (full trace in
[`docs/tdtp-protocol-schema.md`](docs/tdtp-protocol-schema.md) → "v1.5" →
"xZMercury pairing"): `xzmercury/internal/keystore/store.go`'s
`Bind`/`BurnOnRead` and `xzmercury/internal/api/keys.go`'s HTTP handlers
treat the AES key as opaque bytes keyed only by `packageUUID` — no
assumption anywhere about how many times or in what shape the key gets
used to encrypt something downstream. `pkg/mercury/client.go`'s
`BindKey`/`RetrieveKey` need no signature change either. v1.5 is a
**client-side-only** change: one `BindKey` call still returns one key;
that key just seals three sections (QueryContext/Schema/Data) instead of
one whole-packet blob, each with its own nonce. Don't scope an xZMercury
API-side task for this — there isn't one.

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
