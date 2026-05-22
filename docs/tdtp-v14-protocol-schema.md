# TDTP v1.4 — схема использования протокола

## Участники

```
┌─────────────┐        ┌──────────────────┐        ┌─────────────┐
│  PRODUCER   │        │   xzMercury      │        │  CONSUMER   │
│ (exporter)  │        │ (hash registry)  │        │ (importer)  │
└──────┬──────┘        └────────┬─────────┘        └──────┬──────┘
       │                        │                          │
       │     ◄── Redis ──────── │                          │
       │     mercury:hash:      │                          │
       │       {uuid}:{part}    │                          │
       │     (SET NX, TTL 24h)  │                          │
```

---

## PRODUCER: подготовка пакета

```
┌──────────────────────────────────────────────────────────────────┐
│  1. GenerateReference(schema, rows)                              │
│     → DataPacket{Version:"1.4", Header.MessageID: uuid4}        │
│                                                                  │
│  2. ComputeIntegrity(pkt)                                        │
│     → Schema.xxh3  = xxh3_128(UUID || Schema_bytes)             │
│     → Data.xxh3    = xxh3_128(UUID || row_bytes)                │
│     → pkt.xxh3     = xxh3_128(schema_xxh3 + "|" + data_xxh3)   │
│                                                                  │
│  3. mercury.RegisterHash(uuid, part, pkt.xxh3, table, sender)   │
│     POST /api/hashes                                             │
│     → Mercury: SET NX mercury:hash:{uuid}:{part}                │
│     → 201 Created  ✓                                            │
│     → 409 Conflict ✗ (слот занят — атакер опередил, LOG+ALERT)  │
│                                                                  │
│  4. Send packet to queue / S3 / broker                          │
└──────────────────────────────────────────────────────────────────┘
```

XML заголовок пакета:
```xml
<DataPacket protocol="TDTP" version="1.4"
            xxh3="a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5">
  <Header>
    <MessageID>550e8400-e29b-41d4-a716-446655440000</MessageID>
    <PartNumber>0</PartNumber>
    <TableName>payroll_q1</TableName>
    ...
  </Header>
  <Schema xxh3="1122334455667788aabbccddeeff0011">
    <Field name="id" type="INTEGER" key="true"/>
    <Field name="ns" type="TEXT"/>
    <Dictionary>
      <Entry short="@W3" full="http://www.w3.org/2000/svg"/>
    </Dictionary>
  </Schema>
  <Data xxh3="ffeeddccbbaa99887766554433221100" compression="zstd">
    <R>1|@W3</R>
    <R>2|plain</R>
  </Data>
</DataPacket>
```

---

## CONSUMER: pre-flight → обработка

```
receive packet
     │
     ▼
┌─────────────────────────────────────────────────────────────────┐
│  pipeline.VerifyAndPrepare(ctx, pkt, mercuryClient, policy)     │
└──────────────────────────┬──────────────────────────────────────┘
                           │
              pkt.Version == "1.4" ?
              NO  ──────────────────────────────► legacy pass-through
              YES ↓
                           │
          ┌────────────────▼──────────────────────────────────┐
          │  STEP 1: Mercury executor check                   │
          │  GET /api/hashes/{uuid}/{part}?xxh3={pkt.xxh3}   │
          └────────────────┬──────────────────────────────────┘
                           │
          ┌────────────────┼───────────────────────────────────┐
          │                │                                    │
    registered=true  registered=false              Mercury недоступен
    match=true       (слот не найден)              (ErrMercuryUnavailable)
          │                │                                    │
          │       ErrHashNotRegistered              ┌───────────┴──────────────┐
          │          BLOCK + LOG ✗           policy=Block  policy=Degrade  policy=Downgrade
          │                                     │         │               │
          │                              BLOCK + LOG ✗  warn,       Downgrade(pkt)
          │                                            continue     → v1.3.1 path
          │                                                │               │
          ◄───────────────────────────────────────────────┘               │
          │  Degraded=true, DegradedReason="Mercury unavailable"          │
          │                                                                │
   match=false                                                             │
   (stored_xxh3 ≠ pkt.xxh3)                                               │
   ErrHashTampered                                                         │
   BLOCK + LOG ✗                                                           │
                                                                           │
          │                                                                │
          ▼                                                                │
┌─────────────────────────────────────────────┐                           │
│  STEP 2: Local xxh3 integrity               │                           │
│  packet.VerifyIntegrity(pkt)               │                           │
│  recompute xxh3_128(UUID||schema)           │                           │
│  recompute xxh3_128(UUID||rows)             │                           │
│  compare with pkt.Schema.xxh3, Data.xxh3   │                           │
│  → mismatch: BLOCK + LOG ✗                 │                           │
└──────────────────┬──────────────────────────┘                           │
                   │                                                       │
          ┌────────▼───────────────────────────────────────────────┐      │
          │  STEP 3: Dictionary expansion                          │      │
          │  NewDictExpander(pkt.Schema.Dictionary)                │      │
          │  for each row: ExpandRow("1|@W3") → "1|http://..."    │      │
          │  pkt.Schema.Dictionary = nil (downstream sees plain)   │      │
          └────────┬───────────────────────────────────────────────┘      │
                   │                                                       │
                   ▼                                                       ▼
          VerifyResult{                                         VerifyResult{
            Version:    "1.4",                                   Version:    "1.3.1",
            Degraded:   false,                                   Degraded:   true,
            MercuryRec: {table, sender, ...},                    DegradedReason: "...",
          }                                                    }
                   │                                                       │
                   └───────────────────┬───────────────────────────────────┘
                                       │
                                       ▼
                              DB write / adapter
```

---

## Три политики fallback

| Policy | Mercury недоступен | Безопасность | Доступность |
|---|---|---|---|
| `FallbackBlock` | Блок, ошибка | ★★★ | ★ |
| `FallbackDegrade` | Продолжить, только локальный xxh3 | ★★ | ★★★ |
| `FallbackDowngrade` | Конвертировать в v1.3.1 in-place | ★ | ★★★ |

**Рекомендации по выбору политики:**

```yaml
# Финансовые отчёты, медицинские данные, юридически значимые документы:
fallback_policy: block        # нет Mercury = нет данных

# Операционные данные с требованием непрерывности (логи, метрики):
fallback_policy: degrade      # локальная целостность гарантирована

# Интеграция с legacy-системами только v1.3.1:
fallback_policy: downgrade    # автоматический откат версии
```

---

## Что проверяет каждый уровень

```
Level 1: Mercury (executor control)
  ✓ Пакет зарегистрирован аутентифицированным продюсером
  ✓ UUID+part → stored_xxh3 == pkt.xxh3 (не подменён после регистрации)
  ✓ Повторная регистрация заблокирована (SET NX)
  ✗ Не защищает: если Mercury недоступен

Level 2: Local xxh3_128 (integrity)
  ✓ Schema не изменена (поля, типы, Dictionary)
  ✓ Строки данных не изменены
  ✓ UUID использован как соль — хэш уникален для каждого пакета
  ✗ Не защищает: атакер знает алгоритм и UUID (публичны)

Level 3: Dictionary expansion (transparency)
  ✓ @tokens заменены полными значениями до записи в БД
  ✓ Downstream-система видит только plain-значения
  ✓ Обратная совместимость с pre-v1.4 адаптерами

Data.checksum (legacy, v1.3.1+):
  ✓ xxh3_64 сжатого блоба — защита от битого сжатия
  ✗ Не заменяет уровни 1-2
```

---

## Pre-v1.4 пакеты — без изменений

```
v1.0 / v1.3.1 packet
     │
     ▼
VerifyAndPrepare(pkt, ...)
     │
pkt.Version != "1.4"
     │
     ▼
pass-through (return immediately)
     │
     ▼
DB write / adapter
```

Ни одна из новых проверок не запускается. Поведение идентично предыдущим версиям.

---

## Использование в коде

```go
// PRODUCER
pkt, _ := gen.GenerateReference("payroll_q1", schema, rows)
packet.ComputeIntegrity(pkt)
mercuryClient.RegisterHash(ctx,
    pkt.Header.MessageID, pkt.Header.PartNumber,
    pkt.XXH3, pkt.Header.TableName, "svc-exporter", pkt.Version)
broker.Publish(pkt)

// CONSUMER
received := broker.Consume()
result, err := pipeline.VerifyAndPrepare(ctx, received, mercuryClient, pipeline.FallbackDegrade)
if err != nil {
    log.Error().Err(err).
        Str("uuid", received.Header.MessageID).
        Msg("BLOCK: packet integrity check failed")
    broker.Nack(received)
    return
}
if result.Degraded {
    log.Warn().Str("reason", result.DegradedReason).Msg("degraded mode")
}
// received теперь готов к записи в БД:
// - Dictionary развёрнут
// - Version может быть "1.3.1" если применён FallbackDowngrade
adapter.Write(received)
broker.Ack(received)
```

---

## Атаки и защита

| Атака | Защита |
|---|---|
| Изменить строки данных | Level 2: Data.xxh3 не совпадёт |
| Изменить схему (поле/тип) | Level 2: Schema.xxh3 не совпадёт |
| Обновить pkt.xxh3 под новый контент | Level 1: stored_xxh3 в Mercury ≠ pkt.xxh3 |
| Зарегистрировать фейк в Mercury заранее | SET NX: слот уже занят продюсером |
| DDoS Mercury для обхода проверки | FallbackBlock: без Mercury = нет данных |
| Replay: отправить старый пакет ещё раз | UUID уникален; Level 1 вернёт stored hash |
| Изменить только Dictionary | Level 2: Schema.xxh3 включает Dictionary bytes |
