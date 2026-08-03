# xZMercury: the service as it actually stands

This is a from-the-code account of what xZMercury does today, replacing
`docs/xZMercury-TDTP-TZ-v1.2.md` (dated 2026-05-22), which described a design
under way rather than the shipped result. Checked against the code on
2026-07-30. Where the two disagree, this document says so — the original is
archived at [`docs/ru-archive/docs/xZMercury-TDTP-TZ-v1.2.md`](ru-archive/docs/xZMercury-TDTP-TZ-v1.2.md)
for the historical record.

For the base key-binding mechanics — burn-on-read, HMAC verification, the
two-Redis design — see [`xzmercury/docs/architecture.md`](../xzmercury/docs/architecture.md),
[`api.md`](../xzmercury/docs/api.md) and [`security.md`](../xzmercury/docs/security.md).
This document covers what those do not: the integrity notary, the consumer
pre-flight pipeline, and the trust/licensing layer, none of which appear in
`xzmercury/docs/` at all despite being live code.

---

## 1. The one-paragraph version

xZMercury is three services that happen to share infrastructure, not one:

1. **Key custody** — binds an AES-256 key to a packet UUID, hands it out once
   (burn-on-read), gated by LDAP group membership and an hourly quota.
2. **Integrity notary** — a producer registers a packet's fingerprint; any
   consumer can ask "does this match what was registered," with no
   authentication needed to ask.
3. **Trust authority** — issues environment certificates through a
   challenge-response enrollment, tying a licence to a specific machine and
   feeding an online/offline status a consumer can gate on.

The statement of work covered service 2 in detail and treated service 3 as an
optional, unbuilt, separately-licensed future product ("chiptdtp"). It shipped
instead as part of the same product this document describes — see §5.

---

## 2. Integrity notary

### 2.1 The three-hash model

A TDTP v1.4+ packet carries three XXH3-128 hashes, each salted with the
packet's own UUID (`Header.MessageID`):

```
Schema.xxh3  = xxh3_128(UUID || canonical_schema_xml)
Data.xxh3    = xxh3_128(UUID || joined_row_values)
pkt.xxh3     = xxh3_128(Schema.xxh3 + "|" + Data.xxh3)
```

Implemented in [`pkg/core/packet/integrity.go`](../pkg/core/packet/integrity.go)
(`ComputeIntegrity`, `VerifyIntegrity`). The salt is public — it lives in the
plain `<Header>` — but unique per packet, which is what makes a captured hash
useless against any other packet.

Three independent hashes rather than one is a diagnostic, not a redundancy: a
corrupted transfer breaks all three at once; a producer that only edited the
data breaks `Data.xxh3` alone; one that edited the schema (a type, a dictionary
entry) breaks `Schema.xxh3` alone. The pattern of what broke says what changed.

### 2.2 The registry

`POST /api/hashes`, `GET /api/hashes/{uuid}/{part}`, `DELETE /api/hashes/{uuid}/{part}`
— implemented in [`xzmercury/internal/api/hashes.go`](../xzmercury/internal/api/hashes.go),
backed by [`xzmercury/internal/hashstore`](../xzmercury/internal/hashstore/store.go).

- **Register** (`POST`) requires an `X-Caller` header and a `packet_version`
  of at least `1.4`; it `SET NX`s the Redis key `mercury:hash:{uuid}:{part}`,
  so a slot can be filled exactly once, ever — a second registration attempt
  gets `409 Conflict`, not an overwrite.
- **Verify** (`GET`) needs no authentication — any consumer can ask. It
  returns `registered` and `match` as separate booleans:

  | `registered` | `match` | Meaning |
  |---|---|---|
  | `false` | — | No such slot — the packet was never registered |
  | `true` | `false` | Registered, but the presented hash disagrees — tampered |
  | `true` | `true` | Proceed |

- **Revoke** (`DELETE`) also only requires a non-empty `X-Caller` — **there is
  no admin-role check**. The statement of work specified `403 Forbidden` for a
  caller without admin rights; the code accepts any caller identity at all.
  This is the sharpest gap between what was planned and what shipped: anyone
  who can reach the endpoint and knows a UUID can revoke its registration.

Default TTL is 24 hours (`hashstore.New`, `ttl <= 0` falls back to it),
matching the plan.

### 2.3 What the plan promised and the code does not do

- **No audit trail for hash operations.** The plan called for every
  Register/Verify/Revoke to append to a Redis sorted set,
  `mercury:audit:hashes:{YYYYMMDD}`, giving a queryable, date-ranged log.
  Searching the codebase for that key or for a `ZADD` call in this context
  finds neither. What exists instead is a `zerolog` line to stdout on each
  operation (`hashes.go:102-124`) — useful for a human tailing the process,
  useless for "show me every registration by sender X this month."
- **No quota on registration.** The plan's "forgetful accountant" scenario —
  cap registrations per sender per day, alert on a burst — has no code at all.
  `grep` for `hash-quota` or `429` inside `hashesHandler` returns nothing. A
  *different* quota exists and works (§3), but it guards key binding, not hash
  registration, and the statement of work is explicit that these are two
  separate quota loops.
- **Six of seven Dictionary tokens are unimplemented.** Of `@MRC`, `@SHA`,
  `@SZ`, `@LOCK`, `@TTL`, `@ALG`, `@SRC`, `@VER`, only `@MRC` exists in code
  (written by `--integrity --mercury-url` at
  [`cmd/tdtpcli/commands/export.go:140`](../cmd/tdtpcli/commands/export.go)).
  This is not news — [`docs/dictionary-as-dependency-manifest.md`](dictionary-as-dependency-manifest.md)
  already tracks it honestly in its own implementation-status table (`@LOCK`
  consumer support and `@SHA` pre-verification both marked "not yet
  implemented"). That document is the design reference for the tokens
  themselves; this one only notes that the gap is real and still open.

---

## 3. Consumer pre-flight

`pipeline.VerifyAndPrepare` ([`pkg/pipeline/verify.go`](../pkg/pipeline/verify.go))
is the single entry point a consumer calls before trusting a packet's contents.

For anything below v1.4 it is a no-op — `packet.NeedsRowCountCheck` returns
false and the packet passes through unexamined, which is deliberate backward
compatibility, not an oversight.

For v1.4+:

1. **Mercury check** — calls `Verify` above. A `registered:false` or a hash
   mismatch **returns an error unconditionally, before the fallback policy is
   even consulted** — confirmed by reading the code path directly: the
   tamper/not-registered branch returns at line 109, and only *then* does the
   switch on `FallbackPolicy` begin, and only for the separate case of Mercury
   being unreachable. This matches the plan's explicit warning that these two
   error kinds must never be subject to a fallback policy, and the code
   honours it.
2. **Local xxh3 check** — recomputes the three hashes and compares, independent
   of whether Mercury was reachable.
3. **Dictionary expansion** — `@`-tokens replaced with full values, the
   Dictionary cleared afterward so nothing downstream ever sees a token.

Three fallback policies apply only when Mercury itself could not be reached
(`ErrMercuryUnavailable`/`ErrMercuryError`), never to a definitive tamper or
not-registered verdict:

| Policy | Behaviour | When to pick it |
|---|---|---|
| `FallbackBlock` | Refuse the packet | Financial, medical, legally significant data |
| `FallbackDegrade` | Skip the Mercury check, run the local hash check anyway, mark the result degraded | Operational data where continuity matters more than the extra assurance |
| `FallbackDowngrade` | Convert the packet to v1.3.1 in place and skip the rest of v1.4 verification | Talking to a consumer that never understood v1.4 |

One detail the plan does not mention: if no `HashVerifier` is supplied at all
(`verifier == nil` in `runMercuryCheck`), the Mercury check is skipped
entirely rather than treated as unreachable — useful for unit tests, but worth
knowing if you expect `FallbackBlock` to fire when nobody wired a verifier in.

---

## 4. Key binding and its quota

The base flow — `POST /api/keys/bind`, burn-on-read `POST /api/keys/retrieve`
— is covered in `xzmercury/docs/`. Two pieces of it are worth restating here
because they are easy to confuse with the integrity-side mechanisms above.

`Bind` ([`xzmercury/internal/api/keys.go`](../xzmercury/internal/api/keys.go))
runs, in order: an LDAP membership check against the group a `pipeline-acl.yaml`
entry names for that pipeline (skipped only when the caller is empty), then an
atomic Lua-scripted hourly quota deduction
([`xzmercury/internal/quota/manager.go`](../xzmercury/internal/quota/manager.go)),
keyed `quota:{group}:{YYYYMMDDHH}`. Exhausting it returns `429`.

This is a **different quota from the hash-registration one the statement of
work describes as pending** (§2.3) — this one exists, is wired in, and has
existed since before this document's predecessor was written. `pipeline-acl.yaml`
(loaded by [`xzmercury/internal/acl/acl.go`](../xzmercury/internal/acl/acl.go))
maps a pipeline name to an AD group and a per-execution cost; an unlisted
pipeline falls back to `default_group`/`default_cost`.

---

## 5. Trust and licensing — the part neither document set out to describe

This is the largest gap between the statement of work and the product. §15 of
the original framed Ed25519 signing, a licence authority and a self-checking
binary as **`chiptdtp`** — a hypothetical, separately licensed, proprietary
tool, explicitly *not* the free `tdtpcli` this repository ships. Read literally,
that document says licensing infrastructure does not exist yet in the product
you are using.

It exists, and it is not a separate binary.

### 5.1 `pkg/license` — three real commercial tiers

[`pkg/license/license.go`](../pkg/license/license.go) defines
`TierCommunity`, `TierProfessional`, `TierEnterprise`. A licence is an
Ed25519-signed JSON document (`License.Sign`/`VerifyWith`) naming a licensee,
an expiry, a tier, a list of permitted adapters, and a list of permitted
features. `tdtpcli` gates on it directly:
[`cmd/tdtpcli/commands/license_gate.go`](../cmd/tdtpcli/commands/license_gate.go)'s
`GateFeature` is called before `--enc` and before `--unsafe`
([`cmd/tdtpcli/main.go:837-842`](../cmd/tdtpcli/main.go)), and the community
floor is restricted to the SQLite adapter alone. This is live, not draft —
every session in this repository's history has run against it (the
`License: ... tier=enterprise features=[etl,enc,s3,unsafe]` banner printed on
every non-quiet invocation is this system talking).

[`pkg/license/cert.go`](../pkg/license/cert.go)'s `CapabilityCert` is a
second, narrower instrument: a short-lived, host-locked, single-operation
token (`"unsafe-sql"`, `"schema-write"`, `"cross-schema"`, `"drop-allowed"`),
scoped to specific tables or a specific database, replay-protected by a nonce
recorded on first use. It authorizes one `--unsafe` operation on top of a
licence that already grants the `unsafe` feature — a licence says "this
installation may do dangerous things"; a `CapabilityCert` says "this specific
dangerous thing, on this specific table, once."

### 5.2 `xzmercury/internal/ca` — the certificate authority

Two-step, challenge-response protocols, not a simple key exchange:

- **Enroll** (`POST /api/env/enroll` → `/enroll/confirm`) — a licence key and
  an environment's Ed25519 public key (meant to come from a TPM or an
  environment-bound key) go in; the server never trusts the raw key alone. It
  issues a nonce, and only a signature over that nonce from the matching
  private key completes enrollment and returns an `EnvCert` plus a four-hour
  session token. Re-enrolling the same environment under the same licence is
  idempotent; under a *different* licence it conflicts rather than silently
  overwriting ([`enroll.go`](../xzmercury/internal/ca/enroll.go), confirmed
  by `TestReEnrollSameEnvDifferentLicense_Conflicts` in
  [`integration_test.go`](../xzmercury/internal/ca/integration_test.go)).
- **Authorize** (`POST /api/env/authorize` → `/authorize/confirm`) — the same
  challenge-response shape, but to renew a session against an *existing* cert.
  The design note in the code is worth repeating: possession of the cert file
  is not proof of anything, since it is a signed blob that can be copied — the
  proof is that only the original hardware holding the matching private key
  can sign a fresh nonce.
- **Offline authorize** (`POST /api/env/authorize/offline`) — for air-gapped
  deployments where the interactive round-trip cannot happen.
- **Seat limits.** Each licence carries a `SeatLimit` (default 1); enrollment
  counts active certs for that licence and refuses a new one past the limit,
  confirmed by `TestSeatLimitExhausted`.
- **Revocation** by cert ID or by licence hash
  (`DELETE /api/env/certs/{cert_id}`, `DELETE /api/env/licenses/{license_hash}`).

### 5.3 What ties it to the product a consumer actually runs

`GET /status` on xZMercury reports `mode` (`dev`/`prod`), `ca_authorized`, and
the permission list the CA granted
([`xzmercury/internal/api/router.go:95-113`](../xzmercury/internal/api/router.go)).
The orchestrator's trust gate reads this directly:
[`cmd/orchestrator/preflight.go`](../cmd/orchestrator/preflight.go) intersects
its own offline `tdtp.lic` (which scenario permissions it may run at all, and
how many concurrently) with this online Mercury status (is this Mercury a
production, CA-authorized instance, and does its licensed permission set cover
what the scenario asks for) before a scenario is allowed to execute. Neither
side of that intersection is optional — a licensed orchestrator pointed at a
dev-mode Mercury is exactly the case this gate exists to catch. See also
[`docs/SCENARIO_TRUST.md`](SCENARIO_TRUST.md) for the layer above this one
(whether a scenario's *content* is trusted, as distinct from whether the
*environment* running it is).

### 5.4 `tdtp-certify`

[`xzmercury/cmd/tdtp-certify`](../xzmercury/cmd/tdtp-certify/main.go) is the
vendor-side CLI for this whole chain: issuing licences, issuing and revoking
capability certs, listing what is currently active. It is the operational tool
behind everything in §5.1 and 5.2 — nothing there requires hand-editing Redis
or a database.

### 5.5 What this means for the statement of work's framing

Section 15's "L2 free / L3 proprietary" split does not describe the shipped
product. What shipped is one product with tiered licensing and an integrated
CA — closer in spirit to the plan's *goal* (stop a specific class of
determined, competent attacker, not just fat-fingers) than to its proposed
*shape* (a second, separately sold binary). Whether a `chiptdtp`-style
separate tier is still wanted is a product decision outside this document's
scope; what belongs here is simply that the infrastructure the plan reserved
for that hypothetical tier is already live, in the product the plan called
free.

---

## 6. Gaps and a roadmap

In rough order of how much they change the security story if left alone:

### 6.1 `DELETE /api/hashes/{uuid}/{part}` has no admin check

Right now any caller supplying any non-empty `X-Caller` can revoke another
producer's hash registration — the same authorization level as *registering*
one, where the statement of work specified admin-only. Revoking a registration
does not expose data, but it does let an unprivileged caller force a
registered packet back into "unknown" state, at which point pre-flight blocks
it as `ErrHashNotRegistered`. That is a denial-of-service primitive against a
specific packet, not a confidentiality break, but it is a real gap against the
written spec. Worth an ACL check — reusing the same LDAP/group mechanism §4
already has, or an admin flag on `CapabilityCert` — before this matters in a
real deployment.

### 6.2 No audit trail for hash operations

`Register`/`Verify`/`Revoke` land in a process log, not in a queryable store.
Reconstructing "who registered what, when" today means grepping stdout across
however many xZMercury instances exist. The plan's design
(`ZADD mercury:audit:hashes:{YYYYMMDD}`, one sorted set per day, auto-expiring)
is a reasonable target and does not require new infrastructure — it is the
same Redis already in use, a new namespace. Given `mercury:audit:alerts` and
similar patterns already exist for other subsystems, this is mechanical to
add, not a design problem.

### 6.3 No quota on hash registration

The "forgetful accountant" scenario in the original plan — cap registrations
per sender per day at a level generous enough for retries but tight enough to
make a brute-force attempt visible, alert on exhaustion — has no code. The
existing key-bind quota (§4) is architecturally the right template to copy: an
atomic Lua INCR against a per-sender-per-day key, configurable per sender.

### 6.4 Dictionary pre-flight is one token deep

Only `@MRC` exists; see [`docs/dictionary-as-dependency-manifest.md`](dictionary-as-dependency-manifest.md)
for the design and its own status table. The most valuable of the missing
pieces is `@SHA` pre-verification: it is specifically what prevents a
corrupted file from burning a one-time Mercury key on data that turns out to
be garbage — key burned, data lost, no way back. `@LOCK` and the rest are
lower priority: genuinely new capabilities rather than closing an existing
hole.

### 6.5 Suggested order

1. Admin check on hash revocation (6.1) — smallest change, closes a real gap
   against the written authorization model, no new infrastructure.
2. Hash audit log (6.2) — mechanical, and a prerequisite for reasoning about
   6.3 and 6.1 abuse after the fact.
3. Hash registration quota (6.3) — copy the key-bind quota's shape.
4. `@SHA` pre-flight token (6.4) — closes the burn-on-corrupted-data failure
   mode; the other five tokens can follow individually as their use cases
   arrive rather than as a batch.
5. A decision on §5.5 — is a separate high-assurance tier (hardware-backed
   signing beyond Ed25519, a dedicated compliance story) still wanted now that
   its infrastructure has already shipped inside the main product, or does
   this document's account of the current licensing model already cover the
   real requirement.

None of this blocks anything else — the integrity notary, pre-flight, and
trust layers all work today for what they cover; these are the edges.
