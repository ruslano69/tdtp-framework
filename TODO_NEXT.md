# TODO NEXT

## Контекст

Поточний стан (v1.10.0):
- `tdtpcli` — production-ready ETL CLI (всі адаптери, enc, CSV, XLSX, HTML, integrity).
- `xZMercury` — key store + hash notary + burn marker + mode-in-HMAC + CA-guard.
- `tdtp-ca` — CA-сервер (enrollment, re-auth, 24h cert, /hello DDoS-гейт).
- `tdtp-certify` — CLI адміна CA (keygen, issue-license, revoke, list-active).
- `orchestrator` — scenario runner + cron scheduler (SQLite, HTTP API).

Ланцюг довіри ЗАМКНЕНО на рівні Mercury: prod Mercury авторизується в CA при старті,
ключі не видаються без живої CA-сесії. Наступне — донести ліцензію до tdtpcli і
додати UserApp-шар.

---

## ✅ Priority 1 — DONE: xZMercury → CA при старті

Виконано (коміт `72c80bd`):
- `infra.BootstrapCA` — envkey.Load → enroll/authorize → AutoRenew на server-ctx.
- `infra.CASession.Valid()` + `api.caGuardMiddleware` → 503 на `/api/keys/*` коли сесія мертва.
- `CAConfig` у конфізі, `TDTPCA_LICENSE_KEY` env fallback, dev пропускає CA.
- `ca.NewRouter` винесено (shared з тестами).
- Тести: `ca/integration_test.go` (5), `infra/ca_bootstrap_test.go` (3).

## ✅ Priority 2 — DONE: `tdtp-certify` CLI

Виконано (коміт у цьому наборі):
- `keygen` · `issue-license` · `revoke-cert` · `revoke-license` ·
  `list-licenses` · `list-active` · `list-certs`.
- `issue-license` генерує human-friendly ключ `TDTP-XXXXX-…`, показує раз; CA тримає лише hash.
- `list-active` рахує середовища з `last_seen` у вікні (default 24h) — реальна активність.
- DB-методи: `ListLicenses`, `ListActiveCerts`, `ListAllCerts`.
- Тести: `ca/db_listing_test.go` (3), `cmd/tdtp-certify/main_test.go` (4).
- Живий end-to-end перевірено: keygen → issue → enroll×3 → seat-limit 409 →
  revoke-cert звільняє місце → revoke-license вбиває всі.

---

## Priority 3 — `pkg/license/`: offline перевірка tdtp.lic у tdtpcli

**Статус: ✅ DONE.**

Виконано:
- `pkg/license/` — `License`/`Limits`/`Tier`, `Load`/`Verify`/`VerifyWith`/`Sign`/`New`,
  accessors (`AllowsAdapter`/`AllowsFeature`/`RowLimit`/…), `Community()` floor.
- `pkg/license/pubkey.go` — вшитий vendor Ed25519 public key (PKIX PEM), `VendorPublicKey()`.
- `cmd/tdtpcli/commands/license_gate.go` — `ResolveLicense` (flag → `TDTP_LICENSE` →
  `./tdtp.lic` → community), `GateAdapter`/`GateFeature`/`GateRowCount`.
- `cmd/tdtpcli/main.go` — `--license` flag; резолвинг при старті (tampered/expired = fatal);
  gate адаптера (config.Database.Type), `--enc` (feature enc), `--unsafe` (feature unsafe).
- `cmd/tdtp-license/` — vendor tool: `keygen` / `issue` / `verify` (Ed25519, окремий від CA-ключа).
- Тести: `pkg/license/license_test.go` (9) — sign/verify roundtrip, tampered, wrong key,
  expired, file load, community floor, embedded-key parse.
- Живо перевірено: community блокує mssql/enc; ліцензія пропускає; `TDTP_LICENSE` env;
  tampered (professional→enterprise) = fatal; community sqlite import працює.

**Backward compat дотримано**: відсутній `tdtp.lic` → community mode, не помилка.

**Дві гілки довіри тепер обидві в бінарі:**
- ONLINE (CA/Mercury) — авторизація середовища виконання.
- OFFLINE (tdtp.lic) — можливості CLI без мережі. Air-gapped tdtpcli поважає ліцензію.

---

## ✅ Priority 4 — DONE: Orchestrator інтеграція з CA + ліцензією

Виконано:
- Mercury `/status` endpoint → `{mode, dev, ca_authorized, permissions}`.
  `api.CAGuard` розширено `Permissions()`; `infra.CASession` його реалізує.
- `cmd/orchestrator/preflight.go` — `TrustGate`:
  - OFFLINE: резолвинг+верифікація власної tdtp.lic (pkg/license).
  - ONLINE: `FetchMercuryStatus`; `--require-prod` відмовляє проти dev / не-CA-authorized.
  - `GateScenario` — scenario.permissions ⊆ (license features ∩ Mercury permissions).
  - `CheckPipelineLimit` — активні задачі < license.PipelineLimit (0 = unlimited).
- `main.go` flags: `--license`, `--mercury-url`, `--require-prod`; gate у run-handler
  (403 на permission, 429 на pipeline-limit). Scheduler теж gate'ить cron-задачі.
- `db.CountActiveJobs` — підрахунок pending/running.
- Тести: `orchestrator/preflight_test.go` (8), `api/status_test.go` (3).
- Живо: `--require-prod` блокує dev-Mercury; community-ліцензія блокує сценарій з `etl` (403).

---

## ✅ Priority 5 — DONE: Orchestrator автентифікація

Виконано:
- **Token-based auth з ролями** (`cmd/orchestrator/auth.go`):
  - Ролі: `consumer` < `activator` < `admin` (roleRank).
  - Токени зберігаються хешовано (SHA-256); raw показується раз при створенні.
  - Per-token scenario allowlist (порожній = всі сценарії).
  - `Middleware` (Bearer → Principal у ctx), `RequireRole`, `PrincipalFrom`.
  - Bootstrap admin-токен при порожній таблиці (друкується раз у лог).
  - `--no-auth` для локальної розробки (кожен запит = admin).
- **DB** (`tokens` таблиця): InsertToken, GetTokenByHash, TouchToken (last_used_at),
  ListTokens, DeleteToken, CountTokens + `ListJobsByScenario`.
- **Role-gates на endpoints**:
  - consumer: `GET /scenarios`, `/scenarios/{name}`, `/jobs`, `/jobs/{id}`, `/results/{scenario}`
  - activator: `POST /scenarios/{name}/run` (+ scenario allowlist)
  - admin: `/schedules` CRUD, `/tokens` CRUD
  - `/healthz` — публічний (liveness probe).
- **Новий endpoint** `GET /results/{scenario}` — недавні задачі сценарію (consumer view).
- **Token mgmt API**: `POST /tokens` (видає raw раз), `GET /tokens`, `DELETE /tokens/{id}`.
- Тести: `cmd/orchestrator/auth_test.go` (8): hash stability, bootstrap (одноразовість),
  allowlist, middleware (missing/invalid/valid token), RequireRole 403, no-auth=admin.
- Живо: bootstrap-токен → 401 без токена / 200 з admin; consumer 200 на read,
  403 на admin-route і на run; activator 403 на сценарій поза allowlist.

**Примітка**: LDAP-варіант (як у xZMercury) — можливе майбутнє розширення, але
token+role вже покриває Activator/Consumer розділення з достатньою гранулярністю.

---

## ✅ Priority 6 — DONE (частково): Тести

Виконано:
- **CA-сервер**: повний enroll → authorize → renew цикл + idempotent + seat-limit +
  revoke + invalid-license (`ca/integration_test.go`, 5). Bootstrap + guard
  (`infra/ca_bootstrap_test.go`, 3).
- **Orchestrator executor** (`executor_test.go`, 5): параметрична підстановка +
  persistence (pending→running→done), failed-run records error, missing-param
  fails before run, scheduled run carries schedule_id, CountActiveJobs.
  Executor зроблено тестованим — інжектований `runnerFunc` + `done`-канал.
- **Scenario loading** (`scenario_test.go`, 6): orchestrator-блок, fallback на
  ім'я файлу, ValidateParams (required/pattern/default), LoadScenariosDir, magic params.
- **Orchestrator: 26 тестів усього** (auth 8 + preflight 8 + executor 5 + scenario 5).

Залишилось (потребує зовнішніх ресурсів, відкладено):
- **`--limit -N`** end-to-end з реальним MSSQL — потребує живий SQL Server.
- **CA renewal у часі** — потребує прискорення годинника або фейк-clock у AutoRenew.

**Документація**: `docs/ORCHESTRATOR_SCENARIOS.md` — як додавати разові/періодичні,
прості/шифровані YAML-сценарії на сервер оркестрації.

---

## Open Questions (з design-сесії)

1. **Seat policy edge case**: що якщо той самий `env_id_pub` enrolls з іншим `license_hash`?
   Заблокувати (один env = одна ліцензія) чи дозволити (env може мати кілька ліцензій)?

2. **Air-gap enrollment**: CA підписує offline-cert на наданий `env_id_pub` з фіксованим
   `not_after`. Ціна — нема live-revocation. Зробити `tdtp-certify issue-offline-cert`?

3. **Mercury ↔ Orchestrator permissions**: де перевіряти `scenario.permissions` —
   в Orchestrator (before submit) чи Mercury відмовляє у bind якщо permissions не вистачає?
   Рекомендація: Orchestrator перевіряє раніше (fail-fast, без round-trip до Mercury).

4. **Job log обсяг**: поточно — весь stdout+stderr у одному TEXT полі в SQLite.
   При великих pipeline може бути MB. Рішення: обрізати до 64KB + окремий log-файл?

5. **`{{current_month}}` і timezone**: поточно UTC. Для клієнтів у UTC+3 може бути неправильний
   місяць в перші 3 години. Додати `timezone` у schedule config?
