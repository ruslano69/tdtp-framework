# TODO NEXT — Sprint план

## Поточний стан (v1.11.0 + sprint 2 + sprint 3)

Замкнено повний ланцюг довіри. Закрито Sprint 2:
- P1: Air-gap offline cert (`issue-offline-cert` + `AuthorizeOffline` endpoint)
- P2: Seat policy (one env = one license, HTTP 409 cross-license re-enroll)
- P3: CA renewal з mock clock (`MockClock`, `AutoRenew` 100ms polling)
- P4: `issue-unsafe-cert` subcommand в `tdtp-certify`

Закрито Sprint 3:
- P5: Structured audit log + syslog hook (`AuditEntry` JSON format, `TDTP_AUDIT_FORMAT` env var)
- P6: Orchestrator per-job artifact (`GET /jobs/{id}/artifact`, SHA-256 + size в DB)
- P7: LDAP auth в Orchestrator (`--auth-type=ldap` flags, `LDAPAuthenticator`)

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

## Sprint 4 — Cross-system Integration Pipeline (`--map`)

**Мета**: перетворити фреймворк на повноцінний інструмент міжсистемних інтеграцій
з контрольованим маппінгом, валідацією і захистом від логічних петель.

### Архітектура потоку

```
STAGE 1: Export (параметризований pipeline)
  --export + --where + --fields → *.tdtp.xml   [checkpoint]
        ↓
STAGE 2: Validate
  --validate rules.yaml → pass/fail            [checkpoint, exit on fail]
        ↓
STAGE 3: Split/Map (новий --map)
  --map mapping.yaml → header.tdtp + lines.tdtp  [checkpoint per fragment]
        ↓
STAGE 4a: Import header          STAGE 4b: Import lines (foreach)
  --import + key control           --import + parent_key з 4a
```

Кожен TDTP-пакет між стадіями = checkpoint. Replay можливий з будь-якої точки
без повторного export/validate. `correlation_id` прошиває всі стадії.

---

### P8 — `--map`: базовий крос-маппінг JOIN→split

**Навіщо**: одна source-таблиця або JOIN кількох → одна або кілька target-таблиць.
Покриває 80% реальних ERP-інтеграцій.

**YAML-схема карти**:
```yaml
id: orders-sync-v1
version: "1.0"
approved_by: arch-board          # governance: хто затвердив
loop_guard:
  source_system: mssql-1c
  target_system: postgres-erp2
  lock_field: _synced_by         # поле яке маркує рядок як "вже синхронізований нами"
  lock_value: "tdtpcli"

sources:
  - id: doc
    table: dbo.Documents
  - id: lines
    table: dbo.DocumentLines
    join: "doc.id = lines.doc_id"

targets:
  - id: header
    table: invoices
    fields:
      - {from: "doc.Number",   to: "invoice_number"}
      - {from: "doc.Date",     to: "created_at"}
  - id: items
    table: invoice_lines
    foreach: lines
    parent_key: {from: "doc.id", to: "invoice_id"}
    fields:
      - {from: "lines.ItemCode", to: "sku"}
      - {from: "lines.Qty",      to: "quantity"}
```

**Що треба реалізувати**:
- `pkg/core/mapping/` — парсер YAML-карти, executor JOIN→split
- `cmd/tdtpcli/commands/map.go` — нова команда `--map`
- `--map mapping.yaml --key "id=12345"` — маппінг конкретного запису
- `--map mapping.yaml --validate` — dry-run: перевірити schema_match без запису
- Lookup-таблиця як TDTP-пакет: `lookup: kved_to_nace.tdtp.xml`
- Enum remap: `enum: {"Проведений": 3, "Чернетка": 1}`

---

### P9 — Loop Guard: захист від рекурсивних обмінів ⚠️ КРИТИЧНО

**Проблема**: тригер в системі A → синхронізація в B → тригер в B → синхронізація
назад в A → нескінченний цикл. **Може покласти обидві БД за хвилини.**

**Сценарії петлі**:
1. A→B→A (пряма рекурсія)
2. A→B→C→A (транзитивна петля через кілька систем)
3. A→A (маппінг в ту саму БД, інша таблиця, але тригер на UPDATE)

**Механізми захисту** (шарами):

```
Шар 1 — lock_field у рядку
  Перед записом в target: встановити _synced_by = 'tdtpcli:<mapping_id>'
  Тригер target перевіряє: IF _synced_by IS NOT NULL → пропустити синхронізацію
  Мінус: потребує зміни схеми target (не завжди можливо в legacy)

Шар 2 — correlation_id + registry
  Кожен запуск --map отримує UUID correlation_id
  Перед стартом: записати в mapping_log (джерело: будь-яка доступна БД або файл)
  Якщо знайдено correlation_id з тим самим source+target за останні N секунд → STOP

Шар 3 — depth counter в пакеті
  TDTP Header розширити полем sync_depth (int, default 0)
  --map збільшує sync_depth при передачі
  Якщо sync_depth > max_depth (конфіг, default 3) → STOP з помилкою

Шар 4 — cooldown на рівні mapping.yaml
  min_interval: "30s"   # не запускати маппінг частіше ніж раз на 30 секунд
  max_depth: 3          # максимальна глибина ланцюга синхронізацій
```

**Що треба реалізувати**:
- `pkg/core/mapping/loopguard.go` — перевірка всіх 4 шарів
- `sync_depth` у TDTP Header (backward-compatible, optional field)
- `mapping_log` — легкий SQLite або файл-семафор поруч з mapping.yaml
- Тести: симуляція A→B→A, перевірка що зупиняється на шарі 2

---

### P10 — Pipeline steps: `on_error` + `depends_on`

**Навіщо**: зараз pipeline.yaml не має контролю порядку і поведінки при помилці.
Для multi-stage інтеграції це критично.

**Що додати в pipeline.yaml**:
```yaml
steps:
  - id: export
    command: "--export ..."
  - id: validate
    command: "--validate ..."
    depends_on: export
    on_error: stop              # stop | skip | retry(3)
  - id: map_header
    command: "--map header ..."
    depends_on: validate
    on_error: retry(3)
  - id: map_lines
    command: "--map lines ..."
    depends_on: map_header      # потребує parent_key з попереднього кроку
    on_error: stop
```

- `depends_on` → топологічне сортування кроків, паралельне виконання де можливо
- `on_error: retry(N)` з exponential backoff
- `correlation_id` автоматично передається між кроками

---

### Вузькі місця та ризики

| Ризик | Рівень | Мітигація |
|---|---|---|
| Логічна петля тригерів | 🔴 КРИТИЧНИЙ | Loop Guard (P9), обов'язково перед деплоєм |
| JOIN на великих таблицях без індексів | 🟠 ВИСОКИЙ | `--map --validate` виводить EXPLAIN, попереджає |
| Частковий запис (header OK, lines fail) | 🟠 ВИСОКИЙ | correlation_id + replay з checkpoint |
| Schema drift між системами | 🟡 СЕРЕДНІЙ | `--validate schema_match` перед кожним запуском |
| Lock_field недоступний в legacy схемі | 🟡 СЕРЕДНІЙ | Шар 2 (registry) як fallback |
| Паралельні запуски одного маппінгу | 🟡 СЕРЕДНІЙ | File-lock на mapping.yaml під час виконання |

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
