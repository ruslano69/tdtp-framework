# orchestrator

Scenario execution server: a thin HTTP wrapper over `tdtpcli --pipeline` with
cron scheduling, token authentication, and trust-chain enforcement.

## What it does

- **Scenarios** = existing pipeline YAML + an optional `orchestrator:` header
  (params schema, required permissions). `tdtpcli` ignores the header.
- **Activation**: `POST /scenarios/{name}/run` renders the YAML with params and
  runs `tdtpcli --pipeline` as a subprocess; the job is tracked in SQLite.
- **Scenario approval**: nothing runs — cron, direct activation, or an approved
  request — unless an admin has approved the scenario's exact current content
  (SHA-256 pinned in SQLite). Closes both editing an already-known scenario
  unnoticed and planting a new, never-reviewed one.
- **Scheduling**: cron entries in SQLite (seeded from YAML), managed at runtime.
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
| `--license` | env/./tdtp.lic/community | tdtp.lic for offline capability gating |
| `--mercury-url` | `` | xZMercury base URL for online preflight (empty = skip) |
| `--require-prod` | false | refuse to start against dev-mode / non-CA-authorized Mercury |
| `--no-auth` | false | disable token auth (local dev only — every request is admin) |
| `--auth-type` | `token` | authentication type: `token` or `ldap` |
| `--ldap-url` | `` | LDAP server, e.g. `ldap://corp.example.com:389` (ldap mode) |
| `--ldap-bind-dn` | `` | service account DN for user search (ldap mode) |
| `--ldap-bind-pass` | `` | service account password (ldap mode) |
| `--ldap-base-dn` | `` | LDAP search base DN (ldap mode) |

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
