# Travel Agency NEXT: Orchestrator-Governed, Encrypted Showcase

Implementation plan for converting this example from ad-hoc Python glue
(`coordinator.py` + `consumer.py` + Redis notify) to a full demonstration of
the framework's governance and security stack: **orchestrator** (scenario
execution, approval, RBAC, audit, scheduling, pub/sub triggers) +
**xZMercury** (key/quota/ACL) + **tdtp-ca** (environment certification) +
**encrypted TDTP packets** (AES-256-GCM, burn-on-read).

Not a rewrite of the data model or the Postgres setup — those stay as-is.
This replaces *how transfers are triggered, secured, authorized, and
audited*, using this example as the framework's reference "storefront."

Status: **plan only, nothing implemented yet.**

---

## Why

Today, this example (and the two real production integrations it was meant
to rehearse — ZTR-Live↔AX2009 and ZTR-Live↔Polynet) all bypass the
governance layer entirely: `coordinator.py`/`consumer.py` shell out to
`tdtpcli` directly, and the two live integrations are raw T-SQL scripts on
a schedule. None of it goes through checksum-approval, RBAC, quota, or
encryption. That's a real, observed pattern, not hypothetical — see the
session note on why this keeps happening (friction: a script is faster to
stand up than a properly-approved scenario) and the counter-argument (this
is the one example where paying that friction is the point — it's the
demo of the governance layer, not a production shortcut).

## What changes vs. today

**Correction (this section was wrong in an earlier draft):** `consumer.py`
does **not** use `--import-broker` + staging + SQL merge — per `TODO_NEXT.md`
sprints 6-7, it was already migrated to `tdtpcli --map <mapping.yaml>
--input broker://<queue>` (direct upsert, no staging tables, no merge
procs). It also already writes an S3 audit marker and a Redis state key
per sync. Only `coordinator.py` (the export side, still `--export-broker`)
is on the old, ungoverned path. Fixed below.

| Today | NEXT |
|---|---|
| `coordinator.py` polls RabbitMQ, shells to `tdtpcli --export-broker` | Orchestrator scenario (cron or pub/sub-triggered), same `--export-broker` call, via a registered runner |
| `consumer.py` subscribes to ad-hoc Redis channel `tdtp:travel:notify`, runs `tdtpcli --map <mapping.yaml> --input broker://<queue>` (already no-staging, already working) | Orchestrator's built-in `Subscriber` (`pubsub.go`) subscribes to `tdtp:pipeline:*` — the framework's own convention (`pkg/resultlog`), not a bespoke channel. The `--map` call itself is barely touched — it becomes the scenario's runner invocation instead of a Python subprocess call |
| Plain `.tdtp.xml` packets over RabbitMQ | Encrypted TDTP over the same RabbitMQ queues (v1.5 format, see `TODO_NEXT.md`) — export side needs new work (Phase 1.5); `--map`'s decrypt path already works with the *current* format and needs only the dual-format detection update, not new plumbing |
| No approval, no RBAC, no audit (though `consumer.py`'s S3 marker + Redis state key are a partial, bespoke audit trail already) | Every scenario checksum-approved; separate central-operator / branch-operator tokens; full job audit trail — likely replaces the bespoke S3 marker (see open questions) |
| No quota/ACL | xZMercury `pipeline-acl.yaml` gates cost/group per scenario |
| No environment trust | xZMercury enrolled with `tdtp-ca`; orchestrator's trust gate checks Mercury's CA-authorized status |

## Encryption — full design lives in root `TODO_NEXT.md` now

The wire-format redesign (encrypt `<QueryContext>`/`<Schema>`/`<Data>` in
place, keep `<Header>` plain, targeting spec version v1.5) turned out to be
a core-framework decision, not example-scoped — moved to the repo root
[`TODO_NEXT.md`](../../TODO_NEXT.md) ("Encryption format redesign" section,
implementation tracking) and [`docs/tdtp-protocol-schema.md`](../../docs/tdtp-protocol-schema.md)
("v1.5 (planned)", full producer/consumer flow diagrams — same depth as the
existing v1.4 writeup). Those are now the canonical sources. Summary here:

- Why the current binary-blob format (`[2B ver][1B algo][16B uuid][12B nonce][ciphertext]`)
  breaks XML validity and why that's specifically what blocks the broker path.
- **The full blast radius already depending on the current format** —
  `--import` and, critically, **`--map --input broker://queue`** (what
  `consumer.py` actually calls today) already decrypt it via
  `IsEncryptedBlob`/`DecryptEncBlob`. The fix must be additive
  (dual-format detection), not a replacement — this example's import side
  is a real, working consumer of the current format, not a blank slate.
- Why v1.5 (not revising v1.4 in place) — resolved with reasoning, not just
  asserted.

What stays example-scoped here: which scenarios actually turn encryption
on, and verifying the end-to-end cycle works for this topology once the
core-framework work lands (Phase 2 below).

`--pipeline`-side encryption (`output.tdtp.encryption: true` +
`security.mercury_url`, source `type: tdtp-enc`) already works today and
is unaffected by any of this — it's a separate command path this example
doesn't currently use for the broker-transported entities, though the
central↔branch reference-data scenarios could use it directly if a broker
hop turns out not to be needed for those (open question, unresolved).

## Target architecture

```
activity.py (unchanged: DB writes + RabbitMQ event, simulates real usage)
        |
        v
tdtp-ca  <---enroll/AutoRenew--->  xZMercury  <---trust-gate check--->  orchestrator (Paris + Hawaii + London —
   |                                  |    |                                |          each node runs its own)
   |                          redis-mercury redis-pipeline <--pub/sub-->    |
   |                          (AES keys,    (quota, ACL,                    |
   |                           RAM-only)     tdtp:pipeline:* events)        |
   v                                                                        v
env certs                                                        scenarios (YAML, checksum-approved)
                                                                            |
                                                    +---------------------+---------------------+
                                                    v                                             v
                                     Central<->Branch reference data          Branch(Hawaii)->Central(Paris) sales/customers
                                     (cron scenarios, low churn)               export scenario (cron) --export-broker
                                                                                -> encrypted payload -> RabbitMQ queue
                                                                                (Airline/London -> Central: same shape)
                                                                                        |
                                                                        result_log publish (NEW, Phase 1.5)
                                                                                        v
                                                                        import scenario, pub/sub-triggered on
                                                                        tdtp:pipeline:<name> --map <mapping.yaml>
                                                                        --input broker://<queue>
                                                                        (decrypts, dual-format) -> direct upsert
                                                                        (already no staging/merge — unchanged from today)
```

RabbitMQ is the only thing that physically crosses Paris/Hawaii/London — it
*is* the network boundary between nodes, same as it is today. What changes
is everything around that one hop: who's allowed to trigger it, whether the
payload is encrypted in transit, and what gets logged.

Two trigger models on display side by side, matching the "cron or event,
no third option" principle already agreed for this project:

- **Central → Branch** (countries, tours, guides, schedule): reference data,
  low churn — **cron** scenarios, same cadence style as the real AX2009/
  Polynet integrations (daily Job Queue equivalent).
- **Branch → Central** (customers, sales): export scenario runs with
  `result_log.type: redis` configured; on completion it publishes to
  `tdtp:pipeline:<result_name>`; the orchestrator's `Subscriber` picks that
  up via a `pubsub.yaml` mapping and triggers the import scenario
  automatically — **this is the exact mechanism built this session**,
  demonstrated end-to-end on real infrastructure instead of unit tests.

**The broker stays — this is not negotiable, it's the point of the demo.**
Central (Paris), Branch (Hawaii), Airline (London) are genuinely distributed
nodes with no shared filesystem; `--export-broker` (producer) / `--map
--input broker://` (consumer) over RabbitMQ is the actual network transport
between them, not an implementation detail to simplify away. File-based
`--pipeline` transfer (mentioned as an alternative in an earlier draft of
this plan) is **wrong for this topology** and is dropped.

## Infrastructure

`deployments/docker/docker-compose.prod.yml` already exists (CA, two Redis
instances, xZMercury, a generic worker) — it's the starting skeleton, not
something to build from scratch. Needed additions:

- `orchestrator` service (currently the compose file's "worker" is generic;
  needs the actual `--scenarios`/`--runners`/`--schedules-seed`/`--pubsub`/
  `--redis-addr` flags wired to this example's config directory).
- The three existing Postgres services (central/branch/airline) from this
  example, unchanged.
- Volume-mount this example's `scenarios/`, `pipelines/`, `runners.yaml`,
  `pubsub.yaml`, `schedules.yaml` into the orchestrator container.

`xzmercury/docs/LOCAL_PROD.md` covers the no-Docker equivalent sequence
(build binaries → `tdtp-redis` → `tdtp-certify keygen`/`tdtp-ca` →
`tdtp-certify issue-license` → `xzmercury` with `xzmercury.prod-local.yaml`)
— useful for local iteration without standing up the full compose stack.

## Implementation phases

**Phase 0 — infra skeleton**
- [ ] Adapt `docker-compose.prod.yml` for this example: add central/branch/
      airline Postgres, add a proper `orchestrator` service, wire volumes.
- [ ] Confirm `tdtp-ca` + `xZMercury` enroll successfully in this compose
      context (`/status` reports `ca_authorized: true`).

**Phase 1 — scenario-ize the existing pipelines**
- [ ] Add `orchestrator:` headers (name, permissions, runner if non-default,
      params) to each file under `pipelines/` (`extract_*.yaml`, `load_*.yaml`).
- [ ] Decide param/schedule shape per entity (most are static/no-param cron
      jobs; sales/customers need the `{{current_date}}`-style incremental
      window already used elsewhere in the framework).

**Phase 1.5 — close a real gap in tdtpcli itself (prerequisite for Phase 2/3)**

Checked directly (`grep` on `cmd/tdtpcli/commands/broker.go`): the
export-broker path has **neither** `result_log` publish **nor** any
encryption support — both exist only on the `--pipeline` command path.
The import side is in a different, better state: `--map --input
broker://queue` (`map.go`, what `consumer.py` actually calls) **already
decrypts** the current whole-blob format via `IsEncryptedBlob`/
`DecryptEncBlob`. So this phase has two very differently-sized halves —
full detail and the exact wire-format redesign in root
[`TODO_NEXT.md`](../../TODO_NEXT.md), summarized here:

- [ ] **Export side (new work):** extend `broker.go`'s export-broker path
      with the same `result_log` publish `pipeline.go` already has (mirror
      `pipeline.go:144-153`), and encrypt `<QueryContext>`/`<Schema>`/`<Data>`
      in place per the v1.5 format — sends a still-valid-XML message body,
      `Header` stays plain for routing.
- [ ] **Import side (small update, not new plumbing):** `--map`'s existing
      `IsEncryptedBlob`/`DecryptEncBlob` calls gain a second detection
      branch for the new attribute-based format alongside the old binary
      header — old and new packets both keep decrypting correctly.
- [ ] Tests for both, matching this session's established bar (real
      round-trip, not mocks — same spirit as the `miniredis` pub/sub tests).

**Phase 2 — encryption (example-side, once Phase 1.5 lands)**
- [ ] Turn on encryption for the branch→central and airline→central export
      scenarios (customers, sales, flights, bookings) — no changes needed
      on the `consumer.py`→orchestrator-scenario side beyond what Phase 1.5
      already fixed centrally.
- [ ] Central→branch reference data (countries, tours, guides, schedule)
      can go encrypted too, or stay plain — lower sensitivity, decide when
      writing the actual scenario YAMLs, not now.
- [ ] Verify a full encrypt → RabbitMQ → `--map` decrypt → upsert cycle
      manually before wiring the trigger.

**Phase 3 — triggers**
- [ ] `schedules.yaml`: cron entries for the central→branch reference-data
      scenarios.
- [ ] Confirm the new (Phase 1.5) `result_log` publish on export-broker
      actually reaches `tdtp:pipeline:<result_name>` end-to-end in this setup.
- [ ] `pubsub.yaml`: map that `result_name` to the corresponding import
      scenario (branch→central sales/customers; airline→central
      flights/bookings) — replaces `consumer.py`'s `QUEUE_HANDLERS`
      dict + `handle_notify`, same underlying `--map <mapping.yaml>
      --input broker://<queue>` call as today, just orchestrator-triggered
      instead of a Python subprocess call.
- [ ] `runners.yaml`: needs two non-default runner entries — export-broker
      (positional table/queue args) and `--map` (mapping YAML + broker URI
      args) — neither matches the default `--pipeline {{.tmpfile}}` shape.

**Phase 4 — governance**
- [ ] `pipeline-acl.yaml` (xZMercury): group + cost per scenario.
- [ ] Two orchestrator tokens: `branch-operator` (activator, scoped to
      branch-side scenarios), `central-operator` (activator, scoped to
      central-side scenarios). Admin token for approvals.
- [ ] Approve every scenario's checksum before first run
      (`POST /scenarios/{name}/approve`).

**Phase 5 — decommission old path, document**
- [ ] `coordinator.py` — genuinely replaced (export becomes a governed
      scenario); `consumer.py` — most of its logic (`QUEUE_HANDLERS`,
      `run_map_broker`'s `--map <yaml> --input broker://<queue>` call) is
      already correct and mostly just moves into `runners.yaml` +
      `pubsub.yaml`; only the Redis-subscribe loop and the standalone
      process itself go away, not the underlying approach.
- [ ] Decide: delete both scripts, or keep them under e.g. `legacy/` as an
      explicit "before" reference for the README's before/after story
      (leaning toward keeping — the contrast is the point of a showcase).
- [ ] Rewrite `TRAVEL-AGENCY.md` (or add a new top-level README) telling
      the before/after story: what governance is now enforced that wasn't,
      and why that matters for the two real integrations this example
      rehearses.

## Open questions to resolve before Phase 3

Resolved during planning (kept here for the record, not re-open): whether
the broker path needs its own `result_log`/encryption work — yes, confirmed
by direct grep, see Phase 1.5.

Still open:

- **Shared vs. per-node security stack.** Does this demo run ONE shared
  `tdtp-ca` + xZMercury + Redis pair (simpler, and arguably realistic — a
  single trust authority is normal even for geographically distributed
  nodes) with three separate orchestrator instances (one per node,
  matching the real topology), or does each node get its own full stack?
  Leaning toward one shared CA/xZMercury (that's the actual point of a CA —
  one root of trust for many environments), three orchestrators.
- `activity.py`'s RabbitMQ event → `coordinator.py` trigger: does this
  layer stay (a third trigger model, "external app event"), or does
  Phase 1's cron cadence replace the need for it entirely? Leaning toward
  keeping `activity.py` as the traffic simulator (it writes real data) but
  removing `coordinator.py` as the consumer of its events, once cron/pub-sub
  cover the actual sync triggering.
- MinIO/S3 audit marker (`consumer.py`'s current last step): does this stay
  as a separate write, or does the orchestrator's own per-job audit trail
  (`cancelled_by`/artifact SHA-256/log, already built) replace the need for
  a bespoke S3 marker entirely?

## What stays unchanged

- `setup/*.sql`, `activity.py` (as a traffic/data simulator), the three
  Postgres schemas, the overall Central/Branch/Airline topology and sync
  map from `TRAVEL-AGENCY.md`.
