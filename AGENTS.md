# AGENTS.md — tdtpcli для агентной работы с данными

## Главное

`tdtpcli` — единственный инструмент для работы с данными.
Один бинарник = глаза (исследование) + руки (преобразование).
**Физически не может навредить данным** — все операции READ-ONLY по умолчанию.

---

## Рабочий процесс агента

```
1. --list              → что есть в БД?
2. --inspect <file>    → какая структура?   (понять — поля, типы, UUID)
3. --test    <file>    → файл не повреждён? (целостность — чексумма, строки)
4. --export --limit 5  → как выглядят данные?
5. --pipeline etl.yaml → трансформация
6. --diff a.xml b.xml  → что изменилось?
```

> **--inspect vs --test:**
> - `--inspect` — **понимание**: что за файл, какие поля, сколько строк, сжат ли.
> - `--test` — **контроль целостности**: данные не повреждены, чексумма OK, multi-part полный.
> - Оба не требуют подключения к БД. Оба работают с `s3://`.

---

## Команды

### Исследование (READ-ONLY, безопасно)

```bash
# Что есть в БД
tdtpcli --list
tdtpcli --list=order*          # glob фильтр
tdtpcli --list=%log%           # SQL-стиль фильтр

# Views (U* = updatable, R* = read-only)
tdtpcli --list-views
# U* orders_view   → можно импортировать
# R* orders_summary → только экспорт

# Структура таблицы (типы, ключи, subtypes)
tdtpcli --inspect orders
tdtpcli --inspect orders.tdtp.xml   # или TDTP файл

# Посмотреть данные
tdtpcli --export orders --limit 10                          # первые 10
tdtpcli --export orders --limit -1                          # последняя 1 (tail mode)
tdtpcli --export orders --limit -10                         # последние 10
tdtpcli --export orders --order-by "id ASC" --limit -1      # последняя по id
tdtpcli --export orders --offset 100 --limit 50             # пагинация

# Фильтрация
tdtpcli --export orders --where 'status = active'
tdtpcli --export orders --where 'amount > 1000' --limit 5

# Только нужные колонки
tdtpcli --export orders --fields id,status,total_amount

# Включить read-only поля (timestamp, computed, identity)
tdtpcli --export orders --readonly-fields

# Маскировка PII перед экспортом
tdtpcli --export customers --mask email,phone --output safe.tdtp.xml

# Сжатие для больших таблиц
tdtpcli --export logs --compress --output logs.tdtp.xml          # zstd level 3
tdtpcli --export logs --compress --compress-level 19 --output logs.tdtp.xml  # архив

# Сохранить в файл
tdtpcli --export orders --limit 100 --output sample.tdtp.xml
```

### Создать конфиг с нуля

```bash
tdtpcli --create-config-pg     > pg.yaml
tdtpcli --create-config-sqlite > sqlite.yaml
tdtpcli --create-config-mysql  > mysql.yaml
tdtpcli --create-config-mssql  > mssql.yaml
```

### --to-csv (конвертация без БД + фильтрация)

`--to-csv` не требует конфига БД — работает с любым TDTP-файлом, включая сжатые и v1.4.
Все TDTQL-фильтры применяются **в памяти** после декомпрессии.

```bash
# Базовая конвертация
tdtpcli --to-csv users.tdtp.xml

# Разделитель + кодировка + BOM для Excel/1С
tdtpcli --to-csv report.tdtp.xml --delimiter ';' --bom --output report.csv
tdtpcli --to-csv report.tdtp.xml -d '\t' --cp 1251 --bom    # Windows-1251 для старых систем

# Только нужные колонки (без пересоздания таблицы!)
tdtpcli --to-csv users.tdtp.xml --fields 'id,email,balance'
tdtpcli --to-csv staff.tdtp.xml --fields '[Last Name],[First Name],[Birth Date]'

# Фильтрация строк (как --export --where, но из файла)
tdtpcli --to-csv orders.tdtp.xml --where 'total > 1000'
tdtpcli --to-csv users.tdtp.xml --where 'status = active' --where 'balance > 0'

# Сортировка + лимит
tdtpcli --to-csv orders.tdtp.xml --order-by 'total DESC' --limit 10
tdtpcli --to-csv events.tdtp.xml --order-by 'created_at ASC' --limit -100  # последние 100

# Пагинация
tdtpcli --to-csv big_table.tdtp.xml --limit 100 --offset 500

# Полный запрос: проекция + фильтр + сортировка + CSV-настройки
tdtpcli --to-csv orders.tdtp.xml \
  --fields 'id,customer_id,total,status' \
  --where 'status = completed' \
  --where 'total >= 1000' \
  --order-by 'total DESC' \
  --limit 50 \
  --delimiter ';' --bom \
  --output top_orders.csv
```

> Сжатые файлы (zstd/kanzi), compact v1.3.1 и v1.4 integrity-пакеты обрабатываются
> автоматически. БД не нужна.

### Сравнение и слияние

```bash
tdtpcli --diff before.tdtp.xml after.tdtp.xml
tdtpcli --diff a.xml b.xml --key-fields order_id --ignore-fields updated_at

# Стратегии слияния: union (default) | intersection | left | right | append
tdtpcli --merge file1.xml,file2.xml,file3.xml --output merged.xml
tdtpcli --merge old.xml,new.xml --merge-strategy right --show-conflicts
```

### ETL пайплайн (трансформация)

```bash
tdtpcli --pipeline etl.yaml          # безопасный режим (только SELECT)
tdtpcli --pipeline etl.yaml --unsafe # полный SQL (только при необходимости)
```

### Проверка файла перед импортом

```bash
# ШАГ 1: понять структуру (поля, типы, имя таблицы)
tdtpcli --inspect delivery.tdtp.xml

# ШАГ 2: проверить целостность (распаковка, чексумма, строки)
tdtpcli --test delivery.tdtp.xml
# ✓ algo=zstd, 5000 rows, decompressed 23ms, checksum OK

# ШАГ 3: только после этого — импорт
tdtpcli --import delivery.tdtp.xml --config pg.yaml
```

### Импорт результата

```bash
# Стратегии: replace | ignore | fail | copy
tdtpcli --import result.tdtp.xml --strategy replace
tdtpcli --import result.tdtp.xml --strategy ignore   # не перезаписывать существующие
tdtpcli --import result.tdtp.xml --table new_table_name

# Импорт только нужных колонок (whitelist)
tdtpcli --import wide.tdtp.xml --fields id,email,status --table slim_table

# Импорт с санитизацией имён полей (экзотические имена из MSSQL/Access/ERP)
tdtpcli --import access_export.tdtp.xml --clear --strategy replace
# "Order ID" → Order_ID  |  "Total Cost $" → Total_Cost_usd_  |  "Discount %" → Discount_pct_

tdtpcli --import erp_1c.tdtp.xml --translit --clear --strategy replace
# "Имя пользователя" → Imia_polzovatelia  |  "Дата рождения" → Data_rozhdeniia

# Только транслитерация (нет спецсимволов, только кириллица/диакритики)
tdtpcli --import eu_staff.tdtp.xml --translit --strategy replace
# "Österreich" → Osterreich

# --clear и --translit НЕ применяются к --export (экспорт = источник истины)
```

### Квотирование таблиц и полей — Enterprise-режим

Реальные ERP/1С/NAV/Access базы содержат имена вида `ZTR$Employee`, `Last Name`,
`Дата рождения`, `Total Cost $`. Вот полная шпаргалка.

#### Правила квотирования

| Ситуация | Синтаксис | Пример |
|---|---|---|
| Таблица со спецсимволом (`$`, пробел) | `[TableName]` | `[ZTR$Employee]` |
| Поле с пробелом в `--where` | `[Field Name]` или `"Field Name"` | `[Last Name]` |
| Поле с пробелом в `--fields` | `[Field Name]` | `[Last Name],[Birth Date]` |
| Строковое значение в `--where` | одинарные `'...'` | `'Иванов'` |
| Строковое значение с `%` (LIKE) | `"%pattern%"` | `"%ЧЕРКАС%"` |

> Двойные кавычки `"..."` в `--where` = **идентификатор** (ANSI SQL).
> Одинарные кавычки `'...'` в `--where` = **строковый литерал**.

#### Пример — полный энтерпрайз-запрос

```bash
# bash / zsh / Linux / macOS
tdtpcli --config mssql.yaml \
  --export '[ZTR$Employee]' \
  --where '[Last Name] LIKE "%ЧЕРКАСОВ%" AND [Termination Date] = "1753-01-01"' \
  --fields 'No_,FullName,[Last Name],[Birth Date],[Termination Date]' \
  --compress --compress-algo kanzi --compress-level 6 --hash \
  --output exports/cherkasov_active.tdtp.xml
```

```powershell
# PowerShell — таблица в одинарных (защита от $), значение LIKE в одинарных внутри двойных
.\tdtpcli.exe --config mssql.yaml `
  --export '[ZTR$Employee]' `
  --where "[Last Name] LIKE '%ЧЕРКАСОВ%' AND [Termination Date] = '1753-01-01'" `
  --fields 'No_,FullName,[Last Name],[Birth Date],[Termination Date]' `
  --compress --compress-algo kanzi --compress-level 6 --hash `
  --output exports\cherkasov_active.tdtp.xml
```

> **PowerShell 5.x баг с двойными кавычками:** при передаче нативным `.exe`-файлам
> Windows парсит командную строку и `"[Last Name] LIKE "%С%""` разрезается на три куска —
> внутренние `"` теряются. Поэтому правило:
> - `--export` и `--fields` — **одинарные кавычки** (защита от `$`)
> - значения в `--where` — **одинарные внутри двойных**: `"[Field] LIKE '%pattern%'"`
> - `--where` без `$` и без `%` в значении — оба варианта работают

#### PowerShell — правило кавычек для нативных .exe

```powershell
# ❌ двойные снаружи: $Employee раскрывается → таблица "[ZTR]"
.\tdtpcli.exe --export "[ZTR$Employee]"

# ❌ одинарные снаружи + двойные внутри для значения LIKE:
#    Windows CommandLineToArgvW разрезает строку, кавычки теряются
.\tdtpcli.exe --where '[Last Name] LIKE "%ЧЕРКАСОВ%"'
# программа получает: [Last Name] LIKE %ЧЕРКАСОВ%  → parse error

# ✅ таблица и --fields — одинарные кавычки ($ не раскрывается)
.\tdtpcli.exe --export '[ZTR$Employee]'
.\tdtpcli.exe --fields 'No_,[Last Name],FullName'

# ✅ --where со строковым значением — внешние двойные, значение в одинарных
.\tdtpcli.exe --where "[Last Name] LIKE '%ЧЕРКАСОВ%'"
.\tdtpcli.exe --where "[Termination Date] = '1753-01-01'"
.\tdtpcli.exe --where "[Age] > 30 AND [Last Name] LIKE '%ов%'"
```

#### Быстрые примеры

```bash
# Поле с долларом в значении
tdtpcli --export orders --where '[Total Cost $] > 100'

# LIKE по кириллическому полю (bash/zsh: двойные внутри одинарных — ок)
tdtpcli --export '[ZTR$Employee]' --where '[Last Name] LIKE "%Черкас%"'
# LIKE по кириллическому полю (PowerShell: одинарные внутри двойных)
.\tdtpcli.exe --export '[ZTR$Employee]' --where "[Last Name] LIKE '%Черкас%'"

# Несколько составных полей в проекции
tdtpcli --export employees --fields '[Last Name],[First Name],[Birth Date],id'

# inspect-table с составным именем
tdtpcli --inspect-table '[ZTR$Employee]' --config mssql.yaml

# Дата 1753-01-01 — "нулевая" дата в MSSQL (Dynamics NAV/BC)
# bash/zsh: одинарные кавычки снаружи, одинарные для строкового значения внутри (escaping)
tdtpcli --export '[ZTR$Employee]' --where '[Termination Date] = '"'"'1753-01-01'"'"'
# PowerShell: --export одинарные (защита от $), --where двойные, значение в одинарных
.\tdtpcli.exe --export '[ZTR$Employee]' --where "[Termination Date] = '1753-01-01'"
```

### Инкрементальная синхронизация

```bash
# Синхронизирует только новые/изменённые строки по tracking field
tdtpcli --sync-incremental orders --tracking-field updated_at
tdtpcli --sync-incremental orders --tracking-field updated_at --checkpoint-file orders.checkpoint.yaml
```

---

## TDTP XML — что внутри

Каждый файл содержит всё необходимое:

```xml
<QueryContext>
  <OriginalQuery>          → что запрашивалось (поля, ORDER BY, Limit)
  <ExecutionResults>
    <TotalRecordsInTable>  → сколько всего строк в таблице
    <RecordsReturned>      → сколько вернули
    <MoreDataAvailable>    → есть ли ещё данные
</QueryContext>
<Schema>                   → типы, ключи, subtypes (uuid, jsonb, ...)
<Data>
  <R>val1|val2|val3</R>   → строки через разделитель
```

Агенту не нужен отдельный `DESCRIBE table` — схема уже в файле.

---

## Отрицательный --limit (tail mode)

```bash
--limit  5   # первые 5 строк
--limit -5   # последние 5 строк (как tail -n 5)
--limit -1   # последняя 1 строка
```

Без `--order-by` порядок не определён. Для надёжного "последнего" — всегда указывать `--order-by`.

---

## Шаблон пайплайна

```yaml
name: "pipeline-name"
sources:
  - name: src
    type: postgres          # postgres | sqlite | mysql | mssql
    dsn: "postgres://user:pass@host:5432/db?sslmode=disable"
    query: "SELECT ..."     # или table: orders

workspace:
  type: sqlite
  mode: ":memory:"          # трансформация в памяти, не трогает источник

transform:
  result_table: "result"
  sql: |
    SELECT ... FROM src WHERE ...

output:
  type: tdtp
  tdtp:
    format: xml
    destination: "/tmp/result.tdtp.xml"
    # или S3:
    # destination: "s3://bucket/path/result.tdtp.xml"
```

---

## Конфиг БД

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

```bash
tdtpcli --export orders --config config.yaml
```

---

## Безопасность

| Режим | SQL | Риск |
|-------|-----|------|
| `--export`, `--list`, `--inspect`, `--test` | Нет | Нулевой |
| `--pipeline` (default) | SELECT/WITH only | Нулевой |
| `--pipeline --unsafe` | Любой SQL | Только при явном указании |
| `--import` | INSERT/UPDATE | Только явный импорт |

Агент **не может случайно** выполнить UPDATE/DELETE/DROP.

## Важные навыки

### --test (контроль целостности пакета)

```bash
# Всегда запускать перед импортом сжатого/внешнего файла
tdtpcli --test <file>

# Работает с S3
tdtpcli --test s3://bucket/key.tdtp.xml --config cfg.yaml

# В скриптах: только импортировать если --test прошёл
tdtpcli --test delivery.tdtp.xml && tdtpcli --import delivery.tdtp.xml --config pg.yaml
```

### --import с санитизацией полей (--translit / --clear)

Для файлов из MSSQL/Access/1С/ERP где имена полей содержат кириллицу, пробелы, %, $:

```bash
# Access / Legacy MSSQL: пробелы и спецсимволы
tdtpcli --import legacy.tdtp.xml --clear

# 1С / Russian ERP: кириллица + спецсимволы
tdtpcli --import 1c_export.tdtp.xml --translit --clear

# European data: только диакритики (ö, ñ, ü, ...)
tdtpcli --import eu_data.tdtp.xml --translit
```

Оригинальные имена сохраняются как комментарии к колонкам (PostgreSQL / MySQL).
**`--export` всегда без санитизации** — он источник истины.
