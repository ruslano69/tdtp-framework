# TODO NEXT — Sprint план

## Поточний стан (v1.11.0 + sprint patches)

Замкнено повний ланцюг довіри. Закрито цим спринтом:
- `unsafe-op.cert` (Tier 2 capability certificate + audit log)
- Job log truncation (64KB cap + overflow file)
- Schedule timezone (IANA tz у magic params)
- Lint/gofmt/CI fixes після main merge

---

## Sprint 2 — Операційна зрілість

**Мета**: зробити систему придатною до реального prod-розгортання без ручних доробок.

### P1 — Air-gap enrollment (`tdtp-certify issue-offline-cert`)

**Навіщо**: клієнти на ізольованих мережах (заводи, держструктури) не можуть
дістатися CA для challenge-response. Зараз вони взагалі не можуть отримати cert.

**Що робити**:
- `xzmercury/cmd/tdtp-certify/` — нова subcommand `issue-offline-cert`
- Приймає `--env-pub <ed25519.pub>`, `--license-key`, `--not-after`, `--permissions`
- Підписує `EnvCert` без live-challenge (CA бере відповідальність за identity)
- Виводить cert у JSON + попередження "no live-revocation"
- xZMercury: приймає offline-cert на `POST /api/env/authorize` без challenge
  (новий поле `offline: true` у cert)
- Тести: `ca/offline_cert_test.go` (sign → verify → Mercury accept)

**Цінність**: розблоковує air-gapped enterprise клієнтів.

---

### P2 — Seat policy: один env + кілька ліцензій

**Навіщо**: зараз при `enroll` з іншим `license_hash` поведінка невизначена —
можлива помилка або подвійне зарахування seat.

**Що робити**:
- `xzmercury/internal/ca/db.go` — при enroll перевіряти: чи існує активний cert
  з тим самим `env_id_pub` але іншим `license_hash`
- Рішення: **один env = одна активна ліцензія** (заблокувати re-enroll з іншим license)
- При зміні ліцензії — спочатку `revoke-cert` старого, потім новий enroll
- Тести: `ca/integration_test.go` — додати кейс "re-enroll same env different license → 409"

**Цінність**: визначена семантика, немає витоку seats при ротації ліцензій.

---

### P3 — CA renewal з mock-clock

**Навіщо**: `infra.AutoRenew` не покритий тестом — тестувати 24h renewal у реальному
часі неможливо. Регресії тихо ламаються.

**Що робити**:
- Ввести `clock interface { Now() time.Time }` у `infra.CASession`
- `RealClock` (дефолт) vs `MockClock` (тести) — інжекція через параметр
- `infra/ca_bootstrap_test.go` — додати тест: cert з TTL=5s, MockClock.Advance(4s+1ms),
  перевірити що AutoRenew викликав `authorize` повторно
- Той самий підхід у `xzmercury/internal/ca/` для 24h cert TTL логіки

**Цінність**: покриття критичного шляху renewal без реальних затримок.

---

### P4 — `tdtp-certify` видача `unsafe-op.cert`

**Навіщо**: `pkg/license/cert.go` вже реалізовано, але видавати cert нема чим —
`tdtp-certify` поки не має subcommand для capability certs.

**Що робити**:
- `xzmercury/cmd/tdtp-certify/` — subcommand `issue-unsafe-cert`
- Flags: `--to`, `--op` (unsafe-sql/schema-write/cross-schema/drop-allowed),
  `--tables`, `--db`, `--host`, `--ttl`, `--key`, `--out`
- Підписує `CapabilityCert` тим самим vendor Ed25519 ключем
- Тести: sign → `LoadCert` → `VerifyWith` roundtrip
- Документація: `docs/UNSAFE_CERTS.md` (оператор-гайд)

**Цінність**: замикає весь unsafe workflow — від видачі до enforcement.

---

## Sprint 3 — Спостережуваність і SIEM

### P5 — Structured audit log + syslog hook

**Навіщо**: зараз `tdtp-audit.log` — plain text файл. Для enterprise SIEM (Splunk,
ELK, QRadar) потрібен JSON або syslog.

**Що робити**:
- `pkg/license/audit.go` — додати `AuditEntry` struct (JSON per line: timestamp,
  nonce, operation, issued_to, host, tdtpcli_version)
- `NewAuditLog(path, format)` де format = "text" | "json"
- Опціональний syslog writer (build tag `+syslog`): `NewSyslogAuditLog(tag, facility)`
- `TDTP_AUDIT_FORMAT` env var
- Тести: JSON parse roundtrip

### P6 — Orchestrator: per-job result export

**Навіщо**: після виконання pipeline результат є тільки у файлі на диску.
Consumer API (`GET /results/{scenario}`) показує лише status, не дані.

**Що робити**:
- Після успішного job: читати output file → зберігати sha256 + size у jobs таблиці
- `GET /jobs/{id}/artifact` — повертає redirect або inline до файлу
- Або: зберігати до S3 (якщо license.AllowsFeature("s3"))
- Тести: job done → artifact available

### P7 — LDAP auth в Orchestrator (опціонально)

**Навіщо**: корпоративні клієнти хочуть SSO. xZMercury вже має LDAP.

**Що робити**:
- `cmd/orchestrator/auth.go` — додати `LDAPAuthenticator` поруч з `TokenAuthenticator`
- Config: `auth.type: ldap | token`, `auth.ldap: {url, bind_dn, base_dn, group_attr}`
- `--auth-type` flag
- Тести: mock LDAP (як у xZMercury)

---

## Open Questions (залишились з попереднього спринту)

1. **Mercury ↔ Orchestrator permissions** (open q #3 закритий де-факто):
   Orchestrator вже перевіряє permissions before submit через `GateScenario`.
   Mercury також відмовить у bind якщо permissions не вистачає.
   → Вважаємо закритим: fail-fast у Orchestrator + defense-in-depth у Mercury.

2. **Grace period для tdtp.lic**: hard stop або 30-day read-only?
   Зараз: expired = fatal. Для integrators під час активного проекту — проблема.
   → Спринт 2/3: `--grace-period 30d` flag, read-only mode після expiry.

3. **Host fingerprint composite**: зараз тільки hostname.
   `hostname + MAC + CPU-ID` — більш стійко, але ламається при VM migration.
   → Відкладено: поки hostname достатньо для більшості deployments.
