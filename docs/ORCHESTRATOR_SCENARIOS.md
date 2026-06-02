# Adding scenarios to the orchestration server

A **scenario** is an ordinary `tdtpcli` pipeline YAML with an optional
`orchestrator:` header. The orchestrator renders the YAML with parameters and
runs `tdtpcli --pipeline` for you, tracking each run as a job.

This guide covers four combinations:

|              | Plain                         | Encrypted                       |
|--------------|-------------------------------|---------------------------------|
| **One-off**  | drop file → `POST …/run`      | add encryption block → `POST …/run` |
| **Periodic** | drop file → add a schedule    | add encryption block → add a schedule |

## Where files live

```
scenarios/        scenario YAML files (one per scenario)
schedules/        schedule seed YAML (loaded into the DB once on startup)
orchestrator.db   SQLite: jobs, schedules (runtime source of truth), tokens
```

Start the server pointing at those directories:

```bash
orchestrator --scenarios ./scenarios --schedules-seed ./schedules \
  --db orchestrator.db --tdtpcli ./tdtpcli \
  --license ./tdtp.lic --mercury-url http://mercury:3000 --addr :8080
```

New scenario files are picked up at **startup**. After dropping a file into
`scenarios/`, restart the orchestrator (or POST it via a future hot-reload).

---

## 1. Plain scenario (one-off)

`scenarios/export-users.yaml`:

```yaml
orchestrator:
  name: export-users
  description: "Export the users table for a given status"
  permissions: [etl]               # must be covered by license ∩ Mercury env
  params:
    - name: status
      required: true
      pattern: '^(active|inactive)$'

# ── normal tdtpcli pipeline below; {{.status}} substituted before the run ──
sources:
  - name: users
    type: postgres
    dsn: "postgres://user:pass@db:5432/app?sslmode=disable"
    query: "SELECT id, email, status FROM users WHERE status = '{{.status}}'"

output:
  type: tdtp
  tdtp:
    destination: "/exports/users_{{.status}}.tdtp.xml"
    format: "xml"
    compression: true
```

Run it once (activator token required):

```bash
curl -X POST http://localhost:8080/scenarios/export-users/run \
  -H "Authorization: Bearer $ACTIVATOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"params":{"status":"active"}}'
# → {"job_id":"…"}

# Check status + log
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/jobs/<job_id>
```

---

## 2. Plain scenario (periodic)

Reuse the same scenario file; add a schedule seed.

`schedules/users-nightly.yaml`:

```yaml
schedules:
  - id: users-nightly
    scenario: export-users
    schedule: "0 3 * * *"          # every day at 03:00 (cron)
    params:
      status: "active"
```

Seed files are loaded into the DB on startup (idempotent). After that, manage
schedules at runtime via the admin API — no file editing:

```bash
# Add a schedule at runtime
curl -X POST http://localhost:8080/schedules \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"id":"users-hourly","scenario":"export-users","cron_expr":"0 * * * *","params":{"status":"active"}}'

# Pause / resume / remove
curl -X PATCH  http://localhost:8080/schedules/users-hourly/disable -H "Authorization: Bearer $ADMIN_TOKEN"
curl -X PATCH  http://localhost:8080/schedules/users-hourly/enable  -H "Authorization: Bearer $ADMIN_TOKEN"
curl -X DELETE http://localhost:8080/schedules/users-hourly         -H "Authorization: Bearer $ADMIN_TOKEN"

# Inspect (admin): last_run_at, last_status, next_run_at
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8080/schedules
```

### Time-relative parameters

Schedules support magic values resolved at fire time:

| Token              | Value example |
|--------------------|---------------|
| `{{current_month}}`| `2026-06`     |
| `{{current_date}}` | `2026-06-02`  |
| `{{yesterday}}`    | `2026-06-01`  |

```yaml
schedules:
  - id: payroll-monthly
    scenario: export-payroll
    schedule: "0 2 1 * *"          # 1st of each month, 02:00
    params:
      period: "{{current_month}}"   # → 2026-06 when it fires
```

---

## 3. Encrypted scenario (one-off)

Encryption is a property of the **pipeline output**, not of the orchestrator.
Add an `encryption: true` output and a `security.mercury_url`. The key is bound
in xZMercury and burned on first read by the consumer.

`scenarios/export-payroll-enc.yaml`:

```yaml
orchestrator:
  name: export-payroll-enc
  description: "Encrypted payroll export by period"
  permissions: [etl, enc]          # 'enc' must be in license AND Mercury env
  params:
    - name: period
      required: true
      pattern: '^\d{4}-\d{2}$'

sources:
  - name: payroll
    type: mssql
    dsn: "sqlserver://user:pass@nav:1433?database=NAV"
    query: "SELECT * FROM Payroll WHERE Period = '{{.period}}'"

output:
  type: tdtp
  tdtp:
    destination: "/exports/payroll_{{.period}}.tdtp.enc"
    format: "xml"
    compression: true
    encryption: true               # AES-256-GCM via xZMercury

security:
  mercury_url: "http://mercury:3000"
  key_ttl_seconds: 3600
  mercury_timeout_ms: 5000
```

Run it the same way:

```bash
curl -X POST http://localhost:8080/scenarios/export-payroll-enc/run \
  -H "Authorization: Bearer $ACTIVATOR_TOKEN" -H "Content-Type: application/json" \
  -d '{"params":{"period":"2026-06"}}'
```

The output is a `.tdtp.enc` blob. The consumer decrypts it with:

```bash
TDTPCLI_CALLER=svc_central tdtpcli --import payroll_2026-06.tdtp.enc \
  --mercury-url http://mercury:3000
```

> The orchestrator requires `permissions: [..., enc]` for encrypted scenarios.
> The run is rejected (403) unless BOTH the orchestrator's `tdtp.lic` and the
> Mercury environment's licensed permissions include `enc`.

---

## 4. Encrypted scenario (periodic)

Same encrypted scenario file; add a schedule:

`schedules/payroll-enc-monthly.yaml`:

```yaml
schedules:
  - id: payroll-enc-monthly
    scenario: export-payroll-enc
    schedule: "0 2 1 * *"
    params:
      period: "{{current_month}}"
```

Scheduled encrypted runs go through the same trust gate as manual ones: if the
license expires or `enc` is revoked, the scheduled run is skipped (recorded as
`failed`), never silently producing plaintext.

---

## Permissions cheat-sheet

`orchestrator.permissions` in a scenario must be a subset of:

- the orchestrator's own `tdtp.lic` features (offline), AND
- the Mercury environment's licensed permissions (online, via `/status`),
  when `--mercury-url` is configured.

| Scenario needs | Required in license + Mercury |
|----------------|-------------------------------|
| plain export   | `etl`                         |
| encrypted      | `etl`, `enc`                  |
| S3 destination | `etl`, `s3`                   |

A run that asks for a permission missing from either side returns **403**.

## Roles needed

| Action                         | Minimum role |
|--------------------------------|--------------|
| view scenarios / jobs / results| consumer     |
| run a scenario (`POST …/run`)  | activator    |
| add/remove schedules, tokens   | admin        |

Activator tokens may be scoped to specific scenarios; see
[cmd/orchestrator/README.md](../cmd/orchestrator/README.md).

---

## Client submit → admin approve workflow

Direct `POST /scenarios/{name}/run` is for trusted activators. For clients whose
runs must be reviewed first, use the project-request workflow.

A **client** (consumer role) browses scenarios and submits a proposal:

```bash
# Browse what I'm allowed to run
curl -H "Authorization: Bearer $CLIENT" http://localhost:8080/scenarios

# Submit a project request (proposal) — not executed yet
curl -X POST http://localhost:8080/requests \
  -H "Authorization: Bearer $CLIENT" -H "Content-Type: application/json" \
  -d '{"scenario":"export-payroll","title":"June payroll","params":{"period":"2026-06"}}'
# → {"id":"…","status":"pending", …}

# See my own requests (consumers see only their own)
curl -H "Authorization: Bearer $CLIENT" http://localhost:8080/requests
```

An **admin** reviews pending requests, dry-runs them, then approves or rejects:

```bash
# All pending proposals
curl -H "Authorization: Bearer $ADMIN" "http://localhost:8080/requests?status=pending"

# Dry-run: validates params + trust gate, executes NOTHING
curl -X POST -H "Authorization: Bearer $ADMIN" \
  http://localhost:8080/requests/$RID/test
# → {"would_run":true,"resolved_params":{"period":"2026-06"},"blocked_reason":""}

# Approve → executes the scenario, links the resulting job
curl -X POST -H "Authorization: Bearer $ADMIN" \
  http://localhost:8080/requests/$RID/approve
# → {"status":"approved","job_id":"…"}

# Or reject with a note
curl -X POST -H "Authorization: Bearer $ADMIN" -H "Content-Type: application/json" \
  -d '{"note":"need a budget code first"}' \
  http://localhost:8080/requests/$RID/reject
```

Notes:

- A proposal still passes the same trust gate at approval time (license ∩ Mercury
  permissions, pipeline limit). `test` shows the verdict before committing.
- Approved requests carry the resulting `job_id` — full traceability from proposal
  to execution.
- The scenario allowlist on a token applies to proposals too: a client can only
  submit requests for scenarios their token permits.
