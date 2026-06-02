# orchestrator

Scenario execution server: a thin HTTP wrapper over `tdtpcli --pipeline` with
cron scheduling, token authentication, and trust-chain enforcement.

## What it does

- **Scenarios** = existing pipeline YAML + an optional `orchestrator:` header
  (params schema, required permissions). `tdtpcli` ignores the header.
- **Activation**: `POST /scenarios/{name}/run` renders the YAML with params and
  runs `tdtpcli --pipeline` as a subprocess; the job is tracked in SQLite.
- **Scheduling**: cron entries in SQLite (seeded from YAML), managed at runtime.
- **Trust gates** (see preflight.go): own `tdtp.lic` (offline) + Mercury `/status`
  (online). A scenario runs only if its permissions are covered by both, and the
  licensed concurrent-pipeline limit is respected.
- **Authentication**: Bearer tokens with roles.

## Roles

| Role | Can |
|------|-----|
| `consumer` | read scenarios, jobs, results |
| `activator` | consumer + run scenarios (within token's scenario allowlist) |
| `admin` | everything + schedules + token management |

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

GET    /jobs                          consumer  recent jobs (100)
GET    /jobs/{id}                     consumer  job status + log
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

### Issuing a token

```bash
curl -X POST http://localhost:8080/tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"branch-runner","role":"activator","scenarios":["export-payroll"]}'
# → {"token":"tdtp_…","note":"store this token now — it is not retrievable later"}
```

`scenarios: []` (or omitted) grants access to all scenarios.

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

| Flag | Purpose |
|------|---------|
| `--license` | tdtp.lic for offline capability gating (default: env/./tdtp.lic/community) |
| `--mercury-url` | xZMercury base URL for online preflight (empty = skip) |
| `--require-prod` | refuse to start against a dev-mode / non-CA-authorized Mercury |
| `--no-auth` | disable token auth (local dev only) |

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
