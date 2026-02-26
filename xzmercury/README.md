# xzmercury

Zero-Knowledge key management microservice for TDTP pipelines.

xzmercury generates, stores and delivers AES-256 keys using a **burn-on-read**
pattern: each key is retrieved exactly once via `GETDEL` and then deleted from
Redis. The service never persists plaintext data — only the encrypted payload
lives downstream.

Encryption is performed automatically by `FileEncryptor` — a processor in
`pkg/processors/encryption.go` that runs inside the `tdtpcli` ETL pipeline.
Only the **data area** of the TDTP package is encrypted; the structural binary
header (version, algorithm, UUID, nonce) remains in plaintext to allow format
detection without a key.

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

## E2E demo

From the repository root (requires `go.work`):

```bash
go run ./xzmercury/test/demo/
```

Output shows: bind → AES-256-GCM encrypt (data area) → write blob →
burn-on-read retrieve → decrypt → verify.

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

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/healthz` | Liveness probe — always 200 |
| `GET`  | `/readyz` | Readiness — pings both Redis instances |
| `POST` | `/api/keys/bind` | Generate & store AES-256 key |
| `POST` | `/api/keys/retrieve` | Burn-on-read key retrieval |
| `GET`  | `/api/requests/{id}` | Request lifecycle state |

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

- **Privilege guard**: refuses to start as `root` (Linux) or elevated Administrator (Windows)
- **Burn-on-read**: `GETDEL` — key exists in Redis for at most one retrieval
- **HMAC**: every bind response is signed with `HMAC-SHA256(uuid, SERVER_SECRET)`
- **LDAP cache**: membership results cached 120 s in Pipeline Redis to avoid DC overload
- **TTL**: unread keys auto-expire (`key_ttl`, default 5 min)

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
