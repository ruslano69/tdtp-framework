# TODO NEXT — Sprint план

## Поточний стан (v1.16.0, 2026-06-15)

### Закриті спринти

| Sprint | Версія | Що закрили |
|--------|--------|------------|
| 1 | v1.11.0 | Повний ланцюг довіри: CA/TPM → xZMercury → tdtp.lic → Orchestrator TrustGate |
| 2 | v1.12.0 | Air-gap offline cert, seat policy, mock-clock renewal test, `issue-unsafe-cert` |
| 3 | v1.12.0 | Structured audit log (JSON/syslog), per-job artifact, LDAP auth, Prometheus, Docker stack |
| 4 | v1.13.0 | `--map --input file.tdtp.xml` (P8: mapping YAML, executor, enum remap, loop guard layers 2+4) |
|   | v1.13.0 | Schema passthrough (`applySchemaPassthrough`) — type-drift bug у SQLite workspace (8 тестів) |
|   | v1.13.0 | `cmd/tdtp-xray` aligned with framework core (~600 рядків видалено) |
| 5 | v1.14.0 | `--map --input s3://bucket/key` — S3-sourced packets в mapping flow |
| 6 | v1.15.0 | `--map --input broker://queue` — RabbitMQ-sourced packets, yaml tags на brokers.Config |
|   | v1.15.0 | consumer.py: `tdtp.sync.branch.customers` → `--map broker://` (staging + merge_proc більше не потрібні) |
| 7 | v1.15.0 | consumer.py: всі 7 entity мігровані на `--map broker://`; staging tables і merge procs видалені |
|   | v1.15.0 | 7 нових mapping YAML: `sync_flights`, `sync_reservations`, `sync_countries`, `sync_guides`, `sync_tours`, `sync_schedule`, `sync_branch_sales` |
| 8 | v1.16.0 | `--map --input broker://queue --listen` — daemon mode; NACK+requeue on error; graceful SIGTERM/SIGINT |

---

## Sprint 9 — `--map --listen --workers N` (parallel daemon)

**Навіщо**: зараз один daemon обробляє одне повідомлення за раз.
При великому трафіку або повільній target DB — вузьке місце.

**Що робити**:
- `--workers N` flag — запускає N горутин-воркерів, кожна зі своїм `Receive`
- ACK кожного воркера незалежно від інших (not в shared state)
- graceful shutdown: wait for all workers to finish current message before exit

Залежить від того чи broker driver підтримує concurrent `Receive`.
RabbitMQ: так (multiple consumers на одній channel не рекомендовано — N connections).
Kafka: так (consumer group, N partitions → N workers).

---

## Open Items

### 1. P10 — Pipeline steps: `depends_on` + `on_error`

Не реалізовано. Потрібно для multi-stage ETL (export → validate → map → import).

```yaml
steps:
  - id: export
    command: "--export ..."
  - id: map
    command: "--map ..."
    depends_on: export
    on_error: retry(3)
```

- Топологічне сортування кроків, паралельне виконання де можливо
- `on_error: stop | skip | retry(N)` з exponential backoff

### 2. Streaming CLI (`--export-stream` / `--import-stream`)

Код готовий: `pkg/core/packet/streaming.go` — `StreamingGenerator` з channel-based API.
Не підключено до CLI. Це `v2.0` scope.

### 3. Schema migration (`ALTER TABLE`)

Add/drop columns, type changes при schema drift між версіями пакета і target таблицею.
`v2.0` scope.

### 4. Grace period для tdtp.lic

Зараз: expired = fatal. Для integrators під час активного проекту — проблема.
Варіант: `--grace-period 30d` flag, read-only mode після expiry. Не критично зараз.
