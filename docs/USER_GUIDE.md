# TDTP CLI - Руководство пользователя

**tdtpcli** - утилита командной строки для работы с TDTP (Table Data Transfer Protocol).

**Версия:** 1.7.0
**Дата:** 10.03.2026

---

## Содержание

1. [Установка](#установка)
2. [Быстрый старт](#быстрый-старт)
3. [Конфигурация](#конфигурация)
4. [Команды](#команды)
5. [Compact Format (v1.3.1)](#compact-format-v131)
6. [ETL Pipeline](#etl-pipeline)
7. [Шифрование AES-256-GCM](#шифрование-aes-256-gcm)
8. [Фильтрация данных (TDTQL)](#фильтрация-данных-tdtql)
9. [Работа с Message Brokers](#работа-с-message-brokers)
10. [Примеры использования](#примеры-использования)
11. [Устранение неполадок](#устранение-неполадок)

---

## Установка

### Требования

- **Go** 1.21 или выше (для сборки из исходников)
- **Доступ к БД:** SQLite, PostgreSQL, или MS SQL Server
- **Message Broker** (опционально): RabbitMQ или MSMQ

### Сборка из исходников

```bash
git clone https://github.com/ruslano69/tdtp-framework
cd tdtp-framework
go mod tidy
go build -o tdtpcli ./cmd/tdtpcli
```

### Проверка установки

```bash
./tdtpcli --help
```

---

## Быстрый старт

### 1. Создание конфигурации

Выберите тип базы данных:

**SQLite:**
```bash
./tdtpcli --create-config-sqlite
```

**PostgreSQL:**
```bash
./tdtpcli --create-config-pg
```

**MS SQL Server:**
```bash
./tdtpcli --create-config-mssql
```

Будет создан файл `config.{dbtype}.yaml` с шаблоном настроек.

### 2. Редактирование конфигурации

Откройте созданный файл и укажите параметры подключения:

**config.postgres.yaml:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: myuser
  password: mypassword
  dbname: mydb
  schema: public
  sslmode: disable
```

### 3. Проверка подключения

Получите список таблиц:

```bash
./tdtpcli -config config.postgres.yaml --list
```

### 4. Экспорт данных

Экспортируйте таблицу в файл:

```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

### 5. Импорт данных

Импортируйте данные из файла:

```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

---

## Конфигурация

### Структура конфигурационного файла

```yaml
# Настройки базы данных
database:
  type: postgres         # sqlite | postgres | mssql

  # SQLite
  path: database.db     # Путь к файлу БД (только для SQLite)

  # PostgreSQL / MS SQL
  host: localhost
  port: 5432            # 5432 для PostgreSQL, 1433 для MS SQL
  user: username
  password: password
  dbname: database_name

  # PostgreSQL specific
  schema: public        # Схема БД (default: public)
  sslmode: disable      # disable | require | verify-ca | verify-full

  # MS SQL specific
  instance: SQLEXPRESS  # Имя инстанса (опционально)
  encrypt: false        # Шифрование соединения
  trustServerCertificate: true

# Настройки message broker (опционально)
broker:
  type: rabbitmq        # rabbitmq | msmq
  host: localhost
  port: 5672            # 5672 для RabbitMQ
  user: guest
  password: guest
  queue: tdtp_queue     # Имя очереди
  vhost: /              # Virtual host (RabbitMQ)
  durable: true         # Устойчивость очереди
  auto_delete: false    # Автоудаление очереди
  exclusive: false      # Эксклюзивность очереди
```

### Примеры конфигураций

**SQLite:**
```yaml
database:
  type: sqlite
  path: ./database.db
```

**PostgreSQL с RabbitMQ:**
```yaml
database:
  type: postgres
  host: localhost
  port: 5432
  user: tdtp_user
  password: secure_password
  dbname: production_db
  schema: public
  sslmode: require

broker:
  type: rabbitmq
  host: rabbitmq.example.com
  port: 5672
  user: tdtp
  password: broker_password
  queue: tdtp_production_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
```

**MS SQL Server:**
```yaml
database:
  type: mssql
  host: sql-server.example.com
  port: 1433
  user: sa
  password: MyStr0ngP@ssw0rd
  dbname: MyDatabase
  instance: SQLEXPRESS
  encrypt: true
  trustServerCertificate: false
```

---

## Команды

### --list

Показать список таблиц в базе данных.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --list
```

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --list
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📋 Tables in database (4):
  1. users
  2. products
  3. orders
  4. activity_logs
```

---

### --export

Экспортировать таблицу в файл или stdout.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --export <table> [--output <file>]
         [--compact [--fixed-fields <поля>]]
```

**Параметры:**
- `<table>` - имя таблицы или VIEW (обязательно)
- `--output <file>` - выходной файл (опционально, по умолчанию stdout)
- `--compact` - включить compact-формат TDTP v1.3.1 (carry-forward для fixed-полей)
- `--fixed-fields <поля>` - список fixed-полей через запятую (используется совместно с `--compact`); если не задан, fixed-поля определяются автоматически по `_prefix` или данным

**Примеры:**

Экспорт в stdout:
```bash
./tdtpcli -config config.postgres.yaml --export users
```

Экспорт в файл:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

Автоматическое добавление расширения:
```bash
./tdtpcli -config config.postgres.yaml --export users --output users
# Создаст файл: users.tdtp.xml
```

Compact-экспорт VIEW с `_prefix`-колонками (auto-detect):
```bash
./tdtpcli -config config.yaml --export dept_employees_report \
  --compact --output report_compact.tdtp.xml
```

Compact-экспорт с явным указанием fixed-полей:
```bash
./tdtpcli -config config.yaml --export employees \
  --compact --fixed-fields dept_id --output emp_compact.tdtp.xml
```

---

### --import

Импортировать данные из TDTP файла.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --import <file> [--table <name>] [--strategy <strategy>]
```

**Параметры:**
- `<file>` - путь к TDTP файлу (обязательно)
- `--table <name>` - имя целевой таблицы (опционально, по умолчанию из пакета)
- `--strategy <strategy>` - стратегия импорта: `replace` | `copy` (опционально)

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --import users.tdtp.xml
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📥 Importing from file: users.tdtp.xml
✅ Imported 100 rows into table 'users'
```

**Compact-формат (v1.3.1):**

При импорте файла с атрибутом `compact="true"` carry-forward раскрывается **автоматически** — все строки восстанавливаются до полных значений до записи в БД. Дополнительных флагов не требуется.

```bash
# Compact-файл импортируется так же, как обычный
./tdtpcli -config config.yaml --import dept_report_compact.tdtp.xml --table dept_emp_imported --strategy replace
```

**Стратегии импорта:**

По умолчанию используется стратегия на основе типа пакета:
- **reference** → REPLACE (полная замена через temp table)
- **delta** → COPY (вставка новых записей)
- **response** → REPLACE

---

### --export-broker

Экспортировать таблицу в message broker queue.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --export-broker <table>
```

**Параметры:**
- `<table>` - имя таблицы (обязательно)

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --export-broker users
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

📤 Exporting table: users
✅ Successfully published 1 packets to queue 'tdtp_queue'
   Total rows: 100
```

---

### --import-broker

Импортировать данные из message broker queue.

**Синтаксис:**
```bash
tdtpcli -config <config.yaml> --import-broker
```

**Работа:**
- Подключается к очереди
- Ожидает сообщения (blocking mode)
- Импортирует данные в БД
- Подтверждает получение (manual ACK)
- Продолжает ожидать следующих сообщений

**Пример:**
```bash
./tdtpcli -config config.postgres.yaml --import-broker
```

**Вывод:**
```
📁 Using config: config.postgres.yaml
🔌 Connecting to postgres...
✅ Connected to postgres (PostgreSQL 15.15)

📡 Connecting to rabbitmq broker...
✅ Connected to broker

🎧 Listening for messages on queue 'tdtp_queue'...
   Press Ctrl+C to stop

📦 Received reference packet for table 'users' (100 rows)
   Type: REFERENCE - full sync via temp table
📋 Import to temporary table: users_tmp_20251116_204210
✅ Data loaded to temporary table
🔄 Replacing production table: users
✅ Production table replaced successfully
   ✓ Message acknowledged and removed from queue
✅ Imported 100 rows into table 'users' (total: 1 packets, 100 rows)

🎧 Waiting for next message...
```

**Остановка:**
- Нажмите `Ctrl+C` для корректного завершения

---

### --diff

Сравнить два TDTP файла и показать различия.

**Синтаксис:**
```bash
tdtpcli --diff <file-a> <file-b> [опции]
```

**Параметры:**
- `<file-a>` - первый TDTP файл (обязательно)
- `<file-b>` - второй TDTP файл (обязательно)
- `--key-fields <поля>` - ключевые поля для сравнения (опционально, через запятую)
- `--ignore-fields <поля>` - поля для игнорирования (опционально, через запятую)
- `--case-sensitive` - учитывать регистр при сравнении (по умолчанию - нет)

**Примеры:**

Сравнить два файла:
```bash
./tdtpcli --diff users-old.xml users-new.xml
```

Сравнить с указанием ключевого поля:
```bash
./tdtpcli --diff users-old.xml users-new.xml --key-fields user_id
```

Игнорировать временные поля:
```bash
./tdtpcli --diff users-old.xml users-new.xml --ignore-fields created_at,updated_at
```

Сравнение с учетом регистра:
```bash
./tdtpcli --diff users-old.xml users-new.xml --case-sensitive
```

**Вывод:**
```
=== Diff Statistics ===
Total in A: 100
Total in B: 105
Added:      5
Removed:    2
Modified:   3
Unchanged:  95

=== Added (5) ===
+ 101 | John Doe | john@example.com
+ 102 | Jane Smith | jane@example.com
...

=== Removed (2) ===
- 50 | Old User | old@example.com
...

=== Modified (3) ===
~ Key: 10
  [2] email: 'old@mail.com' → 'new@mail.com'
...
```

**Exit codes:**
- 0 - Файлы идентичны или сравнение успешно
- 1 - Произошла ошибка

---

### --merge

Объединить несколько TDTP файлов в один.

**Синтаксис:**
```bash
tdtpcli --merge <file1,file2,file3,...> --output <result> [опции]
```

**Параметры:**
- `<file1,file2,...>` - список файлов через запятую (минимум 2 файла)
- `--output <file>` - выходной файл (обязательно)
- `--merge-strategy <strategy>` - стратегия объединения (опционально, по умолчанию `union`)
- `--key-fields <поля>` - ключевые поля для дедупликации (опционально, через запятую)
- `--show-conflicts` - показать детальную информацию о конфликтах
- `--compress` - сжимать результат с помощью zstd
- `--compress-level <1-22>` - уровень сжатия (по умолчанию 3)

**Стратегии объединения:**

1. **union** (по умолчанию) - объединение всех уникальных строк с дедупликацией по ключу
2. **intersection** - только строки, присутствующие во ВСЕХ файлах
3. **left** / **left-priority** - при конфликте оставлять значение из первого файла
4. **right** / **right-priority** - при конфликте оставлять значение из последнего файла
5. **append** - просто объединить все строки без дедупликации

**Примеры:**

Объединить 3 файла (union с дедупликацией):
```bash
./tdtpcli --merge users-1.xml,users-2.xml,users-3.xml --output users-merged.xml
```

Intersection (только общие записи):
```bash
./tdtpcli --merge file1.xml,file2.xml --output common.xml --merge-strategy intersection
```

Left priority (при конфликтах - первый файл):
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy left --key-fields user_id
```

Right priority (при конфликтах - последний файл):
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy right --key-fields user_id
```

Append (без дедупликации):
```bash
./tdtpcli --merge part1.xml,part2.xml,part3.xml --output all.xml --merge-strategy append
```

С сжатием:
```bash
./tdtpcli --merge file1.xml,file2.xml --output merged.xml --compress --compress-level 9
```

Показать конфликты:
```bash
./tdtpcli --merge old.xml,new.xml --output result.xml --show-conflicts
```

**Вывод:**
```
=== Merge Statistics ===
Packets merged: 3
Total rows in:  300
Total rows out: 250
Duplicates:     50
Conflicts:      10

=== Conflicts ===
Key 42: used_new
Key 55: used_new
...
```

**Примечания:**
- Все файлы должны относиться к одной таблице
- Схема (список полей) должна совпадать
- Для дедупликации требуются ключевые поля (или primary key в схеме)

---

### --to-compact

Конвертировать существующий TDTP v1.x файл в compact-формат v1.3.1.

**Синтаксис:**
```bash
tdtpcli --to-compact <input-file> [--output <output-file>] --fixed-fields <поля> [-config <config.yaml>]
```

**Параметры:**
- `<input-file>` - исходный TDTP файл (v1.0 или v1.3.x, обязательно)
- `--output <file>` - выходной файл (опционально; если не задан — файл перезаписывается на месте)
- `--fixed-fields <поля>` - список fixed-полей через запятую (если не задан — auto-detect по `_prefix` или данным)
- `-config <config.yaml>` - конфигурационный файл (опционально, нужен только при подключении к БД)

**Приоритет определения fixed-полей:**
1. Явный `--fixed-fields f1,f2` — используются указанные поля
2. `_prefix` — поля с именем, начинающимся на `_`, становятся fixed (имя `_dept_id` → `dept_id`)
3. Анализ данных — поля с одинаковым значением по всем строкам помечаются как fixed

**Примеры:**

Конвертация с явным указанием fixed-полей:
```bash
./tdtpcli --to-compact employees_plain.tdtp.xml \
  --output employees_compact.tdtp.xml \
  --fixed-fields dept_id
```

Auto-detect по данным (VIEW с `_prefix`):
```bash
./tdtpcli --to-compact dept_report.tdtp.xml --output dept_report_compact.tdtp.xml
```

Конвертация на месте (overwrite):
```bash
./tdtpcli --to-compact report.tdtp.xml --fixed-fields dept_id,region
```

**Примечания:**
- Версия пакета устанавливается в `1.3.1`, добавляется атрибут `compact="true"` в `<Data>`
- Fixed-поля в `<Schema>` получают атрибут `fixed="true"`
- Строки кодируются carry-forward: в каждой группе только первая строка содержит значения fixed-полей, остальные — пустые (`||`)
- Совместим с `--compress`: можно скомбинировать compact + zstd-сжатие

---

## Compact Format (v1.3.1)

TDTP v1.3.1 вводит **compact-формат** для эффективного хранения данных с повторяющимися значениями в группах строк.

### Принцип работы

Поля, помеченные как `fixed="true"` в схеме, записываются только один раз на группу строк (при первом появлении). Остальные строки группы содержат пустые значения на месте fixed-полей — это **carry-forward** (перенос значения вперёд).

**Пример compact-файла:**
```xml
<DataPacket version="1.3.1" ...>
  <Schema>
    <Field name="dept_id"   type="INTEGER" fixed="true"/>
    <Field name="dept_name" type="TEXT"    fixed="true"/>
    <Field name="emp_id"    type="INTEGER"/>
    <Field name="full_name" type="TEXT"/>
  </Schema>
  <Data compact="true">
    <R>10|Sales|101|Ivan Petrov</R>     <!-- dept 10: header row -->
    <R>|||102|Anna Sidorova</R>          <!-- dept 10: carry-forward -->
    <R>|||103|Boris Kozlov</R>           <!-- dept 10: carry-forward -->
    <R>20|Engineering|201|Alice Volkov</R> <!-- dept 20: новая группа -->
    <R>|||202|Charlie Morozov</R>        <!-- dept 20: carry-forward -->
  </Data>
</DataPacket>
```

### Соглашение `_prefix`

При экспорте VIEW, где колонки с групповыми данными имеют имя с `_` в начале, `--compact` автоматически:
- Определяет эти поля как fixed
- Удаляет `_` из имени в Schema (`_dept_id` → `dept_id`)

```sql
CREATE VIEW dept_employees_report AS
SELECT
    d.dept_id   AS _dept_id,    -- станет fixed="true", имя dept_id
    d.dept_name AS _dept_name,  -- станет fixed="true", имя dept_name
    e.emp_id,
    e.full_name
FROM employees e JOIN departments d ON e.dept_id = d.dept_id
ORDER BY d.dept_id, e.emp_id;
```

### Экономия размера

| Таблица | Обычный | Compact | Экономия |
|---------|---------|---------|----------|
| 15 строк, 3 группы × 5 emp, 3 fixed поля | 100% | ~60% | ~40% |
| 1000 строк, 10 групп × 100 emp, 5 fixed полей | 100% | ~30% | ~70% |

Эффективность растёт с количеством строк в группе и количеством fixed-полей.

---

## ETL Pipeline

### --pipeline

Выполнить ETL pipeline из YAML-конфигурации: загрузить несколько источников, трансформировать в in-memory SQLite workspace, экспортировать результат.

Подробная документация с примерами: [docs/ETL_PIPELINE.md](ETL_PIPELINE.md)

**Синтаксис:**
```bash
tdtpcli --pipeline <config.yaml> [--unsafe] [--enc] [--enc-dev]
```

**Параметры:**

| Флаг | Описание |
|------|----------|
| `--pipeline <file>` | Путь к YAML-конфигурации pipeline |
| `--unsafe` | Разрешить все SQL операции (требует права admin) |
| `--enc` | Переопределить `output.tdtp.encryption: true` (шифрование через xZMercury) |
| `--enc-dev` | Dev-режим: локальная генерация ключа без xZMercury (только !production сборки) |

**Режимы безопасности SQL:**

| Режим | SQL операции | Права |
|-------|-------------|-------|
| Safe (по умолчанию) | Только SELECT / WITH | Нет |
| Unsafe (`--unsafe`) | Все операции | Admin |

**Примеры:**

Базовый запуск:
```bash
./tdtpcli --pipeline pipeline.yaml
```

Запуск с шифрованием (override):
```bash
./tdtpcli --pipeline pipeline.yaml --enc
```

Dev-режим шифрования (ключ генерируется локально):
```bash
./tdtpcli --pipeline pipeline.yaml --enc-dev
```

Unsafe mode:
```bash
sudo ./tdtpcli --pipeline pipeline.yaml --unsafe
```

**Вывод при успехе:**
```
Pipeline: employee-dept-report
   Зарплатный отчёт по отделам
   Version: 1.0
   Mode: SAFE (READ-ONLY: SELECT/WITH only)
   Sources: 2
   Workspace: sqlite (:memory:)
   Output: tdtp [ENC: xZMercury]

Starting ETL pipeline execution...

ETL Pipeline completed successfully!
   Duration: 1.23s
   Sources loaded: 2
   Rows loaded: 14
   Rows exported: 4
   Package UUID: 550e8400-e29b-41d4-a716-446655440000
```

**Вывод при деградации xZMercury:**
```
WARNING: Encryption degraded: bind key: MERCURY_UNAVAILABLE: ...
   Error packet written to output. Pipeline completed with errors (exit 0).
```

---

## Шифрование AES-256-GCM

TDTP CLI поддерживает шифрование выходного файла через **xZMercury UUID-binding флоу**:

```
tdtpcli ──→ POST /api/keys/bind ──→ xZMercury
                                       │
                                ┌──────┘
                                │ {key_b64, hmac}
                                ▼
              Verify HMAC (MERCURY_SERVER_SECRET)
                                │
                                ▼
              AES-256-GCM encrypt(XML, key)
                                │
                                ▼
              Write .tdtp.enc (binary header + ciphertext)
```

### Конфигурация YAML

```yaml
output:
  type: tdtp
  tdtp:
    destination: "out/report.tdtp.enc"
    encryption: true          # активирует шифрование

security:
  mercury_url: "http://mercury:3000"
  key_ttl_seconds: 86400      # TTL ключа (24 часа)
  mercury_timeout_ms: 5000    # таймаут обращения к Mercury
```

### Переменные окружения

```bash
MERCURY_SERVER_SECRET=<secret>   # для верификации HMAC подписи ключа
```

### Тестирование с mock-сервером

```bash
# 1. Запустить mock xZMercury
go run ./cmd/xzmercury-mock/ --addr :3000 --secret dev-secret

# 2. Установить секрет
export MERCURY_SERVER_SECRET=dev-secret

# 3. Запустить pipeline
./tdtpcli --pipeline examples/encryption-test/pipeline-enc.yaml
```

### Dev-режим (без xZMercury)

В dev-сборках (`go build` без тега `production`) доступен `--enc-dev`:

```bash
./tdtpcli --pipeline pipeline.yaml --enc-dev
```

- Ключ AES-256 генерируется локально
- xZMercury НЕ нужен
- HMAC не верифицируется
- В production-сборке (`-tags production`) флаг недоступен

### Формат зашифрованного файла

```
[2 байта: версия] [1 байт: алгоритм] [16 байт: package UUID]
[12 байт: nonce AES-GCM] [N байт: ciphertext+tag]
```

### Graceful degradation

При недоступности xZMercury:
- Незашифрованные данные **не записываются**
- В destination записывается `error` пакет (TDTP `Type=error`)
- Pipeline завершается с **exit code 0**
- ResultLog получает статус `completed_with_errors` с `package_uuid`

---

## Фильтрация данных (TDTQL)

### Параметры фильтрации

| Параметр | Описание | Пример |
|----------|----------|--------|
| `--where` | Условие фильтрации | `--where "age > 25"` |
| `--order-by` | Сортировка | `--order-by "balance DESC"` |
| `--limit` | Лимит записей | `--limit 100` |
| `--offset` | Пропустить записей | `--offset 50` |

### Операторы WHERE

**Числовые сравнения:**
```bash
--where "age > 25"
--where "balance >= 1000.50"
--where "quantity < 10"
--where "price <= 99.99"
```

**Текстовые совпадения:**
```bash
--where "username = 'admin'"
--where "status != 'deleted'"
```

**Boolean:**
```bash
--where "is_active = 1"
--where "is_verified = 0"
```

**NULL проверки:**
```bash
--where "deleted_at IS NULL"
--where "email IS NOT NULL"
```

> **Важно:** Для проверки NULL всегда используй `IS NULL` / `IS NOT NULL`.
> Конструкция `field = NULL` в SQL некорректна — всегда возвращает false.

### Сортировка

**Одиночная:**
```bash
--order-by "created_at DESC"
--order-by "username ASC"
```

**Множественная:**
```bash
--order-by "balance DESC, age ASC"
--order-by "city ASC, created_at DESC"
```

### Пагинация

**Первые 100 записей:**
```bash
--limit 100
```

**Записи 51-100 (пропустить первые 50):**
```bash
--limit 50 --offset 50
```

### Комбинированные запросы

**Фильтр + Сортировка + Лимит:**
```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "balance >= 5000" \
  --order-by "balance DESC" \
  --limit 20
```

**Пагинация + Фильтр:**
```bash
./tdtpcli -config config.postgres.yaml --export orders \
  --where "status = 'completed'" \
  --order-by "order_date DESC" \
  --limit 50 --offset 100
```

### Фильтрация при экспорте в broker

```bash
./tdtpcli -config config.postgres.yaml --export-broker users \
  --where "is_active = 1" \
  --limit 1000
```

---

## Работа с Message Brokers

### RabbitMQ

**Настройка конфигурации:**
```yaml
broker:
  type: rabbitmq
  host: localhost
  port: 5672
  user: guest
  password: guest
  queue: tdtp_queue
  vhost: /
  durable: true
  auto_delete: false
  exclusive: false
```

**Параметры очереди:**
- `durable: true` - очередь сохраняется при перезапуске RabbitMQ
- `auto_delete: false` - очередь не удаляется автоматически
- `exclusive: false` - очередь доступна для нескольких подключений

**Типичный workflow:**

1. **Система A** - экспорт данных:
```bash
./tdtpcli -config config.postgres.yaml --export-broker users --where "updated_at >= '2025-11-16'"
```

2. **Система B** - импорт данных:
```bash
./tdtpcli -config config.sqlite.yaml --import-broker
```

### MSMQ (Windows)

**Настройка конфигурации:**
```yaml
broker:
  type: msmq
  queue: .\\private$\\tdtp_queue
```

**Особенности:**
- Работает только на Windows
- Использует локальные или сетевые очереди MSMQ
- Поддерживает транзакционные очереди

**Пример:**
```bash
tdtpcli.exe -config config.mssql.yaml --export-broker users
```

---

## Примеры использования

### Пример 1: Синхронизация справочников между PostgreSQL и SQLite

**Задача:** Синхронизировать справочник пользователей из PostgreSQL в SQLite.

**Шаг 1:** Экспорт из PostgreSQL в файл
```bash
./tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml
```

**Шаг 2:** Импорт в SQLite
```bash
./tdtpcli -config config.sqlite.yaml --import users.tdtp.xml
```

### Пример 2: Выборочный экспорт активных пользователей

**Задача:** Экспортировать только активных пользователей с балансом > 1000.

```bash
./tdtpcli -config config.postgres.yaml --export users \
  --where "is_active = 1" \
  --where "balance > 1000" \
  --order-by "balance DESC" \
  --output active_users.tdtp.xml
```

**Примечание:** Текущая версия CLI поддерживает один `--where` параметр. Для сложных запросов используйте SQL-like синтаксис или модифицируйте код CLI.

### Пример 3: Репликация через RabbitMQ

**Задача:** Непрерывная репликация заказов из MS SQL в PostgreSQL через RabbitMQ.

**Терминал 1 (MS SQL - Publisher):**
```bash
# Экспорт новых заказов каждые 5 минут (через cron/scheduled task)
./tdtpcli -config config.mssql.yaml --export-broker orders \
  --where "created_at >= '2025-11-16 12:00:00'"
```

**Терминал 2 (PostgreSQL - Subscriber):**
```bash
# Непрерывное ожидание сообщений
./tdtpcli -config config.postgres.yaml --import-broker
```

### Пример 4: Топ-20 клиентов по балансу

**Задача:** Получить топ-20 клиентов с максимальным балансом.

```bash
./tdtpcli -config config.postgres.yaml --export customers \
  --order-by "balance DESC" \
  --limit 20 \
  --output top_customers.tdtp.xml
```

### Пример 5: Пагинация больших таблиц

**Задача:** Экспортировать таблицу с миллионом записей порциями по 10000.

```bash
# Первая порция (0-9999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 0 --output part_01.tdtp.xml

# Вторая порция (10000-19999)
./tdtpcli -config config.postgres.yaml --export large_table \
  --limit 10000 --offset 10000 --output part_02.tdtp.xml

# И так далее...
```

### Пример 6: Экспорт в stdout и обработка

**Задача:** Экспортировать данные и сразу обработать через pipe.

```bash
./tdtpcli -config config.postgres.yaml --export users | \
  grep "balance" | \
  wc -l
```

---

## Устранение неполадок

### Проблема: "Database connection failed"

**Симптомы:**
```
❌ Error connecting to database: connection refused
```

**Решение:**
1. Проверьте, что БД запущена:
   ```bash
   # PostgreSQL
   sudo systemctl status postgresql

   # MS SQL (Docker)
   docker ps | grep mssql
   ```

2. Проверьте параметры подключения в config.yaml
3. Проверьте firewall и доступность порта:
   ```bash
   telnet localhost 5432
   ```

### Проблема: "Table not found"

**Симптомы:**
```
❌ Table 'users' does not exist
```

**Решение:**
1. Проверьте список таблиц:
   ```bash
   ./tdtpcli -config config.yaml --list
   ```

2. Для PostgreSQL проверьте схему:
   ```yaml
   database:
     schema: public  # или другая схема
   ```

### Проблема: "Permission denied"

**Симптомы:**
```
❌ Error: permission denied for table users
```

**Решение:**
1. Проверьте права пользователя БД
2. Для PostgreSQL:
   ```sql
   GRANT SELECT, INSERT, UPDATE ON TABLE users TO tdtp_user;
   ```

### Проблема: "Broker connection failed"

**Симптомы:**
```
❌ Failed to connect to broker: dial tcp: connection refused
```

**Решение:**
1. Проверьте, что RabbitMQ запущен:
   ```bash
   sudo systemctl status rabbitmq-server
   ```

2. Проверьте параметры подключения:
   ```yaml
   broker:
     host: localhost  # правильный хост?
     port: 5672       # правильный порт?
   ```

3. Проверьте учетные данные:
   ```bash
   # RabbitMQ default: guest/guest (только для localhost)
   ```

### Проблема: "Packet too large"

**Симптомы:**
```
⚠️ Warning: Packet size exceeds recommended limit
```

**Решение:**
1. Используйте фильтрацию для уменьшения размера:
   ```bash
   --limit 1000
   ```

2. Модифицируйте `MaxMessageSize` в коде:
   ```go
   generator.SetMaxMessageSize(5000000) // 5MB
   ```

### Проблема: "Invalid TDTP format"

**Симптомы:**
```
❌ Failed to parse TDTP file: invalid XML
```

**Решение:**
1. Проверьте, что файл является валидным XML:
   ```bash
   xmllint --noout users.tdtp.xml
   ```

2. Убедитесь, что файл не поврежден
3. Проверьте, что файл создан tdtpcli, а не вручную

---

## Дополнительные ресурсы

- **[SPECIFICATION.md](SPECIFICATION.md)** - полная спецификация TDTP v1.3.1 (включая Compact Format и Special Values)
- **[MODULES.md](MODULES.md)** - описание модулей фреймворка
- **[PACKET_MODULE.md](PACKET_MODULE.md)** - API для работы с пакетами
- **[SCHEMA_MODULE.md](SCHEMA_MODULE.md)** - валидация и типы данных
- **[TDTQL_TRANSLATOR.md](TDTQL_TRANSLATOR.md)** - язык запросов TDTQL

---

## Обратная связь

Нашли баг или хотите предложить улучшение?

- **GitHub Issues:** https://github.com/ruslano69/tdtp-framework/issues
- **Email:** ruslano69@gmail.com

---

*Последнее обновление: 10.03.2026*
