# TODO NEXT — Sprint план

## Поточний стан (v1.17.0, 2026-06-15)

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
|   | v1.16.1 | RabbitMQ resilience: deliveryChan reset, QoS prefetch=1, heartbeat 10s, exponential reconnect backoff |
| 9 | v1.17.0 | P10 `--steps workflow.yaml` — DAG orchestration, parallel waves, on_error: stop/skip/retry(N) |

---

## Open Items (v1.x)

### 1. Grace period для tdtp.lic

Зараз: expired = fatal. Для integrators під час активного проекту — проблема.
Варіант: `--grace-period 30d` flag, read-only mode після expiry. Не критично зараз.

---

## v2.0 Roadmap — Масштабування, паралелізм, стрімінг

Великий перехід: від single-threaded ETL до паралельної та потокової обробки.
Всі пункти залежать один від одного — вводяться разом як breaking architecture change.

### 2.1 Parallel daemon (`--map --listen --workers N`)

- `--workers N` — N незалежних goroutine-воркерів, кожен зі своїм AMQP connection
- ACK кожного воркера незалежно (жодного shared state між воркерами)
- graceful shutdown: дочекатись завершення поточного повідомлення в кожному воркері
- RabbitMQ: N окремих connections (multiple consumers на одному channel — anti-pattern)
- Kafka: consumer group, N partitions → N workers (нативна модель)

### 2.2 Streaming CLI (`--export-stream` / `--import-stream`)

Код вже готовий: `pkg/core/packet/streaming.go` — `StreamingGenerator` з channel-based API.
Не підключено до CLI.

- `--export-stream` → пише рядки в output по мірі читання з DB (без буферизації всього набору)
- `--import-stream` → читає з stdin/broker рядок за рядком, upsert без accumulation
- Дозволяє обробляти таблиці розміром більше RAM

### 2.3 Schema migration (`ALTER TABLE`)

Add/drop columns, type changes при schema drift між версіями пакета і target таблицею.
Потрібно як основа для `--import-stream` (streaming import потребує schema negotiation).
