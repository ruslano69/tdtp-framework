# orchestrator

Scenario execution server: a thin HTTP wrapper over pluggable command-line
runners (`tdtpcli` by default) with cron scheduling, token authentication,
and trust-chain enforcement.

## Why not just cron?

Cron (or Task Scheduler) plus a YAML-parameterized script gets you the
*execution* part for free — that's the least interesting piece here. What
this actually adds is a governance layer around execution that a bare
scheduler has no concept of:

- **Nothing runs unapproved.** Every scenario's exact content must have an
  admin-approved SHA-256 on record before it executes — by cron, by direct
  activation, or via the request workflow. Editing an already-approved
  scenario invalidates the approval; a new, never-reviewed one has none to
  begin with. A plain scheduler will happily run whatever file is sitting in
  the directory, no questions asked.
- **RBAC lives in the launcher, not the OS.** Consumer / activator / admin
  roles, per-token scenario allowlists, and job ownership (only your own job
  or an admin can stop/cancel it) are enforced by the service itself —
  cron/Task Scheduler have no equivalent; access control is whatever
  filesystem permissions happen to allow.
- **A submit → dry-run → approve/reject workflow** for callers who shouldn't
  run things directly — a lightweight four-eyes principle a bare scheduler
  has no way to express.
- **License/environment trust gates**: a scenario runs only if its declared
  permissions are covered by both an offline license and an online
  environment check (xZMercury) — business-level capability gating baked
  into the launch path itself.
- **Every job is audited**: who submitted it, who cancelled or stopped it and
  when, the output artifact's SHA-256, the full log — queryable in SQLite,
  not scattered across log files.
- **Process lifecycle done properly**: each job gets its own context
  independent of whatever triggered it, so a run started via the HTTP API
  keeps going after the request that started it returns; Stop sends SIGTERM
  and only force-kills after a grace period. Naive scheduler wrappers
  routinely get exactly this wrong.
- **The runner is pluggable, everything above is not.** `tdtpcli` is the
  default execution backend, but approval, RBAC, trust gates, and audit
  apply identically to any registered runner — the governance doesn't have
  to be rebuilt per tool.

What it deliberately is *not*: no distributed execution (one node, one
subprocess per job), no DAG/dependencies between scenarios, no retry/backoff
policy, no UI. It's not a workflow engine like Airflow or Argo — it's a
governed way to run one command at a time, safely, with a record of who
allowed it and who touched it.

## What it does

- **Scenarios** = a rendered text file + an optional `orchestrator:` header
  (params schema, required permissions, which runner it needs). A runner that
  doesn't understand the header (e.g. `tdtpcli`) just ignores the unknown key.
- **Runners**: which command actually executes a scenario is pluggable and
  named centrally (`--runners`), not hardcoded — `tdtpcli --pipeline` is the
  default, but any binary invocation can be registered and referenced by
  scenarios via `orchestrator.runner:`. See [Runners](#runners).
- **Activation**: `POST /scenarios/{name}/run` renders the scenario with
  params and runs its resolved runner as a subprocess; the job is tracked in
  SQLite, including which runner actually ran it.
- **Scenario approval**: nothing runs — cron, direct activation, or an approved
  request — unless an admin has approved the scenario's exact current content
  (SHA-256 pinned in SQLite). Closes both editing an already-known scenario
  unnoticed and planting a new, never-reviewed one. Since `orchestrator.runner:`
  lives inside that same hashed content, silently reassigning a scenario to a
  different (more dangerous) runner also invalidates its approval.
- **Scheduling**: cron entries in SQLite (seeded from YAML), managed at runtime.
- **Pub/sub triggers**: subscribe to `pkg/resultlog`'s pipeline-completion
  events (`tdtp:pipeline:*` — already published by any `tdtpcli --pipeline`
  run with `result_log.type: redis` configured) and run the scenario mapped
  to each `result_name`. Same approval/trust-gate/audit path as every other
  trigger. See [Pub/sub triggers](#pubsub-triggers).
- **Trust gates** (see preflight.go): own `tdtp.lic` (offline) + Mercury `/status`
  (online). A scenario runs only if its permissions are covered by both, and the
  licensed concurrent-pipeline limit is respected.
- **Authentication**: Bearer tokens with roles.

## Roles

| Role | Can |
|------|-----|
| `consumer` | read scenarios, jobs, results |
| `activator` | consumer + run scenarios (within token's scenario allowlist) + stop/cancel own jobs |
| `admin` | everything + schedules, token management, scenario approval, any job's stop/cancel |

On first run with an empty token table, a **bootstrap admin token** is generated
and printed once to the log. Store it immediately.

`--no-auth` disables authentication (local dev only — every request is admin).

## API

Auth: `Authorization: Bearer <token>` on every route except `/healthz`.

```
GET    /healthz                       public liveness

GET    /scenarios                     consumer  list scenarios
GET    /scenarios/{name}              consumer  scenario definition
POST   /scenarios/{name}/run          activator run with {params} → {job_id}
POST   /scenarios/{name}/approve      admin     approve currently loaded content
GET    /scenarios/{name}/approval     admin     view approval + whether content still matches
DELETE /scenarios/{name}/approval     admin     revoke approval

GET    /jobs                          consumer  recent jobs (100)
GET    /jobs/{id}                     consumer  job status + log
GET    /jobs/{id}/artifact            consumer  download output file (if any)
POST   /jobs/{id}/cancel              activator abort a pending job (own; admin: any)
POST   /jobs/{id}/stop                activator stop a running job (own; admin: any)
GET    /results/{scenario}            consumer  recent jobs for one scenario

GET    /schedules                     admin     list schedules
POST   /schedules                     admin     add schedule
PATCH  /schedules/{id}/enable         admin     resume
PATCH  /schedules/{id}/disable        admin     pause
DELETE /schedules/{id}                admin     remove

GET    /tokens                        admin     list tokens (no raw values)
POST   /tokens                        admin     issue token → raw shown once
DELETE /tokens/{id}                   admin     revoke token

POST   /requests                      consumer  propose a run (project request)
GET    /requests[?status=]            consumer  own requests (admin: all)
GET    /requests/{id}                 consumer  one request (own; admin: any)
POST   /requests/{id}/test            admin     dry-run: validate + trust gate
POST   /requests/{id}/approve         admin     execute → links job_id
POST   /requests/{id}/reject          admin     reject {note}
```

## Two ways to run a scenario

**Direct activation** — for trusted automation/users:
`POST /scenarios/{name}/run` (activator role) runs immediately.

**Project-request workflow** — for clients whose runs need approval:

```
client (consumer) → POST /requests {scenario, params, title}   status=pending
admin             → POST /requests/{id}/test                   dry-run verdict
admin             → POST /requests/{id}/approve                execute → job_id
   or             → POST /requests/{id}/reject {note}          status=rejected
```

A consumer can browse scenarios, submit proposals, and see only their own
requests. The admin reviews every pending request, can dry-run it (params +
trust gate, nothing executed), then approves (runs it) or rejects it with a note.
Approved requests carry the resulting `job_id` for traceability.

## Scenario approval

Scenario files load once at startup (`--scenarios`); editing one on disk, or
dropping in a new one, has no effect on what the orchestrator will run until
it restarts and re-loads that directory. Loading is not the same as
*approving* — execution (cron, `POST /scenarios/{name}/run`, or an approved
request) is refused with `403` unless the scenario's exact loaded content has
a matching admin-approved SHA-256 on record. A never-approved scenario and a
tampered one both fail the same way; there is no partial trust.

```bash
# Bless whatever content is currently loaded for this scenario.
curl -X POST http://localhost:8080/scenarios/export-payroll/approve \
  -H "Authorization: Bearer $ADMIN_TOKEN"
# → {"name":"export-payroll","sha256":"…","approved_by":"alice","approved_at":"…","enabled":true}

# Check status — "matches":false means the loaded content drifted since
# approval (edited + orchestrator restarted, without a re-approve) and
# execution is currently blocked.
curl http://localhost:8080/scenarios/export-payroll/approval \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

There is no way to submit a checksum by hand — `approve` always hashes
whatever the orchestrator actually has in memory, so an admin can only bless
real loaded content, never an arbitrary value.

## Stopping and cancelling jobs

Two operations, each valid in exactly one job state — calling the wrong one
for the job's current state returns `409`:

- **Cancel** (`POST /jobs/{id}/cancel`) — aborts a job still `pending`
  (queued but its subprocess hasn't started). No process to kill; it never runs.
- **Stop** (`POST /jobs/{id}/stop`) — requests termination of a `running`
  job: SIGTERM first, force-kill after a 10s grace period if it hasn't
  exited (Linux; on Windows SIGTERM isn't supported and it force-kills
  immediately after the grace period).

Both are asynchronous — the call returns once the request is recorded and
(for Stop) the process signalled, not once it has actually exited. The job
lands on `status=cancelled` when its goroutine observes the exit, with
`cancelled_by`/`cancelled_at` set. Only the job's own submitter or an admin
may call either.

### Issuing a token

```bash
curl -X POST http://localhost:8080/tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"branch-runner","role":"activator","scenarios":["export-payroll"]}'
# → {"token":"tdtp_…","note":"store this token now — it is not retrievable later"}
```

`scenarios: []` (or omitted) grants access to all scenarios.

## Job artifacts

After a successful job the orchestrator records the output file path, its
SHA-256 fingerprint, and byte size in the DB.

```bash
# Download the artifact for job abc123
curl -OJ -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/jobs/abc123/artifact
# → saves as the original filename (Content-Disposition: attachment)
```

`GET /jobs/{id}/artifact` returns `404` if the job produced no output file
(e.g. import-only pipeline, or the job failed).

---

## Run

```bash
orchestrator \
  --scenarios ./scenarios \
  --schedules-seed ./schedules \
  --db orchestrator.db \
  --tdtpcli ./tdtpcli \
  --license ./tdtp.lic \
  --mercury-url http://mercury:3000 \
  --require-prod \
  --addr :8080
```

| Flag | Default | Purpose |
|------|---------|---------|
| `--runners` | `` | path to `runners.yaml` (empty = synthesize a single `tdtpcli` runner from `--tdtpcli`) |
| `--license` | env/./tdtp.lic/community | tdtp.lic for offline capability gating |
| `--mercury-url` | `` | xZMercury base URL for online preflight (empty = skip) |
| `--require-prod` | false | refuse to start against dev-mode / non-CA-authorized Mercury |
| `--no-auth` | false | disable token auth (local dev only — every request is admin) |
| `--auth-type` | `token` | authentication type: `token` or `ldap` |
| `--ldap-url` | `` | LDAP server, e.g. `ldap://corp.example.com:389` (ldap mode) |
| `--ldap-bind-dn` | `` | service account DN for user search (ldap mode) |
| `--ldap-bind-pass` | `` | service account password (ldap mode) |
| `--ldap-base-dn` | `` | LDAP search base DN (ldap mode) |
| `--redis-addr` | `` | Redis address for pipeline-completion pub/sub triggers (empty = disabled) |
| `--redis-password` | `` | Redis password (pubsub trigger only) |
| `--redis-db` | `0` | Redis DB number (pubsub trigger only) |
| `--pubsub` | `` | path to `pubsub.yaml` mapping pipeline `result_name` → scenario (requires `--redis-addr`) |

### Token auth (default)

Bootstrap admin token is printed once on first run. After that, tokens are
managed via `POST /tokens` (admin only). In LDAP mode, `/tokens` returns 501
— group membership drives roles instead.

### LDAP auth

```bash
orchestrator \
  --auth-type ldap \
  --ldap-url ldap://dc.corp.local:389 \
  --ldap-bind-dn "cn=svc_orch,ou=service,dc=corp,dc=local" \
  --ldap-bind-pass "$LDAP_PASS" \
  --ldap-base-dn "dc=corp,dc=local" \
  --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
```

Group-to-role mapping is configured via `--ldap-*` flags (see `auth.go`
`LDAPConfig.RoleMap`). Default role when no group matches: `consumer`.
LDAP bind happens per-request using HTTP Basic Auth (`Authorization: Basic`).
In LDAP mode, `/tokens` endpoints are not available.

## Scenario file

```yaml
orchestrator:
  name: export-payroll
  description: "Payroll export by period"
  runner: tdtpcli              # optional — omit to use the configured default
  permissions: [etl, enc]      # must be covered by license ∩ Mercury env
  params:
    - name: period
      required: true
      pattern: '^\d{4}-\d{2}$'

# Below: normal pipeline YAML; {{.period}} substituted before tdtpcli runs.
sources:
  - name: payroll
    type: mssql
    query: "SELECT * FROM Payroll WHERE Period = '{{.period}}'"
```

## Runners

A runner is a named execution backend: a binary plus an argument template.
`orchestrator.runner:` in a scenario picks one by name; omitted, it falls
back to the configured default (`tdtpcli`, invoked exactly as before this
feature existed — nothing about an existing deployment changes unless you
opt in).

```yaml
# runners.yaml
runners:
  tdtpcli:
    binary: ./tdtpcli
    args: ["--pipeline", "{{.tmpfile}}"]
  python-etl:
    binary: python
    args: ["etl_runner.py", "--config", "{{.tmpfile}}", "--period", "{{.period}}"]
  powershell:
    binary: pwsh
    args: ["-File", "{{.tmpfile}}"]
```

```bash
orchestrator --scenarios ./scenarios --db orchestrator.db --runners ./runners.yaml
```

Each arg is rendered with the same `{{.param}}` substitution as the scenario
body, plus one reserved key: `{{.tmpfile}}` — the path to the scenario's
rendered content. An arg that references a param with no value errors before
the subprocess ever starts; a param nobody's args reference is simply unused.

Binary paths and invocation shape live in `runners.yaml`, not in the scenario
file — scenarios stay portable across environments where `python` or
`tdtpcli` sit at different paths. Passing `--tdtpcli <path>` without
`--runners` still works exactly as before: it synthesizes a single `tdtpcli`
runner behind the scenes.

Every job records which runner actually executed it (`GET /jobs/{id}` →
`"runner"`) — if a scenario's declared runner ever changes, past jobs still
show what really ran them, not what the scenario currently says. A scenario
naming an unregistered runner fails the orchestrator's startup with a clear
error, and `Submit()` refuses it at request time too, before touching the DB.

## Pub/sub triggers

`pkg/resultlog` already publishes after every `tdtpcli --pipeline` run
configured with `result_log.type: redis` in its own YAML (see
`examples/07-redis-orchestration` for that side, which predates and is
independent of the orchestrator):

```
SET  tdtp:pipeline:<result_name>:state  <JSON>  EX <ttl>   ← for polling
PUB  tdtp:pipeline:<result_name>        <JSON>              ← for event-driven
```

Passing `--redis-addr` subscribes the orchestrator to `tdtp:pipeline:*` —
one connection covers every pipeline; adding a new one needs no orchestrator
restart, only a `pubsub.yaml` entry:

```yaml
# pubsub.yaml
subscriptions:
  - result_name: MASK_V001
    scenario: reconcile-mask-sync
    on_status: [success]        # default; failed/completed_with_errors are opt-in
    params:
      note: triggered-by-pipeline-completion   # static, merged with pipeline_name/result_name/status
```

```bash
orchestrator --scenarios ./scenarios --db orchestrator.db \
  --redis-addr localhost:6379 --pubsub ./pubsub.yaml
```

**The event never picks the scenario or bypasses anything.** `result_name`
is only ever used as a lookup key into the admin-configured mapping above —
nothing in the message payload can name an arbitrary scenario. Everything
downstream of that lookup goes through the exact same path as cron and
manual activation: scenario approval, trust gate, runner resolution, job
audit. An unapproved or gate-refused scenario is silently skipped (logged),
not run.

A limited, whitelisted set of the pipeline result — `pipeline_name`,
`result_name`, `status` — is available as scenario params (only if the
scenario declares them, same as any other param), merged with whatever
static `params:` the subscription sets. Nothing else from the payload
reaches the scenario.

Redis pub/sub is at-most-once — if the orchestrator is down when an event
fires, it's gone. This is fine for a "go check what changed" nudge (the real
data already sits durably wherever the pipeline wrote it) but not a
substitute for a queue if the event itself is the only carrier of data that
matters. `GET /healthz` → `"pubsub"` reports `"connected"` / `"disconnected"`
/ `"skip"` (not configured) — a dead broker doesn't fail silently.

## Schedule seed file

```yaml
schedules:
  - id: monthly-payroll
    scenario: export-payroll
    schedule: "0 2 1 * *"        # 1st of month, 02:00
    params:
      period: "{{current_month}}"   # → 2026-06 at run time
```

Magic params: `{{current_month}}`, `{{current_date}}`, `{{yesterday}}`.

## Adding scenarios

Step-by-step guide for one-off vs periodic and plain vs encrypted scenarios:
[docs/ORCHESTRATOR_SCENARIOS.md](../../docs/ORCHESTRATOR_SCENARIOS.md).
