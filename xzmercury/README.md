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

### Key store guarantees
- **Privilege guard**: refuses to start as `root` (Linux) or elevated Administrator (Windows)
- **Burn-on-read**: `GETDEL` — key exists in Redis for at most one retrieval
- **HMAC**: every bind response is signed with `HMAC-SHA256(uuid, SERVER_SECRET)`
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
- ⬜ **v1.3 (planned)**: hash registration quotas (`mercury:hash-quota:*`),
  Dictionary pre-flight (`@SHA`, `@LOCK`, `@TTL` consumer support), SIEM connector
- ⬜ **chiptdtp (separate product)**: proprietary L3 tier with Ed25519 signatures,
  License Authority, ephemeral configs, smart-card auth (see v1.2 §15)
