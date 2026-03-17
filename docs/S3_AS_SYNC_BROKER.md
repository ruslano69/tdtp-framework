# S3 как брокер синхронизации данных в TDTP

## Зачем это нужно

Классические message brokers (RabbitMQ, Kafka) работают по модели **push/subscribe** — данные
исчезают из очереди после получения. Это отлично для потоков событий, но плохо подходит для
передачи **снапшотов таблиц** между изолированными узлами: нет гарантий повторного чтения, нет
встроенного хранения, сложна маршрутизация между дата-центрами с закрытыми сетями.

S3-совместимое хранилище (AWS S3, MinIO, **SeaweedFS**) даёт другую модель:
**store-and-forward** — производитель записал объект, потребитель прочитал когда готов.
Объект живёт столько, сколько нужно.

TDTP + S3 = **асинхронная, decoupled, самодокументируемая передача таблиц** между любыми узлами.

---

## Что S3 даёт как транспортный слой

### 1. Persist-первый, consume-по-готовности

```
Node A (PostgreSQL)                         Node B (PostgreSQL / другая СУБД)
──────────────────                          ──────────────────────────────────
tdtpcli --export users                      tdtpcli --import s3://bucket/users_20260317.tdtp.xml
  → s3://bucket/users_20260317.tdtp.xml         → import в локальную таблицу
```

Node A и Node B не знают друг о друге. Не нужен прямой сетевой тоннель между ними.
Достаточно, чтобы оба имели доступ к одному S3-endpoint.

### 2. Объект — самодокументируемый пакет

Каждый объект несёт TDTP-метаданные в S3 headers (`x-amz-meta-tdtp-*`):

```
x-amz-meta-tdtp-protocol: TDTP 1.0
x-amz-meta-tdtp-table:    users
x-amz-meta-tdtp-rows:     1          ← число пакетов в файле
```

Потребитель может вызвать `HEAD /key` и понять **что** лежит, без скачивания.
Это позволяет строить роутинг, фильтрацию и оркестрацию на уровне S3 metadata.

### 3. Compression без накладных расходов

Данные уходят в S3 уже сжатыми (zstd level 3, ~4× ratio):

```
PostgreSQL 100 строк → 24 932 байт TDTP XML → 6 024 байт zstd → S3
```

Bandwidth между дата-центрами — дорогой ресурс. 4× экономия встроена в протокол.

### 4. Отказоустойчивость без оркестратора

Нет единой точки отказа. Если потребитель упал — объект остался в S3.
Перезапустился — читает с того же ключа. Логика идемпотентности на стороне TDTP:
стратегии `replace`, `upsert`, `append`.

---

## Топологии синхронизации

### Hub-and-spoke (звезда через центральный S3)

```
  ДЦ-1 (PostgreSQL)                ДЦ-2 (PostgreSQL)
       │  export                         │  import
       ▼                                 ▼
  s3://central-bucket/          s3://central-bucket/
       │                                 │
       └─────────── S3 Broker ───────────┘
                         │
               ДЦ-3 (MSSQL / SQLite)
                    import
```

Все узлы пишут/читают в один бакет. Нет прямых соединений ДЦ↔ДЦ.

### Pipeline (ETL → S3 → потребители)

```
PostgreSQL (source A)  ─┐
PostgreSQL (source B)  ─┤─ ETL pipeline (JOIN + transform) ──► s3://reports/daily_2026-03-17.tdtp.xml
                        │
                   (zstd 4×)
                        │
              ┌─────────┴──────────────┐
              ▼                        ▼
      ДЦ Аналитика               ДЦ Архив
  tdtpcli --import ...       tdtpcli --import ...
```

Один объект в S3 — несколько потребителей. Данные прошли ETL (маскировка PII, JOIN, агрегация)
**до** попадания в S3, не после.

### Edge-to-cloud (периферийный узел)

```
Промышленный объект (закрытая сеть)     Облако / корпоративный ДЦ
─────────────────────────────────       ──────────────────────────
SQLite / PostgreSQL на edge-узле        Центральный PostgreSQL
tdtpcli --export sensors                tdtpcli --import s3://...
  → s3://seaweedfs-edge:8333/...    ──► читает тот же SeaweedFS
     (SeaweedFS локально)               или AWS S3
```

Edge-узел разворачивает **SeaweedFS локально** — полноценный S3 без интернета.
При восстановлении сети — объект реплицируется или вычитывается централизованно.

---

## S3 vs классические message brokers

| Характеристика        | RabbitMQ / Kafka     | S3 / SeaweedFS           |
|-----------------------|----------------------|--------------------------|
| Модель                | Push / Subscribe     | Store-and-Forward        |
| Хранение после чтения | ❌ Удаляется          | ✅ Остаётся              |
| Повторное чтение      | Kafka: да; RMQ: нет  | ✅ Всегда                |
| Маршрутизация         | Exchange / Topics    | Бакеты / префиксы        |
| Доступ без прямой сети | Сложно              | ✅ Через любой HTTP       |
| Размер сообщения      | Ограничен (МБ)       | ✅ Гигабайты             |
| Schema / формат       | Нет стандарта        | ✅ TDTP metadata в headers|
| Оффлайн-узлы          | Проблема             | ✅ Читают при подключении |
| Self-hosted без cloud | Да                   | ✅ SeaweedFS              |

**Вывод**: S3 — не замена брокерам событий. Это другая ниша:
передача **батчевых снапшотов** между узлами с разной доступностью сети и разным расписанием.

---

## SeaweedFS как self-hosted S3-брокер

Ключевое преимущество SeaweedFS перед AWS S3 / MinIO в контексте TDTP:

1. **Один бинарник** — `weed server` запускает master + volume + filer + S3 gateway
2. **Нет cloud-зависимости** — работает в изолированных сетях, на edge, в air-gap ДЦ
3. **IAM через локальный JSON** — не нужен AWS IAM, Vault, или внешний identity provider
4. **Тот же AWS SDK** — `ForcePathStyle: true`, и любой S3-клиент работает без изменений
5. **Встроенная репликация** — volume replication между несколькими SeaweedFS-узлами

Запуск (обязательные флаги для sandbox / private network):
```bash
/tmp/weed server \
    -ip=127.0.0.1 \
    -ip.bind=127.0.0.1 \
    -dir=/data/seaweedfs \
    -filer \
    -s3 \
    -s3.port=8333 \
    -s3.config=/etc/seaweedfs/iam.json
```

IAM конфиг (`/etc/seaweedfs/iam.json`):
```json
{
  "identities": [
    {
      "name": "tdtp-node",
      "credentials": [{"accessKey": "...", "secretKey": "..."}],
      "actions": ["Read", "Write", "List", "Admin"]
    }
  ]
}
```

> **Критично**: `identities`, не `accounts` — SeaweedFS отличается от документации MinIO.

---

## Как TDTP-инструмент использует S3

### Экспорт таблицы → S3 URI

```bash
tdtpcli --config config.yaml \
        --export users \
        --output "s3://tdtp-sync/snapshots/users_$(date +%Y%m%d).tdtp.xml" \
        --compress
```

Конфиг `config.yaml`:
```yaml
storage:
  type: s3
  s3:
    endpoint: "http://seaweedfs-node:8333"
    region: "us-east-1"
    bucket: "tdtp-sync"
    access_key: "testkey"
    secret_key: "testsecret"
```

### Импорт из S3 URI

```bash
tdtpcli --config config.yaml \
        --import "s3://tdtp-sync/snapshots/users_20260317.tdtp.xml" \
        --table users \
        --strategy replace
```

### ETL pipeline с S3 destination

```yaml
output:
  type: tdtp
  tdtp:
    format: xml
    compression: true
    destination: "s3://tdtp-sync/etl/report_20260317.tdtp.xml"
    s3:
      endpoint: "http://seaweedfs-node:8333"
      region: "us-east-1"
      access_key: "testkey"
      secret_key: "testsecret"
```

---

## Что это даёт инструменту в целом

До S3-интеграции TDTP был **точка-в-точку**: экспорт в файл → ручная передача → импорт.

После S3-интеграции TDTP стал **распределённым**: любой узел с доступом к S3-endpoint
может быть производителем или потребителем данных, без прямой связи с источником.

Это превращает TDTP из утилиты ETL в **протокол синхронизации данных** между:
- географически разнесёнными дата-центрами
- cloud и on-premise узлами
- edge-устройствами и центральным хранилищем
- любыми СУБД с разными расписаниями работы

S3-bucket становится **точкой рандеву** — производитель и потребитель встречаются
в хранилище, а не в сети.
