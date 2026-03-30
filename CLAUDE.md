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

## Сжатие (zstd + kanzi)

Бенчмарк на 100k строк SQLite (benchmark_100k.db, синтетические данные Users):

| Режим              | Время    | Размер | Коэф. |
|--------------------|----------|--------|-------|
| Без сжатия         | 673 мс   | 9.9 MB | —     |
| zstd level 3       | 751 мс   | 2.9 MB | 3.4×  |
| zstd level 19      | 2363 мс  | 2.4 MB | 4.1×  |
| kanzi level 6      | 1279 мс  | 1.5 MB | 6.6×  |
| kanzi level 7      | 1449 мс  | 1.4 MB | 7.1×  |

**Вывод по алгоритмам:**
- `zstd level 3` — дефолт для потоков реального времени: почти бесплатен, 3× экономия
- `kanzi level 6` — оптимум для архивов и бэкапов: **в 2 раза плотнее zstd3**, быстрее zstd19
- `kanzi level 7` — максимум плотности, +170 мс к level 6, выгоден только при медленном канале

На реальных данных с разнородным текстом (кадровые приказы, нарративные описания) kanzi
показывает x10-12 против исходного размера — BWT разворачивается на полную мощность.
На синтетических коротких строках — 6-7×, но это всё равно **на 30-50% плотнее zstd**.

`compress: true` и `compress_level: 3` — дефолт в шаблоне конфига (`CreateSampleConfig`). Не менять.
Для архивных задач: `--compress-algo kanzi --compress-level 6 --hash`.

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

---

## TODO: Переключение SQLite-драйвера на mattn/go-sqlite3

**Мотивация:** `modernc.org/sqlite` медленнее нативного libsqlite3 в ~3× на больших SELECT.
Замеры из [cvilsmeier/go-sqlite-bench](https://github.com/cvilsmeier/go-sqlite-bench):

| Драйвер  | 50k строк | 100k строк | 200k строк |
|----------|-----------|------------|------------|
| mattn    | 122ms     | 207ms      | 376ms      |
| modernc  | 401ms     | 629ms      | 1094ms     |

Наш замер: modernc читает 100k строк за ~800ms, Python (C sqlite3) за ~270ms — тот же 3×.

**Что нужно сделать:**
- Добавить build-тег `cgo_sqlite` (или `!purgo_sqlite`)
- `pkg/adapters/sqlite/adapter.go` — условный импорт: `mattn` (имя `"sqlite3"`) vs `modernc` (имя `"sqlite"`)
- `cmd/tdtpcli/drivers_sqlite.go` — аналогично
- `cmd/bench_raw/main.go` — аналогично
- `cmd/tdtp-xray/...` — аналогично
- Обновить `driverSqlite` константу под тег
- Проверить что `nokafka` + `cgo_sqlite` собираются вместе
- CGO требует gcc; для CI добавить `apt-get install gcc`

**Ожидаемый результат:** экспорт 100k строк SQLite ~500ms вместо ~1500ms.
