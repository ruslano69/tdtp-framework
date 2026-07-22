# TDTP protocol schema — chronological, by release version

One running document instead of one file per version, so the evolution and
the reasoning behind each change stays visible in one place, tied to the
release version that shipped it. See `docs/SPECIFICATION.md` →
"Версионирование" for the terse changelog form of the same history; this
document is the deep-dive flow/sequence-diagram form, superseding the
now-historical `docs/tdtp-v14-protocol-schema.md` (kept in place,
not deleted, for anyone with existing links to it).

---

## v1.4 (2026-05-26) — Three-level integrity (xxh3_128)

### Participants

```
┌─────────────┐        ┌──────────────────┐        ┌─────────────┐
│  PRODUCER   │        │   xZMercury      │        │  CONSUMER   │
│ (exporter)  │        │ (hash registry)  │        │ (importer)  │
└──────┬──────┘        └────────┬─────────┘        └──────┬──────┘
       │                        │                          │
       │     ◄── Redis ──────── │                          │
       │     mercury:hash:      │                          │
       │       {uuid}:{part}    │                          │
       │     (SET NX, TTL 24h)  │                          │
```

### Producer: packet preparation

```
┌──────────────────────────────────────────────────────────────────┐
│  1. GenerateReference(schema, rows)                              │
│     → DataPacket{Version:"1.4", Header.MessageID: uuid4}        │
│                                                                  │
│  2. ComputeIntegrity(pkt)                                        │
│     → Schema.xxh3  = xxh3_128(UUID || Schema_bytes)             │
│     → Data.xxh3    = xxh3_128(UUID || row_bytes)                │
│     → pkt.xxh3     = xxh3_128(schema_xxh3 + "|" + data_xxh3)   │
│                                                                  │
│  3. mercury.RegisterHash(uuid, part, pkt.xxh3, table, sender)   │
│     POST /api/hashes                                             │
│     → Mercury: SET NX mercury:hash:{uuid}:{part}                │
│     → 201 Created  ✓                                            │
│     → 409 Conflict ✗ (slot taken — an attacker got there first, │
│                        LOG+ALERT)                                │
│                                                                  │
│  4. Send packet to queue / S3 / broker                          │
└──────────────────────────────────────────────────────────────────┘
```

Packet header:
```xml
<DataPacket protocol="TDTP" version="1.4"
            xxh3="a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5">
  <Header>
    <MessageID>550e8400-e29b-41d4-a716-446655440000</MessageID>
    <PartNumber>0</PartNumber>
    <TableName>payroll_q1</TableName>
    ...
  </Header>
  <Schema xxh3="1122334455667788aabbccddeeff0011">
    <Field name="id" type="INTEGER" key="true"/>
    <Field name="ns" type="TEXT"/>
    <Dictionary>
      <Entry short="@W3" full="http://www.w3.org/2000/svg"/>
    </Dictionary>
  </Schema>
  <Data xxh3="ffeeddccbbaa99887766554433221100" compression="zstd">
    <R>1|@W3</R>
    <R>2|plain</R>
  </Data>
</DataPacket>
```

### Consumer: pre-flight → processing

```
receive packet
     │
     ▼
┌─────────────────────────────────────────────────────────────────┐
│  pipeline.VerifyAndPrepare(ctx, pkt, mercuryClient, policy)     │
└──────────────────────────┬──────────────────────────────────────┘
                           │
              pkt.Version == "1.4" ?
              NO  ──────────────────────────────► legacy pass-through
              YES ↓
                           │
          ┌────────────────▼──────────────────────────────────┐
          │  STEP 1: Mercury executor check                   │
          │  GET /api/hashes/{uuid}/{part}?xxh3={pkt.xxh3}   │
          └────────────────┬──────────────────────────────────┘
                           │
          ┌────────────────┼───────────────────────────────────┐
          │                │                                    │
    registered=true  registered=false              Mercury unavailable
    match=true       (slot not found)               (ErrMercuryUnavailable)
          │                │                                    │
          │       ErrHashNotRegistered              ┌───────────┴──────────────┐
          │          BLOCK + LOG ✗           policy=Block  policy=Degrade  policy=Downgrade
          │                                     │         │               │
          │                              BLOCK + LOG ✗  warn,       Downgrade(pkt)
          │                                            continue     → v1.3.1 path
          │                                                │               │
          ◄───────────────────────────────────────────────┘               │
          │  Degraded=true, DegradedReason="Mercury unavailable"          │
          │                                                                │
   match=false                                                             │
   (stored_xxh3 ≠ pkt.xxh3)                                               │
   ErrHashTampered                                                         │
   BLOCK + LOG ✗                                                           │
                                                                           │
          │                                                                │
          ▼                                                                │
┌─────────────────────────────────────────────┐                           │
│  STEP 2: Local xxh3 integrity               │                           │
│  packet.VerifyIntegrity(pkt)               │                           │
│  recompute xxh3_128(UUID||schema)           │                           │
│  recompute xxh3_128(UUID||rows)             │                           │
│  compare with pkt.Schema.xxh3, Data.xxh3   │                           │
│  → mismatch: BLOCK + LOG ✗                 │                           │
└──────────────────┬──────────────────────────┘                           │
                   │                                                       │
          ┌────────▼───────────────────────────────────────────────┐      │
          │  STEP 3: Dictionary expansion                          │      │
          │  NewDictExpander(pkt.Schema.Dictionary)                │      │
          │  for each row: ExpandRow("1|@W3") → "1|http://..."    │      │
          │  pkt.Schema.Dictionary = nil (downstream sees plain)   │      │
          └────────┬───────────────────────────────────────────────┘      │
                   │                                                       │
                   ▼                                                       ▼
          VerifyResult{                                         VerifyResult{
            Version:    "1.4",                                   Version:    "1.3.1",
            Degraded:   false,                                   Degraded:   true,
            MercuryRec: {table, sender, ...},                    DegradedReason: "...",
          }                                                    }
                   │                                                       │
                   └───────────────────┬───────────────────────────────────┘
                                       │
                                       ▼
                              DB write / adapter
```

### Three fallback policies

| Policy | Mercury unavailable | Security | Availability |
|---|---|---|---|
| `FallbackBlock` | Block, error | ★★★ | ★ |
| `FallbackDegrade` | Continue, local xxh3 only | ★★ | ★★★ |
| `FallbackDowngrade` | Convert to v1.3.1 in-place | ★ | ★★★ |

**Policy choice guidance:**

```yaml
# Financial reports, medical data, legally significant documents:
fallback_policy: block        # no Mercury = no data

# Operational data requiring continuity (logs, metrics):
fallback_policy: degrade      # local integrity still guaranteed

# Integration with legacy v1.3.1-only systems:
fallback_policy: downgrade    # automatic version rollback
```

### What each level checks

```
Level 1: Mercury (executor control)
  ✓ Packet registered by an authenticated producer
  ✓ UUID+part → stored_xxh3 == pkt.xxh3 (not swapped post-registration)
  ✓ Re-registration blocked (SET NX)
  ✗ Does not protect: if Mercury is unavailable

Level 2: Local xxh3_128 (integrity)
  ✓ Schema unchanged (fields, types, Dictionary)
  ✓ Data rows unchanged
  ✓ UUID used as salt — hash is unique per packet
  ✗ Does not protect: an attacker who knows the algorithm and UUID (both public)

Level 3: Dictionary expansion (transparency)
  ✓ @tokens replaced with full values before DB write
  ✓ Downstream system only ever sees plain values
  ✓ Backward compatible with pre-v1.4 adapters

Data.checksum (legacy, v1.3.1+):
  ✓ xxh3_64 of the compressed blob — protects against corrupted compression
  ✗ Does not replace levels 1-2
```

### Pre-v1.4 packets — unchanged

```
v1.0 / v1.3.1 packet
     │
     ▼
VerifyAndPrepare(pkt, ...)
     │
pkt.Version != "1.4"
     │
     ▼
pass-through (return immediately)
     │
     ▼
DB write / adapter
```

None of the new checks run. Behavior is identical to earlier versions.

### Code usage

```go
// PRODUCER
pkt, _ := gen.GenerateReference("payroll_q1", schema, rows)
packet.ComputeIntegrity(pkt)
mercuryClient.RegisterHash(ctx,
    pkt.Header.MessageID, pkt.Header.PartNumber,
    pkt.XXH3, pkt.Header.TableName, "svc-exporter", pkt.Version)
broker.Publish(pkt)

// CONSUMER
received := broker.Consume()
result, err := pipeline.VerifyAndPrepare(ctx, received, mercuryClient, pipeline.FallbackDegrade)
if err != nil {
    log.Error().Err(err).
        Str("uuid", received.Header.MessageID).
        Msg("BLOCK: packet integrity check failed")
    broker.Nack(received)
    return
}
if result.Degraded {
    log.Warn().Str("reason", result.DegradedReason).Msg("degraded mode")
}
// received is now ready for a DB write:
// - Dictionary expanded
// - Version may be "1.3.1" if FallbackDowngrade was applied
adapter.Write(received)
broker.Ack(received)
```

### Attacks and defenses

| Attack | Defense |
|---|---|
| Modify data rows | Level 2: Data.xxh3 won't match |
| Modify schema (field/type) | Level 2: Schema.xxh3 won't match |
| Update pkt.xxh3 to match new content | Level 1: stored_xxh3 in Mercury ≠ pkt.xxh3 |
| Pre-register a fake entry in Mercury | SET NX: slot already taken by the producer |
| DDoS Mercury to bypass verification | FallbackBlock: no Mercury = no data |
| Replay: resend an old packet | UUID is unique; Level 1 returns the stored hash |
| Modify only the Dictionary | Level 2: Schema.xxh3 includes Dictionary bytes |

---

## v1.5 (planned) — Section-level encryption, not whole-packet

### Why this version exists

Found while designing the `examples/travel-agency` orchestrator-governed
showcase (see root `TODO_NEXT.md` → "Encryption format redesign" for the
original investigation): v1.3 introduced encryption
(`cmd/tdtpcli/commands/encrypt.go`) as a **whole-packet binary envelope** —
serialize the entire XML, wrap it in
`[2B ver][1B algo][16B uuid][12B nonce][ciphertext]`. That's not XML at
all: no `<DataPacket>`, nothing readable without the key, including
transport-layer routing metadata that doesn't need to be secret.

Compression (v1.2+) never had this problem: `<Header>`/`<Schema>` stay
plain XML, only `<Data>`'s rows collapse into one opaque value with a
`Compression="zstd"` marker attribute. A parser can always read
Header/Schema without decompressing anything. v1.5 brings encryption to
the same shape, for the same reason: **the packet should always parse as
valid XML**, with only the genuinely sensitive sections turned opaque.

Concretely, the whole-blob approach is what blocks
`cmd/tdtpcli/commands/broker.go`'s `--export-broker` from ever supporting
encryption — a raw binary blob can't flow through the broker import path's
XML parser (`ParseBytesWithDecompression`) the way a
compressed-but-still-XML packet can.

### Design: encrypt QueryContext, Schema, and Data — never Header

```xml
<DataPacket protocol="TDTP" version="1.5">
  <Header>...</Header>                                                    <!-- stays plain: routing/dedup/part-reassembly need no key -->
  <QueryContext encryption="aes-256-gcm">BASE64(nonce||ciphertext)</QueryContext>  <!-- was: filter conditions, business logic -->
  <Schema encryption="aes-256-gcm">BASE64(nonce||ciphertext)</Schema>              <!-- was: field names/types -->
  <Data compression="zstd" encryption="aes-256-gcm">
    <R>BASE64(nonce||ciphertext)</R>                                       <!-- same opaque-row shape compression already uses -->
  </Data>
</DataPacket>
```

**Why `Header` stays plain:** the transport layer needs *something* to
route/dedup/reassemble multi-part packets on without a key — same
reasoning as a broker queue name or a `pkg/resultlog` `result_name` not
being secret either. `TableName` is included in that trade-off; it's
metadata every existing transport layer (queue naming, `result_log`
channel naming) already exposes today.

**Why `QueryContext` and `Schema`, specifically, join `Data`:** these are
the two places that leak real information without ever touching row
values — which filter conditions were interesting enough to select on
(business logic, e.g. `balance >= 1000`) and which fields/types exist at
all (structure). Leaving them exposed while only encrypting `Data` would
be a partial fix that misses exactly the metadata an attacker would want
most.

**One key, per-section nonces, not per-section keys.** A single
`POST /api/keys/bind` call at export time (keyed by `Header.MessageID`,
same convention v1.3 already uses) returns one AES-256 key covering the
whole packet. Each of the three sections is encrypted with that same key
but its **own** 12-byte nonce (AES-GCM requires a unique nonce per
encryption under the same key — reusing one across sections would be a
real confidentiality break). The nonce travels inline with each section's
ciphertext (`BASE64(nonce || ciphertext)`), so only the *key* needs a
Mercury round-trip, not three. Consumer does exactly one
`POST /api/keys/retrieve` (burn-on-read) per packet, then decrypts all
three sections locally with the one retrieved key.

### Producer: packet preparation

```
┌──────────────────────────────────────────────────────────────────┐
│  1. GenerateReference(schema, rows) → DataPacket{Version:"1.5"}  │
│                                                                  │
│  2. mercury.BindKey(ctx, packageUUID)                            │
│     POST /api/keys/bind                                          │
│     → xZMercury: generate AES-256 key, HMAC, TTL                │
│     → { key_b64, hmac, mode }                                    │
│                                                                  │
│  3. For each of QueryContext, Schema, Data:                      │
│     nonce := random(12 bytes)                                    │
│     ciphertext := AES-256-GCM-Seal(key, nonce, section_xml_bytes)│
│     section.encryption = "aes-256-gcm"                           │
│     section.text = base64(nonce || ciphertext)                   │
│     (Header untouched)                                           │
│                                                                  │
│  4. Send still-valid-XML packet to queue / file / S3             │
└──────────────────────────────────────────────────────────────────┘
```

### Consumer: dual-format detection, then selective decrypt

```
receive raw bytes
     │
     ▼
┌─────────────────────────────────────────────────────────────────┐
│  PRE-FLIGHT: which encryption shape is this?                     │
│  IsEncryptedBlob(raw)?  (binary header magic, v1.0-v1.4 shape)  │
└──────────────────────┬──────────────────────────────────────────┘
          YES ──────────┤                          NO
          │              │                           │
          ▼              │                           ▼
  DecryptEncBlob(raw)     │                  parse as XML directly
  (existing v1.3 path,    │                  (Header always readable,
   unchanged — old        │                   whole packet or not)
   packets keep working)  │                           │
          │              │                           │
          └──────────────┴───────────────────────────┘
                          │
                          ▼
                 got a parsed DataPacket
                          │
          ┌───────────────┴────────────────────────────┐
          │  any of QueryContext/Schema/Data carry       │
          │  encryption="aes-256-gcm"?                   │
          └───────────────┬────────────────────────────┘
               NO ─────────┤                    YES
               │            │                     │
               ▼            │                     ▼
          plain packet,     │        mercury.RetrieveKey(ctx, packageUUID)
          proceed as-is     │        POST /api/keys/retrieve (burn-on-read)
                            │                     │
                            │        ┌────────────┼─────────────────┐
                            │        │            │                  │
                            │   200 OK       410 KEY_BURNED     404 KEY_EXPIRED
                            │   {key_b64}    (already consumed) (TTL elapsed /
                            │        │        BLOCK + LOG ✗      never existed)
                            │        │                            BLOCK + LOG ✗
                            │        ▼
                            │  for each encrypted section:
                            │    (nonce, ciphertext) := split(base64_decode(section.text))
                            │    plaintext := AES-256-GCM-Open(key, nonce, ciphertext)
                            │    section.text = plaintext; section.encryption = ""
                            │        │
                            └────────┴──────────────────┐
                                                          ▼
                                                 DB write / adapter
```

### xZMercury pairing — verified zero server-side changes required

This redesign only ever changes what happens **client-side**, inside
`tdtpcli`, between `BindKey`/`RetrieveKey`. Verified directly against
xZMercury's implementation, not assumed:

- `xzmercury/internal/keystore/store.go` `Bind(ctx, uuid, pipelineName)`
  generates 32 random bytes, stores them base64-encoded in Redis under
  `mercury:key:{uuid}` with a TTL. It has no concept of how many times, or
  in what shape, that key will be used to encrypt anything downstream.
- `BurnOnRead(ctx, uuid)` atomically reads-and-deletes the same Redis key
  via a Lua script, keyed only by `uuid`. Same story: opaque bytes in,
  opaque bytes out.
- `xzmercury/internal/api/keys.go`'s `Bind`/`Retrieve` HTTP handlers
  (ACL/LDAP check → quota check → `store.Bind`/`store.BurnOnRead`) carry no
  encryption-shape assumption either — the request/response contract is
  `{package_uuid, pipeline_name} → {key_b64, hmac, mode}` and
  `{package_uuid, caller} → {key_b64}`, unchanged from v1.3.

Consequence: v1.3's "one key, one `Seal` call" and v1.5's "one key, three
`Seal` calls (QueryContext/Schema/Data), one nonce each" are both just
different **client-side** uses of the exact same bind/retrieve contract.
`pkg/mercury/client.go`'s `BindKey`/`RetrieveKey` need no signature change.
The pairing this section set out to confirm already holds today — nothing
in xZMercury blocks or needs to anticipate v1.5; only `pkg/crypto`,
`pkg/core/packet`, and the `cmd/tdtpcli/commands/*` call sites change.

### No graceful degrade — this is the one real asymmetry with v1.4

v1.4's integrity checks have three fallback tiers (Block / Degrade /
Downgrade) because local xxh3 verification can substitute for the
Mercury-side check when Mercury is down. Encryption has no equivalent: the
AES key **only exists in xZMercury's RAM-only key store** (that's the
entire point of burn-on-read) — there is no local-key fallback mode, for
either direction. If Mercury is unreachable, decryption cannot proceed by
definition, not by policy choice.

| Policy | Mercury unavailable | Note |
|---|---|---|
| (only mode) | Block, error | No `Degrade`/`Downgrade` equivalent exists — nothing to degrade *to* without the key |

### Backward compatibility — this is additive, not a replacement

`IsEncryptedBlob`/`DecryptEncBlob` (`cmd/tdtpcli/commands/encrypt.go`)
already have real, working callers on the current whole-blob format:

- `cmd/tdtpcli/commands/import.go:127` (`--import`)
- `cmd/tdtpcli/commands/map.go:163,338` (`--map`, including the `--listen`
  daemon path — this is what `examples/travel-agency/consumer.py` calls
  today via `--map --input broker://queue`)

v1.5 packets are a **second, additive** detection branch in both call
sites, not a replacement of the first. Old packets (whole-blob, v1.0-v1.4)
keep decrypting exactly as they do today; new packets (v1.5,
section-level) take the new path. Nothing currently working regresses.

### CLI flag naming — `--enc` moves to v1.5, `--enc13` keeps the old format

Encode-side (unlike decode-side detection above) needs an explicit choice —
nothing to auto-detect before a packet exists yet. Current flag, unchanged
since its introduction: `--enc` (`cmd/tdtpcli/flags.go:241`,
`PipelineOptions.Encrypt` in `pipeline.go`) triggers whole-packet
`EncryptPacket` (`encrypt.go:156`). That whole-blob format is what v1.3
introduced — v1.4 added integrity hashing (xxh3), not a new encryption
shape, so the old format is correctly named after **1.3**, not 1.4.

- **`--enc`** (bare, name unchanged) — now means "encrypt with the current
  default format." Once v1.5 lands, that default becomes v1.5's
  section-level encryption. This is a **behavior change on an existing
  flag name**, not a new flag — anyone scripting `--enc` today gets v1.5
  output the moment this ships, without touching their command line.
- **`--enc13`** (new flag) — explicitly requests the legacy v1.3
  whole-packet blob format, for producers that must interoperate with a
  consumer that only understands the old shape (e.g. not yet upgraded, or
  a third-party integration built against `IsEncryptedBlob`/
  `DecryptEncBlob` directly). Maps to the exact same `EncryptPacket` call
  `--enc` makes today — no behavior change to that path, just a new name
  that also happens to be the honest one.
- **`--enc-dev`** stays orthogonal to both — it swaps the key *source*
  (local `DevClient` instead of live xZMercury), not the wire *format*.
  Combines with either `--enc` or `--enc13`.

Decode side needs no equivalent flag: `IsEncryptedBlob` vs. the new
`encryption="aes-256-gcm"` attribute check (see "Consumer: dual-format
detection" above) already disambiguates the two shapes automatically from
the bytes on the wire — a reader never needs to be told in advance which
format it's about to see.

### Wire-transform order — hash, then compress, then encrypt (fixed, no policy)

Verified against `pkg/core/packet/integrity.go`'s `computeHashes`: v1.4's
xxh3 is computed over **raw plaintext** row values and the uncompressed
`Schema`, explicitly "before compression" per its own comment. That fixes
the full order for a packet carrying all three features at once, and it
never varies — there is no configuration knob for this, anywhere:

- **Write:** `ComputeIntegrity` (stamps `xxh3` attrs on `Schema`/`Data`
  from plaintext) → compress `Data`'s rows → encrypt `QueryContext`/
  `Schema`/`Data` content. The `xxh3`/`compression` attributes stay on the
  element (outside the encrypted text node) exactly like `compression`
  already does today — only the section's *content* goes opaque, never
  its attributes.
- **Read:** decrypt → decompress → recompute xxh3 over the recovered
  plaintext → compare to the (never-encrypted) `xxh3` attribute.

Encrypting first would make compression pointless (ciphertext has
maximal entropy, doesn't compress) and would make the Mercury hash
registry check integrity of ciphertext instead of content — neither is
ever correct, so this isn't a per-deployment choice.

### Multi-part packets — each part already has its own `MessageID`, so nothing special is needed

`EncryptPacket` (`encrypt.go:156`) today generates a **fresh random UUID
per call**, unrelated to `Header.MessageID`, and that UUID travels inside
the binary blob's own header — readable only after nothing needs
decrypting first, since the whole blob is opaque anyway. v1.5 cannot reuse
that: `Header` stays plain specifically so a consumer can route without a
key, which only works if the UUID passed to `RetrieveKey` is something
readable *before* decryption — i.e. `Header.MessageID` itself, not a
separate generated UUID smuggled inside ciphertext.

**Corrected after checking `GenerateReference` directly** (an earlier draft
of this section wrongly assumed multi-part packets share one `MessageID`
across parts — they don't): `generateMessageID` produces one base ID per
call (e.g. `REF-2026-a1b2c3d4`), and each part gets its own **distinct**
`Header.MessageID` built from it — `fmt.Sprintf("%s-P%d", messageIDBase,
i+1)`, i.e. `REF-2026-a1b2c3d4-P1`, `-P2`, etc. (`generator.go`'s
`GenerateReference`). Every part therefore already binds under a different
Redis key (`mercury:key:{uuid}` keyed by the *full* MessageID string,
suffix included) — no shared identifier, no `keystore.Bind` overwrite
race, no special "bind once before generating parts" handling required.
**`BindKey` simply happens once per part, exactly where legacy `--enc13`
already calls it once per part today** (`pkg/etl/exporter.go`'s
`exportToTDTP` loop) — v1.5 slots into the same per-part call site, just
keyed by `part.Header.MessageID` instead of a separately generated UUID.

Consumer-side mirrors this: multi-part reassembly (`import.go`'s
`validateMultiPartSession`) already waits for every part file to arrive
before processing; each part is decrypted independently with its own
`RetrieveKey(part.Header.MessageID)` call, not one shared retrieval.

Separately, worth flagging (not a v1.5 concern, pre-existing behavior):
legacy `--enc13` multi-part export reuses **one** Exporter-wide
`packageUUID` (unrelated to any part's `MessageID`, set once outside the
per-part loop) for every part's `BindKey` call — repeated binds under the
same UUID against `keystore.Bind`'s plain `SET` *would* have exactly the
overwrite race described above, if multi-part `--enc13` export is ever
exercised. Out of scope here; noted for whoever next touches that path.

### `Schema`/`QueryContext` need one new struct field each; `Data` needs none

Checked directly against `pkg/core/packet/types.go` and `query.go`:

- **`Data`** already has `Rows []Row` where `Row.Value` is
  `xml:",chardata"` — the exact shape compression already exploits
  (`<Data compression="zstd"><R>OPAQUE</R></Data>`). Encryption reuses
  this unchanged: one `<R>` holding `base64(nonce||ciphertext)`, no struct
  change.
- **`Schema`** (`Fields []Field \`xml:"Field"\``) and **`QueryContext`**
  (`OriginalQuery Query`, `ExecutionResults ExecutionResults`, no chardata
  field at all) have no field to hold text content. Unmarshaling
  `<Schema encryption="aes-256-gcm">BASE64</Schema>` into today's struct
  silently succeeds with `Fields == nil` — indistinguishable from a
  genuinely empty schema, not an error, which is the dangerous part: any
  code touching `pkt.Schema.Fields` before checking the `encryption`
  attribute would misread "encrypted" as "empty" with no warning.
  **Fix:** add `Encrypted string \`xml:",chardata"\`` to both structs,
  guarded everywhere by checking a new `Encryption string
  \`xml:"encryption,attr,omitempty"\`` field first. Additive and harmless
  for existing unencrypted packets — chardata around child elements is
  just inter-tag whitespace there, already ignored.

### Streaming export — out of scope for the first v1.5 landing

`pkg/core/packet/streaming.go`'s `StreamingGenerator` isn't wired to any
CLI flag today (`TODO_NEXT.md`'s v2.0 roadmap: "`--export-stream`... code
ready, not connected to CLI"). Nothing currently produces an encrypted
*or* compressed streaming packet, so v1.5 introduces no regression by not
touching it. Revisit only if/when streaming CLI wiring itself is scheduled
— not part of this feature.

### Error reporting on key-retrieval failure — reuse v1.3's path unchanged

`RetrieveKey`'s failure modes (`ErrMercuryUnavailable`, `ErrKeyExpired`,
`KeyBurnedError`, ...) are identical between v1.3 and v1.5 — same Mercury
API, same `mercury.ErrorCode()` mapping, same `encrypt.go` `WriteErrorPacket`
helper. Nothing new to design here: v1.5's import path calls the same
error-packet writer v1.3's already does, with the same error codes.

### What's protected vs. what isn't

```
Protected (encryption="aes-256-gcm"):
  ✓ QueryContext — filter conditions, business logic behind the query
  ✓ Schema — field names, types, table structure
  ✓ Data — actual row values

NOT protected (Header always plain):
  ✗ MessageID, PartNumber, TableName, Timestamp, Sender
  — needed by transport (routing, dedup, multi-part reassembly) without a key.
    Same trade-off as a broker queue name or a pkg/resultlog result_name
    already not being secret. If TableName itself is considered sensitive
    metadata, this format is insufficient — that's a conscious limit of
    this design, not an oversight, and would need a different approach
    (e.g. opaque queue/channel naming upstream of TDTP) to close.
```

### Attacks and defenses

| Attack | Defense |
|---|---|
| Read row values off the wire without a key | `Data` ciphertext — AES-256-GCM, key never leaves xZMercury unencrypted |
| Infer schema/field names without a key | `Schema` ciphertext — same as Data |
| Infer what was queried (business logic) without a key | `QueryContext` ciphertext — same as Data |
| Replay: read the same packet's data twice | Burn-on-read: second `RetrieveKey` call returns `410 KEY_BURNED_BY_OTHER` |
| Nonce reuse across sections under the same key | Each section gets its own random 12-byte nonce — never shared |
| Route/process the packet without decrypting anything | Not an attack — this is intended: `Header` is deliberately readable for exactly this |
