# TODO NEXT: Commercial Licensing & Capability Certificate System

## Context

TDTP Framework is production-ready for system integrators (ERP migrations, ETL pipelines,
regulated data exchange). The next step is a **two-tier licensing model** inspired by
Axapta/Dynamics partner licensing: a persistent license key that defines the adapter
matrix + feature set, and short-lived capability certificates that authorize sensitive
operations (--unsafe SQL, custom DDL, cross-schema writes).

---

## Tier 1 — `tdtp.lic`: Persistent License File

Signed JSON, delivered once at purchase. Defines **what the runtime can do**.

```json
{
  "licensee":  "Contoso Integration GmbH",
  "issued":    "2026-06-01",
  "expires":   "2027-06-01",
  "tier":      "professional",
  "adapters":  ["postgres", "mssql", "mysql"],
  "features":  ["etl", "enc", "s3", "svg"],
  "limits": {
    "rows_per_export": 1000000,
    "pipelines":       10
  },
  "signature": "<Ed25519 base64>"
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `tier` | string | `community` / `professional` / `enterprise` |
| `adapters` | []string | Allowed DB adapters. Community: `sqlite` only |
| `features` | []string | Feature flags: `etl`, `unsafe`, `enc`, `s3`, `svg` |
| `limits.rows_per_export` | int | 0 = unlimited (enterprise) |
| `limits.pipelines` | int | Max concurrent pipelines |
| `signature` | base64 | Ed25519 over canonical JSON (sorted keys, no whitespace) |

### Commercial Tiers

| Tier | Adapters | Features | Row limit | Pipelines | Target |
|------|----------|----------|-----------|-----------|--------|
| Community | SQLite | core | 50 000 | 1 | OSS / eval |
| Professional | + Postgres, MySQL | + ETL, ENC, S3 | 1 000 000 | 10 | SMB integrators |
| Enterprise | All (+ MSSQL, Access, MSMQ, Kafka) | All incl. `unsafe` | unlimited | unlimited | ERP consultancies |

### Enforcement Points

```
cmd/tdtpcli/commands/export.go     → check rows_per_export before flush
cmd/tdtpcli/commands/import.go     → check adapter allowed
pkg/adapters/factory.go            → Register() checks license at init
pkg/brokers/kafka.go               → gated on features["etl"]
commands/encrypt.go                → gated on features["enc"]
pkg/storage/s3/                    → gated on features["s3"]
pkg/svg/                           → gated on features["svg"]
```

---

## Tier 2 — `unsafe-op.cert`: Capability Certificate

Signed JSON, issued per-operation by a CA tool (`tdtp-certify`). Short TTL.
Required in addition to `tdtp.lic` whenever `--unsafe` is used.

```json
{
  "issued_to":  "admin@contoso.com",
  "operation":  "unsafe-sql",
  "scope": {
    "tables":   ["[ZTR$Employee]", "[ZTR$Ledger]"],
    "database": "navision_prod"
  },
  "issued_at":  "2026-06-15T10:00:00Z",
  "expires":    "2026-06-15T18:00:00Z",
  "host_lock":  "WORKSTATION-07",
  "nonce":      "8f3a1c...",
  "signature":  "<Ed25519 base64>"
}
```

### Capability Fields

| Field | Description |
|-------|-------------|
| `operation` | `unsafe-sql`, `schema-write`, `cross-schema`, `drop-allowed` |
| `scope.tables` | Whitelist of table patterns the cert covers |
| `scope.database` | DB name lock (prevents cert reuse on other DBs) |
| `expires` | Short TTL (hours, not days). No renewal without new issuance. |
| `host_lock` | Hostname or FQDN the cert is bound to |
| `nonce` | Prevents replay. Stored in audit log after first use. |

### --unsafe Enforcement Flow

```
tdtpcli --unsafe --config ... --export [ZTR$Employee]
  │
  ├─ 1. Load tdtp.lic  →  verify Ed25519 signature (Anthropic CA pubkey embedded)
  │      └─ check features["unsafe"] present
  │
  ├─ 2. Load unsafe-op.cert  →  verify Ed25519 signature (same or partner CA)
  │      ├─ check expires > now()
  │      ├─ check host_lock == os.Hostname()
  │      ├─ check scope.tables covers requested table
  │      └─ check nonce not in audit log (replay protection)
  │
  ├─ 3. Record nonce + timestamp in audit log
  │
  └─ 4. Execute --unsafe operation
```

Current `IsAdmin()` OS-level check becomes a **third factor** (belt-and-suspenders),
not the primary gate.

---

## CA Tooling: `tdtp-certify`

Standalone CLI for license issuance and capability certificate signing.
**Not shipped in the framework** — operator/vendor tool.

```bash
# Issue license
tdtp-certify issue-license \
  --licensee "Contoso GmbH" \
  --tier professional \
  --adapters postgres,mysql \
  --features etl,enc,s3 \
  --expires 2027-06-01 \
  --key vendor.ed25519.priv \
  --out contoso.lic

# Issue capability cert
tdtp-certify issue-cert \
  --to admin@contoso.com \
  --op unsafe-sql \
  --tables "[ZTR$Employee],[ZTR$Ledger]" \
  --db navision_prod \
  --host WORKSTATION-07 \
  --ttl 8h \
  --key vendor.ed25519.priv \
  --out unsafe-today.cert
```

---

## Implementation Plan

### Phase 1 — Core license verification (no --unsafe changes yet)

```
pkg/license/
├── license.go      # LicenseFile struct, Load(), Verify()
├── pubkey.go       # embedded vendor Ed25519 public key
└── license_test.go # golden file tests with known key pair

cmd/tdtpcli/
├── license_check.go  # init() loads license, exposes global License
```

Gate: adapter factory + feature flags. No cert logic yet.

### Phase 2 — Capability certificate + --unsafe gate

```
pkg/license/
├── cert.go         # CapabilityCert struct, LoadCert(), VerifyCert()
└── audit.go        # nonce log (local BoltDB or append-only file)

cmd/tdtpcli/commands/
└── security.go     # applyUnsafeGate() replaces IsAdmin() check
```

### Phase 3 — `tdtp-certify` CA tool

```
cmd/tdtp-certify/
├── main.go
├── issue_license.go
└── issue_cert.go
```

### Phase 4 — Docs + integration tests

```
docs/LICENSING.md         # buyer-facing: what each tier unlocks
docs/UNSAFE_CERTS.md      # operator guide: CA setup, cert issuance, audit
tests/integration/license/
```

---

## Open Questions

1. **CA hierarchy** — single vendor CA or two-tier (vendor root + partner intermediate)?
   Partner CAs would let resellers issue certs without contacting vendor.

2. **License format** — JSON+Ed25519 (simple, human-readable) vs JWT (ecosystem tooling)
   vs custom binary (harder to forge offline)?

3. **Offline enforcement** — clock skew tolerance for air-gapped installations?
   Suggestion: ±15 min, configurable via `--clock-tolerance`.

4. **Host fingerprint** — hostname only is spoofable. Consider: `hostname + MAC + CPU-ID`
   composite. Tradeoff: VM migrations break the lock.

5. **Revocation** — cert TTL handles most cases. License revocation: OCSP-style check
   vs CRL file vs "phone home" (unacceptable for air-gapped). Likely: embedded CRL in
   license renewal + manual revoke list at `pkg/license/revoked.go`.

6. **Tooling distribution** — `tdtp-certify` is vendor-only; ship as separate private repo?
   Or include in framework under `cmd/tdtp-certify/` with build tag `//go:build certify`?

7. **Grace period** — `tdtp.lic` expires: hard stop or 30-day grace (read-only mode)?
   For integrators, hard stop during a migration project is unacceptable. Grace = read-only.

8. **Audit log format** — local file (simple, no deps) vs structured log shipped to SIEM?
   Phase 1: append-only local file. Phase 2: optional syslog/SIEM hook.

---

## Key Design Constraints

- **Zero network calls** at runtime — license and cert verified locally (offline-first)
- **No new runtime deps** for Phase 1 — Ed25519 is in Go stdlib (`crypto/ed25519`)
- **Backward compatible** — no license file = community mode (SQLite only, no --unsafe)
- **Cert is not a password** — it's a scoped, time-limited, host-locked authorization token;
  leaking it does not compromise other systems or other time windows
