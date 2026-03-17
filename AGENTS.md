# AGENTS.md — tdtpcli для агентной работы с данными

## Главное

`tdtpcli` — единственный инструмент для работы с данными.
Один бинарник = глаза (исследование) + руки (преобразование).
**Физически не может навредить данным** — все операции READ-ONLY по умолчанию.

---

## Рабочий процесс агента

```
1. --list              → что есть в БД?
2. --inspect <table>   → какая структура?
3. --export --limit 5  → как выглядят данные?
4. --pipeline etl.yaml → трансформация
5. --diff a.xml b.xml  → что изменилось?
```

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

### Импорт результата

```bash
# Стратегии: replace | ignore | fail | copy
tdtpcli --import result.tdtp.xml --strategy replace
tdtpcli --import result.tdtp.xml --strategy ignore   # не перезаписывать существующие
tdtpcli --import result.tdtp.xml --table new_table_name

# Импорт только нужных колонок (whitelist)
tdtpcli --import wide.tdtp.xml --fields id,email,status --table slim_table
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
| `--export`, `--list`, `--inspect` | Нет | Нулевой |
| `--pipeline` (default) | SELECT/WITH only | Нулевой |
| `--pipeline --unsafe` | Любой SQL | Только при явном указании |
| `--import` | INSERT/UPDATE | Только явный импорт |

Агент **не может случайно** выполнить UPDATE/DELETE/DROP.
