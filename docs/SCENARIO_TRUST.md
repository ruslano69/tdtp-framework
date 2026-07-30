# Scenario trust — signing and integrity for orchestrator scenarios

> **Status: partly implemented.** Stage 0 below shipped, and shipped stronger
> than it was drafted — see [What exists today](#what-exists-today). Stages 1
> through 5 remain a design draft: no signing code exists. This document records
> the agreed model so there is something concrete to argue with and to build
> from.
>
> Related: [ROADMAP.md](../ROADMAP.md) — "Schema migration" and "Orchestrator
> scenario integrity registration".

---

## 1. The problem

Two independent threats. They are easy to confuse, and tempting to try to close
with one mechanism, but both layers are needed at once.

| # | Threat | The attacker | What is compromised |
|---|--------|--------------|---------------------|
| A | A compromised **producer** sends a packet with an "evolved" schema through a legitimate, unmodified `--map --listen` or pipeline | whoever holds the broker or branch-node credentials | the data inside an honest pipeline |
| B | Someone with write access to `--scenarios/` alters **the YAML itself** — adds `--unsafe`, changes a DSN, enables unauthorised DDL | whoever can write to the scenarios directory or to CI | the instructions the orchestrator executes |

Threat A remains open. The packet's `<Schema>` is composed entirely by the
producer and used for `CREATE TABLE IF NOT EXISTS` with no gate at all, which
becomes architecturally dangerous the moment auto-`ALTER TABLE` arrives (see the
roadmap).

Threat B is now closed at the file level — see below — but not
cryptographically: an approval records that an admin accepted a hash, not that a
named authority signed it.

## What exists today

`cmd/orchestrator/scenario_approval.go` implements content pinning, and it went
further than the Stage 0 draft below proposed:

- `scenarioChecksum` takes the SHA-256 of the scenario's raw YAML.
- `VerifyScenarioChecksum` runs at **every execution entry point** — cron, manual
  run, request approval — and **refuses to run** the scenario unless an admin has
  registered that exact hash through `POST /scenarios/{name}/approve`.
- A missing registration and a mismatched hash fail identically. A brand-new
  scenario file dropped into the directory cannot run until someone approves it,
  and an edited one stops running until someone re-approves it.
- An approval can be revoked (`DELETE /scenarios/{name}/approval`), and a revoked
  approval fails with its own message.
- The failure text names when the approval was made and by whom.

The draft's Stage 0 suggested logging a warning and requiring a reindex. The
implementation blocks instead, which is the stricter and better choice: a
warning in a log nobody reads is not a gate.

**What Stage 0 still lacks:** the job record does not carry
`scenario_content_hash` or `signed_by`, so the provenance of a completed run is
not recoverable from the job alone, and there is no monotonic version, so the
downgrade concern in [4.3](#43-downgrade-attack) is untouched.

## 2. What already exists and can be reused

The conclusion of the earlier analysis: most of the signing infrastructure is
already here. What is missing is the *right root of trust* and the *point of
enforcement*.

| Building block | File | What it gives |
|----------------|------|---------------|
| `CapabilityCert` | `pkg/license/cert.go` | An Ed25519-signed token: the operation (`"schema-write"` already exists), a scope over tables and databases (`CoversTable`, globs), host locking, an expiry, and **a nonce with replay protection through the audit log** |
| `applyUnsafeGate` | `cmd/tdtpcli/commands/unsafe_gate.go` | A worked example of a gate: a certificate, or a fallback to `IsAdmin()` |
| CA / EnvCert | `xzmercury/internal/ca`, `cmd/tdtp-ca` | Challenge-response, hardware attestation, a separate root for environments |
| `tdtp-certify` | `xzmercury/cmd/tdtp-certify` | Vendor-side `issue-license`, `revoke-cert`, `list-active` — an established CLI pattern for issuing and revoking |
| `ProjectRequest` workflow | `cmd/orchestrator/requests.go` | Staged approval (`submit → test → approve/reject`). Today it is a status flag in SQLite rather than a cryptographic act, but the place to attach a signature already exists |
| Job artifact hash | `cmd/orchestrator/executor.go` (`fileHashAndSize`) | SHA-256 is already computed for the *output* artifact; the same treatment for the *input* definition follows by analogy |

One divergence from the target model: `CapabilityCert` is signed by **the
vendor**. For DDL rights over a particular database that is the wrong root — the
right belongs to whoever owns the database, the DBA, not to the software
supplier.

## 3. The target model

### 3.1 Roles and roots of trust

```
Vendor root (Ed25519, offline)          — exists: signs tdtp.lic
        │
        ├─ CA root (xzmercury)          — exists: signs an EnvCert per environment
        │
        └─ Signer cert (NEW)            — the vendor or CA delegates, to one DBA,
              │                            the right to sign scenarios
              ▼
        DBA signs a scenario (NEW)      — "I approve exactly this content"
```

A `SignerCert` is a delegation, not an independent root: the vendor — or the CA,
if that is where this ends up living — certifies that a particular DBA's public
key may sign scenarios carrying DDL operations within a given scope. Revocation
goes through the same registry `tdtp-certify revoke-cert` already uses.

### 3.2 New data structures

```go
// pkg/license/signer.go (new file, modelled on cert.go)

// SignerCert delegates scenario-signing authority to a DBA/privileged user.
// Signed by the vendor or CA root — NOT self-signed.
type SignerCert struct {
    IssuedTo   string    `json:"issued_to"`   // DBA identity (e.g. email, principal)
    PublicKey  string    `json:"public_key"`  // base64 Ed25519 pubkey of the DBA
    Operations []string  `json:"operations"`  // subset of: schema-write, create-table, create-view
    Scope      CertScope `json:"scope"`       // reuse existing CertScope (tables/database)
    IssuedAt   time.Time `json:"issued_at"`
    Expires    time.Time `json:"expires"`
    Signature  string    `json:"signature"`   // base64(Ed25519 over canonical JSON, by vendor/CA root)
}

// ScenarioSignature is the DBA's approval of one exact scenario file.
// Verifying it requires first verifying the SignerCert that names PublicKey.
type ScenarioSignature struct {
    ScenarioName string    `json:"scenario_name"`
    ContentHash  string    `json:"content_hash"`  // sha256(canonical scenario YAML)
    Version      int       `json:"version"`       // monotonic — see 4.3 downgrade protection
    SignedBy     string    `json:"signed_by"`     // must match a SignerCert.IssuedTo
    IssuedAt     time.Time `json:"issued_at"`
    Expires      time.Time `json:"expires"`
    Signature    string    `json:"signature"`     // base64(Ed25519 over canonical JSON, by DBA key)
}
```

### 3.3 The check performed when a scenario runs

`POST /scenarios/{name}/run`, and the cron trigger, would perform this **on every
run**, not once at process start:

1. Re-read the scenario file from disk. This closes the TOCTOU: a check at
   startup is not enough, because the file may have changed since it was loaded.
2. Compute `sha256(canonical YAML)`.
3. Find the registered `ScenarioSignature` by name and compare `content_hash`.
   A mismatch is a refusal — what is on disk differs from what was approved.
4. Check that `version` is not below the highest seen for this name (downgrade
   protection, see 4.3).
5. Load the `SignerCert` named by `SignedBy`; verify its signature against the
   root, its expiry, that its `Scope` covers the scenario's target tables, and
   that it has not been revoked.
6. Verify the `ScenarioSignature` against the public key from the `SignerCert`.
7. **Only** if the scenario declares DDL operations — `schema-write`,
   `create-table`, `create-view`, the same set already declared in
   `Orchestrator.Permissions` — require that `SignerCert.Operations` contains
   the matching string. **An auto-migration flag in the YAML itself is neither
   read nor honoured**: the right comes from a valid signature only, because the
   YAML is precisely what an attacker can change.
8. Record `scenario_content_hash`, `signed_by` and `signer_cert_id` on the job —
   full provenance for the execution, by analogy with the existing
   `ArtifactSHA256`.

### 3.4 The link to schema migration

Auto-`ALTER TABLE` driven by a producer-supplied packet schema would be
permitted **only** where the running scenario carries a valid
`ScenarioSignature` from a signer whose `SignerCert.Operations` includes
`schema-write` for the target table. Without one, the default holds: detect the
drift, report it, apply nothing.

This is the single point where the two roadmap items physically meet.

## 4. Open risks and questions

### 4.1 Storing the DBA's key

The weakest link in the whole scheme. The minimum is a passphrase-protected key
file, as `tdtp-ca` does with `ca.ed25519.priv`. Better is signing on a separate
machine or a hardware token, so the orchestrator never sees the private key.

### 4.2 Who issues the SignerCert — the vendor or the CA?

The vendor root already signs `tdtp.lic` and `CapabilityCert`, so reusing it is
the least work. But semantically, DDL rights over a specific database are the
operating organisation's decision, not the software supplier's. The cleaner
option is to delegate issuance to the CA (`xzmercury/internal/ca`), which
already carries trust *inside the customer's environment* rather than trust in
the product. This needs deciding before any code is written.

### 4.3 Downgrade attack

An older, honestly signed version of a scenario — one without the table
restrictions, say — stays cryptographically valid after a newer and stricter
version is issued. The monotonic `version` in `ScenarioSignature` closes this
only if the orchestrator **stores** the highest version it has seen, per
instance. The signature registry therefore has to be persistent: not just a
hash, but `{hash, version, signed_by}`, checked monotonically on every run.

### 4.4 Where approval happens

`approve` in `requests.go` is a status change in SQLite today. The choice is
between the DBA signing offline in advance — the scenario arrives in
`--scenarios/` already signed and the UI approval is unnecessary — or
`POST /requests/{id}/approve` asking for a signature interactively, which is
harder and requires the private key to be reachable from an HTTP server, exactly
what 4.1 argues against.

Recommendation: signing is an offline step, through a `tdtp-scenario-sign` CLI
modelled on `tdtp-certify`, and the orchestrator's `approve` stays a UX record.

---

## 5. Stages

Ordered so that each stage is useful on its own and none blocks the rest of the
work — schema migration should not have to wait for the entire signing chain.

### Stage 0 — fingerprint without signatures — **DONE, and stricter than drafted**

Delivered in `cmd/orchestrator/scenario_approval.go`: the checksum is verified at
every execution entry point and a scenario cannot run at all until an admin
registers its hash. See [What exists today](#what-exists-today).

Still outstanding from this stage:
- `scenario_content_hash` on the job record, so a finished run's provenance
  survives in the job itself
- a persistent monotonic version, which stage 3 needs for downgrade protection

### Stage 1 — `SignerCert`, the delegation

- `pkg/license/signer.go`: the struct plus `Verify()`/`VerifyWith()`, modelled on `cert.go`, reusing `CertScope` and `matchGlob`
- Resolve question 4.2 (vendor or CA) before writing the issuing code
- An issuing CLI: `tdtp-certify issue-signer --key <root> --dba <email> --ops schema-write,create-table --scope-db orders --expires ...`, following the existing `issue-license`
- No enforcement yet — issuance and verification in isolation, with unit tests along the lines of `cert_test.go`

### Stage 2 — `ScenarioSignature` and offline signing

- `pkg/license/scenario_sig.go`: the struct and its verification, which depends on an already-verified `SignerCert` (3.3, steps 5–6)
- `tdtp-scenario-sign` CLI: `tdtp-scenario-sign --scenario flights.yaml --dba-key dba.ed25519.priv --signer-cert dba.cert.json --version 1 --out flights.yaml.sig`
- Storage: `flights.yaml.sig` beside `flights.yaml` in `--scenarios/`, or a row in the orchestrator database. The registry from stage 0 already exists, which argues for the database

### Stage 3 — enforcement in the orchestrator

- `TrustGate.GateScenario` gains steps 1–7 from section 3.3
- Without a signature a scenario runs as it does today (`permissions ⊆ license ∩ Mercury`) but **cannot** declare DDL operations; those require a signature. Every other permission behaves as before, so this is backward compatible
- The job record gains `scenario_content_hash`, `signed_by`, `signer_cert_id`
- Downgrade protection (4.3): the persistent monotonic version in the stage 0 registry

### Stage 4 — binding schema migration

- Only after stage 3
- Auto-`ALTER TABLE` reads `Operations` from the already-verified `SignerCert` of the current run. Without `schema-write` in scope it falls back to detect-only, regardless of any flag in the YAML

### Stage 5 — revocation and monitoring

- `tdtp-certify revoke-cert` extended to `SignerCert` — the registry already exists for licence certificates, and it is the same mechanism
- An orchestrator metric, `orchestrator_scenario_signature_status{name,status}`, alongside the existing job metrics
- Audit log: every signature check, pass or fail, as its own record — not only the fact that a job ran

---

Each stage leaves the system working and backward compatible. Stopping after
stage 1 is a real improvement on its own; there is no need to reach stage 5 in
one go.
