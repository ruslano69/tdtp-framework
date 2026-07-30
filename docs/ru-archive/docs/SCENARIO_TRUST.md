# Scenario Trust — подпись и целостность оркестраторных сценариев

> Статус: **design draft**, не реализовано. Ничего из описанного здесь не существует
> в коде — этот документ фиксирует обсуждённую модель, чтобы было что предметно
> критиковать и от чего отталкиваться при реализации.
>
> Связанные пункты: [ROADMAP.md](../ROADMAP.md) — "Schema migration" и
> "Orchestrator scenario integrity registration".

---

## 1. Проблема

Обсуждались две независимые угрозы, которые ошибочно можно спутать друг с другом или
попытаться закрыть одним и тем же механизмом — на самом деле нужны оба слоя одновременно:

| # | Угроза | Кто атакующий | Что компрометируется |
|---|---|---|---|
| A | Скомпрометированный **producer** шлёт пакет с "эволюционировавшей" схемой через легитимный, неизменный `--map --listen`/pipeline | владелец учётных данных брокера/branch-узла | данные внутри честного пайплайна |
| B | Кто-то с доступом к `--scenarios/` подменяет **сам YAML** сценария (добавляет `--unsafe`, меняет DSN, включает несанкционированный DDL) | тот, у кого есть запись в директорию сценариев / в CI | инструкции, которые исполняет оркестратор |

Сегодня ни то, ни другое не защищено:

- Схема пакета (`<Schema>`) полностью формируется producer'ом и используется для
  `CREATE TABLE IF NOT EXISTS` без какого-либо гейта — угроза A открыта архитектурно,
  как только появится auto-`ALTER TABLE` (см. roadmap).
- `cmd/orchestrator` грузит `--scenarios/*.yaml` один раз при старте
  (`LoadScenariosDir`, без `fsnotify`), `POST /scenarios/{name}/run` исполняет то, что
  уже лежит в памяти, не перечитывая и не хэшируя файл. `TrustGate.GateScenario`
  проверяет только строки прав (`scenario.permissions ⊆ license ∩ Mercury`) — никогда
  содержимое файла. Угроза B открыта уже сейчас, независимо от schema migration.

## 2. Что уже есть и можно переиспользовать

Вывод предыдущего анализа: инфраструктура подписи в основном уже существует, не хватает
только *правильного корня доверия* и *точки применения*.

| Кирпич | Файл | Что даёт |
|---|---|---|
| `CapabilityCert` | `pkg/license/cert.go` | Ed25519-подписанный токен: операция (уже есть `"schema-write"`), scope по таблицам/БД (`CoversTable`, glob), host-lock, срок действия, **nonce + replay-защита через audit log** |
| `applyUnsafeGate` | `cmd/tdtpcli/commands/unsafe_gate.go` | пример гейта: cert или fallback на `IsAdmin()` |
| CA / EnvCert | `xzmercury/internal/ca`, `cmd/tdtp-ca` | challenge-response, hardware attestation, отдельный корень для окружений |
| `tdtp-certify` | `xzmercury/cmd/tdtp-certify` | vendor-side `issue-license`/`revoke-cert`/`list-active` — готовый паттерн CLI для выпуска/отзыва |
| `ProjectRequest` workflow | `cmd/orchestrator/requests.go` | staged approve (`submit → test → approve/reject`) — сейчас это просто флаг статуса в SQLite, не крипто-акт, но UX-точка для встраивания подписи уже есть |
| Job artifact hash | `cmd/orchestrator/executor.go` (`fileHashAndSize`) | SHA-256 уже считается для *выходного* артефакта — по аналогии добавить хэш для *входного* определения сценария |

Расхождение с целевой моделью — сегодня `CapabilityCert` подписывает **вендор**. Для
DDL-прав над конкретной базой это неправильный корень: право должен выдавать тот, кто
владеет базой (DBA), а не поставщик софта.

## 3. Целевая модель

### 3.1 Роли и корни доверия

```
Vendor root (Ed25519, offline)          — уже есть: подписывает tdtp.lic
        │
        ├─ CA root (xzmercury)          — уже есть: подписывает EnvCert окружениям
        │
        └─ Signer cert (НОВОЕ)          — вендор/CA делегирует конкретному DBA
              │                            право подписывать сценарии
              ▼
        DBA подписывает сценарий (НОВОЕ) — акт "я одобряю именно это содержимое"
```

`Signer cert` — это делегирование полномочий, а не отдельный независимый корень:
вендор (или CA, если решаем держать это в xzmercury) удостоверяет, что публичный ключ
конкретного DBA имеет право подписывать сценарии с DDL-операциями в заданном scope.
Отзыв — тем же реестром, что уже используется для `tdtp-certify revoke-cert`.

### 3.2 Новые структуры данных

```go
// pkg/license/signer.go (новый файл, по образцу cert.go)

// SignerCert delegates scenario-signing authority to a DBA/privileged user.
// Signed by the vendor or CA root — NOT self-signed.
type SignerCert struct {
    IssuedTo   string    `json:"issued_to"`   // DBA identity (e.g. email, principal)
    PublicKey  string    `json:"public_key"`  // base64 Ed25519 pubkey of the DBA
    Operations []string  `json:"operations"`  // subset of: schema-write, create-table, create-view
    Scope      CertScope `json:"scope"`       // reuse existing CertScope (tables/database)
    IssuedAt   time.Time `json:"issued_at"`
    Expires    time.Time `json:"expires"`
    Signature  string    `json:"signature"`   // base64(Ed25519 over canonical JSON, by vendor/CA root)
}

// ScenarioSignature is the DBA's approval of one exact scenario file.
// Verifying it requires first verifying the SignerCert that names PublicKey.
type ScenarioSignature struct {
    ScenarioName string    `json:"scenario_name"`
    ContentHash  string    `json:"content_hash"`  // sha256(canonical scenario YAML)
    Version      int       `json:"version"`       // monotonic — see 4.3 downgrade protection
    SignedBy     string    `json:"signed_by"`      // must match a SignerCert.IssuedTo
    IssuedAt     time.Time `json:"issued_at"`
    Expires      time.Time `json:"expires"`
    Signature    string    `json:"signature"`      // base64(Ed25519 over canonical JSON, by DBA key)
}
```

### 3.3 Порядок проверки при запуске сценария

`POST /scenarios/{name}/run` (и cron-триггер) выполняет **на каждый запуск**, не только
при старте процесса:

1. Прочитать файл сценария с диска заново (закрывает TOCTOU — проверка при старте
   недостаточна, файл мог поменяться после загрузки).
2. Посчитать `sha256(канонический YAML)`.
3. Найти зарегистрированную `ScenarioSignature` по имени; сверить `content_hash` —
   несовпадение → отказ (сценарий на диске отличается от одобренного).
4. Проверить `version` не ниже последней известной для этого имени (защита от
   downgrade, см. 4.3).
5. Загрузить `SignerCert` по `SignedBy`; проверить его подпись корнем (вендор/CA), срок
   действия, что `Scope` покрывает целевые таблицы сценария, что не отозван.
6. Проверить подпись `ScenarioSignature` публичным ключом из `SignerCert`.
7. Только если сценарий требует DDL-операций (`schema-write`/`create-table`/
   `create-view` — то же множество, что уже объявлено в `Orchestrator.Permissions`),
   требовать, чтобы `SignerCert.Operations` включал соответствующую строку. **Флаг
   auto-migration в самом YAML не проверяется и не учитывается** — право даёт только
   валидная подпись, потому что YAML — это ровно то, что могут подменить.
8. Записать в job: `scenario_content_hash`, `signed_by`, `signer_cert_id` — полный
   provenance выполнения, по аналогии с уже существующим `ArtifactSHA256`.

### 3.4 Связь со Schema Migration (roadmap)

Auto-`ALTER TABLE` из producer-предоставленной схемы пакета разрешается **только**
если у выполняемого сценария есть валидная `ScenarioSignature` от подписанта, чей
`SignerCert.Operations` содержит `schema-write` для целевой таблицы. Без такой подписи
— поведение по умолчанию: детект дрейфа + отчёт, без применения. Это единственная
точка, где два ранее обсуждённых пункта roadmap физически пересекаются.

## 4. Открытые риски / вопросы для додумывания

### 4.1 Хранение ключа DBA
Самое слабое звено всей схемы. Минимум — passphrase-защищённый файл ключа
(аналогично `ca.ed25519.priv` в `tdtp-ca`); в идеале — вынесение подписи на отдельную
машину/аппаратный токен, оркестратор никогда не видит приватный ключ DBA.

### 4.2 Кто подписывает: вендор или CA?
Вендорский корень уже подписывает `tdtp.lic` и `CapabilityCert` — переиспользовать его
для `SignerCert` проще всего, но семантически права на DDL в конкретной базе — это
решение эксплуатирующей организации, а не поставщика ПО. Более чистый вариант: делегировать
выпуск `SignerCert` в CA (`xzmercury/internal/ca`), т.к. CA уже отвечает за
доверие внутри окружения заказчика, а не продукта. Требует решения до реализации.

### 4.3 Downgrade-атака
Старая, честно подписанная версия сценария (например, без ограничений на таблицы)
остаётся криптографически валидной после того, как вышла новая, более строгая версия.
Монотонный `version` в `ScenarioSignature` (п. 3.2) закрывает это только если
оркестратор **хранит** последнюю известную версию на инстанс — то есть реестр подписей
должен быть персистентным (расширение уже предложенной в roadmap checksum-регистрации:
храним не просто hash, а `{hash, version, signed_by}` с монотонной проверкой при каждой
проверке).

### 4.4 UX точка одобрения
`requests.go`'s `approve` сегодня — просто смена статуса в SQLite. Нужно решить: подпись
создаётся отдельным CLI-шагом DBA заранее (сценарий кладётся в `--scenarios/` уже
подписанным, `approve` в UI не нужен), или `POST /requests/{id}/approve` сам запрашивает
подпись интерактивно (сложнее: требует держать приватный ключ доступным HTTP-серверу —
нежелательно, см. 4.1). Рекомендация: подпись — offline-шаг, `tdtp-scenario-sign`
CLI-утилита по образцу `tdtp-certify`, `approve` в оркестраторе остаётся просто UX-record.

---

## 5. Этапы доработки

Порядок выбран так, чтобы каждый этап был самостоятельно полезен и не блокировал
остальную разработку — schema migration (roadmap "Next") не должна ждать всей цепочки
подписи целиком.

### Этап 0 — Fingerprint без подписи (быстрый, закрывает TOCTOU)
- Пересчитывать `sha256(scenario.yaml)` на каждый `run`, не только при старте.
- Хранить последний известный хэш в БД оркестратора (`scenarios` таблица: `name`,
  `content_hash`, `first_seen_at`).
- Если хэш изменился relative к запомненному — не блокировать автоматически (ещё нет
  подписи, которая скажет "это одобрено"), но **логировать WARN + требовать явного
  `POST /scenarios/{name}/reindex`** перед следующим запуском. Это уже устраняет
  "подменили файл — никто не заметил", даже без крипто.
- Добавить `scenario_content_hash` в запись job (провенанс появляется сразу, подпись
  можно добавить позже без изменения схемы job).

### Этап 1 — `SignerCert` (делегирование)
- `pkg/license/signer.go`: структура + `Verify()`/`VerifyWith()` по образцу
  `cert.go` (переиспользовать `CertScope`, `matchGlob`).
- Решить вопрос 4.2 (вендор vs CA) до написания кода выпуска.
- CLI-утилита выпуска: `tdtp-certify issue-signer --key <root> --dba <email> \
  --ops schema-write,create-table --scope-db orders --expires ...` (по образцу
  существующего `issue-license`).
- Пока без применения в оркестраторе — только выпуск и верификация в изоляции +
  unit-тесты (аналог `cert_test.go`).

### Этап 2 — `ScenarioSignature` + офлайн-подпись
- `pkg/license/scenario_sig.go`: структура + верификация (сигнатура зависит от
  предварительно проверенного `SignerCert`, см. 3.3 пп. 5-6).
- `tdtp-scenario-sign` CLI: `tdtp-scenario-sign --scenario flights.yaml \
  --dba-key dba.ed25519.priv --signer-cert dba.cert.json --version 1 \
  --out flights.yaml.sig`.
- Формат хранения: `flights.yaml.sig` рядом с `flights.yaml` в `--scenarios/`, либо
  запись в БД оркестратора — выбрать по итогам этапа 0 (реестр уже есть).

### Этап 3 — Enforcement в оркестраторе
- `TrustGate.GateScenario` расширяется шагами 1-7 из раздела 3.3.
- Без подписи — сценарий работает как сегодня (permissions ⊆ license ∩ Mercury), но
  **не может** декларировать DDL-операции (`schema-write` и т.п.) — те требуют подписи
  обязательно, остальные permissions — как раньше, обратная совместимость сохранена.
- Job record расширяется: `scenario_content_hash`, `signed_by`, `signer_cert_id`.
- Downgrade-защита (4.3): персистентный монотонный `version` в реестре из этапа 0.

### Этап 4 — Привязка Schema Migration
- Реализуется только после этапа 3.
- Auto-`ALTER TABLE` (roadmap "Schema migration") читает `Operations` уже
  верифицированного `SignerCert` текущего запуска; без `schema-write` в scope —
  fallback на detect-only, независимо от любых флагов в самом YAML.

### Этап 5 — Отзыв и мониторинг
- `tdtp-certify revoke-cert` расширяется на `SignerCert` (реестр уже существует для
  license-сертификатов — тот же механизм).
- Метрика в оркестраторе (`orchestrator_scenario_signature_status{name,status}`) —
  по аналогии с уже существующими Prometheus-метриками джобов.
- Audit log: каждая проверка подписи (успех/отказ) — отдельная запись, не только
  facт запуска джобы.

---

Каждый этап оставляет систему в рабочем, обратно-совместимом состоянии — можно
остановиться после этапа 0 или 1 и уже получить ощутимое усиление, не обязательно
доводить до этапа 5 за один заход.
