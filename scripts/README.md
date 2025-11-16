# TDTP Framework - Scripts

## create_test_db.py

Python скрипт для создания тестовой SQLite базы данных.

### Зачем?

- **Нет внешних зависимостей** - Python sqlite3 встроен
- **Быстрое создание БД** - одна команда
- **Готовые тестовые данные** - 3 таблицы с 28 записями
- **Для integration тестов** - используется в Go тестах

### Использование

```bash
# Создание БД в testdata/test.db
python3 scripts/create_test_db.py

# Создание в произвольном месте
python3 scripts/create_test_db.py /path/to/mytest.db
```

### Что создается?

**Таблица Users (10 записей):**
- ID, Name, Email, Balance, IsActive, City, CreatedAt, LastLoginAt

**Таблица Orders (8 записей):**
- OrderID, UserID, ProductName, Amount, Status, CreatedAt

**Таблица Products (10 записей):**
- ProductID, Name, Category, Price, Stock, IsAvailable, UpdatedAt

### Примеры SQL запросов

Скрипт показывает готовые SQL запросы для тестирования:

```sql
-- Активные пользователи с балансом > 1000
SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000

-- Пользователи из Москвы или СПб
SELECT * FROM Users WHERE City IN ('Moscow', 'SPb')

-- Топ 3 по балансу
SELECT * FROM Users ORDER BY Balance DESC LIMIT 3

-- Ожидающие заказы
SELECT * FROM Orders WHERE Status = 'pending'

-- Обновленные продукты
SELECT * FROM Products WHERE UpdatedAt > '2025-11-01'
```

### Использование с TDTP

#### 1. Integration тесты

```bash
# Создать БД
python3 scripts/create_test_db.py

# Установить SQLite драйвер
go get modernc.org/sqlite

# Запустить тесты
cd pkg/adapters/sqlite
go test -v
```

#### 2. Live Demo

```bash
# Создать БД
python3 scripts/create_test_db.py

# Запустить demo
cd examples/live_demo
go run main.go
```

#### 3. Query Integration

```bash
cd examples/query_integration
go run main.go
```

### Структура БД

```
test.db (16 KB)
├── Users (10 records)
│   ├── Active: 7
│   ├── Inactive: 3
│   └── Cities: Moscow (5), SPb (3), Kazan (2)
├── Orders (8 records)
│   ├── Completed: 4
│   ├── Pending: 3
│   └── Cancelled: 1
└── Products (10 records)
    ├── Electronics: 4
    ├── Accessories: 3
    └── Audio: 3
```

### Требования

- Python 3.6+
- sqlite3 module (встроен в Python)
- Никаких дополнительных зависимостей!

### Пример вывода

```
╔══════════════════════════════════════════════════════════════╗
║       TDTP Framework - Test Database Generator              ║
║                   SQLite + Python                            ║
╚══════════════════════════════════════════════════════════════╝

Creating test database: testdata/test.db
Creating table: Users
Inserting test data...
Creating table: Orders
Creating table: Products

============================================================
Database created successfully!
============================================================
Users: 10 records
Orders: 8 records
Products: 10 records
============================================================

Database file: /path/to/testdata/test.db
Size: 16384 bytes

Ready for TDTP integration tests!

...
```

### Расширение

Легко добавить свои таблицы и данные:

```python
# В create_test_database():

cursor.execute("""
    CREATE TABLE MyTable (
        ID INTEGER PRIMARY KEY,
        Name TEXT,
        ...
    )
""")

my_data = [
    (1, "Value1", ...),
    (2, "Value2", ...),
]

cursor.executemany("""
    INSERT INTO MyTable (ID, Name, ...)
    VALUES (?, ?, ...)
""", my_data)
```

### Troubleshooting

**"No such file or directory: testdata/"**
```bash
mkdir -p testdata
python3 scripts/create_test_db.py
```

**"Database is locked"**
```bash
# Закройте все подключения и удалите
rm testdata/test.db
python3 scripts/create_test_db.py
```

---

## create_postgres_test_db.py

Python скрипт для создания тестовой PostgreSQL базы данных с PostgreSQL-специфичными типами данных.

### Зачем?

- **PostgreSQL-специфичные типы** - UUID, JSONB, ARRAY, SERIAL, INET
- **Реалистичные данные** - 100 пользователей, 50 продуктов, 200 заказов
- **Message Broker тесты** - готовые данные для RabbitMQ интеграции
- **Для integration тестов** - тестирование PostgreSQL адаптера

### Требования

```bash
pip install psycopg2-binary
```

### Использование

```bash
# 1. Запустите PostgreSQL контейнер
cd tests/integration
docker-compose up -d postgres

# 2. Создайте тестовые данные
python3 scripts/create_postgres_test_db.py
```

### Что создается?

**Таблица users (100 записей):**
- UUID primary key
- JSONB metadata (preferences, login info)
- NUMERIC balance with precision
- TIMESTAMP WITH TIME ZONE

**Таблица products (50 записей):**
- BIGSERIAL primary key
- JSONB dimensions
- TEXT[] categories (массив)
- REAL weight

**Таблица orders (200 записей):**
- SERIAL primary key
- UUID foreign key → users
- TEXT[] tags (массив)
- JSONB items (структурированные данные)

**Таблица activity_logs (~600 записей):**
- BIGSERIAL primary key
- INET ip_address
- JSONB details

### Тестирование с TDTP CLI

```bash
# 1. Создайте конфиг
tdtpcli --create-config-pg

# 2. Список таблиц
tdtpcli -config config.postgres.yaml --list

# 3. Экспорт в файл
tdtpcli -config config.postgres.yaml --export users --output users.tdtp.xml

# 4. Экспорт в RabbitMQ
tdtpcli -config config.postgres.yaml --export-broker users

# 5. Импорт из RabbitMQ
tdtpcli -config config.postgres.yaml --import-broker

# 6. Фильтрация с TDTQL (SQL-level optimization)
tdtpcli -config config.postgres.yaml --export users \
  --where "balance >= 5000" \
  --order-by "balance DESC" \
  --limit 10

# 7. Фильтрация в RabbitMQ export
tdtpcli -config config.postgres.yaml --export-broker users \
  --where "is_active = 1" \
  --limit 50
```

### TDTQL Фильтры (с оптимизацией SQL)

PostgreSQL адаптер автоматически транслирует TDTQL фильтры в SQL для максимальной производительности:

**Простые условия:**
```bash
# Числовые сравнения
tdtpcli -config config.postgres.yaml --export users --where "age > 25"
tdtpcli -config config.postgres.yaml --export users --where "balance >= 1000.00"

# Текстовые совпадения
tdtpcli -config config.postgres.yaml --export users --where "username = 'admin'"

# Boolean поля
tdtpcli -config config.postgres.yaml --export users --where "is_active = 1"
```

**Сортировка:**
```bash
# Одно поле
tdtpcli -config config.postgres.yaml --export users --order-by "created_at DESC"

# Множественная сортировка
tdtpcli -config config.postgres.yaml --export users --order-by "balance DESC, age ASC"
```

**Пагинация:**
```bash
# Первые 100 записей
tdtpcli -config config.postgres.yaml --export users --limit 100

# Пропустить 100, взять следующие 50
tdtpcli -config config.postgres.yaml --export users --limit 50 --offset 100
```

**Комбинированные запросы:**
```bash
# Активные пользователи с балансом > 5000, сортировка по балансу, топ 20
tdtpcli -config config.postgres.yaml --export users \
  --where "balance > 5000" \
  --order-by "balance DESC" \
  --limit 20

# Экспорт в RabbitMQ с фильтрацией
tdtpcli -config config.postgres.yaml --export-broker orders \
  --where "total_amount >= 1000" \
  --order-by "order_date DESC" \
  --limit 100
```

**Как это работает:**
1. TDTQL фильтры транслируются в SQL: `WHERE balance > 5000 ORDER BY balance DESC LIMIT 20`
2. Фильтрация происходит на уровне PostgreSQL (быстро!)
3. Поддержка schemas: автоматически добавляется `schema.table_name`
4. Если трансляция невозможна - fallback на in-memory фильтрацию

### Примеры PostgreSQL-специфичных возможностей

**UUID операции:**
```sql
SELECT id, username FROM users WHERE id = 'e5f1c2a3-...'::uuid;
```

**JSONB запросы:**
```sql
SELECT username, metadata->>'preferences' FROM users
WHERE metadata @> '{"preferences": {"theme": "dark"}}';
```

**ARRAY операции:**
```sql
SELECT order_number FROM orders WHERE 'urgent' = ANY(tags);
```

**NUMERIC с precision:**
```sql
SELECT username, balance FROM users WHERE balance > 5000.00;
```

### Пример вывода

```
===========================================================
🐘 PostgreSQL Test Database Creator for TDTP Framework
===========================================================

🔌 Connecting to PostgreSQL...
✅ Connected successfully
   Host: localhost
   Port: 5432
   Database: tdtp_test_db
   User: tdtp_test

📋 Creating table: users...
📋 Creating table: orders...
📋 Creating table: products...
📋 Creating table: activity_logs...
✅ Tables created successfully

📊 Generating test data...
   Users: 100
   Products: 50
   Orders: 200

👥 Inserting users...
📦 Inserting products...
🛒 Inserting orders...
📝 Inserting activity logs...
✅ Test data inserted successfully

📊 Database Statistics:
============================================================
  users                | Rows:    100 | Size: 128 kB
  products             | Rows:     50 | Size: 80 kB
  orders               | Rows:    200 | Size: 144 kB
  activity_logs        | Rows:    600 | Size: 256 kB
============================================================
```

---

*Версия: 1.0*
*Совместимость: TDTP v0.6+*
