# ETL Pipeline — Руководство и сценарии

Документация по ETL pipeline функциональности `tdtpcli --pipeline`.

**Версия:** 1.3
**Дата:** 26.02.2026

---

## Содержание

1. [Обзор](#обзор)
2. [Справочник конфигурации](#справочник-конфигурации)
3. [Сценарий 1: Два TDTP-источника → JOIN → TDTP](#сценарий-1-два-tdtp-источника--join--tdtp)
4. [Сценарий 2: PostgreSQL → TDTP](#сценарий-2-postgresql--tdtp)
5. [Сценарий 3: Шифрованный вывод через xZMercury](#сценарий-3-шифрованный-вывод-через-xzmercury)
6. [Сценарий 4: Redis оркестрация](#сценарий-4-redis-оркестрация)
7. [Сценарий 5: Graceful degradation при отказе xZMercury](#сценарий-5-graceful-degradation)
8. [CLI-флаги pipeline](#cli-флаги-pipeline)
9. [Exit codes](#exit-codes)

---

## Обзор

ETL Pipeline выполняет три фазы:

```
Extract                Transform              Load
────────               ────────────           ────────
TDTP File ──┐                                 TDTP File
PostgreSQL ─┼─→ SQLite Workspace ─→ SQL ─┬─→ RabbitMQ
MSSQL ──────┘    (:memory:)         JOIN  └─→ Kafka
MySQL ──────┘                              └─→ XLSX
SQLite ─────┘
```

Все источники загружаются как таблицы в in-memory SQLite. SQL трансформация объединяет их и формирует результат. Экспорт пишет результат в сконфигурированный выход.

---

## Справочник конфигурации

### Полная схема YAML

```yaml
name: "pipeline-name"       # обязательно — имя pipeline
version: "1.0"              # опционально
description: "..."          # опционально

# ─── ИСТОЧНИКИ ────────────────────────────────────────────────────────────────
sources:
  - name: table_alias       # имя таблицы в SQLite workspace (обязательно)
    type: sqlite            # sqlite | postgres | mssql | mysql | tdtp
    dsn: "path/to/db.db"   # DSN или путь к TDTP файлу
    query: |               # SQL запрос (не для type: tdtp)
      SELECT id, name FROM users
    timeout: 30             # таймаут в секундах (0 = без таймаута)
    multi_part: false       # для type: tdtp — загружать все части набора

# ─── WORKSPACE ────────────────────────────────────────────────────────────────
workspace:
  type: sqlite
  mode: ":memory:"          # ":memory:" или путь к файлу ("workspace.db")

# ─── ТРАНСФОРМАЦИЯ ────────────────────────────────────────────────────────────
transform:
  result_table: "result"    # имя таблицы с результатом (опционально)
  timeout: 60               # таймаут выполнения в секундах
  sql: |
    SELECT ...
    FROM table_alias_1 t1
    JOIN table_alias_2 t2 ON t1.id = t2.fk_id
    WHERE ...

# ─── ВЫВОД ────────────────────────────────────────────────────────────────────
output:
  type: tdtp                # tdtp | rabbitmq | kafka | xlsx

  tdtp:
    destination: "out/result.xml"
    format: "xml"           # xml (единственный поддерживаемый)
    compression: false      # zstd сжатие (true/false)
    encryption: false       # AES-256-GCM через xZMercury (true/false)

  rabbitmq:                 # если type: rabbitmq
    host: localhost
    port: 5672
    user: guest
    password: guest
    queue: etl_results
    vhost: "/"

  kafka:                    # если type: kafka
    brokers: "localhost:9092"
    topic: etl_results

  xlsx:                     # если type: xlsx
    destination: "out/result.xlsx"
    sheet: "Sheet1"

# ─── БЕЗОПАСНОСТЬ (для encryption: true) ────────────────────────────────────
security:
  mercury_url: "http://mercury:3000"  # URL xZMercury
  key_ttl_seconds: 86400              # TTL ключа в Redis (по умолчанию 86400)
  mercury_timeout_ms: 5000            # таймаут обращения (по умолчанию 5000)

# ─── RESULTLOG ────────────────────────────────────────────────────────────────
result_log:
  type: redis               # redis (пустое = отключено)
  address: "127.0.0.1:6379"
  name: "PIPELINE_V001"     # ключ/канал в Redis
  password: ""              # опционально
  db: 0                     # индекс Redis БД
  ttl: 3600                 # TTL в секундах

# ─── ПРОИЗВОДИТЕЛЬНОСТЬ ──────────────────────────────────────────────────────
performance:
  timeout: 300              # максимальное время pipeline (секунды)
  batch_size: 10000
  parallel_sources: true    # загружать источники параллельно
  max_memory_mb: 2048

# ─── ОБРАБОТКА ОШИБОК ────────────────────────────────────────────────────────
error_handling:
  on_source_error: "fail"   # fail | continue
  on_transform_error: "fail"
  on_export_error: "fail"
  retry_count: 3
  retry_delay_sec: 5
```

### Типы источников

| type | DSN формат | query |
|------|-----------|-------|
| `sqlite` | `path/to/db.sqlite` | SQL SELECT |
| `postgres` | `postgres://user:pass@host:5432/db?sslmode=disable` | SQL SELECT |
| `mssql` | `server=host;user id=sa;password=X;database=DB` | SQL SELECT |
| `mysql` | `user:pass@tcp(host:3306)/db?parseTime=true` | SQL SELECT |
| `tdtp` | `path/to/file.tdtp.xml` | не используется |

---

## Сценарий 1: Два TDTP-источника → JOIN → TDTP

**Задача:** Объединить данные из двух TDTP-файлов (сотрудники + отделы), вычислить зарплатную статистику по отделам, записать в новый TDTP-файл.

**Файлы:**
- `employees.tdtp.xml` — 10 записей (employee_id, full_name, department_id, salary, ...)
- `departments.tdtp.xml` — 4 записи (department_id, department_name, ...)

**`pipeline-basic.yaml`:**

```yaml
name: "employee-dept-report"
version: "1.0"
description: "Зарплатный отчёт по отделам"

sources:
  - name: employees
    type: tdtp
    dsn: "examples/encryption-test/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "examples/encryption-test/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_salary_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id)    AS headcount,
      ROUND(AVG(e.salary), 2) AS avg_salary,
      SUM(e.salary)           AS total_salary,
      MIN(e.salary)           AS min_salary,
      MAX(e.salary)           AS max_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name
    ORDER BY total_salary DESC

output:
  type: tdtp
  tdtp:
    destination: "out/dept_salary_report.xml"

error_handling:
  on_source_error: "fail"
```

**Запуск:**
```bash
mkdir -p out
./tdtpcli --pipeline pipeline-basic.yaml
```

**Результат** (`out/dept_salary_report.xml`):
```xml
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>dept_salary_report</TableName>
    ...
  </Header>
  <Schema>
    <Field name="department_name" type="TEXT"></Field>
    <Field name="headcount"       type="INTEGER"></Field>
    <Field name="avg_salary"      type="REAL"></Field>
    <Field name="total_salary"    type="REAL"></Field>
    <Field name="min_salary"      type="REAL"></Field>
    <Field name="max_salary"      type="REAL"></Field>
  </Schema>
  <Data>
    <R>Engineering|5|98000.00|490000|70000|120000</R>
    <R>Product|2|101000.00|202000|92000|110000</R>
    <R>Finance|1|88000.00|88000|88000|88000</R>
    <R>Human Resources|1|75000.00|75000|75000|75000</R>
  </Data>
</DataPacket>
```

---

## Сценарий 2: PostgreSQL → TDTP

**Задача:** Выгрузить данные о заказах из PostgreSQL, отфильтровать активные, записать в TDTP-файл.

**`pipeline-pg-orders.yaml`:**

```yaml
name: "active-orders-export"
version: "1.0"

sources:
  - name: orders
    type: postgres
    dsn: "postgres://user:password@localhost:5432/shop_db?sslmode=disable"
    query: |
      SELECT
        order_id,
        customer_id,
        order_date,
        total_amount,
        status
      FROM orders
      WHERE status IN ('pending', 'processing')
        AND order_date >= NOW() - INTERVAL '30 days'
      ORDER BY order_date DESC

  - name: customers
    type: postgres
    dsn: "postgres://user:password@localhost:5432/shop_db?sslmode=disable"
    query: |
      SELECT customer_id, name, email, city
      FROM customers
      WHERE active = true

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "active_orders_with_customers"
  sql: |
    SELECT
      o.order_id,
      o.order_date,
      o.total_amount,
      o.status,
      c.name     AS customer_name,
      c.email    AS customer_email,
      c.city
    FROM orders o
    JOIN customers c ON o.customer_id = c.customer_id
    ORDER BY o.total_amount DESC
    LIMIT 500

output:
  type: tdtp
  tdtp:
    destination: "out/active_orders.xml"
    compression: true     # zstd сжатие для больших файлов

performance:
  parallel_sources: true  # загружать обе таблицы одновременно
  timeout: 120
```

**Запуск:**
```bash
./tdtpcli --pipeline pipeline-pg-orders.yaml
```

**Multi-source из разных СУБД** — просто укажи разные DSN и type для каждого источника. Workspace объединит их через JOIN.

---

## Сценарий 3: Шифрованный вывод через xZMercury

**Задача:** Выгрузить конфиденциальные данные (зарплаты), зашифровать AES-256-GCM через xZMercury, записать `.tdtp.enc`.

### Шаг 1: Запустить mock xZMercury

```bash
go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret
# или через Docker:
# docker run -p 3000:3000 -e MERCURY_SERVER_SECRET=dev-secret xzmercury-mock
```

### Шаг 2: Установить секрет

```bash
export MERCURY_SERVER_SECRET=dev-secret
```

### Шаг 3: Создать `pipeline-enc.yaml`

```yaml
name: "employee-dept-report-encrypted"
version: "1.0"
description: "Зарплатный отчёт — AES-256-GCM через xZMercury"

sources:
  - name: employees
    type: tdtp
    dsn: "examples/encryption-test/employees.tdtp.xml"

  - name: departments
    type: tdtp
    dsn: "examples/encryption-test/departments.tdtp.xml"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  result_table: "dept_salary_report"
  sql: |
    SELECT
      d.department_name,
      COUNT(e.employee_id) AS headcount,
      SUM(e.salary)        AS total_salary
    FROM employees e
    JOIN departments d ON e.department_id = d.department_id
    WHERE e.is_active = 1
    GROUP BY d.department_id, d.department_name

output:
  type: tdtp
  tdtp:
    destination: "out/dept_salary_report.tdtp.enc"
    encryption: true          # AES-256-GCM через xZMercury

security:
  mercury_url: "http://localhost:3000"
  key_ttl_seconds: 86400
  mercury_timeout_ms: 5000

result_log:
  type: redis
  address: "127.0.0.1:6379"
  name: "EMP_DEPT_RPT_V001"
  ttl: 3600
```

### Шаг 4: Запустить

```bash
./tdtpcli --pipeline pipeline-enc.yaml
```

**Вывод:**
```
Pipeline: employee-dept-report-encrypted
   Version: 1.0
   Mode: SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 2
   Workspace: sqlite (:memory:)
   Output: tdtp [ENC: xZMercury]

Starting ETL pipeline execution...

ETL Pipeline completed successfully!
   Duration: 0.54s
   Sources loaded: 2
   Rows loaded: 14
   Rows exported: 4
   Package UUID: 550e8400-e29b-41d4-a716-446655440000
```

### Альтернатива: --enc-dev (без xZMercury)

В dev-сборках можно использовать `--enc-dev` — ключ генерируется локально:

```bash
./tdtpcli --pipeline pipeline-enc.yaml --enc-dev
```

Полезно для разработки и CI без развёрнутого xZMercury. В production-сборке (`-tags production`) флаг недоступен.

### --enc: переопределить encryption из CLI

Можно включить шифрование без изменения YAML (например, в CD/CD):

```bash
./tdtpcli --pipeline pipeline-basic.yaml --enc
# эквивалентно output.tdtp.encryption: true в YAML
# требует секции security.mercury_url в YAML
```

---

## Сценарий 4: Redis оркестрация

**Задача:** Несколько pipeline запускаются параллельно, оркестратор отслеживает их статусы через Redis.

**`pipeline-with-resultlog.yaml`:**

```yaml
name: "daily-summary-v2"
version: "2.1"

sources:
  - name: sales
    type: postgres
    dsn: "postgres://user:pass@db:5432/warehouse"
    query: "SELECT * FROM daily_sales WHERE sale_date = CURRENT_DATE"

workspace:
  type: sqlite
  mode: ":memory:"

transform:
  sql: |
    SELECT
      region,
      SUM(amount) AS total_sales,
      COUNT(*)    AS orders_count
    FROM sales
    GROUP BY region

output:
  type: tdtp
  tdtp:
    destination: "out/daily_summary.xml"

result_log:
  type: redis
  address: "redis:6379"
  name: "DAILY_SUMMARY_V2"   # ключ в Redis
  password: "redispass"
  db: 0
  ttl: 7200                  # 2 часа
```

**Запуск:**
```bash
./tdtpcli --pipeline pipeline-with-resultlog.yaml
```

**Что пишется в Redis после завершения:**

```json
{
  "pipeline":      "daily-summary-v2",
  "status":        "success",
  "started_at":    "2026-02-26T10:00:00Z",
  "finished_at":   "2026-02-26T10:00:05Z",
  "duration":      "5.12s",
  "sources_loaded": 1,
  "rows_loaded":   1500,
  "rows_exported": 8,
  "package_uuid":  ""
}
```

**Statuses:**
- `success` — pipeline выполнен без ошибок
- `failed` — pipeline завершился с ошибкой
- `completed_with_errors` — pipeline завершён, но xZMercury недоступен (error-пакет записан)

**Чтение статуса из Python:**

```python
import redis, json

r = redis.Redis(host='redis', port=6379, password='redispass')
result = json.loads(r.get('pipeline:DAILY_SUMMARY_V2'))
print(result['status'])   # "success"
print(result['package_uuid'])  # UUID для расшифровки (при encryption: true)
```

**Graceful degradation Redis:** Если Redis недоступен — pipeline записывает предупреждение в лог и завершается с exit 0. Отсутствие resultlog не считается ошибкой.

---

## Сценарий 5: Graceful Degradation

**Задача:** Понять поведение системы при недоступности xZMercury.

**Условие:** `encryption: true`, но xZMercury недоступен.

**Поведение:**

1. ETL-процесс загружает данные, выполняет SQL трансформацию — всё нормально
2. При попытке `POST /api/keys/bind` → connection refused (или timeout)
3. Незашифрованные данные **НЕ записываются**
4. В destination записывается `error` пакет (`Type=error`, таблица `tdtp_errors`)
5. ResultLog получает статус `completed_with_errors` с `package_uuid`
6. Pipeline завершается с **exit code 0**

**Вывод:**
```
Pipeline: employee-dept-report-encrypted
   ...
Starting ETL pipeline execution...

WARNING: Encryption degraded: bind key: MERCURY_UNAVAILABLE: connect: connection refused
   Error packet written to output. Pipeline completed with errors (exit 0).
```

**Содержимое destination (`out/dept_salary_report.tdtp.enc`):**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>error</Type>
    <TableName>tdtp_errors</TableName>
    <MessageID>ERR-2026-a1b2c3d4-P1</MessageID>
    ...
  </Header>
  <Schema>
    <Field name="package_uuid"  type="TEXT" length="36" key="true"></Field>
    <Field name="pipeline"      type="TEXT" length="255"></Field>
    <Field name="error_code"    type="TEXT" length="64"></Field>
    <Field name="error_message" type="TEXT" length="1000"></Field>
    <Field name="created_at"    type="TIMESTAMP" timezone="UTC"></Field>
  </Schema>
  <Data>
    <R>550e8400-...|employee-dept-report-encrypted|MERCURY_UNAVAILABLE|connect: connection refused|2026-02-26T10:00:00Z</R>
  </Data>
</DataPacket>
```

**Downstream-потребитель** получает стандартный TDTP-пакет и может:
- Записать запись в таблицу `tdtp_errors`
- Уведомить оркестратор о необходимости повтора
- Не падать — `error` пакет имеет стандартную структуру Schema+Data

**Коды ошибок:**

| Код | Ситуация |
|-----|---------|
| `MERCURY_UNAVAILABLE` | connection refused, timeout |
| `MERCURY_ERROR` | HTTP 5xx от xZMercury |
| `HMAC_VERIFICATION_FAILED` | HMAC подпись не совпала (неверный `MERCURY_SERVER_SECRET`) |
| `KEY_BIND_REJECTED` | HTTP 403 (нет прав) или 429 (rate limit) |

---

## CLI-флаги pipeline

```
--pipeline <file>     Путь к YAML-конфигурации
--unsafe              Разрешить все SQL (требует admin, используй sudo)
--enc                 Override: включить output.tdtp.encryption=true
--enc-dev             Dev-режим: локальный ключ (только !production сборки)
```

**Приоритеты:**
- `--enc` / `--enc-dev` **переопределяют** `output.tdtp.encryption` в YAML
- `encryption: true` в YAML без флагов работает так же как `--enc`
- При `--enc` без `security.mercury_url` в YAML → ошибка конфигурации

**Production сборка:**
```bash
# Исключает --enc-dev, DevClient и любой dev-only код
go build -tags production -o tdtpcli ./cmd/tdtpcli/
```

---

## Exit codes

| Code | Ситуация |
|------|---------|
| 0 | Pipeline выполнен успешно |
| 0 | `completed_with_errors` — Mercury недоступен, error-пакет записан |
| 1 | Ошибка конфигурации, SQL validation, источник данных, экспорт |
| 1 | Unsafe mode без прав admin |
