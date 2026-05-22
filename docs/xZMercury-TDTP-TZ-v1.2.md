# ТЕХНИЧЕСКОЕ ЗАДАНИЕ

## xZMercury + TDTP Framework v1.2

**Zero-Knowledge Delivery + Packet Integrity Notary**

> xZMercury — eXtreme Zero-trust Mercury
> Интеграция системы одноразового доступа с ETL-пайплайнами + центр сертификации целостности пакетов

| | |
|---|---|
| **Версия:** | 1.2 |
| **Дата:** | 22.05.2026 |
| **Статус:** | DRAFT |
| **Предыдущая версия:** | v1.1 от 25.02.2026 |

---

## Что нового в v1.2 относительно v1.1

| Раздел | Изменение |
|---|---|
| §3.1 | Уточнение: соль для PBKDF2 = UUID пакета (публичная, уникальная) |
| §6 | Добавлена гарантия неподделываемости данных пакета |
| §8 | План реализации расширен этапами 7-13 |
| **§11** | **НОВЫЙ:** Hash Registry — «центр сертификации» целостности пакетов |
| **§12** | **НОВЫЙ:** Consumer Pre-flight Pipeline + три fallback-политики |
| **§13** | **НОВЫЙ:** Quota для регистрации хэшей («пищалка на выходе») |
| **§14** | **НОВЫЙ:** Dictionary as Dependency Manifest |
| **§15** | **НОВЫЙ (опционально):** chiptdtp Enterprise Tier |

**Принципиальное концептуальное расширение:** v1.1 закрывала **C**onfidentiality (zero-knowledge delivery шифрованных результатов). v1.2 добавляет **I**ntegrity + non-repudiation (неподделываемость данных пакета как такового) и **A**vailability (graceful degradation через три fallback-политики).

---

## 1-10. Без изменений относительно v1.1

Разделы 1-10 ТЗ v1.1 остаются в силе. Ниже фиксируются только уточнения.

### 3.1 (уточнение) — Salt = UUID пакета

В v1.1 указано «PBKDF2 + 16-байтная соль, генерируемая для каждого результата». В v1.2 эта формулировка уточняется:

> **Salt** = первые 16 байт `Header.MessageID` (UUID v4 пакета TDTP).
>
> Соль публична (живёт в header пакета в открытом виде), но криптографически уникальна для каждого пакета: UUID v4 имеет 122 бита энтропии, что исключает коллизии в обозримом будущем. Это даёт три выгоды:
>
> 1. **Не нужно отдельное поле в зашифрованном blob** — соль уже есть в header
> 2. **Невозможность атак по радужным таблицам** — даже если злоумышленник перехватит миллион пакетов с одним паролем, у каждого своя соль
> 3. **Привязка ключа к пакету** — расшифровка любого другого пакета тем же ключом даст gibberish

Формат заголовка зашифрованного блоба обновляется:

```
[2B version][1B algorithm][12B nonce][...encrypted data]
```

(блок salt удалён — соль берётся из `Header.MessageID`).

### 6 (дополнение) — таблица гарантий безопасности

К таблице из v1.1 добавляются строки:

| Гарантия | Механизм |
|---|---|
| **Невозможность подделки данных пакета** | Three-level xxh3_128 с UUID-солью (§11) + Mercury `SET NX` (§11.3). Изменение пакета после регистрации → stored `xxh3` ≠ presented `xxh3` → BLOCK. Перерегистрация заблокирована атомарно. |
| **Различение случайности от намерения** | Три независимых хэша (Schema/Data/Packet). Случайная битая запись ломает все три. Намеренная подделка с пересчётом — оставляет след доступа к Mercury (X-Caller, IP, timestamp). |
| **Защита от replay-атак** | UUID v4 как соль. Хэш одного пакета не валиден для любого другого, даже с идентичным содержимым. |
| **Защита от deniability** | Хэши + Mercury registry + квота создают конструкцию, в которой «случайное» изменение технически невозможно. Все следы фиксируются в audit log Mercury. |

### 8 (обновление) — план реализации v1.2

К этапам 1-6 из v1.1 добавляются:

| Этап | Задачи | Оценка | Статус |
|---|---|---|---|
| 7 | Hash registry в xZMercury (`hashstore` + 3 endpoints) | 3 дня | ✅ выполнено |
| 8 | Three-level xxh3_128 в `pkg/core/packet/integrity.go` | 2 дня | ✅ выполнено |
| 9 | `pkg/pipeline/VerifyAndPrepare` + 3 fallback-политики | 2 дня | ✅ выполнено |
| 10 | `tdtpcli --integrity` + `@MRC` self-discovery в Dictionary | 1 день | ✅ выполнено |
| 11 | Quota для hash registration (`pkg/hashquota`) | 2 дня | ⬜ pending |
| 12 | Dictionary pre-flight tokens (`@SHA`, `@LOCK`, `@TTL`) | 3 дня | ⬜ pending |
| 13 | E2E test: encrypt + integrity + pre-flight + quota | 2 дня | ⬜ pending |

**Итого по v1.2:** дополнительно 8-10 рабочих дней (этапы 7-10 уже сделаны).

---

## 11. Hash Registry — центр сертификации целостности пакетов

### 11.1. Концепция

xZMercury расширяется ролью **«паспортный стол для пакетов TDTP v1.4»**. По аналогии с государственным удостоверением личности:

| Реальный мир | xZMercury |
|---|---|
| Гражданин приходит с заявлением + биометрия | Producer вычисляет `xxh3` пакета |
| Паспортный стол выдаёт паспорт под номером | Mercury регистрирует `(uuid, part) → xxh3` |
| Контроль на границе сверяет паспорт с базой | Consumer запрашивает `VerifyHash(uuid, part, xxh3)` |
| Подделать паспорт — нужны типография + бланки + договорённости | Подделать `xxh3` — нужно пересчитать 3 хэша с UUID-солью + обойти `SET NX` |

**Цель:** обеспечить неподделываемость данных пакета **без HSM, PKI, X.509, RSA, ГОСТ-сертификации** — используя только xxh3_128 + Redis `SET NX`. Эмуляция центра сертификации **минимальными средствами**.

### 11.2. Three-level XXH3-128 integrity

Пакет TDTP v1.4 стампируется тремя хэшами **с UUID пакета как солью**:

```
Schema.xxh3  = xxh3_128(UUID || canonical_schema_xml)   ← "паспорт данных"
Data.xxh3    = xxh3_128(UUID || joined_row_values)      ← "сами данные"
pkt.xxh3     = xxh3_128(Schema.xxh3 + "|" + Data.xxh3)  ← "fingerprint пакета"
```

Все хэши — 32-символьные lowercase hex строки (128 бит = 16 байт).

**Зачем три хэша вместо одного:**

| Сценарий | Что увидит консьюмер |
|---|---|
| Битая передача / распаковка | Все три mismatch (`Data.checksum` xxh3_64 должен был поймать раньше) |
| Подмена Dictionary / типов полей | Только `Schema.xxh3` mismatch — **намерение** |
| Подмена данных строк | Только `Data.xxh3` mismatch — **намерение** |
| Попытка обновить `pkt.xxh3` без знания формулы | `pkt.xxh3` mismatch при совпадающих Schema+Data — **намерение** |
| Подмена с пересчётом всех трёх хэшей локально | Все три совпадают, но Mercury возвращает stored ≠ presented — **квалифицированное намерение** |

Три хэша — это **три независимых свидетеля**. Случайно расходятся все одновременно, намеренно — выборочно. Это даёт диагностическое различение случайности от умысла.

### 11.3. Redis namespace: `mercury:hash:*`

Hash registry живёт в **том же Mercury Redis**, что и ключи, но в отдельном namespace:

```
mercury:hash:{uuid}:{part}    TTL=HashTTL (default 24h)
```

**Ключевое отличие от keystore (`mercury:key:*`):**

| Свойство | `mercury:key:*` (v1.1) | `mercury:hash:*` (v1.2) |
|---|---|---|
| Операция чтения | `GETDEL` (burn-on-read) | `GET` (read-only) |
| Назначение | Расшифровка результата | Верификация целостности |
| Сколько раз читается | **1** (одноразово) | **N** (любое число консьюмеров) |
| Кто читает | Конкретный получатель | Любой консьюмер в пайплайне |
| TTL по умолчанию | 60 сек | **24 часа** (`HashTTL`) |
| Защита от перерегистрации | Не требуется | **`SET NX`** (атомарно) |

**Композитный ключ `{uuid}:{part}` как защита от перерегистрации:**

UUID v4 имеет 122 бита энтропии — глобальная уникальность гарантирована вероятностно. Это означает:

1. **`SET NX` блокирует любую перерегистрацию** — слот занят навечно (до Revoke или TTL expiry)
2. **После TTL expiry слот свободен, но UUID уже не появится снова** — новый пакет имеет новый UUID
3. **Атакер не может «дождаться TTL и подделать»** — UUID одноразовый по природе

**Конфигурация:**

```yaml
# xzmercury/config.yaml
hash_ttl: 24h   # время жизни регистрации в hash registry
```

### 11.4. HTTP API

Расширение существующего xZMercury API тремя endpoint:

#### `POST /api/hashes`

Producer регистрирует пакет после `ComputeIntegrity`, до публикации в брокер / S3 / файл.

**Request:**
```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "part": 0,
  "xxh3": "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5",
  "table": "payroll_q1",
  "sender": "svc-exporter",
  "packet_version": "1.4"
}
```

**Headers:** `X-Caller: svc-exporter` (обязательно, для audit log)

**Responses:**
- `201 Created` — слот зарегистрирован
- `409 Conflict` — слот уже занят (предыдущая регистрация или атака на пред-занятие)
- `429 Too Many Requests` — превышена квота сендера (см. §13)
- `503 Service Unavailable` — Mercury недоступен

#### `GET /api/hashes/{uuid}/{part}?xxh3={presented_xxh3}`

Consumer верифицирует пакет перед обработкой.

**Headers:** не требуются (read-only operation).

**Response:** всегда `200 OK` с JSON body:

```json
{
  "registered": true,
  "match": false,
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "part": 0,
  "stored_xxh3": "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5",
  "table": "payroll_q1",
  "sender": "svc-exporter",
  "packet_version": "1.4",
  "expires_in_seconds": 82394
}
```

**Семантика полей `registered` + `match`:**

| `registered` | `match` | Действие консьюмера |
|---|---|---|
| `true` | `true` | ✅ Proceed — пакет подлинный |
| `true` | `false` | ❌ BLOCK — `ErrHashTampered` (подмена после регистрации) |
| `false` | — | ❌ BLOCK — `ErrHashNotRegistered` (слот неизвестен) |

#### `DELETE /api/hashes/{uuid}/{part}`

Admin Revoke — досрочное аннулирование регистрации (для компрометированных пакетов).

**Headers:** `X-Caller` с admin-привилегиями.

**Responses:**
- `204 No Content` — слот удалён
- `404 Not Found` — слот не существует
- `403 Forbidden` — нет admin-прав

### 11.5. Структура хранения

В Redis по ключу `mercury:hash:{uuid}:{part}` лежит JSON:

```json
{
  "uuid": "550e8400-e29b-41d4-a716-446655440000",
  "part": 0,
  "xxh3": "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5",
  "table": "payroll_q1",
  "sender": "svc-exporter",
  "packet_version": "1.4",
  "registered_at": "2026-05-22T09:14:32Z"
}
```

### 11.6. Audit log

Каждая операция (`Register`, `Verify`, `Revoke`) пишется в Mercury Pipeline Redis:

```
ZADD mercury:audit:hashes:{YYYYMMDD} <unix_ts> "<json_event>"
```

Это даёт:
- Полный chronological лог за день
- Range query «все операции по uuid X»
- Range query «все операции от sender Y»
- Auto-expiry по дню через keyspace notifications

---

## 12. Consumer Pre-flight Pipeline

### 12.1. `pkg/pipeline/VerifyAndPrepare`

Единая точка входа консьюмера для всех TDTP-пакетов:

```go
result, err := pipeline.VerifyAndPrepare(ctx, pkt, mercuryClient, policy)
```

Для пакетов `version != "1.4"` — pass-through без проверок (обратная совместимость с v1.0 / v1.3.1).

Для пакетов `version == "1.4"` — три последовательных шага:

```
Step 1: Mercury executor check
        GET /api/hashes/{uuid}/{part}?xxh3={pkt.XXH3}
        ├─ registered=true,  match=true  → переход к Step 2
        ├─ registered=true,  match=false → ErrHashTampered  → BLOCK
        ├─ registered=false              → ErrHashNotRegistered → BLOCK
        └─ Mercury unreachable           → apply FallbackPolicy

Step 2: Local xxh3 integrity
        packet.VerifyIntegrity(pkt)
        пересчёт Schema.xxh3, Data.xxh3, pkt.xxh3 локально
        mismatch → BLOCK с указанием конкретного хэша

Step 3: Dictionary expansion
        @tokens → full values
        pkt.Schema.Dictionary = nil после развёртывания
        downstream-адаптеры видят только plain-значения
```

### 12.2. Три политики fallback

При **`ErrMercuryUnavailable`** (Mercury недоступен, не ошибка авторизации) поведение определяет `FallbackPolicy`:

| Policy | Поведение | Безопасность | Доступность | Use case |
|---|---|---|---|---|
| `FallbackBlock` | Отказ + лог | ★★★ | ★ | Финансы, медицина, юридически значимые документы |
| `FallbackDegrade` | Пропуск Step 1, выполнение Step 2+3, `result.Degraded=true` | ★★ | ★★★ | Операционные данные с требованием непрерывности (логи, метрики) |
| `FallbackDowngrade` | Конвертация в v1.3.1 in-place (`packet.Downgrade`), `result.Version="1.3.1"` | ★ | ★★★ | Интеграция с legacy-системами без поддержки v1.4 |

**Рекомендации в YAML pipeline-конфигурации:**

```yaml
pipeline:
  consumer:
    fallback_policy: block  # block | degrade | downgrade
```

**Важно:** `ErrHashTampered` и `ErrHashNotRegistered` блокируют пакет **всегда**, независимо от политики. Fallback применяется только к недоступности Mercury (5xx, connection refused, timeout).

### 12.3. Self-discovery через `@MRC` в Dictionary

При экспорте с `--integrity --mercury-url=<url>` producer автоматически добавляет в `Schema.Dictionary`:

```xml
<Entry short="@MRC" full="http://mercury:3000"/>
```

Это означает: **консьюмеру не нужен внешний конфиг с адресом Mercury** — пакет сам себе говорит, где его регистрировали. Header peek (< 1 KB) даёт всю информацию для pre-flight check.

### 12.4. Pre-v1.4 backward compatibility

```go
if pkt.Version != "1.4" {
    return &VerifyResult{Version: pkt.Version}, nil  // legacy pass-through
}
```

Пакеты v1.0 и v1.3.1 проходят без проверок. Ни одна новая операция не запускается. **Это два условия из `verify.go`, не отдельная инфраструктура.**

---

## 13. Quota для регистрации хэшей

### 13.1. Концепция: «пищалка на выходе»

§3.3 v1.1 ввёл часовое квотирование на **execution pipeline** — это про защиту вычислительных ресурсов (CPU/IO/память).

v1.2 добавляет **второй контур квотирования** — на **hash registration**. Это про защиту от намеренной подделки методом «социальной инженерии»:

```
Сценарий «забывчивого бухгалтера»:

1. Экспортирует отчёт                       → Mercury 201, регистрация #1/50 на день
2. Замечает ошибку, перегенерирует          → Mercury 201, #2/50
3. Замечает ещё ошибку                      → Mercury 201, #3/50
...
50. Случайно нажимал не туда                → Mercury 429 Too Many Requests
51. Решает «подправлю в блокноте»           → не получится: квота исчерпана,
                                              регистрация невозможна 4 часа
                                              → SIEM alert: TDTP-Finance > 50 регистраций
```

**Эффект:** случайные fat-fingers тают в шуме, **серия попыток подобрать xxh3** автоматически палится как «целенаправленные действия».

### 13.2. Redis schema

```
mercury:hash-quota:{sender}:{YYYYMMDD}   TTL=86400  (24h)
```

Атомарный INCR через Lua-скрипт:

```lua
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local n = redis.call('INCR', key)
if n == 1 then
    redis.call('EXPIRE', key, 86400)
end
if n > limit then
    return -1  -- quota exhausted
end
return limit - n  -- remaining
```

### 13.3. Конфигурация лимитов

```yaml
# xzmercury/config.yaml
hash_quotas:
  per_sender_per_day:
    default: 1000        # обычные сервисы
    svc-payroll: 100     # кадры — мало пакетов в день
    svc-1c: 50           # бухгалтерия — ещё меньше
    svc-medical: 30      # медданные — единичные операции
  burst:
    window_seconds: 60
    max_per_window: 10
```

### 13.4. Поведение API при превышении

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 14523
X-Quota-Limit: 50
X-Quota-Used: 50
X-Quota-Resets-At: 2026-05-23T00:00:00Z
Content-Type: application/json

{
  "error": "quota_exhausted",
  "message": "daily hash registration quota exhausted for sender 'svc-payroll'",
  "resets_at": "2026-05-23T00:00:00Z"
}
```

**Headers возвращаются и при успешной регистрации** — продюсер видит остаток квоты в `X-Quota-Remaining`.

### 13.5. SIEM alerting

При превышении квоты или burst-блоке Mercury публикует событие в `mercury:audit:alerts`:

```json
{
  "event": "quota_exhausted",
  "sender": "svc-payroll",
  "limit": 50,
  "window": "daily",
  "ts": "2026-05-22T15:42:11Z",
  "client_ip": "10.0.13.42",
  "last_uuids": ["uuid1", "uuid2", "uuid3"]
}
```

Mercury Pub/Sub-канал `mercury:alerts` слушается SIEM-коннектором (отдельный сервис вне scope этого ТЗ).

---

## 14. Dictionary as Dependency Manifest

### 14.1. Концепция

TDTP v1.4 Dictionary — это `@token → full_value` mapping для дедупликации повторяющихся значений в строках. **Но "full" может быть любой opaque строкой** — не только namespace URI.

Это превращает Dictionary в **самоописательный манифест зависимостей пакета**: консьюмер читает только header (< 1 KB), парсит Dictionary, и принимает go/no-go решение **до** декомпрессии, **до** запроса ключа, **до** записи в БД.

**Killer feature для §3 (шифрование v1.1):**

```
БЕЗ Dictionary pre-flight:               С @SHA + @MRC pre-flight:
1. Получить ключ из Mercury  ← БУРН      1. Прочитать @SHA из header
2. Расшифровать                          2. Сравнить @SHA с SHA блоба
3. Файл оказался битый                      ├─ mismatch → NACK, ключ цел
4. Ключ сгорел, данные потеряны             └─ match    → продолжить
                                          3. Запрос ключа из Mercury
                                          4. Гарантированно успешная расшифровка
```

Это решает **фундаментальную проблему burn-on-read** из §6 v1.1: одноразовый ключ нельзя сжечь на битом файле.

### 14.2. Зарезервированные токены метаданных

| Token | Семантика | Пример значения | Кто читает |
|---|---|---|---|
| `@MRC` | xZMercury base URL | `https://mercury.internal:3000` | Consumer pre-flight |
| `@SHA` | SHA-256 шифрованного blob | `sha256:a3f8b2c1d4e5...` | Pre-verify до запроса ключа |
| `@SZ`  | Размер payload в байтах | `bytes:892441` | Сверка с Content-Length |
| `@LOCK` | Soft-stop пайплайна | `status:available` / `status:locked` | NACK без брокер-конфига |
| `@TTL` | Embargo lift time | `2026-05-22T08:00:00Z` | Финотчёты, эмбарго |
| `@ALG` | Compression+encryption | `kanzi:7+aes256-gcm` | Проверка наличия кодека |
| `@SRC` | Origin system | `axapta://corp/PAYROLL` | Audit / migration tracking |
| `@VER` | Schema migration phase | `migration-phase:2-of-4` | Multi-phase rollouts |

**Конвенция forward compatibility:** консьюмер, не понимающий конкретный токен, **молча игнорирует его** (не блокирует пакет). Это позволяет вводить новые токены без поломки старых консьюмеров.

### 14.3. Pre-flight check algorithm

```
Consumer receives packet (from queue / FS / S3 / HTTP):

  1. PEEK HEADER (< 1 KB, no streaming)
     Read XML until </Schema>.

  2. PARSE DICTIONARY
     Extract all Entry elements. O(N) on entry count.

  3. CHECK DEPENDENCIES
     for each Entry:
       @MRC   → HEAD https://mercury/health
       @SHA   → SHA-256 of compressed+encrypted blob
       @SZ    → file size / Content-Length
       @LOCK  → if "status:locked" → NACK immediately
       @TTL   → if now() < @TTL → leave in queue
       @ALG   → check codec available locally

  4. DECISION
     ALL pass → ACK, proceed to full parse + import
     ANY fail → NACK with reason

  Никакой декомпрессии, никакой расшифровки, никаких ключей не сгорает.
```

### 14.4. `@LOCK` — distributed soft-stop

Producer может **остановить обработку конкретного пакета** через установку `@LOCK`:

```xml
<Entry short="@LOCK" full="status:locked"/>
```

Консьюмер NACK-нет пакет и оставит его в очереди. Когда producer пришлёт новый пакет с `status:available` — обработка возобновится.

**Сравнение с альтернативами:**

| Сценарий | Традиционное решение | С `@LOCK` |
|---|---|---|
| Остановить обработку на maintenance | Pause queue consumer | Set `@LOCK` в следующем пакете |
| Заморозить пайплайн от producer | Требует broker admin | Producer сам ставит lock |
| Гранулярная остановка (одна таблица) | Per-queue config | Per-packet flag |
| Audit hold ("заморозь dataset") | Application-level flag в БД | Встроено в пакет |

### 14.5. Кросс-транспортная применимость

Pattern работает идентично на любом транспорте:

| Transport | Pre-flight механизм |
|---|---|
| RabbitMQ | `basic.get` + reject without requeue |
| Kafka | `poll()` → peek → `seek()` back if NACK |
| Redis Streams | `XREADGROUP` → `XACK` только если pass |
| S3 / filesystem | Open + read first 1 KB → close без delete |
| HTTP webhook | Read body → 503 Retry-After если NACK |

**Логика circuit breaker — внутри данных, не в брокере.** Смена брокера не требует переписывания конфигурации защиты.

---

## 15. chiptdtp Enterprise Tier (опционально, для будущего)

### 15.1. Когда нужен L3

| Уровень | Имя | Угроза | Стоимость |
|---|---|---|---|
| L1 | `tdtpcli` (текущий OSS) | Случайные правки, fat-finger | Бесплатно |
| L2 | `tdtpcli --integrity` (v1.2) | Намеренная подделка с обходом xxh3 | 3 флага CLI |
| **L3** | **`chiptdtp`** (proprietary) | Целенаправленная атака компетентного нарушителя | Контракт + поддержка |

L3 нужен только в нишах, где утечка/подделка стоит дороже инфраструктуры:

- Минобороны (гостайна)
- Топ-банки (данные VIP-клиентов)
- Аэропорты / РЖД (маршруты VIP)
- Госуслуги (биометрия)
- Medical (HIPAA-equivalent, штрафы + иски)

### 15.2. Архитектурные отличия L3

```
                    L2 (tdtpcli)              L3 (chiptdtp)
─────────────────  ───────────────────────  ───────────────────────────
Лицензия            Apache 2.0 / MIT        Proprietary
Исходники           Открытые                Закрытые, подписанный бинарь
Режимы              plain + --integrity     --enc + --integrity ОБЯЗАТЕЛЬНО
Конфиг              Локальный YAML          Ephemeral от сервера (TTL 5 мин)
Подпись пакета      xxh3_128                xxh3_128 + Ed25519
Self-check          Нет                     License Authority + SHA-256 бинаря
Identity            X-Caller header         Smart-card / TOTP / WebAuthn
Audit               Локальные логи          SIEM stream (realtime)
Mercury             mTLS опционально        mTLS + client cert обязательно
```

### 15.3. License Authority

Отдельный сервис **вне** Mercury — управление лицензиями на использование самого бинаря:

```
┌─────────────────────────────────────────────────────────────┐
│  License Authority (Go + Postgres + Ed25519)                │
│  - GET /api/binary/{sha256}    → разрешён / отозван         │
│  - GET /api/operator/{login}/permissions → таблицы          │
│  - POST /api/audit             → все запуски в SIEM         │
└─────────────────────────────────────────────────────────────┘
```

**Workflow запуска chiptdtp:**

```
1. Self-check: SHA-256 собственного бинаря → LA
   └─ если SHA не в whitelist → терминация

2. Operator auth: smart-card / TOTP → LA
   └─ если нет прав на запрошенную таблицу → терминация

3. Config fetch: ephemeral YAML (TTL 5 мин) → НИКОГДА на диск

4. Export: стандартный pipeline + Ed25519 signature

5. SIEM audit: каждое действие → realtime stream
```

### 15.4. Что использует L3 из v1.2

L3 **переиспользует на 70%** компоненты v1.2:
- ✅ `pkg/core/packet/integrity.go` — xxh3 fingerprints
- ✅ `pkg/mercury/hashclient.go` — Mercury hash registry
- ✅ `pkg/pipeline/VerifyAndPrepare` — pre-flight pipeline
- ✅ Dictionary tokens (`@MRC`, `@SHA`, `@LOCK`)

L3 **добавляет сверху**:
- Ed25519 signing layer
- License Authority service
- Ephemeral config server
- Smart-card / TOTP integrations
- SIEM connector

**Это означает:** разработка L3 — это **не переписывание с нуля**, а обёртка вокруг существующего OSS-фундамента. Архитектура v1.2 уже подготовлена для L3.

---

## 16. Итоговый технологический стек v1.2

К стеку v1.1 добавляется:

| Компонент | Технология | Лицензия |
|---|---|---|
| Hash fingerprinting | `github.com/zeebo/xxh3` v1.1.0 | BSD-3 |
| Hash registry | Redis 7 (тот же Mercury Redis) | BSD |
| Hash quotas | Redis 7 (тот же Mercury Redis) | BSD |
| Audit log | Redis 7 Sorted Sets | BSD |

**Дополнительная стоимость лицензий:** $0. Все компоненты open-source или уже используются.

**Никакой новой инфраструктуры**: hash registry живёт в существующем Mercury Redis, в отдельном namespace (`mercury:hash:*`). Не нужно поднимать новые сервисы, обновлять docker-compose, или обучать админов.

---

## 17. Резюме v1.2

| Свойство | v1.1 | v1.2 |
|---|---|---|
| **Confidentiality** (что данные нельзя прочитать) | ✅ AES-256-GCM + burn-on-read | ✅ (без изменений) |
| **Integrity** (что данные нельзя подделать) | ⚠️ только `Data.checksum` xxh3_64 | ✅ Three-level xxh3_128 + Mercury notary |
| **Availability** (graceful degradation) | ⚠️ Mercury недоступен = блок | ✅ 3 политики fallback |
| **Non-repudiation** (нельзя сказать «я не я») | ⚠️ только AD logs | ✅ Hash registry + quota + audit |
| **Self-description** (пакет описывает свои зависимости) | ❌ | ✅ Dictionary as Manifest |
| **Backward compatibility** | ✅ | ✅ pre-v1.4 pass-through |

**Принцип v1.2: минимальными средствами — максимальное покрытие угроз.**

- ~600 строк нового кода (без тестов)
- 1 новый API endpoint pair (`POST/GET/DELETE /api/hashes`)
- 1 новый Redis namespace (`mercury:hash:*`)
- 3 новых CLI флага (`--integrity`, `--mercury-url`, `--mercury-caller`)
- 0 новых сервисов
- 0 новых лицензий
- 0 новых инфраструктурных требований

Это **L2 защита** в терминах §15: останавливает 99% реальных угроз (случайные правки + намеренная подделка без специнструмента). L3 (`chiptdtp`) зарезервирован архитектурно для случаев, когда нужны оставшиеся 1%.

---

*Версия документа: v1.2*
*Предыдущая: v1.1 от 25.02.2026*
*Следующая запланированная: v1.3 — etalon SIEM connector + chiptdtp proof-of-concept*
