# TDTP Framework — Deployment Guide

## Service map

```
┌─────────────────────────────────────────────────────────────────┐
│  PRODUCER / CONSUMER SIDE                                       │
│                                                                 │
│   tdtpcli  ──────────────────────►  xZMercury  :3000           │
│   (export/import/pipeline)          (key service)              │
│              │                           │                      │
│              │                           │  enroll/authorize    │
│              │                           ▼                      │
│   orchestrator :8080             tdtp-ca  :8443                 │
│   (job runner)                   (CA server)                   │
│              │                           │                      │
│              │                    (license DB                   │
│              │                     cert registry)               │
│              ▼                                                  │
│         tdtpcli binary           tdtp-certify  (admin CLI)      │
│         (subprocess)                                            │
└─────────────────────────────────────────────────────────────────┘

Storage:
  xZMercury → Mercury Redis  :6379   (AES keys, RAM-only, TTL)
             → Pipeline Redis :6380  (quota, ACL, request state)
  orchestrator → orchestrator.db     (SQLite: jobs, schedules, tokens)
  tdtp-ca      → ca.db               (SQLite: licenses, certs)
```

---

## Services reference

| Binary | Default port | Purpose |
|--------|-------------|---------|
| `xzmercury` | `:3000` | Zero-knowledge key service for TDTP pipelines |
| `tdtp-ca` | `:8443` | Certificate Authority — issues EnvCerts to xZMercury |
| `tdtp-redis` | `:6379` + `:6380` | In-memory Redis (dev/staging; use real Redis in prod) |
| `orchestrator` | `:8080` | HTTP API wrapper over `tdtpcli --pipeline` |
| `tdtpcli` | — | CLI: export/import/pipeline; called by orchestrator as subprocess |
| `tdtp-certify` | — | Admin CLI: issue licenses, certs, unsafe-op certs |

---

## Minimal setup (local dev)

All services run on localhost without TLS. No real Redis or LDAP required.

### 1. Start in-memory Redis

```bash
tdtp-redis \
  --mercury  127.0.0.1:6379 \
  --pipeline 127.0.0.1:6380
```

Provides two Redis-compatible in-memory servers. State is lost on exit (dev only).

### 2. Generate CA key (one-time)

```bash
openssl genpkey -algorithm ed25519 -out ca.ed25519.priv
openssl pkey -in ca.ed25519.priv -pubout -out ca.ed25519.pub
```

Keep `ca.ed25519.priv` offline or in an HSM in production.

### 3. Start tdtp-ca

```bash
tdtp-ca --db ca.db --key ca.ed25519.priv --addr :8443
```

On first start the DB is empty. Issue a license via `tdtp-certify` before
xZMercury can enroll.

### 4. Issue a license

```bash
tdtp-certify issue-license \
  --key ca.ed25519.priv \
  --to "Acme Corp" \
  --tier enterprise \
  --permissions etl,enc,s3 \
  --paid-until 2027-01-01 \
  --out acme.lic
```

The printed license key (format `TDTP-XXXXXXXX`) is the `ca.license_key` value
for xZMercury's config and must also be distributed as `tdtp.lic` to orchestrator.

### 5. Start xZMercury

Create `configs/xzmercury.yaml` (copy from `xzmercury/configs/xzmercury.example.yaml`):

```yaml
server:
  addr: ":3000"

mercury:
  addr: "127.0.0.1:6379"
pipeline:
  addr: "127.0.0.1:6380"

security:
  server_secret: "change-this-to-32-char-secret!!"

ca:
  url: "http://127.0.0.1:8443"
  license_key: "TDTP-XXXXXXXX"   # from tdtp-certify issue-license
  env_key_dir: "./envkey"
  cert_path: "./env.cert"
```

```bash
xzmercury --config configs/xzmercury.yaml
```

On startup xZMercury enrolls with the CA (generates `envkey/` keypair, requests
EnvCert). After CA authorization, `/api/keys/bind` and `/retrieve` become active.

**Dev shortcut**: `xzmercury --dev` skips CA enrollment, uses miniredis + mock LDAP.

### 6. Start orchestrator

```bash
orchestrator \
  --scenarios ./scenarios \
  --schedules-seed ./schedules \
  --db orchestrator.db \
  --tdtpcli ./tdtpcli \
  --license ./acme.lic \
  --mercury-url http://127.0.0.1:3000 \
  --addr :8080
```

On first run the bootstrap admin token is printed once to stderr. Save it.

---

## Production deployment

### What changes vs dev

| Component | Dev | Production |
|-----------|-----|-----------|
| Redis | `tdtp-redis` in-memory | Redis 7+ daemon, password, persistence |
| LDAP | mock or none | Active Directory / OpenLDAP |
| CA key | local file | HSM or offline signing station |
| TLS | none | TLS on all inter-service traffic |
| xZMercury CA | `--dev` skips | must enroll with `tdtp-ca` |
| orchestrator auth | token or `--no-auth` | token or `--auth-type ldap` |

### Startup order

Services have dependencies. Start in this order:

```
1. Redis (Mercury + Pipeline)
2. tdtp-ca            ← no external deps
3. xzmercury          ← needs Redis + tdtp-ca
4. orchestrator       ← needs tdtpcli binary + (optionally) xzmercury
```

### Air-gapped environments

If orchestrator/xZMercury cannot reach `tdtp-ca` at enrollment time, issue an
**offline cert** instead:

```bash
# On the CA admin machine (has ca.ed25519.priv):
tdtp-certify issue-offline-cert \
  --key ca.ed25519.priv \
  --env-pub /path/to/envkey/env.ed25519.pub \
  --license-key TDTP-XXXXXXXX \
  --not-after 2027-01-01 \
  --permissions etl,enc \
  --out env-offline.cert
```

Copy `env-offline.cert` to the air-gapped machine. xZMercury accepts it via
`POST /api/env/authorize/offline` (no challenge-response, no CA connectivity required).

---

## Orchestrator: authentication modes

### Token auth (default)

```bash
orchestrator --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
```

Bootstrap admin token is printed once on first run:
```
BOOTSTRAP ADMIN TOKEN — store it now, shown once  token=tdtp_...
```

Issue additional tokens:
```bash
# activator scoped to one scenario
curl -X POST http://localhost:8080/tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-runner","role":"activator","scenarios":["export-payroll"]}'
```

### LDAP auth

```bash
orchestrator \
  --auth-type ldap \
  --ldap-url ldap://dc.corp.local:389 \
  --ldap-bind-dn "cn=svc_orch,ou=service,dc=corp,dc=local" \
  --ldap-bind-pass "$LDAP_SVC_PASS" \
  --ldap-base-dn "dc=corp,dc=local" \
  --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
```

Clients authenticate with HTTP Basic Auth (`Authorization: Basic`).
Role is derived from `memberOf` group membership using `LDAPConfig.RoleMap`.
Default role when no group matches: `consumer`.

In LDAP mode `POST /tokens` returns `501` — token management is not available.

---

## Audit log (tdtpcli)

Every operation in tdtpcli that touches nonces (unsafe-op certs, capability
certs) is recorded in the audit log.

| Env var | Default | Purpose |
|---------|---------|---------|
| `TDTP_AUDIT_LOG` | `./tdtp-audit.log` | path to audit log file |
| `TDTP_AUDIT_FORMAT` | `text` | `text` (plain) or `json` (one JSON object per line) |

**JSON format** — for SIEM/ELK/Splunk ingestion:

```bash
TDTP_AUDIT_FORMAT=json tdtpcli --pipeline etl.yaml --unsafe --unsafe-cert op.cert
```

Each line:
```json
{"timestamp":"2026-06-03T10:00:00Z","nonce":"a1b2c3d4","operation":"unsafe-sql","issued_to":"alice","host":"node-01","tdtpcli_version":"1.11.0"}
```

**Syslog** (build tag `syslog`) — compile with:
```bash
go build -tags syslog ./cmd/tdtpcli/
```

Then in code call `license.NewSyslogAuditLog("tdtpcli", syslog.LOG_LOCAL0)`.
Writes JSON entries to syslog, `HasNonce` always returns `false` (syslog is
write-only; replay protection requires a file-backed log alongside it).

---

## Health check endpoints

| Service | Endpoint | Expected |
|---------|----------|---------|
| orchestrator | `GET /healthz` | `{"status":"ok"}` |
| xZMercury | `GET /healthz` | `{"status":"ok"}` or `{"status":"degraded"}` |
| tdtp-ca | _(none; check process)_ | — |

---

## Minimal smoke test after deploy

```bash
# 1. Orchestrator alive
curl http://localhost:8080/healthz

# 2. Auth works
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8080/scenarios

# 3. xZMercury alive (if used)
curl http://localhost:3000/healthz

# 4. Run a scenario
curl -X POST http://localhost:8080/scenarios/export-users/run \
  -H "Authorization: Bearer $ACTIVATOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"params":{"status":"active"}}'
# → {"job_id":"..."}

# 5. Poll job status
curl -H "Authorization: Bearer $ACTIVATOR_TOKEN" \
  http://localhost:8080/jobs/<job_id>
# → {"status":"done", "artifact_path":"...", "artifact_sha256":"...", ...}

# 6. Download artifact
curl -OJ -H "Authorization: Bearer $ACTIVATOR_TOKEN" \
  http://localhost:8080/jobs/<job_id>/artifact
```
