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
```

Данные загружаются **один раз при старте** и хранятся в памяти. Перезапустить сервер для обновления.
