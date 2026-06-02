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

**Що**: tdtpcli перевіряє `tdtp.lic` перед виконанням операцій.
Без ліцензії = community mode (SQLite only, no --unsafe, no --enc).

**Структура ліцензії** (Ed25519-signed JSON):
```json
{
  "licensee":  "Contoso GmbH",
  "issued":    "2026-06-01",
  "expires":   "2027-06-01",
  "tier":      "professional",
  "adapters":  ["postgres", "mssql", "mysql"],
  "features":  ["etl", "enc", "s3"],
  "limits":    {"rows_per_export": 1000000, "pipelines": 10},
  "signature": "<Ed25519 base64>"
}
```

**Файли**: `pkg/license/license.go`, `pkg/license/pubkey.go`
**Gate points**:
- `pkg/adapters/factory.go` → адаптер дозволений?
- `cmd/tdtpcli/commands/encrypt.go` → `features["enc"]`?
- `cmd/tdtpcli/commands/export.go` → `limits.rows_per_export`?

**Backward compat**: відсутній `tdtp.lic` → community mode, не помилка.

---

## Priority 4 — Orchestrator: інтеграція з CA + ліцензією

**Що**: Orchestrator перевіряє що:
1. xZMercury відповідає і має `mode=prod` (не dev).
2. Сесія Mercury має `permissions` що покривають scenario.permissions.
3. Ліцензія дозволяє кількість паралельних pipelines.

**Файли**: `cmd/orchestrator/main.go`

```go
// При старті: перевірити Mercury + permissions
resp, err := http.Get(mercuryURL + "/healthz")
// Перед Submit: перевірити що permissions ∩ scenario.Permissions != ∅
```

---

## Priority 5 — Orchestrator: UserApp API + автентифікація

**Що**: UserApp (Activator/Consumer) потребує автентифікації.
Поточний API — повністю відкритий.

**Мінімальний варіант**: Bearer token у конфігурації (shared secret).
**Правильний варіант**: LDAP через той самий механізм що xZMercury.

**Новий endpoint** для Consumer:
```
GET /results/{scenario}/{date}  → список job_id за дату
GET /jobs/{id}/download         → завантажити output (якщо є)
```

---

## Priority 6 — Тести

**CA-сервер**: integration test повного enrollment → authorize → renewal циклу.
**Orchestrator**: test Submit + параметрична підстановка + job persistence.
**`--limit -N`**: end-to-end test з реальним MSSQL (якщо є доступ).

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
