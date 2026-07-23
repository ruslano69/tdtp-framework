# tdtpserve

HTTP сервер для просмотра данных через браузер. Загружает источники (TDTP-файлы или БД), вычисляет SQL-виды в SQLite `:memory:` и отдаёт интерактивный веб-интерфейс с фильтрацией.

## Сборка

```bash
go build ./cmd/tdtpserve
```

## Запуск

```bash
tdtpserve --config mydata.yaml          # порт из конфига (по умолчанию 8080)
tdtpserve --config mydata.yaml --port 9000
```

Открыть в браузере: `http://localhost:8080`

---

## Конфигурация (YAML)

```yaml
server:
  name: "My Data Server"   # заголовок в UI
  port: 8080               # HTTP порт (необязательно, по умолчанию 8080)

sources:
  - name: Users            # имя таблицы — появится в UI и доступно для JOIN в views
    type: sqlite           # тип: sqlite | postgres | mssql | mysql | tdtp
    dsn: ./data.db
    query: SELECT * FROM users

  - name: Orders
    type: postgres
    dsn: "postgres://user:pass@localhost:5432/shop?sslmode=disable"
    query: SELECT id, user_id, amount, created_at FROM orders

  - name: Events           # TDTP-файл — query не нужен
    type: tdtp
    dsn: ./events.xml

views:                     # необязательно — SQL поверх загруженных источников
  - name: ActiveOrders
    description: "Активные заказы с email пользователя"
    sql: |
      SELECT o.id, u.email, o.amount, o.created_at
      FROM [Orders] o
      JOIN [Users] u ON u.id = o.user_id
      WHERE o.status = 'active'
```

### Типы источников

| type     | dsn формат                                          | query     |
|----------|-----------------------------------------------------|-----------|
| sqlite   | путь к файлу: `./data.db`                           | обязателен |
| postgres | `postgres://user:pass@host:5432/db?sslmode=disable` | обязателен |
| mssql    | `sqlserver://user:pass@host:1433?database=db`       | обязателен |
| mysql    | `user:pass@tcp(host:3306)/db`                       | обязателен |
| tdtp     | путь к XML-файлу: `./data.xml`                      | не нужен  |

### Views

Views выполняются **один раз при старте** в SQLite `:memory:`. Все источники доступны как таблицы по своему `name`. Имена таблиц в квадратных скобках (`[Orders]`) — стандарт TDTP для пробелов в именах.

---

## Веб-интерфейс

### Главная страница `/`

Карточки всех источников и видов: тип, количество строк и полей.

### Страница данных `/data/<name>`

Таблица с фильтрами через URL-параметры или форму:

| Параметр   | Описание                          | Пример                              |
|------------|-----------------------------------|-------------------------------------|
| `where`    | WHERE-условие (TDTQL)             | `status = 'active' AND amount > 100` |
| `order_by` | Сортировка                        | `created_at DESC` или `name ASC, id DESC` |
| `limit`    | Максимум строк                    | `50`                                |
| `offset`   | Пропустить строк (для пагинации)  | `100`                               |

**Примеры URL:**
```
/data/Users
/data/Users?where=email LIKE '%@gmail.com'
/data/Orders?where=amount >= 1000 AND status = 'paid'&order_by=created_at DESC&limit=50
/data/Orders?where=id BETWEEN 100 AND 200
/data/Events?where=level = 'error'&order_by=ts DESC&limit=20&offset=40
```

### Операторы WHERE

| Оператор       | Пример                          |
|----------------|---------------------------------|
| `=`            | `status = 'active'`             |
| `!=`           | `status != 'deleted'`           |
| `>` `<` `>=` `<=` | `amount >= 100`             |
| `LIKE`         | `name LIKE 'John%'`             |
| `IN`           | `status IN ('a','b')`           |
| `BETWEEN`      | `amount BETWEEN 100 AND 500`    |
| `IS NULL`      | `deleted_at IS NULL`            |
| `IS NOT NULL`  | `deleted_at IS NOT NULL`        |

Несколько условий: `AND` или `OR` (не смешивать в одном запросе).

---

## JSON API

Отдельный префикс `/api/*` — те же данные и те же фильтры, что и в
`/data/<name>`, но JSON вместо HTML. Отдельный префикс специально: чтобы
позже можно было навесить auth/rate-limit только на `/api/*`, не трогая
браузерные страницы.

### `GET /api/datasets`

Сводка по всем источникам и видам:

```json
[
  {"name": "Users", "is_view": false, "type": "sqlite", "row_count": 3, "field_count": 4}
]
```

### `GET /api/data/<name>`

Те же `where` / `order_by` / `limit` / `offset`, что и у `/data/<name>`:

```
/api/data/Users
/api/data/Users?where=City='Moscow'&order_by=Balance DESC
```

```json
{
  "name": "Users",
  "is_view": false,
  "type": "sqlite",
  "schema": {"fields": [{"name": "ID", "type": "INTEGER", "key": true}, ...]},
  "rows": [["1", "Ann", "Moscow", "3200"]],
  "row_count": 1
}
```

Несуществующий датасет → `404 {"error": "dataset not found: ..."}`.

### `GET /api/lookup/<name>?<param>=<value>`

В отличие от `sources` (загружаются целиком при старте), `lookups` — это
параметризованный запрос, который выполняется **вживую** на каждый запрос,
через своё отдельное подключение. Для данных, которые дорого или бессмысленно
тянуть заранее для всех строк — фото сотрудника, история проходов по одному
коду и т.п.

```yaml
lookups:
  - name: photo
    type: sqlite            # sqlite | mysql | mssql | postgres
    dsn: ./polynet.db
    query: "SELECT photo FROM employees WHERE code = ?"
    params: [code]           # имена URL query-параметров, по порядку биндинга
    result: binary            # row | rows | binary
    content_type: image/jpeg  # обязателен для result: binary

  - name: employee_info
    type: sqlite
    dsn: ./polynet.db
    query: "SELECT code, full_name FROM employees WHERE code = ?"
    params: [code]
    result: row

  - name: access_history
    type: sqlite
    dsn: ./polynet.db
    query: "SELECT ts, direction, checkpoint FROM checkpoint_log WHERE code = ? ORDER BY ts DESC"
    params: [code]
    result: rows
    max_rows: 50               # сервер-side cap, клиент не может его поднять
```

`query` использует нативный синтаксис плейсхолдеров своей БД — `@p1` для
mssql, `?` для mysql/sqlite, `$1` для postgres — как и `sources.query` уже
требует нативный SQL под свой тип, никакой кросс-диалектной трансляции нет.

**`result: row`** — ровно одна строка → JSON-объект, 0 строк → `404`, больше
одной → `500` (неоднозначно).

**`result: rows`** — 0+ строк → JSON-массив, всегда обрезан `max_rows` на
сервере.

**`result: binary`** — ровно одна строка с одной колонкой → сырые байты
напрямую с заголовком `Content-Type: <content_type>`, без JSON-обёртки.

```
GET /api/lookup/employee_info?code=12620  → {"code": "12620", "full_name": "..."}
GET /api/lookup/access_history?code=12620 → [{"ts": "...", "direction": "in", ...}, ...]
GET /api/lookup/photo?code=12620          → (сырые байты, Content-Type: image/jpeg)
```

Отсутствующий обязательный параметр → `400`. Неизвестный lookup → `404`.

Соединения открываются один раз при старте (как у `sources`), а не на
каждый запрос — но сам запрос выполняется заново каждый раз, в отличие от
`sources`, чьи данные фиксированы до перезапуска сервера.

### `POST /api/refresh`

Перечитывает `sources`/`views` (текущий конфиг в памяти, не файл с диска —
для изменения самого YAML нужен рестарт) без остановки сервера:

```json
{"status": "ok", "sources": 1, "views": 0, "refreshed_at": "2026-07-23T07:55:37+03:00"}
```

Новые данные загружаются полностью, и только затем атомарно подменяют
старые — если reload упал (например БД недоступна), старые данные
продолжают отдаваться, сервер не падает. Второй `/api/refresh`, запущенный
пока первый ещё выполняется, получает `409 Conflict` — параллельные
reload'ы не запрещены из соображений корректности (каждый строит свою
независимую копию), а просто чтобы не долбить продовую БД избыточными
запросами. `lookups` не участвуют — они и так живые на каждый запрос.

`GET /api/refresh` → `405 Method Not Allowed`.

---

## Примеры конфигов

### SQLite + TDTP-файл

```yaml
server:
  name: "Local Dev"

sources:
  - name: Users
    type: sqlite
    dsn: ./dev.db
    query: SELECT * FROM users

  - name: Snapshot
    type: tdtp
    dsn: ./snapshot.xml
```

### Два источника с JOIN-видом

```yaml
server:
  name: "Sales Dashboard"
  port: 9090

sources:
  - name: Customers
    type: postgres
    dsn: "postgres://app:secret@db:5432/sales"
    query: SELECT id, name, email, segment FROM customers

  - name: Invoices
    type: postgres
    dsn: "postgres://app:secret@db:5432/sales"
    query: SELECT id, customer_id, total, paid_at FROM invoices WHERE paid_at IS NOT NULL

views:
  - name: PaidBySegment
    description: "Оплаченные счета по сегменту клиента"
    sql: |
      SELECT c.segment, COUNT(*) as cnt, SUM(i.total) as revenue
      FROM [Invoices] i
      JOIN [Customers] c ON c.id = i.customer_id
      GROUP BY c.segment
      ORDER BY revenue DESC
```

### MSSQL

```yaml
sources:
  - name: Orders
    type: mssql
    dsn: "sqlserver://sa:Password1@localhost:1433?database=ShopDB"
    query: SELECT TOP 10000 id, status, amount FROM dbo.Orders
```

---

## Архитектура

```
Старт
  ├── etl.Loader.LoadAll()       ← загрузить все sources (параллельно)
  └── etl.NewWorkspace()         ← SQLite :memory: только для views
        ├── CreateTable / LoadData  ← залить данные из sources
        ├── ExecuteSQL()            ← вычислить каждый view
        └── workspace.Close()       ← освободить память

HTTP запрос /data/<name>
  ├── найти Dataset в памяти
  ├── разобрать where / order_by / limit / offset
  ├── tdtql.Executor.Execute()   ← фильтрация/сортировка в памяти
  └── renderData()               ← HTML-ответ

POST /api/refresh
  ├── loadDatasets() заново       ← та же логика, что и на старте, в новую карту
  └── атомарная подмена под мьютексом (не блокирует читателей на время самой загрузки)
```

Данные `sources`/`views` — снимок в памяти. Обновляются либо перезапуском
сервера, либо `POST /api/refresh` без остановки (см. JSON API выше).
`lookups` — исключение, они всегда живые, каждый запрос отдельно.
