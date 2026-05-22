# Dictionary as Dependency Manifest — Data-driven Circuit Breaker

> **TDTP v1.4 / .odtf specification note**
> This document captures an architectural pattern that emerged from the Dictionary
> feature implementation. It is not yet part of the formal spec but is a candidate
> for v1.5.

---

## 1. The Core Idea

A TDTP Dictionary entry is just a short token → full string mapping. But nothing in
the spec says the "full" string must be a namespace URI. It can be **any opaque
string** — and that opens up a second use case: **embedding resource metadata into
the packet header itself**.

A consumer can parse *only the XML header* (a few hundred bytes), inspect the
Dictionary entries, and make a go/no-go decision **before** touching the payload —
before decompression, before decryption, before any DB write.

This turns the Dictionary into a **dependency manifest** that travels with the data,
and the consumer's header-peek into a **pre-flight check** — a data-driven circuit
breaker.

---

## 2. Example: Metadata-only Dictionary

```xml
<TDTP version="1.4">
  <Schema name="payroll_q1">
    <Dictionary>
      <Entry short="@DB"   full="mssql://axapta.corp/MAIN"/>
      <Entry short="@MRC"  full="https://xzmercury.internal/keys/doc-abc123"/>
      <Entry short="@SHA"  full="sha256:a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9"/>
      <Entry short="@SZ"   full="bytes:892441"/>
      <Entry short="@LOCK" full="status:available"/>
      <Entry short="@ALG"  full="kanzi:7+aes256-gcm"/>
    </Dictionary>
    <Fields>...</Fields>
  </Schema>
  <Data compressed="true" encrypted="true">
    <!-- payload — NOT read during pre-flight -->
  </Data>
</TDTP>
```

None of the short tokens (`@DB`, `@MRC`, etc.) need to appear in any data row.
Their only purpose is to carry metadata in the header.

---

## 3. Pre-flight Check Algorithm

```
Consumer receives packet (from queue / filesystem / S3 / HTTP):

  1. PEEK HEADER
     Read XML until </Schema> — typically < 1 KB, no streaming needed.

  2. PARSE DICTIONARY
     Extract all Entry elements. O(N) on entry count, never touches <Data>.

  3. CHECK DEPENDENCIES
     for each Entry:
       "@DB"   → ping database, verify connection pool available
       "@MRC"  → HEAD https://xzmercury/.../key — key exists and not consumed?
       "@SZ"   → compare against Content-Length or file size on disk
       "@SHA"  → (deferred — verify after decompression, before decryption)
       "@LOCK" → if full == "status:locked" → NACK immediately (see §4)
       "@ALG"  → check that required codec is available locally

  4. DECISION
     ALL checks pass → ACK, proceed to full parse + import
     ANY check fails → NACK (or leave-in-queue), log which dependency failed

  No decompression. No decryption. No DB writes. One-time keys are NOT consumed.
```

---

## 4. System Lock — `@LOCK`

The `@LOCK` entry is a **soft stop** for the pipeline.

```xml
<Entry short="@LOCK" full="status:locked"/>
```

When `@LOCK` is present and its value is `"status:locked"`, the consumer NACKs
the packet and leaves it in the queue.

**Why this matters:**

| Scenario | Traditional solution | With `@LOCK` |
|---|---|---|
| Stop processing during maintenance | Pause queue consumer (broker config) | Set `@LOCK` in next packet — data carries the signal |
| Freeze pipeline from the producer side | Requires broker admin access | Producer sets `@LOCK`, consumer obeys |
| Gradual rollout (pause one table, not all) | Per-queue config | Per-packet flag — granular to the packet |
| Audit hold ("freeze this dataset") | Application-level flag in DB | Embedded in the packet, travels with it |

The packet stays in the queue without deadlocking it. Other packets (for other
tables / schemas) continue flowing. When the lock is lifted (producer sends a new
packet with `@LOCK = "status:available"`), processing resumes automatically.

**No broker configuration. No admin rights. No out-of-band signalling.**

---

## 5. Pre-verification for Encrypted / Compressed Payloads

This is the killer feature for xzMercury integration.

```
Without pre-flight:
  1. Fetch one-time key from xzMercury  ← KEY IS NOW CONSUMED
  2. Decrypt payload
  3. Discover the file is corrupt / wrong size
  4. Key is gone. Data is unrecoverable.

With @SHA + @SZ pre-flight:
  1. Peek header → read @SZ and @SHA
  2. Verify file size matches @SZ  ← if mismatch: NACK, key not touched
  3. Verify SHA-256 of compressed+encrypted blob matches @SHA  ← fast, one pass
  4. Only if both pass → fetch one-time key from xzMercury
  5. Decrypt → guaranteed to succeed (integrity already proven)
```

The one-time key is only consumed **after** the payload is proven intact.
Corrupt-file attacks (or network corruption) cannot burn a key.

Combined with @MRC (Mercury key URL), the pre-flight check becomes:

```
peek @SHA → verify blob hash
peek @MRC → HEAD request to Mercury (key exists, not consumed yet)
both OK   → fetch key → decrypt
```

---

## 6. Transport-agnostic Circuit Breaker

This pattern works identically on every transport:

| Transport | Pre-flight mechanism |
|---|---|
| RabbitMQ | `basic.get` + reject without requeue / dead-letter |
| Kafka | `poll()` → peek header → `seek()` back if NACK |
| Redis Streams | `XREADGROUP` → `XACK` only on pass; message stays in PEL on NACK |
| MSMQ | `PeekMessage()` → `ReceiveMessage()` only on pass |
| S3 / filesystem | Open + read first 1 KB → close without delete if NACK |
| HTTP webhook | Read body → return 503 (retry-after) if NACK |

The circuit-breaking logic is **in the data**, not in the broker. You can switch
brokers without rewriting the circuit-breaker configuration.

---

## 7. Comparison with Existing Solutions

| Feature | Hystrix / Resilience4j | DLQ (Dead Letter Queue) | TDTP Dictionary pre-flight |
|---|---|---|---|
| Configuration location | Application code / config files | Broker config | Inside the packet (data) |
| Granularity | Service-level | Queue-level | Packet-level |
| Transport dependency | None (library) | Broker-specific | None |
| Pre-flight before decryption | No | No | **Yes** |
| Soft lock without admin | No | No | **Yes (@LOCK)** |
| Travels with the data | No | No | **Yes** |
| Requires broker features | No | Yes | No |
| Usable on filesystem / S3 | No | No | **Yes** |

---

## 8. Named Entries — Recommended Convention

The following `short` tokens are reserved for metadata use:

| Token | Semantics | Full value example |
|---|---|---|
| `@DB` | Target database connection | `mssql://host/DB`, `postgres://host/db` |
| `@MRC` | xzMercury key URL | `https://mercury.internal/keys/<id>` |
| `@SHA` | SHA-256 of compressed+encrypted blob | `sha256:<hex64>` |
| `@SZ` | Uncompressed payload size | `bytes:<n>` |
| `@LOCK` | Pipeline lock state | `status:available` / `status:locked` |
| `@ALG` | Compression+encryption algorithm | `kanzi:7+aes256-gcm`, `zstd:3` |
| `@VER` | Schema version / migration marker | `v3.1.2` |
| `@SRC` | Originating system identifier | `axapta://corp/module/PAYROLL` |
| `@TTL` | Expiry hint (ISO 8601 or seconds) | `2026-12-31T23:59:59Z` |

These tokens follow the same syntax rules as namespace tokens and **may or may not
appear in data rows**. A consumer that does not understand a metadata token should
ignore it (forward-compatible by default).

---

## 9. Use Cases

### 9.1 Medical data (HIPAA / GDPR)

```xml
<Entry short="@MRC"  full="https://mercury.hospital/keys/patient-7a3f"/>
<Entry short="@SHA"  full="sha256:..."/>
<Entry short="@SRC"  full="ehr://hospital-a/cardiology/2026-05-22"/>
```

- Patient data never passes through Mercury — only the key lives there.
- Before decryption: verify SHA, check key availability.
- Mercury can enforce: key only releasable with doctor + patient ACK (Shamir 2-of-2).
- Audit trail: which key was fetched, by whom, when — without logging any PHI.

### 9.2 Financial reporting (insider trading prevention)

```xml
<Entry short="@LOCK" full="status:locked"/>
<Entry short="@TTL"  full="2026-05-22T08:00:00Z"/>
```

- Quarterly results packet prepared in advance, locked.
- `@TTL` signals embargo lift time.
- Consumer checks `@LOCK` → stays in queue.
- At T=08:00, producer sends unlock packet → processing resumes for all consumers
  simultaneously, deterministically.

### 9.3 ERP migration (Axapta / Windchill / Kompas)

```xml
<Entry short="@SRC"  full="axapta://corp/x++/PAYROLL_2009"/>
<Entry short="@DB"   full="mssql://axapta.corp/MAIN"/>
<Entry short="@VER"  full="migration-phase:2-of-4"/>
```

- Migration orchestrator peeks `@VER` to determine processing order.
- Consumers for phase 3 ignore phase 2 packets without consuming them.
- No external state store needed for migration phase tracking.

### 9.4 Dark web / .onion anonymous delivery

```xml
<Entry short="@MRC"  full="http://xzmercury.onion/keys/doc-xyz"/>
<Entry short="@SHA"  full="sha256:..."/>
<Entry short="@LOCK" full="status:available"/>
```

- Packet delivered over Tor: IP-less, identity-less.
- Recipient checks `@SHA` before requesting key (no round-trip on corrupt file).
- Key request also goes through .onion → no IP correlation between data and key.
- `@LOCK` allows the sender to cancel delivery (lock the packet) even after it
  reaches the recipient's queue — they simply never unlock it.

---

## 10. Implementation Status

| Component | Status |
|---|---|
| `Dictionary` struct + XML marshal/unmarshal | ✅ v1.4 |
| `ValidateDictionary()` | ✅ |
| `DictExpander` (O(1) row expansion) | ✅ |
| `Downgrade()` (v1.4 → v1.3.1) | ✅ |
| `tdtpcli --import` auto-expansion | ✅ |
| Pre-flight consumer (peek + check) | ⬜ not yet implemented |
| `@LOCK` consumer support | ⬜ not yet implemented |
| `@SHA` pre-verification in `--import` | ⬜ not yet implemented |
| Named metadata token convention | ⬜ draft (this document) |
| v1.5 spec update | ⬜ pending |

---

## 11. Why This Doesn't Exist Elsewhere

Every existing circuit-breaker (Hystrix, Resilience4j, Polly, go-resilience) is
a **code library** that wraps service calls. Its configuration lives in application
code or config files — separate from the data.

Every existing EMS dead-letter / retry mechanism is a **broker feature** — tied to
one specific broker (RabbitMQ DLX, Kafka DLQ, SQS redrive policy).

The TDTP Dictionary pre-flight pattern is the only approach where:

1. **The circuit-breaking signal travels inside the payload header** — no external
   config, no broker dependency.
2. **The check happens before any side effect** (no key consumed, no DB written,
   no decompression started).
3. **The lock is set by the data producer**, not the infrastructure team.
4. **It works on any transport** including filesystem and dark web overlays.

The closest analogy is an HTTP `Content-Length` + `ETag` header check before
downloading a large body — but generalized to arbitrary dependencies and made
part of a self-describing data format standard.

---

*First described: 2026-05-21, tdtp-framework session*
*Status: architectural proposal, candidate for TDTP v1.5 spec*
