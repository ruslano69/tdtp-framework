# xzmercury

**eXtreme Zero-trust Mercury** — Zero-Knowledge key management + packet
integrity notary for TDTP pipelines.

xzmercury plays two distinct roles for TDTP v1.4 pipelines:

| Role | Namespace | Operation | TDTP version |
|---|---|---|---|
| **Key store** (v1.0+) | `mercury:key:*` | `GETDEL` — burn-on-read | encryption flow |
| **Hash notary** (v1.4) | `mercury:hash:*` | `GET` — read-only, `SET NX` once | integrity flow |

Both roles share the same Redis, same auth, same audit log. They are orthogonal:
the key store enforces **confidentiality**, the hash notary enforces **integrity
+ non-repudiation**.

### Role 1 — Key store (Zero-Knowledge delivery)

xzmercury generates, stores and delivers AES-256 keys using a **burn-on-read**
pattern: each key is retrieved exactly once via `GETDEL` and then deleted from
Redis. The service never persists plaintext data — only the encrypted payload
lives downstream.

Encryption is performed automatically by `FileEncryptor` — a processor in
`pkg/processors/encryption.go` that runs inside the `tdtpcli` ETL pipeline.
Only the **data area** of the TDTP package is encrypted; the structural binary
header (version, algorithm, UUID, nonce) remains in plaintext to allow format
detection without a key.

### Role 2 — Hash notary (TDTP v1.4 packet integrity)

xzmercury acts as a "certificate authority for data packets" implemented with
minimal means: `xxh3_128` fingerprints + Redis `SET NX`. The producer registers
a packet's three-level integrity hash (Schema + Data + Packet fingerprint, all
salted with the packet UUID). The consumer verifies any packet by querying the
notary — a tampered or unregistered packet is blocked at pre-flight.

Unlike key storage, hash records **persist for `HashTTL`** (default 24h) and
survive any number of `Verify` calls — multiple consumers can verify the same
packet independently. Re-registration of the same `{uuid, part}` slot is
**permanently blocked** by `SET NX`, eliminating the attack of overwriting a
legitimate fingerprint with a forged one.

See `docs/xZMercury-TDTP-TZ-v1.2.md` (spec §11) for the full hash-notary model.

```
tdtpcli (ETL pipeline)        xzmercury             Mercury Redis
   │                               │                     │
   │  Processor.Execute()          │                     │
   │  └─ Exporter.exportEncrypted()│                     │
   │     └─ FileEncryptor.Encrypt()│                     │
   │                               │                     │
   │── POST /api/keys/bind ───────>│── LDAP check ──────>│
   │                               │── quota deduct ─────│
   │                               │── SET key TTL=5m ──>│
   │<── {key_b64, hmac, req_id} ───│                     │
   │                               │                     │
   │  [AES-256-GCM encrypt         │                     │
   │   data area, verify HMAC]     │                     │
   │                               │                     │
   ▼  write encrypted blob to file │                     │
                                   │                     │
recipient ─ POST /api/keys/retrieve>│── GETDEL ──────────>│
           <─ {key_b64} ───────────│<── val (key deleted)│
           [AES-256-GCM decrypt]   │                     │
```

### Key-not-found: error receipt instead of silent failure

When `tdtpcli` cannot retrieve a key (HTTP 404 — burned or expired), it does **not**
simply exit with an error code and leave the pipeline with nothing. Instead it writes
a valid TDTP error packet next to the input file:

```
Input:   data/payroll.tdtp.enc        ← encrypted blob (untouched)
Output:  data/payroll_error.tdtp.xml  ← error receipt (auto-generated)
```

The error packet is a valid TDTP XML with `Type="error"` and zero data rows:

```xml
<DataPacket protocol="TDTP" version="1.4">
  <Header>
    <Type>error</Type>
    <TableName></TableName>
    <MessageID>…uuid…</MessageID>
    <Timestamp>2026-06-01T12:11:00Z</Timestamp>
  </Header>
  <Schema/>
  <Data/>
  <AlarmDetails>
    <Severity>error</Severity>
    <Code>KEY_ALREADY_CONSUMED</Code>
    <Message>retrieve key from Mercury (uuid=abc…): KEY_ALREADY_CONSUMED: uuid=abc…</Message>
  </AlarmDetails>
</DataPacket>
```

The receiving side imports this packet the same way it would import data — the
pipeline always gets a receipt, never silence. Error codes match `pkg/mercury`:

| HTTP status | `Code` in error packet | Meaning |
|-------------|------------------------|---------|
| 404 | `KEY_ALREADY_CONSUMED` | Key burned (possibly by attacker) or TTL expired |
| 5xx | `MERCURY_ERROR` | Mercury internal error |
| timeout | `MERCURY_UNAVAILABLE` | Mercury unreachable |

`KEY_ALREADY_CONSUMED` additionally prints a security warning to stderr and should
be treated as a security event: cross-reference with Mercury audit (`consumed_by`,
`consumed_at`) to determine whether the burn was legitimate.

### Hash notary flow (v1.4 integrity)

```
producer (tdtpcli --integrity)    xzmercury           Mercury Redis
   │                                  │                     │
   │  ComputeIntegrity(pkt)            │                     │
   │  ├─ Schema.xxh3 = xxh3_128(UUID‖schema_xml)             │
   │  ├─ Data.xxh3   = xxh3_128(UUID‖rows)                   │
   │  └─ pkt.xxh3    = xxh3_128(Schema.xxh3‖Data.xxh3)       │
   │                                  │                     │
   │── POST /api/hashes ─────────────>│── X-Caller log ────>│
   │     {uuid, part, xxh3, sender}   │── SET NX ──────────>│
   │                                  │   mercury:hash:     │
   │                                  │   {uuid}:{part}     │
   │<── 201 Created ──────────────────│                     │
   │   (or 409 Conflict if slot taken)│                     │
   │                                  │                     │
   ▼  publish packet to broker / S3  │                     │
                                      │                     │
consumer (pipeline.VerifyAndPrepare) │                     │
   │── GET /api/hashes/{uuid}/{part}?xxh3=…──> GET ────────>│
   │<── {registered, match, stored} ──│<── stored value     │
   │                                  │                     │
   │  if !registered → ErrHashNotRegistered → BLOCK         │
   │  if !match      → ErrHashTampered      → BLOCK         │
   │  if match=true  → VerifyIntegrity() locally → proceed  │
```

The key difference from the key flow: **hash records survive** any number of
`Verify` calls until `HashTTL` expires (default 24h). The `SET NX` on register
means a slot, once taken, cannot be overwritten — even after the legitimate
producer's TTL expires, the UUID is globally unique and never reused.

## Quick start — dev mode

```bash
# No external Redis or LDAP required.
# Two in-process miniredis instances + JSON mock LDAP.

cd xzmercury
MERCURY_SERVER_SECRET=dev-secret go run ./cmd/xzmercury/ --dev
```

Verify:
```bash
curl -s http://localhost:3000/healthz
# {"status":"ok"}
```

## Key lifecycle demo (bind → burn-on-read)

Demonstrates xzmercury key issuance and burn-on-read in isolation — no ETL
pipeline, no data loading, no XML generation.

```bash
go run ./xzmercury/test/demo/   # run from repository root
```

Steps (single process, zero external dependencies):

1. Starts xzmercury in-process (miniredis × 2 + mock LDAP)
2. `POST /api/keys/bind` → key issued, stored in Mercury Redis with TTL
3. `out.xml` read as raw bytes, encrypted with AES-256-GCM → `/tmp/out.tdtp`
4. `POST /api/keys/retrieve` → `GETDEL`: key returned **and deleted** from Redis
5. Decrypt `/tmp/out.tdtp`, verify content matches original
6. Second retrieve → HTTP 404 (burn-on-read confirmed)
7. `GET /api/requests/{id}` → state: `consumed`

The key never touches env vars or disk — it lives only as a Go variable between
bind and decrypt, then goes out of scope.

## Building

```bash
cd xzmercury
go build -o xzmercury ./cmd/xzmercury/
```

Production build (disables `DevClient` in `pkg/mercury`):

```bash
go build -tags production -o xzmercury ./cmd/xzmercury/
```

## API

### Key store (v1.0+)

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/healthz` | Liveness probe — always 200 |
| `GET`  | `/readyz` | Readiness — pings both Redis instances |
| `POST` | `/api/keys/bind` | Generate & store AES-256 key |
| `POST` | `/api/keys/retrieve` | Burn-on-read key retrieval (`GETDEL`) |
| `GET`  | `/api/requests/{id}` | Request lifecycle state |

### Hash notary (v1.4)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/hashes` | Register packet xxh3 fingerprint (`SET NX`, 201/409) |
| `GET`  | `/api/hashes/{uuid}/{part}?xxh3=…` | Verify presented hash against stored (read-only, no burn) |
| `DELETE` | `/api/hashes/{uuid}/{part}` | Admin Revoke — invalidate a slot before TTL |

`POST /api/hashes` request body:
```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "part": 0,
  "xxh3": "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5",
  "table": "payroll_q1",
  "sender": "svc-exporter",
  "packet_version": "1.4"
}
```

`GET /api/hashes/{uuid}/{part}` always returns 200 with semantic body:
```json
{
  "registered": true,
  "match": false,
  "stored_xxh3": "a3f8b2c1…",
  "table": "payroll_q1",
  "sender": "svc-exporter",
  "expires_in_seconds": 82394
}
```

| `registered` | `match` | Consumer action |
|---|---|---|
| `true` | `true` | ✅ proceed — packet authentic |
| `true` | `false` | ❌ BLOCK — `ErrHashTampered` |
| `false` | — | ❌ BLOCK — `ErrHashNotRegistered` |

Full reference: [docs/api.md](docs/api.md)

## Configuration

```yaml
# configs/xzmercury.yaml
server:
  addr: ":3000"

mercury:
  addr: "localhost:6379"   # AES keys (RAM-only, no persistence)

pipeline:
  addr: "localhost:6380"   # quota, LDAP cache, request state

key_ttl: 5m                # bound key auto-expires if not retrieved
hash_ttl: 24h              # v1.4 hash notary slot TTL (read-only, survives Verify)

security:
  server_secret: ""        # or set MERCURY_SERVER_SECRET env var

ldap:
  addr: "dc.corp.local:389"
  bind_dn: "cn=svc_xzmercury,ou=service,dc=corp,dc=local"
  base_dn: "dc=corp,dc=local"
  cache_ttl: 120s

quota:
  default_hourly: 1000
  acl_file: "configs/pipeline-acl.yaml"
```

Copy `configs/xzmercury.example.yaml` as starting point.
Full reference: [docs/configuration.md](docs/configuration.md)

## Security

### Key consumption audit

Every successful `POST /api/keys/retrieve` is recorded in Pipeline Redis with full identity:

| Field | Source | Description |
|-------|--------|-------------|
| `consumed_by` | `caller` field in request body | Consumer identity (sAMAccountName, service name) |
| `consumed_at` | server-side `time.Now().UTC()` | Exact burn timestamp |
| `state` | state machine | `approved` → `consumed` |

`caller` is optional but strongly recommended — anonymous burns (`caller=""`) are
distinguishable from TTL expiry (no record at all) but cannot be attributed to a
specific principal.

Three observable states after a key is bound:

| Pipeline Redis state | Meaning |
|---|---|
| `state=consumed, consumed_by=svc_foo` | Legitimate retrieval by `svc_foo` |
| `state=consumed, consumed_by=""` | Anonymous burn — investigate |
| Key record absent (Redis TTL expired) | Key was never retrieved; consumer never ran |

**`tdtpcli` sends its caller identity** via the `TDTPCLI_CALLER` environment variable:
```bash
TDTPCLI_CALLER=svc_tdtp_branch tdtpcli --import data.tdtp.enc --mercury-url http://mercury:3000
```
Pipeline loaders send `source.name` as caller automatically.

### Key store guarantees
- **Privilege guard**: refuses to start as `root` (Linux) or elevated Administrator (Windows)
- **Burn-on-read**: `GETDEL` — key exists in Redis for at most one retrieval
- **HMAC**: every bind response is signed with `HMAC-SHA256(uuid, SERVER_SECRET)`
- **Consumption audit**: every retrieve is stamped with `consumed_by` + `consumed_at` — distinguishes legitimate burn from theft from TTL expiry
- **LDAP cache**: membership results cached 120 s in Pipeline Redis to avoid DC overload
- **TTL**: unread keys auto-expire (`key_ttl`, default 5 min)

### Hash notary guarantees (v1.4)
- **`SET NX` atomicity**: a `{uuid, part}` slot, once registered, cannot be overwritten — ever (until Revoke or TTL expiry)
- **UUID-salted hashes**: each xxh3_128 is computed over `UUID || content`, so a captured hash cannot be replayed for any other packet
- **Three-level fingerprint**: Schema / Data / Packet hashes diagnose *which* part was tampered with — diagnostic separation of accident vs intent
- **Read-only Verify**: the `GET` operation never modifies state, allowing N consumers to verify the same packet independently
- **No deniability**: every `Register` and `Verify` is audited with `X-Caller`, IP, timestamp — see `mercury:audit:hashes:{YYYYMMDD}` (Sorted Set)
- **Cross-version safety**: pre-v1.4 packets pass through without notary checks (backward compatible)

Details: [docs/security.md](docs/security.md)

## CA — environment authorization (`tdtp-ca`)

In production, xZMercury must authorize with the CA before serving any key.
The CA binds a paid **license** (proof of payment) to a specific **hardware
environment** (Ed25519 env-ID from TPM/envkey) and tracks active seats.

```
tdtp-ca (license DB)
   ▲  enroll / authorize  (challenge-response + 2s /hello gate)
   │
xZMercury (prod)
   ├─ BootstrapCA at startup → CASession
   ├─ AutoRenew goroutine (renew 12h before cert expiry)
   └─ caGuard: invalid session → 503 on /api/keys/*
   ▲  bind / retrieve  (only while CA session is live)
   │
orchestrator / tdtpcli
```

### Two phases: enrollment → operational

| Phase | When | Proof required |
|-------|------|----------------|
| **Enroll** | first run | `license_key` + live TPM signature of CA nonce |
| **Authorize** | every restart + every 12h | `cert` + live TPM signature (cert alone is copyable) |

The cert alone is not proof — it is a signed, copyable blob. Authorization always
requires a fresh challenge-response signed by the env private key whose public key
is embedded in the cert. A cloned cert on different hardware cannot sign the nonce.

### Cert TTL = 24h, decoupled from license

- `licenses.paid_until` — commercial period (e.g. 1 year)
- `certs.not_after` — 24h, rolling: every Authorize extends it +24h
- `session_token` — 4h, in-memory in Mercury

`SELECT COUNT(*) FROM certs WHERE last_seen > now-24h AND status='active'`
gives the exact number of **active** environments — not "ever purchased". A stopped
environment disappears from the count within 24h.

### `/hello` DDoS gate

Every enroll/authorize step-1 requires a single-use token from `GET /hello`, which
sleeps 2s before issuing it (max ~30 tokens/min/IP, max 3 concurrent /hello per IP).
Failed step-1 burns the token — a new /hello is required.

### CA administration — `tdtp-certify`

Vendor-only tool. **Not shipped to customers.**

```bash
# One-time: generate CA root key (keep offline / HSM)
tdtp-certify keygen --out ca.ed25519.priv

# Issue a license — prints the raw key once (CA stores only its hash)
tdtp-certify issue-license --db ca.db --licensee "Contoso GmbH" \
    --permissions etl,enc,s3 --seat-limit 3 --expires 2027-06-01

# Inspect
tdtp-certify list-licenses --db ca.db    # seat usage per license
tdtp-certify list-active   --db ca.db    # environments seen in last 24h
tdtp-certify list-certs    --db ca.db    # all certs (active + revoked)

# Revoke
tdtp-certify revoke-cert    --db ca.db --cert-id <uuid>           # frees a seat
tdtp-certify revoke-license --db ca.db --license-key <key>        # kills all certs
```

The license key travels on the wire only at enrollment (under TLS); the CA stores
only `SHA-256(key)`. A leaked hash is useless without the paired TPM env key.

## Repository layout

```
xzmercury/
├── cmd/xzmercury/main.go       entry point (flags, graceful shutdown)
├── configs/
│   ├── xzmercury.example.yaml  full config reference
│   ├── pipeline-acl.example.yaml
│   └── ldap-users.example.json dev LDAP mock users
├── internal/
│   ├── guard/      T3.2 — privilege check (Linux/Windows)
│   ├── ldap/       T3.3 — Client interface, real LDAP, JSON mock, Redis cache
│   ├── keystore/   T3.1 — Bind (crypto/rand AES-256) + BurnOnRead (GETDEL)
│   ├── hashstore/  v1.2 — Register (SET NX) + Verify (GET) + Revoke for v1.4 integrity
│   ├── quota/      T3.3 — atomic hourly credits via Lua script
│   ├── acl/        T3.3 — pipeline-acl.yaml loader
│   ├── request/    T3.3 — job lifecycle (approved/rejected/consumed) + Pub/Sub
│   ├── infra/            Config (YAML) + Setup (dev=miniredis×2 / prod=real)
│   └── api/              chi router, handlers, zerolog middleware
├── test/demo/main.go     in-process E2E demo
└── docs/
    ├── architecture.md
    ├── api.md
    ├── configuration.md
    ├── security.md
    └── deployment.md
```

## Extracting to a standalone repository

The module (`github.com/ruslano69/xzmercury`) has no import dependencies on
`tdtp-framework`. To move it:

```bash
cp -r xzmercury/ ../xzmercury-svc
cd ../xzmercury-svc
git init && git add . && git commit -m "init"
```

Remove the `go.work` file before extracting — it is a workspace convenience
for local development only.

## Specifications

- **v1.1** (Feb 2026) — Zero-Knowledge key delivery + quotas + AD integration
- **v1.2** (May 2026) — adds hash notary (`mercury:hash:*`), three-level xxh3
  integrity, consumer pre-flight pipeline with three fallback policies, and
  Dictionary-as-Dependency-Manifest convention. Document:
  [`../docs/xZMercury-TDTP-TZ-v1.2.md`](../docs/xZMercury-TDTP-TZ-v1.2.md)

## Roadmap

- ✅ **v1.0**: AES-256-GCM key bind + burn-on-read retrieve
- ✅ **v1.1**: quota system, AD/LDAP integration, two-Redis split
- ✅ **v1.2 (current)**: hash notary (`mercury:hash:*`), `pkg/pipeline/VerifyAndPrepare`
- ✅ **v1.3**: key consumption audit (`consumed_by`, `consumed_at`), burn marker
  (theft vs TTL expiry: 410 vs 404), mode-in-HMAC (dev ≠ prod), TDTP error receipt
  on failed decrypt (`*_error.tdtp.xml` with `ServerMode`)
- ✅ **v1.4**: CA environment authorization (`tdtp-ca`) — enroll/authorize with
  TPM challenge-response, 24h rolling cert, seat-count, `/hello` DDoS gate;
  CA admin tool (`tdtp-certify`); prod xZMercury gated on live CA session (503 otherwise)
- ⬜ **v1.5 (planned)**: hash registration quotas (`mercury:hash-quota:*`),
  Dictionary pre-flight (`@SHA`, `@LOCK`, `@TTL` consumer support), SIEM connector
- ⬜ **chiptdtp (separate product)**: proprietary L3 tier with Ed25519 signatures,
  License Authority, ephemeral configs, smart-card auth (see v1.2 §15)
- ⬜ **chiptdtp (separate product)**: proprietary L3 tier with Ed25519 signatures,
  License Authority, ephemeral configs, smart-card auth (see v1.2 §15)
