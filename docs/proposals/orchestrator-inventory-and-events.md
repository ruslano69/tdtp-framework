# Proposal: orchestrator inventory and outbound events

**Status: DRAFT.** Part 3, steps 1 and 2 (outbox, dispatcher, `log` and webhook
sinks, admin routes, metrics) are implemented on `feat/orchestrator-inventory-events`.
The RabbitMQ sink and Part 1 are not. Target: 2.0. Covers two items from the
"Behind the freeze" list in `TODO_NEXT.md`: pre-run inventory of scenario
targets (item 1) and outbound job events (item 3). Nothing here changes how a
scenario runs; both features only observe the orchestrator's state.

Facts below were checked against the code at `main` (`d5eea13`), not taken
from the README.

---

## 0. Context and constraints

- The orchestrator is one node: SQLite, one `tdtpcli` subprocess per job
  (`cmd/orchestrator/executor.go`, `Submit`). `README.md` deliberately excludes
  a DAG between scenarios and any retry policy.
- `TODO_NEXT.md` § 2.4 settles the division of labour: a pipeline run **fails
  loud and fast**; retry and alerting belong to the supervisor, which is this
  service. Inventory and events fit that line: they report, they do not retry.
- Trust boundary. A scenario is approved by SHA-256 (`scenario_approval.go`),
  but its YAML is still data written by its author. Where events go and which
  probes run is therefore configured **by the administrator in a file the
  orchestrator loads at startup**, never by a scenario. The same rule already
  holds for `pubsub.yaml`, which maps `result_name` to a scenario and is never
  read from message content.
- Secrets. The example pipelines carry credentials inline (for example
  `examples/travel-agency/pipelines/extract_tours.yaml` has
  `postgres://tdtp:tdtp@…`). `pkg/etl` does not expand environment variables
  (`grep` for `ExpandEnv` in `pkg/etl` and `cmd/tdtpcli` is empty). The
  orchestrator's own new configs therefore use an explicit `env:VAR` reference,
  so this proposal does not add a third place where plaintext secrets live.

---

# Part 1 — Inventory of scenario targets

## 1.1 Purpose

Answer one question before cron fires: **can every scenario run right now?**
The inventory checks reachability of the endpoints a scenario depends on. It
never executes a scenario, never reads table data, and never writes anything
to a target.

Today a dead source is discovered at 03:00 by a failed job. `preflight.go`
covers only the licence and Mercury; it says nothing about data endpoints.

## 1.2 What is probed

Targets are derived from each scenario's pipeline YAML, rendered with resolved
parameters:

| Where in the pipeline | Probe | Writes anything? |
|---|---|---|
| `sources[].type` = `postgres`, `mysql`, `mssql`, `oracle` | open a connection through `adapters.New(ctx, cfg)`, then close | no |
| `sources[].type` = `sqlite` | file exists and is readable | no |
| `sources[].type` = `csv`, `tdtp`, `tdtp-enc` | local file exists | no |
| `sources[].type` = `tdtp-s3` (or `dsn` is `s3://…`) | `Stat` of the object | no |
| `output.type` = `rabbitmq` | connect, then `Ping` (no queue declaration) | no |
| `output.type` = `kafka` | `Ping` | no |
| `output.type` = `tdtp` / `xlsx`, local path | parent directory exists and is writable (no file is created) | no |
| `output.type` = `tdtp`, `destination: s3://…` | `Stat` of the bucket (see 1.6) | no |
| `result_log.type` = `redis` | `PING` | no |

Licence and Mercury are **not** probed here; they stay in `preflight.go` and
the inventory links to them rather than repeating them.

**Optional `SELECT 1`.** Not in the default set. Some roles lack rights to
run anything, and a connect succeeding is the signal that matters for "can
this scenario start". An operator can enable it per target if they want a
stronger check.

## 1.3 How targets are derived

`cmd/orchestrator` does **not** import `pkg/etl` to parse the pipeline. That
package links kafka-go, RabbitMQ, excelize and kanzi (see the leaf-package
comment in `pubsub.go`), and the orchestrator deliberately avoids them.

Proposal: a small leaf package `pkg/etl/targets` (or `cmd/orchestrator/targets`)
with structs for exactly the fields this feature reads: `sources`, `output`,
`result_log`. Two guards:

1. A test that reflects over `etl.PipelineConfig` and checks that every key the
   leaf package names exists there with the same YAML tag. If `pkg/etl` renames
   a key, the test fails, instead of the inventory silently probing nothing.
2. The leaf package ignores unknown keys (as `tdtpcli` does), so scenarios that
   use features the inventory does not model still parse.

**Parameters.** Render with `resolveMagicParams` (for example `{{SYNC_TS}}`)
plus declared defaults. A required parameter with no default leaves the target
`unresolved` — reported, not treated as an error, because the scenario may be
run later with that parameter supplied.

**Deduplication.** Targets are keyed by kind and a normalised endpoint with
userinfo removed: `postgres://host:5432/db`, `rabbitmq://host:5672/vhost`,
`file:/abs/path`, `s3://bucket`. A database used by ten scenarios is probed once.

## 1.4 Results and error classes

Each probe returns a status and a **class**, not the driver's text. Driver
messages often contain the host and user name, and they can echo fragments of
the DSN.

| Class | Meaning |
|---|---|
| `ok` | connected / exists / writable |
| `auth` | authentication refused |
| `network` | connection refused, no route, DNS |
| `timeout` | probe deadline exceeded |
| `not_found` | file, object or bucket missing |
| `permission` | file or directory not writable/readable |
| `other` | anything else — raw message goes to the orchestrator log only |

Classification is by error type where the driver exposes one, and by a short
list of substrings otherwise. The list is tested; an unclassified error is
`other`, never guessed.

## 1.5 Limits and concurrency

- Per target: 5 s. Whole run: 30 s. Parallelism: 8.
- One run at a time (mutex). A request while a run is in flight returns that
  run's handle, not a second run.
- A snapshot younger than 60 s is returned as is, unless the caller asks for a
  fresh run. This stops the API being used to hammer production databases.
- Context cancellation is honoured by every probe.

## 1.6 S3 specifics

Two findings from the live SeaweedFS run (2026-10-11) shape this:

- `ListBuckets` returned an empty list while `tdtp-test` existed and accepted
  objects. A bucket check must not depend on it.
- The probe therefore uses `Stat` on a key, or `HeadBucket` where the driver
  gains one. `EnsureBucket` creates a bucket, so it must **not** be used for
  probing; it is only a test helper.

A missing object (404) means the endpoint is up: the classifier returns
`not_found`, not `network`. Only a refused or unreachable connection is
`network`.

## 1.7 Storage

```sql
CREATE TABLE IF NOT EXISTS inventory_snapshots (
  run_id      TEXT NOT NULL,
  run_at      DATETIME NOT NULL,
  target_key  TEXT NOT NULL,     -- normalised endpoint, no userinfo
  kind        TEXT NOT NULL,     -- db | file | s3 | broker | redis | dir
  status      TEXT NOT NULL,     -- ok | degraded | unresolved
  error_class TEXT,              -- see 1.4; NULL when ok
  latency_ms  INTEGER,
  scenarios   TEXT NOT NULL,     -- JSON array of scenario names using it
  PRIMARY KEY (run_id, target_key)
);
```

Keeping history (not just the last run) is deliberate: item 5 of the 2.0 list
(dead-man alert) needs "unreachable since when".

Retention: the same policy as job logs (`--log-retention-weeks`).

## 1.8 Surface

| Route | Role | Returns |
|---|---|---|
| `GET /inventory` | admin | last snapshot: every target with status, class, latency, scenarios |
| `POST /inventory/run` | admin | start a run now (subject to 1.5) |
| `GET /scenarios/{name}/inventory` | consumer | aggregate only: `ok` / `degraded` / `unresolved`, no hosts |

Consumers do not see hosts or endpoints: a list of internal database addresses
is topology information, and the consumer role is wider than operators.

CLI, for CI and cron outside the service:

```
orchestrator --scenarios ./scenarios --inventory-once
```

Prints a table and exits 1 if any target is `degraded`. Exit code 2 for an
inventory that could not run at all.

Periodic runs: `--inventory-interval 15m` starts a goroutine. Not via the
existing scheduler — a schedule is a job, and an inventory run is not a
scenario.

Metrics: `orchestrator_inventory_target_up{kind}` (gauge, 0/1) and
`orchestrator_inventory_last_run_timestamp`.

## 1.9 Tests

- Derivation: the examples under `examples/travel-agency/pipelines/` and the
  YAML fixtures in `pkg/etl` must yield the expected targets.
- Key guard: reflection test against `etl.PipelineConfig` (see 1.3).
- Redaction: no userinfo or password appears in any snapshot or API response.
- Classification: a table of driver errors, including a PostgreSQL wrong
  password (`auth`) and a closed port (`network`).
- Probes do not write: the RabbitMQ probe must not declare a queue; the S3
  probe must not `PUT`.
- Live: PostgreSQL, MySQL, MSSQL and Oracle probes run in the existing `ci.yml`
  integration job. Extend the step's guard pattern so a skip fails that job,
  as `CLAUDE.md` requires for every new live test.

## 1.10 Open questions

1. Is the optional `SELECT 1` wanted at all, or is connect enough by default?
2. Should consumers see the aggregate per scenario, or only admins see anything?
3. Default `--inventory-interval`: off, or 15 minutes?

---

# Part 3 — Outbound job events

## 3.1 Purpose

Let a system that does not read Redis learn that a job finished: which scenario,
which job, success or failure, how long, and the artifact's SHA-256. Today the
orchestrator only **consumes** events (`pubsub.go`); it publishes nothing of its own.

## 3.2 Events

| Type | When | Default |
|---|---|---|
| `job.finished` | after the job reaches `done`, `failed` or `cancelled` | on |
| `job.started` | after the job moves to `running` | off per sink |

Not in this proposal: `schedule.missed` and `inventory.degraded`. Both need
Part 1 and the dead-man logic of 2.0 item 5; they become events once those exist.

## 3.3 Payload

```json
{
  "event_id": "9f0c…",            // UUID, stable across retries — the dedupe key
  "type": "job.finished",
  "occurred_at": "2026-10-11T17:40:02Z",
  "job": {
    "id": "…",
    "scenario": "sync-tours",
    "schedule_id": "…",           // null for a manual run
    "submitted_by": "…",          // principal id; null for cron
    "status": "done",             // done | failed | cancelled
    "started_at": "…",
    "finished_at": "…",
    "duration_ms": 1842,
    "runner": "tdtpcli"
  },
  "artifact": { "sha256": "…", "size": 10240 },   // null if none
  "error": null,                  // see below
  "params": null                  // only when include_params is set on the sink
}
```

- **No log.** Job output can contain row data and driver error text with
  hosts in it. Logs stay in the job record, behind the existing RBAC.
- **`error`** is the process error (`exit status 1`), not the output. Today
  `Submit` sets `errMsg` from `runErr.Error()`, so this is already the shape.
- **`params`** are off by default. They are visible to admins through the API;
  pushing them to a third party is an extra disclosure that should be an
  explicit choice per sink.

## 3.4 Delivery: transactional outbox

Writing the event after the status update leaves a window where the job is
done and the event is lost. Writing it before risks announcing a status that
never committed.

**Proposal:** insert the event into an `outbox` table in the same SQL
transaction as the status update.

- `UpdateJobDone` (`db.go:339`) becomes a transaction that updates `jobs`,
  updates `schedules.last_status` (as `syncScheduleStatus` does now) and inserts
  one outbox row per matching sink. Callers in `executor.go` do not change shape.
- A dispatcher goroutine reads `pending` rows where `next_attempt_at <= now`,
  sends them, and marks them `sent`, or reschedules them.
- On start, the dispatcher picks up anything left `pending`. A crash between
  the commit and the send loses nothing.

```sql
CREATE TABLE IF NOT EXISTS outbox (
  id              TEXT PRIMARY KEY,       -- = event_id, one per row
  job_id          TEXT NOT NULL,          -- ordering key with sink (added in implementation)
  sink            TEXT NOT NULL,
  type            TEXT NOT NULL,
  payload         TEXT NOT NULL,          -- JSON, built once per row
  status          TEXT NOT NULL,          -- pending | sent | dead
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,          -- fixed-width UTC, compared as text
  last_error      TEXT,                   -- error class, not raw text (see 1.4)
  created_at      TEXT NOT NULL,
  sent_at         TEXT
);
CREATE INDEX IF NOT EXISTS idx_outbox_due ON outbox(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_outbox_job ON outbox(job_id, sink);
```

Two rules the implementation adds to the list above:

- **`event_id` is per row, not per job.** A job that notifies two sinks
  produces two rows with two ids. A receiver dedupes within its own sink, which
  is all it can see anyway.
- **A row waits for an earlier pending row of the same job and sink.** This is
  what makes "`job.started` before `job.finished`" true when the first attempt
  of `job.started` fails. A dead row does not hold anything back.

**Delivery guarantee: at-least-once.** A receiver deduplicates on `event_id`.
Ordering is guaranteed only within one job (`job.started` before
`job.finished`); across jobs there is no order.

**Retries.** Exponential backoff from 1 s to 5 min, at most 10 attempts
(configurable). After that the row becomes `dead`. A dead row is visible in
the API and in metrics and logged at error level. It does not block other
events or any job. This is the "fail loud" rule from `TODO_NEXT.md` § 2.4
applied to events: the failure is kept, and nothing retries forever.

**Retention.** `sent` rows are deleted after 7 days. `dead` rows are kept until
an operator retries or deletes them.

**No silent drop.** There is no size limit that discards events. If `pending`
grows past a threshold, a metric and a log line say so.

## 3.5 Sinks

### 3.5.1 Webhook

- `POST`, `Content-Type: application/json`, body is the stored payload.
- Headers: `X-TDTP-Event-ID` (same as `event_id`), `X-TDTP-Timestamp`,
  `X-TDTP-Signature: sha256=<HMAC-SHA256(secret, timestamp + "." + body)>`.
  The timestamp inside the signed string lets a receiver reject replays.
- The secret comes from `env:VAR`. Startup fails if the variable is unset.
- Timeout 5 s. Redirects are **not followed**, so the signature cannot be sent
  to a host the administrator did not name.
- TLS required. `allow_insecure_http: true` is accepted only for `localhost`
  and `127.0.0.1`, for development.
- Response handling:
  - `2xx` → `sent`
  - `408`, `429`, `5xx`, network error, timeout → retry with backoff
  - other `4xx` → `dead` at once; retrying a rejected request does not help

### 3.5.2 RabbitMQ

- Publish through `pkg/brokers` (`Exchange`, `RoutingKey` are already fields
  in `brokers.Config`). `MessageId` = `event_id`, persistent delivery mode.
- **Blocking issue.** `pkg/brokers/rabbitmq.go` publishes with
  `PublishWithContext` and no publisher confirms (`grep` for `Confirm` is
  empty). Without confirms an acknowledged-by-broker send is not guaranteed,
  and the outbox guarantee above would be false for this sink. The sink cannot
  ship until confirms are enabled for it. That change touches the shared
  `pkg/brokers` code, so it is its own step (see 3.8).
- The queue is not declared by the sink. The administrator declares it;
  `PassiveDeclare` is used as a check, as the existing config does.

### 3.5.3 Kafka

Not in this proposal. `pkg/brokers` has a Kafka implementation, but the
ETL-side spool (`kafka_spool.go`) is heavy and its semantics differ. Revisit
after the RabbitMQ sink.

## 3.6 Configuration

A file loaded with `--events events.yaml`. Absent flag = no sinks, no outbox
writes.

```yaml
sinks:
  - name: crm-hook
    type: webhook
    url: https://crm.example.com/hooks/tdtp
    secret: env:CRM_HOOK_SECRET
    types: [job.finished]
    scenarios: ["sync-*"]           # glob; empty = all
    statuses: [failed, cancelled]   # empty = all
    include_params: false
    max_attempts: 10

  - name: ops-queue
    type: rabbitmq
    rabbitmq:
      host: mq.internal
      port: 5672
      user: env:OPS_MQ_USER
      password: env:OPS_MQ_PASS
      vhost: /
      exchange: tdtp.events
      routing_key: job.finished
    types: [job.finished]
    statuses: []
```

Rules:

- `env:` is the only way to reference a secret in this file. A literal value
  is accepted only if its key is not one of the secret fields — and the loader
  logs a warning for any literal password it sees.
- Validation at startup, fail fast: URL scheme, env variable present, glob
  compiles, `types` and `statuses` drawn from the known sets.
- **Scenarios cannot declare sinks.** The sink list is read from this file
  only. Nothing in a scenario YAML affects where events go.

## 3.7 Surface

| Route | Role | Returns |
|---|---|---|
| `GET /events/sinks` | admin | sinks, enabled, last delivery time and class (no secrets) |
| `GET /events/outbox?status=dead` | admin | dead and pending rows, error classes |
| `POST /events/outbox/{id}/retry` | admin | move a dead row back to pending |

Consumers see nothing here. Metrics:
`orchestrator_events_sent_total{sink,type}`,
`orchestrator_events_failed_total{sink,class}`,
`orchestrator_outbox_pending`, `orchestrator_outbox_dead`.

## 3.8 Where the code changes

| File | Change |
|---|---|
| `cmd/orchestrator/db.go` | `outbox` table; `UpdateJobDone` becomes one transaction with the outbox insert |
| `cmd/orchestrator/executor.go` | none in shape: the two terminal paths (dispatch-cancelled and run-finished) keep calling `UpdateJobDone`; `job.started` is one insert after `UpdateJobStatus(JobRunning)` |
| `cmd/orchestrator/events.go` (new) | config loader, payload builder, dispatcher, backoff |
| `cmd/orchestrator/events_webhook.go` (new) | webhook sink |
| `cmd/orchestrator/events_rabbitmq.go` (new) | RabbitMQ sink, built on `pkg/brokers` |
| `pkg/brokers/rabbitmq.go` | publisher confirms — shared code, separate PR |
| `cmd/orchestrator/main.go` | `--events` flag |

## 3.9 Tests

- Transaction: a fault injected between the status update and the outbox
  insert leaves **neither** written. A fault after commit leaves the event
  `pending` and the dispatcher delivers it after restart (reopen the same
  SQLite file, start a new executor).
- Webhook (`httptest`): signature verifies with the secret and timestamp;
  `5xx` retried, `4xx` dead at once; a redirect is not followed; a slow
  response times out.
- Redaction: the payload contains no `log`, no DSN, no `password`, and no
  `params` unless the sink asks for them.
- Ordering: `job.started` precedes `job.finished` for the same job.
- RabbitMQ: live test in the `ci.yml` integration job, with the step's guard
  pattern extended so a skip fails the job. Runs only after confirms land.

---

# Delivery order

Rough sizes, for planning only:

1. **Outbox and `job.finished` to a logging-only sink** — the riskiest part
   (transactions), no network. Recovery test first. About 2–3 days.
   *Implemented: `events.go`, `outbox.go`, `FinishJob` in `db.go`, tests in
   `events_test.go`. Fault checks: making the finish non-atomic, or dropping
   the per-job ordering, each fails a test.*
2. **Webhook sink, admin routes, metrics.** About 2 days.
   *Implemented: `events_webhook.go`, `events_routes.go`, metrics in
   `metrics.go`. Deviations from the text above: a literal `secret` is refused
   rather than warned about; a 3xx reply is a permanent refusal (`http_3xx`),
   not a redirect to follow; transport errors have the URL removed from their
   text, since the URL can carry a token. Checked end to end against a Python
   receiver that verifies the HMAC and answers 503 once.*
3. **RabbitMQ publisher confirms** (shared `pkg/brokers`), then the RabbitMQ
   sink with live CI. About 2–3 days.
4. **Inventory: derivation and probes, `--inventory-once`.** About 2–3 days.
5. **Inventory: storage, routes, interval, metrics.** About 2 days.

Events and inventory are independent. Item 4 can run in parallel with 1–3.
The event `inventory.degraded` waits for both sides.

---

## Not in this proposal

- Returning job logs in events, under any setting.
- Scenario-declared sinks or probes.
- Retrying a job from the outbox or from inventory. That is the supervisor
  layer (`TODO_NEXT.md` § 2.4), a separate design.
- A Redis publisher for orchestrator events. The orchestrator already consumes
  Redis; adding a mirror image is possible later, but a webhook covers the
  same consumers without a second broker dependency.
