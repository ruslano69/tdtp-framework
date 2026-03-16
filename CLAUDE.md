# Claude Code Notes — tdtp-framework

## Go module downloads (ВАЖНО!)

Если `go build` не может скачать зависимости (blocked proxy, missing zip, etc.) — **СРАЗУ** используй:

```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build ...
```

Или выставь переменные в сессии:
```bash
export GOPROXY=https://goproxy.io
export GONOSUMDB='*'
```

`proxy.golang.org` → редиректит на `storage.googleapis.com` → заблокирован (`no_proxy=*.googleapis.com`).
`goproxy.io` отдаёт пакеты напрямую без редиректов — **работает всегда**.

---

## Test databases

### Python-скрипты для БД уже есть в `/scripts/`!

- `scripts/create_postgres_test_db.py` — PostgreSQL (users, orders, products, activity_logs, 100/200/50 rows)
- `scripts/create_test_db.py` — SQLite
- `scripts/generate_test_db.py` — SQLite benchmark
- `scripts/create_benchmark_db.py` — SQLite benchmark (large)
- `tests/compact_v131/setup_db.py` — SQLite для compact-тестов

**Не создавать новые скрипты — использовать существующие!**

Существующий PostgreSQL user/password: `tdtp_user` / `tdtp_dev_pass_2025` (из `create_postgres_test_db.py`).

Config для тестов:
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: tdtp_dev_pass_2025
  database: tdtp_test
  sslmode: disable
```

### Запуск PostgreSQL
```bash
pg_ctlcluster 16 main start
pg_isready
```

---

## Build tags

- `nokafka` — исключает kafka-go и его зависимости (для офлайн-сборок / без Kafka)
- `nosqlite` — исключает modernc.org/sqlite (для сборок без SQLite)

Быстрая сборка без Kafka:
```bash
GOPROXY=https://goproxy.io GONOSUMDB='*' go build -tags nokafka -o /tmp/tdtpcli ./cmd/tdtpcli/
```

---

## Dev branch

Feature branches: `claude/test-tdtpcli-new-keys-0Z7iA`
