# TODO NEXT

## Контекст

Поточний стан (v1.10.0):
- `tdtpcli` — production-ready ETL CLI (всі адаптери, enc, CSV, XLSX, HTML, integrity).
- `xZMercury` — key store + hash notary + burn marker + mode-in-HMAC.
- `tdtp-ca` — CA-сервер (enrollment, re-auth, 24h cert, /hello DDoS-гейт).
- `orchestrator` — scenario runner + cron scheduler (SQLite, HTTP API).

Інфраструктура виконання сценаріїв є. Наступний крок — замкнути ланцюг довіри
(ліцензія → CA-авторизація Mercury → виконання) і додати UserApp-шар.

---

## Priority 1 — Замкнути ланцюг: xZMercury → CA при старті

**Що**: `xzmercury` в prod-режимі (без `--dev`) повинен авторизуватись у CA
перед тим як почати приймати `/api/keys/bind`.

**Файли**: `xzmercury/cmd/xzmercury/main.go`, `xzmercury/internal/infra/`

**Flow**:
```
prod start:
  1. envkey.Load(cfg.EnvKeyDir)       → Ed25519 keypair (TPM stub)
  2. cert = loadCertFromDisk()        → якщо є
     або caClient.Enroll(licenseKey)  → перший запуск
  3. caClient.Authorize(cert)         → session_token
  4. caClient.AutoRenew(cert, ...)    → оновлення кожні 12h
  5. ListenAndServe()
  503 якщо session_token відсутній або протух і CA недоступний
```

**Конфіг** (`xzmercury.yaml`):
```yaml
ca:
  url: "https://ca.tdtp.io:8443"
  license_key: ""          # або TDTPCA_LICENSE_KEY env var
  env_key_dir: "./envkey"  # де зберігати Ed25519 keypair
  cert_path: "env.cert"    # sealed cert
```

**Приоритет**: HIGH — без цього CA існує але Mercury ним не користується.

---

## Priority 2 — `tdtp-certify`: CLI для адміністратора CA

**Що**: standalone CLI для видачі ліцензій і управління.

```bash
# Генерація CA-ключа (one-time, offline/HSM)
tdtp-certify keygen --out ca.ed25519.priv

# Видача ліцензії
tdtp-certify issue-license \
  --licensee "Contoso GmbH" \
  --tier professional \
  --seat-limit 3 \
  --permissions etl,enc,s3 \
  --expires 2027-06-01 \
  --key ca.ed25519.priv \
  --db ca.db

# Відкликання
tdtp-certify revoke-cert   --cert-id <uuid> --db ca.db
tdtp-certify revoke-license --hash <hex>    --db ca.db

# Статус
tdtp-certify list-active --db ca.db    # last_seen > now-24h
```

**Файли**: `xzmercury/cmd/tdtp-certify/`

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
